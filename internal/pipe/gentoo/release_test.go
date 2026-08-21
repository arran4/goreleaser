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
