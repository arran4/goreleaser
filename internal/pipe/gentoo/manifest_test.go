package gentoo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestManifestParseManipulateRender(t *testing.T) {
	manifest, err := ParseManifest([]byte("DIST foo.tar.gz 3 SHA256 abc\nAUX systemd/foo.service 2 SHA512 def\n"))
	require.NoError(t, err)
	manifest.RemoveDist("foo.tar.gz")
	manifest.ReplaceDist(ManifestRecord{Name: "bar.tar.gz", Size: 4, Hashes: map[string]string{"SHA256": "123"}})
	manifest.ReplacePackageFile(ManifestRecord{Type: "EBUILD", Name: "foo-1.ebuild", Size: 5, Hashes: map[string]string{"SHA512": "456"}})
	require.Equal(t, "AUX systemd/foo.service 2 SHA512 def\nDIST bar.tar.gz 4 SHA256 123\nEBUILD foo-1.ebuild 5 SHA512 456\n", string(manifest.Render()))
}

func TestManifestRecordsDoNotExposeMutableHashState(t *testing.T) {
	manifest, err := ParseManifest([]byte("DIST foo.tar.gz 3 SHA256 abc\n"))
	require.NoError(t, err)
	records := manifest.Records()
	records[0].Hashes["SHA256"] = "mutated"
	require.Contains(t, string(manifest.Render()), "SHA256 abc")
}

func TestManifestPlannerThinKeepsOnlyDistRecords(t *testing.T) {
	manifest, err := ParseManifest([]byte("DIST old.tar.gz 1 SHA256 old\nEBUILD foo-bin-1.0.ebuild 1 SHA256 old\nAUX nested/foo.patch 1 SHA256 old\n"))
	require.NoError(t, err)
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Category: "app-misc"}, version: "2.0"}
	changes := NewChangeSet(client.RepoFile{Path: cfg.EbuildPath(), Content: []byte("EAPI=8\n")})
	planner := NewManifestPlanner(cfg, Layout{hashes: []string{"SHA256"}, thin: true}, manifest)
	require.NoError(t, planner.Apply(changes))
	file, ok := changes.Find(cfg.ManifestPath())
	require.True(t, ok)
	require.Equal(t, "DIST old.tar.gz 1 SHA256 old\n", string(file.Content))
}

func TestManifestPlannerThickRebuildsRetainedNestedPackageFiles(t *testing.T) {
	manifest, err := ParseManifest([]byte("DIST old.tar.gz 1 SHA256 old\n"))
	require.NoError(t, err)
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Category: "app-misc"}, version: "2.0"}
	retained := []client.RepoFile{
		{Path: "app-misc/foo-bin/foo-bin-1.0.ebuild", Content: []byte("old")},
		{Path: "app-misc/foo-bin/files/nested/foo.patch", Content: []byte("patch")},
		{Path: "app-misc/foo-bin/metadata.xml", Content: []byte("metadata")},
	}
	changes := NewChangeSet(client.RepoFile{Path: cfg.EbuildPath(), Content: []byte("new")})
	planner := NewManifestPlanner(cfg, Layout{hashes: []string{"SHA256"}}, manifest).
		WithPackageState(retained, []string{"foo-bin-2.0.ebuild", "foo-bin-1.0.ebuild"}, nil)
	require.NoError(t, planner.Apply(changes))
	file, ok := changes.Find(cfg.ManifestPath())
	require.True(t, ok)
	text := string(file.Content)
	for _, expected := range []string{"AUX nested/foo.patch", "EBUILD foo-bin-1.0.ebuild", "EBUILD foo-bin-2.0.ebuild", "MISC metadata.xml"} {
		require.Contains(t, text, expected)
	}
}

func TestManifestPlannerRetainsDistUntilFinalRevisionIsDeleted(t *testing.T) {
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Category: "app-misc"}, version: "2.0"}
	input := []byte("DIST foo-1.0-linux.tar.gz 1 SHA256 old\nDIST foo-2.0-linux.tar.gz 1 SHA256 new\n")
	tests := []struct {
		name     string
		retained []string
		wantDist bool
	}{
		{name: "revision remains", retained: []string{"foo-bin-1.0-r1.ebuild"}, wantDist: true},
		{name: "final revision removed", wantDist: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := ParseManifest(input)
			require.NoError(t, err)
			changes := NewChangeSet()
			planner := NewManifestPlanner(cfg, Layout{hashes: []string{"SHA256"}, thin: true}, manifest).
				WithPackageState(nil, tt.retained, []string{"foo-bin-1.0.ebuild"})
			require.NoError(t, planner.Apply(changes))
			file, ok := changes.Find(cfg.ManifestPath())
			require.True(t, ok)
			if tt.wantDist {
				require.Contains(t, string(file.Content), "DIST foo-1.0-linux.tar.gz")
				return
			}
			require.NotContains(t, string(file.Content), "DIST foo-1.0-linux.tar.gz")
			require.Contains(t, string(file.Content), "DIST foo-2.0-linux.tar.gz")
		})
	}
}

func TestManifestPlannerHashesIncomingDistfile(t *testing.T) {
	directory := t.TempDir()
	archive := filepath.Join(directory, "foo.tar.gz")
	require.NoError(t, os.WriteFile(archive, []byte("archive"), 0o644))
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Category: "app-misc"}, version: "1.0"}
	changes := NewChangeSet()
	planner := NewManifestPlanner(cfg, Layout{hashes: []string{"SHA256"}, thin: true}, &Manifest{}).
		WithDistfiles([]DistfileSource{{Name: "foo-1.0.tar.gz", Path: archive}})
	require.NoError(t, planner.Apply(changes))
	file, ok := changes.Find(cfg.ManifestPath())
	require.True(t, ok)
	require.Contains(t, string(file.Content), "DIST foo-1.0.tar.gz 7 SHA256")
}

func TestManifestHasher(t *testing.T) {
	record, err := NewManifestHasher([]string{"BLAKE2B", "SHA512", "SHA256"}).HashBytes("AUX", "nested/foo.conf", []byte("content"))
	require.NoError(t, err)
	require.EqualValues(t, 7, record.Size)
	require.Len(t, record.Hashes["BLAKE2B"], 128)
	require.Len(t, record.Hashes["SHA512"], 128)
	require.Len(t, record.Hashes["SHA256"], 64)
}

func TestManifestRejectsUnsupportedHash(t *testing.T) {
	_, err := NewManifestHasher([]string{"MD5"}).HashBytes("DIST", "foo", []byte("content"))
	require.EqualError(t, err, "unsupported manifest hash algorithm: MD5")
}
