package gentoo

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestExtraFilesRejectsTraversal(t *testing.T) {
	file := filepath.Join(t.TempDir(), "foo")
	require.NoError(t, os.WriteFile(file, []byte("content"), 0o644))
	cfg := &GentooConfig{raw: config.Gentoo{}}
	err := NewExtraFiles(cfg, &Release{}, map[string]string{"../foo": file}).Prepare()
	require.ErrorContains(t, err, "must remain within the files directory")
}

func TestExtraFilesValidationPolicy(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		skip    bool
		wantErr string
	}{
		{name: "large", content: bytes.Repeat([]byte("x"), 20*1024+1), wantErr: "larger than 20KB"},
		{name: "binary", content: []byte{'a', 0, 'b'}, wantErr: "appears to be a binary file"},
		{name: "skip validation", content: bytes.Repeat([]byte{0}, 20*1024+1), skip: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "file")
			require.NoError(t, os.WriteFile(filename, tt.content, 0o644))
			cfg := &GentooConfig{raw: config.Gentoo{SkipFilesValidation: tt.skip}}
			err := NewExtraFiles(cfg, &Release{}, map[string]string{"file": filename}).Prepare()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestExtraFilesRemovesOnlyMembersPresentInEveryArchive(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "foo.conf")
	require.NoError(t, os.WriteFile(filename, []byte("content"), 0o644))
	cfg := &GentooConfig{}
	release := plannerRelease(
		&Archive{gentooArch: "amd64", files: []string{"foo.conf"}},
		&Archive{gentooArch: "arm64", files: []string{"foo.conf"}},
	)
	extras := NewExtraFiles(cfg, release, map[string]string{"foo.conf": filename})
	require.NoError(t, extras.Prepare())
	require.False(t, extras.Contains("foo.conf"))

	release.archives[1].files = []string{"other.conf"}
	extras = NewExtraFiles(cfg, release, map[string]string{"foo.conf": filename})
	require.NoError(t, extras.Prepare())
	require.True(t, extras.Contains("foo.conf"))
}

func TestExtraFilesWrappedArchiveMatchingIsPathExact(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "foo.conf")
	require.NoError(t, os.WriteFile(filename, []byte("content"), 0o644))
	release := plannerRelease(&Archive{wrappedIn: "release", files: []string{"nested/foo.conf"}})
	cfg := &GentooConfig{}

	extras := NewExtraFiles(cfg, release, map[string]string{"foo.conf": filename})
	require.NoError(t, extras.Prepare())
	require.True(t, extras.Contains("foo.conf"))

	extras = NewExtraFiles(cfg, release, map[string]string{"release/nested/foo.conf": filename})
	require.NoError(t, extras.Prepare())
	require.False(t, extras.Contains("release/nested/foo.conf"))
}

func TestExtraFilesWriteProducesNestedAuxReference(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "foo.conf")
	require.NoError(t, os.WriteFile(source, []byte("content"), 0o644))
	cfg := &GentooConfig{raw: config.Gentoo{ID: "default", Name: "foo", Category: "app-misc"}}
	extras := NewExtraFiles(cfg, &Release{}, map[string]string{"nested/foo.conf": source})
	ebuildPath := filepath.Join(directory, "output", "foo-bin-1.0.ebuild")

	generated, err := extras.Write(ebuildPath)
	require.NoError(t, err)
	require.Len(t, generated, 1)
	require.Equal(t, "app-misc/foo-bin/files/nested/foo.conf", generated[0].RepoPath)
	content, err := os.ReadFile(filepath.Join(directory, "output", "files", "nested", "foo.conf"))
	require.NoError(t, err)
	require.Equal(t, "content", string(content))
}

func TestExtraFilesMapsFileDirSources(t *testing.T) {
	extra := NewExtraFiles(&GentooConfig{}, &Release{}, map[string]string{"files/foo.conf": "source"})
	require.Equal(t, "${FILESDIR}/foo.conf", extra.EbuildSource("files/foo.conf"))
	require.Equal(t, "literal.conf", extra.EbuildSource("literal.conf"))
}
