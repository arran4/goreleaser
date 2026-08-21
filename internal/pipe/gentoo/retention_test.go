package gentoo

import (
	"bytes"
	"path"
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestStripCommentsPreservesInlineContent(t *testing.T) {
	input := []byte("\n# comment\n   # indented\ncontent\ncontent # inline\n")
	require.True(t, bytes.Equal([]byte("content\ncontent # inline\n"), stripComments(input)))
}

func retentionConfig(strategy config.VersionRetentionStrategy, keep int) *GentooConfig {
	return &GentooConfig{raw: config.Gentoo{
		Name: "foo", Category: "app-misc", KeepVersions: keep, VersionRetentionStrategy: strategy,
		ConflictResolution: config.ConflictResolutionOverwrite,
	}, version: "1.0"}
}

func retentionState(t *testing.T, names ...string) RetentionState {
	t.Helper()
	state := RetentionState{Files: map[string][]byte{}, MetaCacheFiles: map[string][]byte{}}
	for _, name := range names {
		version := parseGentooVersion(name, "foo-bin-")
		require.NotNil(t, version)
		filename := path.Join("app-misc/foo-bin", name)
		content := []byte("EAPI=8\n")
		state.Ebuilds = append(state.Ebuilds, ExistingEbuild{Name: name, Path: filename, Version: version, Content: content, ContentAvailable: true})
		state.Files[filename] = content
	}
	return state
}

func incomingEbuild(name string) *ChangeSet {
	return NewChangeSet(client.RepoFile{Path: path.Join("app-misc/foo-bin", name), Content: []byte("EAPI=8\n")})
}

func TestRetentionPlannerKeepLatestRanksCombinedVersions(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		incoming string
		keep     int
		deletes  []string
	}{
		{name: "older backfill keeps newer existing", existing: []string{"foo-bin-2.0.ebuild"}, incoming: "foo-bin-1.0.ebuild", keep: 1},
		{name: "newer incoming deletes older existing", existing: []string{"foo-bin-1.0.ebuild"}, incoming: "foo-bin-2.0.ebuild", keep: 1, deletes: []string{"foo-bin-1.0.ebuild"}},
		{name: "combined latest N", existing: []string{"foo-bin-1.0.ebuild", "foo-bin-2.0.ebuild", "foo-bin-3.0.ebuild"}, incoming: "foo-bin-2.5.ebuild", keep: 2, deletes: []string{"foo-bin-1.0.ebuild", "foo-bin-2.0.ebuild"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := NewRetentionPlanner(retentionConfig(config.VersionRetentionStrategyKeepLatest, tt.keep), retentionState(t, tt.existing...), incomingEbuild(tt.incoming)).Plan()
			require.NoError(t, err)
			require.Equal(t, tt.deletes, plan.Deletes)
		})
	}
}

func TestRetentionPlannerKeepPrereleasesRanksEveryBucket(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		incoming string
		deletes  []string
	}{
		{name: "alpha older backfill", existing: "foo-bin-2.0_alpha1.ebuild", incoming: "foo-bin-1.0_alpha1.ebuild"},
		{name: "beta older backfill", existing: "foo-bin-2.0_beta1.ebuild", incoming: "foo-bin-1.0_beta1.ebuild"},
		{name: "pre older backfill", existing: "foo-bin-2.0_pre1.ebuild", incoming: "foo-bin-1.0_pre1.ebuild"},
		{name: "rc older backfill", existing: "foo-bin-2.0_rc1.ebuild", incoming: "foo-bin-1.0_rc1.ebuild"},
		{name: "stable older backfill", existing: "foo-bin-2.0.ebuild", incoming: "foo-bin-1.0.ebuild"},
		{name: "newer alpha", existing: "foo-bin-1.0_alpha1.ebuild", incoming: "foo-bin-2.0_alpha1.ebuild", deletes: []string{"foo-bin-1.0_alpha1.ebuild"}},
		{name: "newer beta", existing: "foo-bin-1.0_beta1.ebuild", incoming: "foo-bin-2.0_beta1.ebuild", deletes: []string{"foo-bin-1.0_beta1.ebuild"}},
		{name: "newer pre", existing: "foo-bin-1.0_pre1.ebuild", incoming: "foo-bin-2.0_pre1.ebuild", deletes: []string{"foo-bin-1.0_pre1.ebuild"}},
		{name: "newer rc", existing: "foo-bin-1.0_rc1.ebuild", incoming: "foo-bin-2.0_rc1.ebuild", deletes: []string{"foo-bin-1.0_rc1.ebuild"}},
		{name: "newer stable", existing: "foo-bin-1.0.ebuild", incoming: "foo-bin-2.0.ebuild", deletes: []string{"foo-bin-1.0.ebuild"}},
		{name: "rc to stable", existing: "foo-bin-2.0_rc1.ebuild", incoming: "foo-bin-2.0.ebuild", deletes: []string{"foo-bin-2.0_rc1.ebuild"}},
		{name: "alpha to beta", existing: "foo-bin-2.0_alpha1.ebuild", incoming: "foo-bin-2.0_beta1.ebuild", deletes: []string{"foo-bin-2.0_alpha1.ebuild"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := NewRetentionPlanner(retentionConfig(config.VersionRetentionStrategyKeepPrereleases, 1), retentionState(t, tt.existing), incomingEbuild(tt.incoming)).Plan()
			require.NoError(t, err)
			require.Equal(t, tt.deletes, plan.Deletes)
		})
	}
}

func TestRetentionPlannerRevisionUsesExactExistingCache(t *testing.T) {
	cfg := retentionConfig("", 0)
	cfg.raw.ConflictResolution = config.ConflictResolutionRevision
	state := retentionState(t, "foo-bin-1.0-r1.ebuild")
	state.Ebuilds[0].Content = []byte("# generated\nEAPI=8\n")
	cachePath := cfg.MetaCachePathForVersion("1.0-r1")
	state.Files[cachePath] = []byte("cache")
	state.MetaCacheFiles[cachePath] = []byte("cache")
	changes := NewChangeSet(
		client.RepoFile{Path: "app-misc/foo-bin/foo-bin-1.0.ebuild", Content: []byte("EAPI=8\n")},
		client.RepoFile{Path: cfg.MetaCachePathForVersion("1.0"), Content: []byte("cache")},
	)

	plan, err := NewRetentionPlanner(cfg, state, changes).Plan()
	require.NoError(t, err)
	require.True(t, plan.Changes.Contains("app-misc/foo-bin/foo-bin-1.0-r1.ebuild"), "%+v", plan.Changes.Files())
	require.True(t, plan.Changes.Contains(cachePath))
	require.False(t, plan.Changes.Contains("app-misc/foo-bin/foo-bin-1.0-r2.ebuild"))
}

func TestRetentionPlannerChangedRevisionUsesNextRevision(t *testing.T) {
	cfg := retentionConfig("", 0)
	cfg.raw.ConflictResolution = config.ConflictResolutionRevision
	state := retentionState(t, "foo-bin-1.0-r1.ebuild")
	state.Ebuilds[0].Content = []byte("EAPI=8\nDESCRIPTION=old\n")
	changes := NewChangeSet(client.RepoFile{Path: "app-misc/foo-bin/foo-bin-1.0.ebuild", Content: []byte("EAPI=8\nDESCRIPTION=new\n")})

	plan, err := NewRetentionPlanner(cfg, state, changes).Plan()
	require.NoError(t, err)
	require.True(t, plan.Changes.Contains("app-misc/foo-bin/foo-bin-1.0-r2.ebuild"))
}

func TestRetentionPlannerRevisionRequiresExistingContent(t *testing.T) {
	cfg := retentionConfig("", 0)
	cfg.raw.ConflictResolution = config.ConflictResolutionRevision
	state := retentionState(t, "foo-bin-1.0-r1.ebuild")
	state.Ebuilds[0].ContentAvailable = false

	_, err := NewRetentionPlanner(cfg, state, incomingEbuild("foo-bin-1.0.ebuild")).Plan()
	require.ErrorContains(t, err, "content is unavailable")
}

func TestRetentionPlannerDeletesExactMetadataCache(t *testing.T) {
	cfg := retentionConfig(config.VersionRetentionStrategyKeepLatest, 1)
	state := retentionState(t, "foo-bin-1.0-r1.ebuild")
	cachePath := cfg.MetaCachePathForVersion("1.0-r1")
	state.MetaCacheFiles[cachePath] = []byte("cache")

	plan, err := NewRetentionPlanner(cfg, state, incomingEbuild("foo-bin-2.0.ebuild")).Plan()
	require.NoError(t, err)
	cacheDelete, ok := plan.Changes.Find(cachePath)
	require.True(t, ok)
	require.True(t, cacheDelete.Delete)
}
