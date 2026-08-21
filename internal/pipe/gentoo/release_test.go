package gentoo

import (
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/testctx"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestReleaseNormalizesArchives(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("2.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "foo-linux-amd64.tar.gz", Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive, Extra: map[string]any{
		artifact.ExtraID: "default", artifact.ExtraWrappedIn: "release", artifact.ExtraBinaries: []string{"foo", "bar"}, artifact.ExtraFiles: []string{"README"},
	}})
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo"}, version: "2.0"}
	release, err := NewRelease(ctx, cfg, client.NewMock())
	require.NoError(t, err)
	require.Equal(t, []string{"amd64"}, release.Architectures())
	require.Equal(t, []string{"~amd64"}, release.Keywords())
	archive := release.Archive("default", "amd64")
	require.Equal(t, "release/foo", archive.Path("foo"))
	require.True(t, archive.Contains("release/README"))
	require.Equal(t, "foo-2.0-foo-linux-amd64.tar.gz", archive.Distfile())
}

func TestReleaseRejectsDuplicateIDArchitecture(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("1.0"))
	for _, name := range []string{"one.tar.gz", "two.tar.gz"} {
		ctx.Artifacts.Add(&artifact.Artifact{Name: name, Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
	}
	_, err := NewRelease(ctx, &GentooConfig{raw: config.Gentoo{Name: "foo"}, version: "1.0"}, client.NewMock())
	require.ErrorContains(t, err, `multiple linux archives map to Gentoo architecture "amd64" for ID "default"`)
}

func TestReleaseFiltersLinuxArchivesAndConfiguredIDs(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("1.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "darwin.tar.gz", Goos: "darwin", Goarch: "amd64", Type: artifact.UploadableArchive})
	ctx.Artifacts.Add(&artifact.Artifact{Name: "binary", Goos: "linux", Goarch: "amd64", Type: artifact.Binary})
	ctx.Artifacts.Add(&artifact.Artifact{Name: "default.tar.gz", Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
	ctx.Artifacts.Add(&artifact.Artifact{Name: "cgi.tar.gz", Goos: "linux", Goarch: "arm64", Type: artifact.UploadableArchive, Extra: map[string]any{artifact.ExtraID: "cgi"}})
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", IDs: []string{"cgi"}}, version: "1.0"}

	release, err := NewRelease(ctx, cfg, client.NewMock())
	require.NoError(t, err)
	require.Len(t, release.Archives(), 1)
	require.Equal(t, "cgi", release.Archives()[0].ID())
	require.Equal(t, "arm64", release.Archives()[0].GentooArch())
}

func TestReleaseAllowsComplementaryIDsOnSameArchitecture(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("1.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "default.tar.gz", Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
	ctx.Artifacts.Add(&artifact.Artifact{Name: "cgi.tar.gz", Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive, Extra: map[string]any{artifact.ExtraID: "cgi"}})
	release, err := NewRelease(ctx, &GentooConfig{raw: config.Gentoo{Name: "foo"}, version: "1.0"}, client.NewMock())
	require.NoError(t, err)
	require.Len(t, release.Archives(), 2)
	require.NotNil(t, release.Archive("default", "amd64"))
	require.NotNil(t, release.Archive("cgi", "amd64"))
}

func TestReleaseNormalizesBinaryFallbackFilesAndOrdering(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("1.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "arm.tar.gz", Goos: "linux", Goarch: "arm64", Type: artifact.UploadableArchive, Extra: map[string]any{artifact.ExtraFiles: []string{"share/config"}}})
	ctx.Artifacts.Add(&artifact.Artifact{Name: "amd.tar.gz", Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
	release, err := NewRelease(ctx, &GentooConfig{raw: config.Gentoo{Name: "foo"}, version: "1.0"}, client.NewMock())
	require.NoError(t, err)
	require.Equal(t, []string{"amd64", "arm64"}, release.Architectures())
	require.Equal(t, []string{"foo"}, release.Archive("default", "amd64").Binaries())
	require.Equal(t, []string{"share/config"}, release.Archive("default", "arm64").Files())
}

func TestReleaseRejectsUnsupportedArchitecture(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("1.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "ppc64le.tar.gz", Goos: "linux", Goarch: "ppc64le", Type: artifact.UploadableArchive})
	_, err := NewRelease(ctx, &GentooConfig{raw: config.Gentoo{Name: "foo"}, version: "1.0"}, client.NewMock())
	require.ErrorContains(t, err, `unsupported or ambiguous architecture "ppc64le"`)
}

func TestReleaseVersionQualifiesStableUpstreamAssetNames(t *testing.T) {
	for _, version := range []string{"1.0", "2.0"} {
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion(version))
		ctx.Artifacts.Add(&artifact.Artifact{Name: "foo-linux-amd64.tar.gz", Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
		release, err := NewRelease(ctx, &GentooConfig{raw: config.Gentoo{Name: "foo"}, version: version}, client.NewMock())
		require.NoError(t, err)
		require.Equal(t, "foo-"+version+"-foo-linux-amd64.tar.gz", release.Archives()[0].Distfile())
	}
}
