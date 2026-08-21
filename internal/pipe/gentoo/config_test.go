package gentoo

import (
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/testctx"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func resolvedConfig(t *testing.T, raw config.Gentoo) *GentooConfig {
	t.Helper()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("1.2.3-rc1"))
	cfg, err := NewGentooConfig(ctx, raw)
	require.NoError(t, err)
	return cfg
}

func TestGentooConfigPackageName(t *testing.T) {
	require.Equal(t, "foo-bin", resolvedConfig(t, config.Gentoo{Name: "foo", Category: "app-misc"}).PackageName())
	require.Equal(t, "foo-bin", resolvedConfig(t, config.Gentoo{Name: "foo-bin", Category: "app-misc"}).PackageName())
}

func TestGentooConfigPackagePaths(t *testing.T) {
	cfg := resolvedConfig(t, config.Gentoo{Name: "foo", Category: "app-misc"})
	require.Equal(t, "app-misc/foo-bin", cfg.PackageDir())
	require.Equal(t, "app-misc/foo-bin/foo-bin-1.2.3_rc1.ebuild", cfg.EbuildPath())
	require.Equal(t, "app-misc/foo-bin/metadata.xml", cfg.MetadataPath())
	require.Equal(t, "app-misc/foo-bin/Manifest", cfg.ManifestPath())
}

func TestGentooConfigOverlayPaths(t *testing.T) {
	cfg := resolvedConfig(t, config.Gentoo{Name: "foo", Category: "app-misc", OverlayPath: "overlay"})
	require.Equal(t, "overlay/app-misc/foo-bin", cfg.PackageDir())
	require.Equal(t, "overlay/metadata/md5-cache/app-misc/foo-bin-1.2.3_rc1", cfg.MetaCachePath())
}

func TestGentooConfigPRStateRepository(t *testing.T) {
	cfg := resolvedConfig(t, config.Gentoo{Name: "foo", Category: "app-misc", Repository: config.RepoRef{
		Owner: "fork", Name: "fork-overlay", Branch: "release",
		PullRequest: config.PullRequest{Enabled: true, Base: config.PullRequestBase{Owner: "upstream", Name: "overlay", Branch: "main"}},
	}})
	require.Equal(t, "fork", cfg.TargetRepository().Owner)
	require.Equal(t, "fork-overlay", cfg.TargetRepository().Name)
	require.Equal(t, "release", cfg.TargetRepository().Branch)
	require.Equal(t, "upstream", cfg.StateRepository().Owner)
	require.Equal(t, "overlay", cfg.StateRepository().Name)
	require.Equal(t, "main", cfg.StateRepository().Branch)
}

func TestGentooConfigRejectsTraversal(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("1.0"))
	_, err := NewGentooConfig(ctx, config.Gentoo{Name: "foo", Category: "../outside"})
	require.ErrorContains(t, err, "must remain within the overlay")
}
