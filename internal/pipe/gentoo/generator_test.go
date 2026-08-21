package gentoo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/testctx"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestGeneratorProducesEbuildAuxAndSafeMetaCacheArtifacts(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	require.NoError(t, os.WriteFile("foo.conf", []byte("config"), 0o644))
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{Dist: filepath.Join(directory, "dist")}, testctx.WithVersion("1.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "foo-linux-amd64.tar.gz", Path: "archive.tar.gz", Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
	cfg := &GentooConfig{raw: config.Gentoo{
		ID: "default", Name: "foo", Category: "app-misc", Bindir: "/opt/bin", License: "MIT", Description: "Foo",
		Files: []config.ExtraFile{{Glob: "foo.conf"}}, MetaCache: true,
	}, version: "1.0"}

	require.NoError(t, NewGenerator(ctx, cfg, client.NewMock()).Generate())
	artifacts := ctx.Artifacts.Filter(artifact.Or(artifact.ByType(artifact.GentooEbuild), artifact.ByType(artifact.GentooFile))).List()
	require.Len(t, artifacts, 3)
	kinds := map[GeneratedFileKind]bool{}
	for _, item := range artifacts {
		generated, err := GeneratedFileFromArtifact(*item)
		require.NoError(t, err)
		kinds[generated.Kind] = true
		require.Equal(t, "default", generated.ConfigID)
	}
	require.True(t, kinds[GeneratedEbuild])
	require.True(t, kinds[GeneratedAux])
	require.True(t, kinds[GeneratedMetaCache])
}

func TestGeneratorSkipsMetaCacheWhenEclassesAreInherited(t *testing.T) {
	directory := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{Dist: directory}, testctx.WithVersion("1.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "foo-linux-amd64.tar.gz", Path: "archive.tar.gz", Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
	cfg := &GentooConfig{raw: config.Gentoo{
		ID: "default", Name: "foo", Category: "app-misc", Bindir: "/opt/bin", License: "MIT", Description: "Foo",
		MetaCache: true, Eclasses: []string{"systemd"},
	}, version: "1.0"}
	require.NoError(t, NewGenerator(ctx, cfg, client.NewMock()).Generate())
	for _, item := range ctx.Artifacts.List() {
		if item.Type != artifact.GentooEbuild && item.Type != artifact.GentooFile {
			continue
		}
		generated, err := GeneratedFileFromArtifact(*item)
		require.NoError(t, err)
		require.NotEqual(t, GeneratedMetaCache, generated.Kind)
	}
}
