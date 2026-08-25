package gentoo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/golden"
	"github.com/goreleaser/goreleaser/v2/internal/testctx"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	import_context "github.com/goreleaser/goreleaser/v2/pkg/context"
	"github.com/stretchr/testify/require"
)

func TestGeneratedArtifactExtraDoesNotContainRepositoryCredentials(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Bin:     true,
			License: "MIT",
			Repository: config.RepoRef{
				Token: "repository-token", Git: config.GitRepoRef{PrivateKey: "private-key"},
				PullRequest: config.PullRequest{Enabled: true, Token: "pull-request-token"},
			},
			Description: "foo",
		}},
	}, testctx.WithVersion("1.0.0"))
	ctx.Artifacts.Add(&artifact.Artifact{
		Name: "foo_1.0.0_linux_amd64.tar.gz", Path: "foo.tar.gz", Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive,
	})
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

	generated := ctx.Artifacts.Filter(artifact.ByType(artifact.GentooEbuild)).List()
	require.Len(t, generated, 1)
	metadata, err := json.Marshal(generated[0].Extra)
	require.NoError(t, err)
	require.NotContains(t, string(metadata), "repository-token")
	require.NotContains(t, string(metadata), "pull-request-token")
	require.NotContains(t, string(metadata), "private-key")
	require.Equal(t, "default", generated[0].Extra[ebuildExtra].(GentooArtifactRef).ConfigID)
}

func TestRunAllRejectsDuplicateResolvedPackageDestination(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		ProjectName: "foo",
		Gentoos: []config.Gentoo{
			{ID: "one", Bin: true, License: "MIT", Repository: config.RepoRef{Owner: "owner", Name: "overlay"}},
			{ID: "two", Bin: true, License: "MIT", Repository: config.RepoRef{Owner: "owner", Name: "overlay"}},
		},
	}, testctx.WithVersion("1.0.0"))
	require.NoError(t, Pipe{}.Default(ctx))
	require.EqualError(t, runAll(ctx, client.NewMock()), `gentoo configs "one" and "two" publish to the same package destination`)
}

func TestDoRunMultiArch(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			License:    "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:    "foo_1.0.0_linux_amd64.tar.gz",
		Path:    "amd64.tar.gz",
		Goos:    "linux",
		Goarch:  "amd64",
		Goamd64: "v1",
		Type:    artifact.UploadableArchive,
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_arm64.tar.gz",
		Path:   "arm64.tar.gz",
		Goos:   "linux",
		Goarch: "arm64",
		Type:   artifact.UploadableArchive,
	})

	cli := client.NewMock()
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

	ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
	bts, err := os.ReadFile(ebuild)
	require.NoError(t, err)
	out := string(bts)
	require.Contains(t, out, "amd64? (")
	require.Contains(t, out, "arm64? (")
	require.Contains(t, out, "doexe \"foo\"")
}

func TestDoRunSingleArch(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Release: config.Release{
			GitHub: config.Repo{
				Owner: "test",
			},
		},
		Env:         []string{"GITHUB_TOKEN=token"},
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			License:    "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   "foo_1.0.0_linux_amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	require.NoError(t, Pipe{}.Default(ctx))
	cli := client.NewMock()
	err := doRun(ctx, ctx.Config.Gentoos[0], cli)
	require.NoError(t, err)

	ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
	require.FileExists(t, ebuild)

	bts, err := os.ReadFile(ebuild)
	require.NoError(t, err)
	golden.RequireEqual(t, bts)
}

func TestDoRunRejectsRawBinaries(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Name:    "foo",
			License: "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_linux_amd64",
		Path:   "foo_linux_amd64",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableBinary,
	})

	err := doRun(ctx, ctx.Config.Gentoos[0], client.NewMock())
	require.EqualError(t, err, "no linux archives found")
}

func TestDoRunCustomBindir(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			Bindir:     "/usr/bin",
			License:    "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:    "foo_1.0.0_linux_amd64.tar.gz",
		Path:    "amd64.tar.gz",
		Goos:    "linux",
		Goarch:  "amd64",
		Goamd64: "v1",
		Type:    artifact.UploadableArchive,
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_arm64.tar.gz",
		Path:   "arm64.tar.gz",
		Goos:   "linux",
		Goarch: "arm64",
		Type:   artifact.UploadableArchive,
	})

	cli := client.NewMock()
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

	ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
	bts, err := os.ReadFile(ebuild)
	require.NoError(t, err)
	out := string(bts)
	require.Contains(t, out, "amd64? (")
	require.Contains(t, out, "arm64? (")
	require.Contains(t, out, "doexe \"foo\"")
	require.Contains(t, out, "exeinto /usr/bin")
}

func TestDoRunWithExtraInstall(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Repository:   config.RepoRef{Name: "overlay"},
			Bin:          true,
			License:      "MIT",
			ExtraInstall: `dobin "${DISTDIR}/foo"`,
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:    "foo_1.0.0_linux_amd64.tar.gz",
		Path:    "amd64.tar.gz",
		Goos:    "linux",
		Goarch:  "amd64",
		Goamd64: "v1",
		Type:    artifact.UploadableArchive,
	})

	cli := client.NewMock()
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

	ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
	bts, err := os.ReadFile(ebuild)
	require.NoError(t, err)

	golden.RequireEqual(t, bts)
}

func TestDoRunWithDoinsAndDoman(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			License:    "MIT",
			Doins: []config.GentooInstallItem{
				{Src: "config.yaml", Dst: "/etc/foo/foo.conf"},
				{Src: "simple.yaml"},
			},
			Doman: []string{"foo.1"},
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   "amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	cli := client.NewMock()
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

	ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
	bts, err := os.ReadFile(ebuild)
	require.NoError(t, err)

	golden.RequireEqual(t, bts)
}

func TestComplexInstallersTracking(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			License:    "MIT",
			Dobin: []config.GentooInstallItem{
				{Src: "bin1"},
			},
			Doexe: []config.GentooInstallItem{
				{Src: "exe1", Dst: "/opt/foo/exe1"},
				{Src: "exe2", Dst: "/opt/foo/exe2"},
				{Src: "exe3", Dst: "/usr/local/bin/exe3"},
			},
			Doins: []config.GentooInstallItem{
				{Src: "ins1", Dst: "/etc/foo/ins1"},
				{Src: "ins2"},
				{Src: "ins3", Dst: "/etc/foo/ins3"},
			},
			Systemd: []config.GentooInstallItem{
				{Src: "sys1.service"},
				{Src: "sys2.service", Dst: "sys2-rename.service"},
			},
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   "amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	cli := client.NewMock()
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

	ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
	bts, err := os.ReadFile(ebuild)
	require.NoError(t, err)

	golden.RequireEqual(t, bts)
}

func TestDoRunWithFiles(t *testing.T) {
	dist := t.TempDir()
	svc := "foo.service"
	require.NoError(t, os.WriteFile(svc, []byte("svc"), 0o644))

	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			License:    "MIT",
			Files: []config.ExtraFile{{
				Glob: "./foo.service",
			}},
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   "amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	cli := client.NewMock()
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

	target := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "files", "foo.service")
	_, err := os.Stat(target)
	os.Remove(svc)
	require.NoError(t, err)
}

func TestDefaultRequiresBin(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{Gentoos: []config.Gentoo{{}}}, testctx.WithVersion("1.0.0"))
	require.Error(t, Pipe{}.Default(ctx))
}

func TestDefaultInvalidMaintainerType(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{})
	cfg := config.Gentoo{
		Bin:         true,
		License:     "MIT",
		Description: "A description",
		Maintainers: []config.GentooMaintainer{
			{Name: "Foo", Email: "foo@example.com", Type: "invalid"},
		},
	}
	require.ErrorContains(t, defaultGentooConfig(ctx, &cfg), "invalid gentoo maintainer type \"invalid\": must be person or project")
}

func TestNewGentooConfigInvalidRemoteIDs(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{})
	ctx.Version = "1.0.0-rc1"
	cfg := config.Gentoo{
		Upstream: config.GentooUpstream{
			RemoteIDs: []config.GentooUpstreamRemoteID{{ID: "foo"}},
		},
	}
	_, err := NewGentooConfig(ctx, cfg)
	require.ErrorContains(t, err, "upstream.remote_ids[0] is invalid: type is required for id \"foo\"")

	cfg = config.Gentoo{
		Upstream: config.GentooUpstream{
			RemoteIDs: []config.GentooUpstreamRemoteID{{Type: "github"}},
		},
	}
	_, err = NewGentooConfig(ctx, cfg)
	require.ErrorContains(t, err, "upstream.remote_ids[0] is invalid: id is required for type \"github\"")

	cfg = config.Gentoo{
		Upstream: config.GentooUpstream{
			RemoteIDs: []config.GentooUpstreamRemoteID{{Type: "github", ID: "  "}},
		},
	}
	_, err = NewGentooConfig(ctx, cfg)
	require.ErrorContains(t, err, "upstream.remote_ids[0] is invalid: id is required for type \"github\"")

	cfg = config.Gentoo{
		Upstream: config.GentooUpstream{
			RemoteIDs: []config.GentooUpstreamRemoteID{{Type: "{{ .ProjectName }}", ID: "foo"}},
		},
	}
	_, err = NewGentooConfig(ctx, cfg)
	require.ErrorContains(t, err, "upstream.remote_ids[0] is invalid: type is required for id \"foo\"")
}

func TestDefaultSetsPath(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Bin:     true,
			License: "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))
	require.NoError(t, Pipe{}.Default(ctx))
	require.Equal(t, "foo", ctx.Config.Gentoos[0].Name)
	require.Equal(t, "app-misc", ctx.Config.Gentoos[0].Category)
	require.Empty(t, ctx.Config.Gentoos[0].OverlayPath)
	resolved, err := NewGentooConfig(ctx, ctx.Config.Gentoos[0])
	require.NoError(t, err)
	require.Equal(t, "app-misc/foo-bin/foo-bin-1.0.0.ebuild", resolved.EbuildPath())
}

func TestDefaultSetsPathWithCategory(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Category: "app-admin",
			Bin:      true,
			License:  "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))
	require.NoError(t, Pipe{}.Default(ctx))
	require.Equal(t, "app-admin", ctx.Config.Gentoos[0].Category)
	require.Empty(t, ctx.Config.Gentoos[0].OverlayPath)
	resolved, err := NewGentooConfig(ctx, ctx.Config.Gentoos[0])
	require.NoError(t, err)
	require.Equal(t, "app-admin/foo-bin/foo-bin-1.0.0.ebuild", resolved.EbuildPath())
}

func TestDefaultWithOverlayPath(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Category:    "app-admin",
			OverlayPath: "my-prefix",
			Bin:         true,
			License:     "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))
	require.NoError(t, Pipe{}.Default(ctx))
	require.Equal(t, "my-prefix", ctx.Config.Gentoos[0].OverlayPath)
	resolved, err := NewGentooConfig(ctx, ctx.Config.Gentoos[0])
	require.NoError(t, err)
	require.Equal(t, "my-prefix/app-admin/foo-bin/foo-bin-1.0.0.ebuild", resolved.EbuildPath())
}

func TestPathWithCategoryAndNameTemplates(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Category: "app-admin",
			Name:     "bar",
			Bin:      true,
			License:  "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "bar_1.0.0_linux_amd64.tar.gz",
		Path:   "dist/bar_1.0.0_linux_amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))
	ebuild := filepath.Join(dist, "gentoo", "default", "app-admin", "bar-bin", "bar-bin-1.0.0.ebuild")
	_, err := os.Stat(ebuild)
	require.NoError(t, err)
}

func TestDefaultRequiresLicense(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Bin: true,
		}},
	}, testctx.WithVersion("1.0.0"))
	require.EqualError(t, Pipe{}.Default(ctx), "license is required")
}

func TestArtifactDerivedKeywords(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			License:    "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   "amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
		Extra:  map[string]any{artifact.ExtraID: "foo"},
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_arm64.tar.gz",
		Path:   "arm64.tar.gz",
		Goos:   "linux",
		Goarch: "arm64",
		Type:   artifact.UploadableArchive,
		Extra:  map[string]any{artifact.ExtraID: "foo"},
	})

	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

	ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
	content, err := os.ReadFile(ebuildPath)
	require.NoError(t, err)
	require.Contains(t, string(content), `KEYWORDS="~amd64 ~arm64"`)
}

func TestHandleGentooManifestAndMetadata(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		ProjectName: "foo",
	})
	cfg := config.Gentoo{
		Category: "app-misc",
		Name:     "foo",
		Maintainers: []config.GentooMaintainer{
			{Name: "M", Email: "m@m.com"},
		},
		BugsTo:   "https://bug",
		Homepage: "https://home",
		UseFlags: []config.GentooUseFlag{
			{Flag: "+systemd", Description: "Enable systemd support"},
			{Flag: "-X", Description: "Disable X"},
		},
	}

	artPath := filepath.Join(dist, "foo_1.0.0_linux_amd64.tar.gz")
	require.NoError(t, os.WriteFile(artPath, []byte("test content"), 0o644))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   artPath,
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	var files []client.RepoFile
	err := handleGentooManifestAndMetadata(ctx, cfg, nil, &files, []string{"foo-bin-0.9.0.ebuild"})
	require.NoError(t, err)
	require.Len(t, files, 2)

	// Check metadata.xml
	require.Contains(t, string(files[0].Content), "<email>m@m.com</email>")
	require.Contains(t, string(files[0].Content), "<bugs-to>https://bug</bugs-to>")
	require.Contains(t, string(files[0].Content), "<flag name=\"systemd\">Enable systemd support</flag>")
	require.Contains(t, string(files[0].Content), "<flag name=\"X\">Disable X</flag>")
	require.NotContains(t, string(files[0].Content), "+systemd")
	require.NotContains(t, string(files[0].Content), "-X")

	// Check Manifest
	require.Contains(t, string(files[1].Content), "DIST foo_1.0.0_linux_amd64.tar.gz")
	require.Contains(t, string(files[1].Content), "BLAKE2B")
	require.Contains(t, string(files[1].Content), "SHA512")
	require.Contains(t, string(files[1].Content), "MISC metadata.xml")
}

func TestHandleGentooMetadata(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{ProjectName: "test-project"}, testctx.WithVersion("1.0.0"))
	raw := config.Gentoo{
		Category: "app-misc",
		Name:     "goreleaser-gentoo-smoke",
		Homepage: "https://github.com/arran4/goreleaser-gentoo-smoke",
		UseFlags: []config.GentooUseFlag{{
			Flag:        "systemd",
			Description: "enables systemd installation",
		}},
		Maintainers: []config.GentooMaintainer{
			{Name: "Maintainer", Email: "maintainer@example.com"},
		},
		LongDescription: "This is a {{.ProjectName}} long description",
		Upstream: config.GentooUpstream{
			BugsTo:    "https://github.com/goreleaser/goreleaser/issues",
			Doc:       "https://goreleaser.com",
			RemoteIDs: []config.GentooUpstreamRemoteID{{Type: "github", ID: "goreleaser/{{.ProjectName}}"}},
		},
	}

	cfg, err := NewGentooConfig(ctx, raw)
	require.NoError(t, err)

	var files []client.RepoFile
	require.NoError(t, handleGentooManifestAndMetadata(ctx, cfg.raw, nil, &files, nil))
	require.NotEmpty(t, files)
	golden.RequireEqual(t, files[0].Content)
}

func TestHandleGentooManifestThick(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{})
	cfg := config.Gentoo{
		Category: "app-misc",
		Name:     "foo",
	}

	artPath := filepath.Join(dist, "foo_1.0.0_linux_amd64.tar.gz")
	require.NoError(t, os.WriteFile(artPath, []byte("test content"), 0o644))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   artPath,
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	files := []client.RepoFile{
		{Content: []byte("ebuild content"), Path: "app-misc/foo-bin/foo-bin-1.0.0.ebuild"},
		{Content: []byte("patch content"), Path: "app-misc/foo-bin/files/foo.patch"},
		{Content: []byte("service content"), Path: "app-misc/foo-bin/files/systemd/foo.service"},
		{Content: []byte("<pkgmetadata></pkgmetadata>"), Path: "app-misc/foo-bin/metadata.xml"},
	}

	err := handleGentooManifestAndMetadata(ctx, cfg, nil, &files, nil)
	require.NoError(t, err)

	manifestIdx := len(files) - 1
	manifestContent := string(files[manifestIdx].Content)
	require.Contains(t, manifestContent, "DIST foo_1.0.0_linux_amd64.tar.gz")
	require.Contains(t, manifestContent, "EBUILD foo-bin-1.0.0.ebuild")
	require.Contains(t, manifestContent, "AUX foo.patch")
	require.Contains(t, manifestContent, "AUX systemd/foo.service")
	require.Contains(t, manifestContent, "MISC metadata.xml")
}

func TestHandleGentooManifestThin(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{})
	cfg := config.Gentoo{
		Category: "app-misc",
		Name:     "foo",
	}

	artPath := filepath.Join(dist, "foo_1.0.0_linux_amd64.tar.gz")
	require.NoError(t, os.WriteFile(artPath, []byte("test content"), 0o644))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   artPath,
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	files := []client.RepoFile{
		{Content: []byte("ebuild content"), Path: "app-misc/foo-bin/foo-bin-1.0.0.ebuild"},
		{Content: []byte("patch content"), Path: "app-misc/foo-bin/files/foo.patch"},
		{Content: []byte("<pkgmetadata></pkgmetadata>"), Path: "app-misc/foo-bin/metadata.xml"},
	}

	downloader := mockFileDownloader{
		content: []byte("manifest-hashes = SHA256\nthin-manifests = true\n"),
	}

	err := handleGentooManifestAndMetadata(ctx, cfg, downloader, &files, nil)
	require.NoError(t, err)

	manifestIdx := len(files) - 1
	manifestContent := string(files[manifestIdx].Content)
	require.Contains(t, manifestContent, "DIST foo_1.0.0_linux_amd64.tar.gz")
	require.NotContains(t, manifestContent, "EBUILD foo-bin-1.0.0.ebuild")
	require.NotContains(t, manifestContent, "AUX foo.patch")
	require.NotContains(t, manifestContent, "MISC metadata.xml")
}

func TestHandleGentooManifestThickExcludesMetaCache(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{})
	cfg := config.Gentoo{
		Category: "app-misc",
		Name:     "foo",
	}

	artPath := filepath.Join(dist, "foo_1.0.0_linux_amd64.tar.gz")
	require.NoError(t, os.WriteFile(artPath, []byte("test content"), 0o644))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   artPath,
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	files := []client.RepoFile{
		{Content: []byte("ebuild content"), Path: "app-misc/foo-bin/foo-bin-1.0.0.ebuild"},
		{Content: []byte("<pkgmetadata></pkgmetadata>"), Path: "app-misc/foo-bin/metadata.xml"},
		{Content: []byte("cache content"), Path: "metadata/md5-cache/app-misc/foo-1.0.0"},
	}

	downloader := mockFileDownloader{
		content: []byte("thin-manifests = false\n"),
	}

	err := handleGentooManifestAndMetadata(ctx, cfg, downloader, &files, nil)
	require.NoError(t, err)

	var manifestContent string
	for _, f := range files {
		if f.Path == "app-misc/foo-bin/Manifest" {
			manifestContent = string(f.Content)
			break
		}
	}

	require.NotEmpty(t, manifestContent)
	require.Contains(t, manifestContent, "EBUILD foo-bin-1.0.0.ebuild")
	require.Contains(t, manifestContent, "MISC metadata.xml")
	require.NotContains(t, manifestContent, "MISC foo-1.0.0")
	require.NotContains(t, manifestContent, "md5-cache")
}

type mockFileDownloader struct {
	client.Client
	content  []byte
	contents map[string][]byte
}

func (m mockFileDownloader) DownloadFile(_ *import_context.Context, _ client.Repo, path string) ([]byte, error) {
	if content, ok := m.contents[path]; ok {
		return content, nil
	}
	if path == "metadata/layout.conf" || strings.HasSuffix(path, "metadata/layout.conf") {
		return m.content, nil
	}
	return nil, client.ErrNotFound
}

func TestHandleGentooManifestUnsupportedHash(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{})
	cfg := config.Gentoo{
		Category: "app-misc",
		Name:     "foo",
	}

	artPath := filepath.Join(dist, "foo_1.0.0_linux_amd64.tar.gz")
	require.NoError(t, os.WriteFile(artPath, []byte("test content"), 0o644))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   artPath,
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	mockClient := mockFileDownloader{
		Client:  client.NewMock(),
		content: []byte("manifest-hashes = SHA256 WHIRLPOOL\n"),
	}

	var files []client.RepoFile
	err := handleGentooManifestAndMetadata(ctx, cfg, mockClient, &files, nil)
	require.ErrorContains(t, err, "unsupported manifest hash algorithm: WHIRLPOOL")
}

func TestDoRunByIDs(t *testing.T) {
	folder := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist: folder,
		Gentoos: []config.Gentoo{{
			IDs: []string{"foo"},
		}},
	}, testctx.WithVersion("1.0.0"))
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "bar-bin.tar.gz",
		Path:   "doesnt matter",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
		Extra: map[string]any{
			artifact.ExtraID: "bar",
		},
	})
	err := doRun(ctx, ctx.Config.Gentoos[0], nil)
	require.ErrorContains(t, err, "no linux archives found")
}

func TestDoRunDifferentBinaries(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Release: config.Release{
			GitHub: config.Repo{
				Owner: "test",
			},
		},
		Env:         []string{"GITHUB_TOKEN=token"},
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			License:    "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:    "foo_1.0.0_linux_amd64.tar.gz",
		Path:    "amd64.tar.gz",
		Goos:    "linux",
		Goarch:  "amd64",
		Goamd64: "v1",
		Type:    artifact.UploadableArchive,
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_arm64.tar.gz",
		Path:   "arm64.tar.gz",
		Goos:   "linux",
		Goarch: "arm64",
		Type:   artifact.UploadableArchive,
	})

	cli := client.NewMock()
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

	ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
	bts, err := os.ReadFile(ebuild)
	require.NoError(t, err)
	golden.RequireEqual(t, bts)
}

func TestTemplateScenarios(t *testing.T) {
	testCases := []struct {
		name   string
		bindir string
		doexe  []config.GentooInstallItem
	}{
		{
			name: "scenario_1",
			doexe: []config.GentooInstallItem{
				{Src: "prog1", Archs: []string{"amd64", "arm64"}},
				{Src: "prog2", Archs: []string{"amd64", "arm64"}},
			},
		},
		{
			name: "scenario_2",
			doexe: []config.GentooInstallItem{
				{Src: "prog1_x86", Dst: "prog1", Archs: []string{"amd64"}},
				{Src: "prog2_x86", Dst: "prog2", Archs: []string{"amd64"}},
				{Src: "prog1_arm", Dst: "prog1", Archs: []string{"arm64"}},
				{Src: "prog2_arm", Dst: "prog2", Archs: []string{"arm64"}},
			},
		},
		{
			name: "scenario_3",
			doexe: []config.GentooInstallItem{
				{Src: "prog1_x86", Dst: "prog1", Archs: []string{"amd64"}},
				{Src: "prog2"},
				{Src: "prog1_arm", Dst: "prog1", Archs: []string{"arm64"}},
				{Src: "prog3", Dst: "prog2", Archs: []string{"arm64"}},
			},
		},
		{
			name: "scenario_doexe",
			doexe: []config.GentooInstallItem{
				{Src: "custom_bin", Dst: "/opt/custom/custom_bin"},
				{Src: "renamed_bin_x86", Dst: "/opt/other/renamed_bin", Archs: []string{"amd64"}},
				{Src: "default_bin"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			installers, err := lowerInstallItemsFromConfig("doexe", tc.doexe, "/opt/bin")
			require.NoError(t, err)
			plan := (&installProgramBuilder{bindir: tc.bindir, explicit: installers}).Build()
			require.NoError(t, plan.Validate())
			reducedPlan := plan.Reduce()
			require.NoError(t, reducedPlan.Validate())
			data := Ebuild{
				Description: "test scenario ebuild",
				License:     "MIT",
				UseFlags:    gentooUseFlags(config.Gentoo{}),
				Plan:        reducedPlan,
			}
			require.NoError(t, data.Validate())
			content, err := data.Render()
			require.NoError(t, err)
			golden.RequireEqualTxt(t, []byte(content))
		})
	}
}

func TestDoRunWithSystemdAndUseFlags(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			License:    "MIT",
			UseFlags: []config.GentooUseFlag{
				{Flag: "+systemd", Description: "Enable systemd support"},
			},
			Systemd: []config.GentooInstallItem{
				{Src: "foo.service", Use: []string{"systemd"}},
			},
			Doinitd: []config.GentooInstallItem{
				{Src: "foo.init", Dst: "/etc/init.d/foo", Use: []string{"!systemd"}},
			},
			Dosym: []config.GentooInstallItem{
				{Src: "/usr/bin/foo", Dst: "/usr/bin/bar"},
			},
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   "amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	cli := client.NewMock()
	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

	ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
	bts, err := os.ReadFile(ebuild)
	require.NoError(t, err)
	golden.RequireEqual(t, bts)
}

func TestDoRunWithSystemdEclass(t *testing.T) {
	t.Run("with systemd eclass uses systemd helpers for default and canonical rename", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "foo",
			Gentoos: []config.Gentoo{{
				Repository: config.RepoRef{Name: "overlay"},
				Bin:        true,
				License:    "MIT",
				Eclasses:   []string{"systemd"},
				Systemd: []config.GentooInstallItem{
					{Src: "foo.service"},
					{Src: "bar.service", Dst: "/usr/lib/systemd/system/bar-custom.service"},
				},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "foo_1.0.0_linux_amd64.tar.gz",
			Path:   "amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
		})

		cli := client.NewMock()
		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

		ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
		bts, err := os.ReadFile(ebuild)
		require.NoError(t, err)

		content := string(bts)
		require.Contains(t, content, `systemd_dounit "foo.service" || die "Failed to install foo.service"`)
		require.Contains(t, content, `systemd_newunit "bar.service" "bar-custom.service" || die "Failed to install bar.service"`)
	})

	t.Run("with systemd eclass lowers explicit /lib to doins and newins", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "foo",
			Gentoos: []config.Gentoo{{
				Repository: config.RepoRef{Name: "overlay"},
				Bin:        true,
				License:    "MIT",
				Eclasses:   []string{"systemd"},
				Systemd: []config.GentooInstallItem{
					{Src: "foo.service", Dst: "/lib/systemd/system/foo.service"},
					{Src: "bar.service", Dst: "/lib/systemd/system/bar-custom.service"},
				},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "foo_1.0.0_linux_amd64.tar.gz",
			Path:   "amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
		})

		cli := client.NewMock()
		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

		ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
		bts, err := os.ReadFile(ebuild)
		require.NoError(t, err)

		content := string(bts)
		require.Contains(t, content, `insinto /lib/systemd/system`)
		require.Contains(t, content, `doins "foo.service" || die "Failed to install foo.service"`)
		require.Contains(t, content, `newins "bar.service" "bar-custom.service" || die "Failed to install bar.service"`)
		require.NotContains(t, content, `systemd_dounit`)
		require.NotContains(t, content, `systemd_newunit`)
	})

	t.Run("without systemd eclass lowers default to /usr/lib/systemd/system doins", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "foo",
			Gentoos: []config.Gentoo{{
				Repository: config.RepoRef{Name: "overlay"},
				Bin:        true,
				License:    "MIT",
				Systemd: []config.GentooInstallItem{
					{Src: "foo.service"},
				},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "foo_1.0.0_linux_amd64.tar.gz",
			Path:   "amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
		})

		cli := client.NewMock()
		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

		ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
		bts, err := os.ReadFile(ebuild)
		require.NoError(t, err)

		content := string(bts)
		require.Contains(t, content, `insinto /usr/lib/systemd/system`)
		require.Contains(t, content, `doins "foo.service" || die "Failed to install foo.service"`)
	})

	t.Run("without systemd eclass preserves custom lib systemd path in fallback with doins and newins", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "foo",
			Gentoos: []config.Gentoo{{
				Repository: config.RepoRef{Name: "overlay"},
				Bin:        true,
				License:    "MIT",
				Systemd: []config.GentooInstallItem{
					{Src: "foo.service", Dst: "/lib/systemd/system/foo.service"},
					{Src: "bar.service", Dst: "/lib/systemd/system/bar-custom.service"},
				},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "foo_1.0.0_linux_amd64.tar.gz",
			Path:   "amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
		})

		cli := client.NewMock()
		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], cli))

		ebuild := filepath.Join(dist, "gentoo", "default", "app-misc", "foo-bin", "foo-bin-1.0.0.ebuild")
		bts, err := os.ReadFile(ebuild)
		require.NoError(t, err)

		content := string(bts)
		require.Contains(t, content, `insinto /lib/systemd/system`)
		require.Contains(t, content, `doins "foo.service" || die "Failed to install foo.service"`)
		require.Contains(t, content, `newins "bar.service" "bar-custom.service" || die "Failed to install bar.service"`)
	})
}

func TestGentooUseFlagsIncludesInstallConditions(t *testing.T) {
	flags := gentooUseFlags(config.Gentoo{
		UseFlags: []config.GentooUseFlag{
			{Flag: "+systemd"},
			{Flag: "doc", Description: "Install documentation"},
		},
		Dobin: []config.GentooInstallItem{{
			Use: []string{"!systemd", "zsh"},
		}},
		Systemd: []config.GentooInstallItem{{
			Use: []string{"bash"},
		}},
	})

	require.Equal(t, []config.GentooUseFlag{
		{Flag: "+systemd"},
		{Flag: "doc", Description: "Install documentation"},
		{Flag: "bash"},
		{Flag: "zsh"},
	}, flags)
}

func TestGentooArch(t *testing.T) {
	tests := []struct {
		name      string
		goarch    string
		expected  string
		expectErr bool
	}{
		{"386", "386", "x86", false},
		{"amd64", "amd64", "amd64", false},
		{"arm", "arm", "arm", false},
		{"arm64", "arm64", "arm64", false},
		{"loong64", "loong64", "loong", false},
		{"riscv64", "riscv64", "riscv", false},
		{"s390x", "s390x", "s390", false},
		{"unsupported ppc64le", "ppc64le", "", true},
		{"unsupported mips64le", "mips64le", "", true},
		{"unsupported sparc64", "sparc64", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gentooArch(tt.goarch)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestDoRunUnsupportedGentooArch(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Category:   "app-misc",
			Name:       "foo",
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			License:    "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_mips64le.tar.gz",
		Path:   "mips64le.tar.gz",
		Goos:   "linux",
		Goarch: "mips64le",
		Type:   artifact.UploadableArchive,
	})

	cli := client.NewMock()
	err := doRun(ctx, ctx.Config.Gentoos[0], cli)
	require.ErrorContains(t, err, `unsupported or ambiguous architecture "mips64le"`)
}

func TestDoRunDuplicateGentooArch(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "foo",
		Gentoos: []config.Gentoo{{
			Category:   "app-misc",
			Name:       "foo",
			Repository: config.RepoRef{Name: "overlay"},
			Bin:        true,
			License:    "MIT",
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:    "foo_1.0.0_linux_amd64_v1.tar.gz",
		Path:    "v1.tar.gz",
		Goos:    "linux",
		Goarch:  "amd64",
		Goamd64: "v1",
		Type:    artifact.UploadableArchive,
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:    "foo_1.0.0_linux_amd64_v2.tar.gz",
		Path:    "v2.tar.gz",
		Goos:    "linux",
		Goarch:  "amd64",
		Goamd64: "v2",
		Type:    artifact.UploadableArchive,
	})

	cli := client.NewMock()
	err := doRun(ctx, ctx.Config.Gentoos[0], cli)
	require.ErrorContains(t, err, `multiple linux archives map to Gentoo architecture "amd64"`)
}

func TestGentooVersion(t *testing.T) {
	// Reference: https://projects.gentoo.org/pms/8/pms.html#x1-250003.2
	tests := []struct {
		in   string
		want string
	}{
		{"1.0.0", "1.0.0"},
		{"1.0.0-rc1", "1.0.0_rc1"},
		{"1.0.0-rc-1", "1.0.0_rc1"},
		{"1.0.0-rc.1", "1.0.0_rc1"},
		{"1.0.0-alpha", "1.0.0_alpha"},
		{"1.0.0-beta.2", "1.0.0_beta2"},
		{"1.0.0-pre3", "1.0.0_pre3"},
		{"1.0.0-p1", "1.0.0_p1"},
		// PMS 3.2 based examples
		{"1.2-alpha", "1.2_alpha"},
		{"1.2-alpha.1", "1.2_alpha1"},
		{"1.2-alpha-1", "1.2_alpha1"},
		{"1.2-beta", "1.2_beta"},
		{"1.2-beta.2", "1.2_beta2"},
		{"1.2-pre", "1.2_pre"},
		{"1.2-pre.3", "1.2_pre3"},
		{"1.2-rc", "1.2_rc"},
		{"1.2-rc.4", "1.2_rc4"},
		{"1.2-p", "1.2_p"},
		{"1.2-p.5", "1.2_p5"},
		// Invented edge cases
		{"1.0.0-alpha0", "1.0.0_alpha0"},
		{"1.0.0-beta00", "1.0.0_beta00"},
		{"1.0.0-rc", "1.0.0_rc"},
		{"1.0.0-pre", "1.0.0_pre"},
		{"1.0.0-p", "1.0.0_p"},
		{"2.0.0-rc-10", "2.0.0_rc10"},
		{"1.0.0-RC1", "1.0.0_rc1"},
		{"1.0.0-Alpha1", "1.0.0_alpha1"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := convertToGentooVersion(tt.in, "gentoo-version")
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("invalid gentoo version", func(t *testing.T) {
		_, err := convertToGentooVersion("1.0.0-nightly", "gentoo-version")
		require.Error(t, err)
		require.ErrorContains(t, err, "cannot be naturally represented in Gentoo")

		_, err = convertToGentooVersion("1.0.0-dev.1", "gentoo-version")
		require.Error(t, err)
		require.ErrorContains(t, err, "cannot be naturally represented in Gentoo")

		_, err = convertToGentooVersion("1.0.0", "invalid-type")
		require.Error(t, err)
		require.ErrorContains(t, err, "unsupported version representation invalid-type")
	})
}

func TestDefaultValidation(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{})
	ctx.Config.Gentoos = []config.Gentoo{
		{
			Bin:         true,
			License:     "MIT",
			Description: "foo",
			Type:        "binn",
		},
	}
	require.ErrorContains(t, Pipe{}.Default(ctx), `invalid gentoo type "binn": currently only "bin" is supported`)

	ctx.Config.Gentoos = []config.Gentoo{
		{
			Bin:          true,
			License:      "MIT",
			Description:  "foo",
			KeepVersions: -1,
		},
	}
	require.ErrorContains(t, Pipe{}.Default(ctx), "keep_versions must be greater than or equal to 0")

	ctx.Config.Gentoos = []config.Gentoo{
		{
			Bin:                      true,
			License:                  "MIT",
			Description:              "foo",
			KeepVersions:             1,
			VersionRetentionStrategy: "invalid",
		},
	}
	require.ErrorContains(t, Pipe{}.Default(ctx), "version_retention_strategy \"invalid\" is not valid, must be one of [keep_latest, keep_prereleases]")

	ctx.Config.Gentoos = []config.Gentoo{
		{
			Bin:                      true,
			License:                  "MIT",
			Description:              "foo",
			KeepVersions:             1,
			VersionRetentionStrategy: "",
		},
	}
	require.ErrorContains(t, Pipe{}.Default(ctx), "version_retention_strategy must be provided if keep_versions > 0")
}

func TestExtraFileValidator(t *testing.T) {
	t.Run("inArchives", func(t *testing.T) {
		arches := []*artifact.Artifact{
			{
				Extra: map[string]any{
					artifact.ExtraFiles: []string{"foo.service"},
				},
			},
			{
				Extra: map[string]any{
					artifact.ExtraFiles: []string{"foo.service"},
				},
			},
		}
		ef := newExtraFilesProcessor(config.Gentoo{}, arches, nil)
		require.True(t, ef.inArchives("foo.service"))
		require.False(t, ef.inArchives("bar.service"))

		arches[0].Extra[artifact.ExtraFiles] = []string{"share/foo.service"}
		ef = newExtraFilesProcessor(config.Gentoo{}, arches, nil)
		require.False(t, ef.inArchives("foo.service"))

		arches[0].Extra[artifact.ExtraFiles] = []string{"foo.service"}
		arches[0].Extra[artifact.ExtraWrappedIn] = "release"
		arches[1].Extra[artifact.ExtraWrappedIn] = "release"
		ef = newExtraFilesProcessor(config.Gentoo{}, arches, nil)
		require.False(t, ef.inArchives("foo.service"))
		require.True(t, ef.inArchives("release/foo.service"))
	})

	t.Run("Filter_valid", func(t *testing.T) {
		tmpDir := t.TempDir()
		fooPath := filepath.Join(tmpDir, "foo.service")
		err := os.WriteFile(fooPath, []byte("valid text content"), 0o644)
		require.NoError(t, err)

		extraFiles := map[string]string{
			"foo.service": fooPath,
		}
		ef := newExtraFilesProcessor(config.Gentoo{}, nil, extraFiles)
		err = ef.Filter()
		require.NoError(t, err)
		require.Contains(t, extraFiles, "foo.service")
	})

	t.Run("Filter_binary", func(t *testing.T) {
		tmpDir := t.TempDir()
		binPath := filepath.Join(tmpDir, "foo.bin")
		err := os.WriteFile(binPath, []byte{0x00, 0x01, 0x02}, 0o644)
		require.NoError(t, err)

		extraFiles := map[string]string{
			"foo.bin": binPath,
		}
		ef := newExtraFilesProcessor(config.Gentoo{}, nil, extraFiles)
		err = ef.Filter()
		require.ErrorContains(t, err, "appears to be a binary file")
	})

	t.Run("Filter_toolarge", func(t *testing.T) {
		tmpDir := t.TempDir()
		largePath := filepath.Join(tmpDir, "foo.large")
		err := os.WriteFile(largePath, make([]byte, 21*1024), 0o644)
		require.NoError(t, err)

		extraFiles := map[string]string{
			"foo.large": largePath,
		}
		ef := newExtraFilesProcessor(config.Gentoo{}, nil, extraFiles)
		err = ef.Filter()
		require.ErrorContains(t, err, "larger than 20KB")
	})

	t.Run("Filter_disabled", func(t *testing.T) {
		tmpDir := t.TempDir()
		binPath := filepath.Join(tmpDir, "foo.bin")
		err := os.WriteFile(binPath, []byte{0x00, 0x01, 0x02}, 0o644)
		require.NoError(t, err)

		extraFiles := map[string]string{
			"foo.bin": binPath,
		}
		ef := newExtraFilesProcessor(config.Gentoo{SkipFilesValidation: true}, nil, extraFiles)
		err = ef.Filter()
		require.NoError(t, err)
		require.Contains(t, extraFiles, "foo.bin")
	})
}

func TestSkipUpload(t *testing.T) {
	t.Run("skip_upload true", func(t *testing.T) {
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Gentoos: []config.Gentoo{
				{
					ID:         "default",
					SkipUpload: "true",
				},
			},
		})
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:  "foo.ebuild",
			Path:  "dist/foo.ebuild",
			Type:  artifact.GentooEbuild,
			Extra: gentooArtifactExtra("default", "app-misc/foo-bin/foo-bin-1.0.0.ebuild"),
		})
		err := Pipe{}.Publish(ctx)
		require.NoError(t, err)
	})

	t.Run("skip_upload auto prerelease", func(t *testing.T) {
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Gentoos: []config.Gentoo{
				{
					ID:         "default",
					SkipUpload: "auto",
				},
			},
		})
		ctx.Semver = import_context.Semver{Prerelease: "beta.1"}
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:  "foo.ebuild",
			Path:  "dist/foo.ebuild",
			Type:  artifact.GentooEbuild,
			Extra: gentooArtifactExtra("default", "app-misc/foo-bin/foo-bin-1.0.0.ebuild"),
		})
		err := Pipe{}.Publish(ctx)
		require.NoError(t, err)
	})
}

func TestConflictResolutionFail(t *testing.T) {
	t.Run("succeeds when publishing a new version alongside existing ebuilds", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "foo",
			Gentoos: []config.Gentoo{{
				Category:           "app-misc",
				Name:               "foo",
				Bin:                true,
				License:            "MIT",
				Description:        "foo",
				ConflictResolution: config.ConflictResolutionFail,
			}},
		}, testctx.WithVersion("2.0.0"))

		artPath := filepath.Join(dist, "foo_2.0.0_linux_amd64.tar.gz")
		require.NoError(t, os.WriteFile(artPath, []byte("content"), 0o644))
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "foo_2.0.0_linux_amd64.tar.gz",
			Path:   artPath,
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
		})

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		clientMock := &client.Mock{
			DirFiles: map[string][]string{
				"app-misc/foo-bin": {"foo-bin-1.0.0.ebuild"},
			},
			Files: map[string][]byte{
				"app-misc/foo-bin/foo-bin-1.0.0.ebuild": []byte("EAPI=8\n"),
			},
		}

		require.NoError(t, publishGenerated(ctx, clientMock))
	})

	t.Run("fails when generated ebuild filename already exists", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "foo",
			Gentoos: []config.Gentoo{{
				Category:           "app-misc",
				Name:               "foo",
				Bin:                true,
				License:            "MIT",
				Description:        "foo",
				ConflictResolution: config.ConflictResolutionFail,
			}},
		}, testctx.WithVersion("1.0.0"))

		artPath := filepath.Join(dist, "foo_1.0.0_linux_amd64.tar.gz")
		require.NoError(t, os.WriteFile(artPath, []byte("content"), 0o644))
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "foo_1.0.0_linux_amd64.tar.gz",
			Path:   artPath,
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
		})

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		clientMock := &client.Mock{
			DirFiles: map[string][]string{
				"app-misc/foo-bin": {"foo-bin-1.0.0.ebuild"},
			},
		}

		err := publishGenerated(ctx, clientMock)
		require.EqualError(t, err, "ebuild foo-bin-1.0.0.ebuild already exists in app-misc/foo-bin")
	})

	t.Run("fails when generated ebuild filename is in thick Manifest", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "foo",
			Gentoos: []config.Gentoo{{
				Category:           "app-misc",
				Name:               "foo",
				Bin:                true,
				License:            "MIT",
				Description:        "foo",
				ConflictResolution: config.ConflictResolutionFail,
			}},
		}, testctx.WithVersion("1.0.0"))

		artPath := filepath.Join(dist, "foo_1.0.0_linux_amd64.tar.gz")
		require.NoError(t, os.WriteFile(artPath, []byte("content"), 0o644))
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "foo_1.0.0_linux_amd64.tar.gz",
			Path:   artPath,
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
		})

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		clientMock := &client.Mock{
			Files: map[string][]byte{
				"metadata/layout.conf":      []byte("thin-manifests = false\n"),
				"app-misc/foo-bin/Manifest": []byte("EBUILD foo-bin-1.0.0.ebuild 100 SHA256 abc\n"),
			},
		}

		err := publishGenerated(ctx, clientMock)
		require.EqualError(t, err, "ebuild foo-bin-1.0.0.ebuild already exists in app-misc/foo-bin")
	})
}

func TestMetaCache(t *testing.T) {
	t.Run("meta_cache disabled by default", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "foo",
			Gentoos: []config.Gentoo{{
				Category: "app-misc",
				Name:     "foo",
				Bin:      true,
				License:  "MIT",
			}},
		}, testctx.WithVersion("1.0.0"))
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "foo_1.0.0_linux_amd64.tar.gz",
			Path:   "dist/foo_1.0.0_linux_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
		})
		require.NoError(t, Pipe{}.Default(ctx))
		require.False(t, ctx.Config.Gentoos[0].MetaCache)
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))
		cacheFile := filepath.Join(dist, "gentoo", "default", "metadata", "md5-cache", "app-misc", "foo-bin-1.0.0")
		_, err := os.Stat(cacheFile)
		require.True(t, os.IsNotExist(err))
	})

	t.Run("meta_cache enabled on ebuild without eclasses generates best-effort cache", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "foo",
			Gentoos: []config.Gentoo{{
				Category:    "app-misc",
				Name:        "foo",
				Bin:         true,
				License:     "MIT",
				Description: "foo package",
				MetaCache:   true,
			}},
		}, testctx.WithVersion("1.0.0"))
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "foo_1.0.0_linux_amd64.tar.gz",
			Path:   "dist/foo_1.0.0_linux_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
		})
		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))
		cacheFile := filepath.Join(dist, "gentoo", "default", "metadata", "md5-cache", "app-misc", "foo-bin-1.0.0")
		content, err := os.ReadFile(cacheFile)
		require.NoError(t, err)
		str := string(content)
		require.Contains(t, str, "DEFINED_PHASES=install")
		require.Contains(t, str, "DESCRIPTION=foo package")
		require.NotContains(t, str, "INHERITED=")
		require.NotContains(t, str, "_eclasses_=")
		require.Contains(t, str, "_md5_=")
	})

	t.Run("meta_cache enabled on ebuild with inherited eclasses skips cache generation", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "foo",
			Gentoos: []config.Gentoo{{
				Category:    "app-misc",
				Name:        "foo",
				Bin:         true,
				License:     "MIT",
				Description: "foo package",
				MetaCache:   true,
				Eclasses:    []string{"desktop"},
			}},
		}, testctx.WithVersion("1.0.0"))
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "foo_1.0.0_linux_amd64.tar.gz",
			Path:   "dist/foo_1.0.0_linux_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
		})
		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))
		cacheFile := filepath.Join(dist, "gentoo", "default", "metadata", "md5-cache", "app-misc", "foo-bin-1.0.0")
		_, err := os.Stat(cacheFile)
		require.True(t, os.IsNotExist(err))
	})

	t.Run("meta_cache disabled by layout.conf", func(t *testing.T) {
		settings := ParseLayout([]byte("cache-formats = pms\n"))
		metaCacheAllowed := !settings.hasCacheFormatsConfigured || slices.Contains(settings.cacheFormats, "md5-dict") || slices.Contains(settings.cacheFormats, "md5-cache")
		require.False(t, metaCacheAllowed)
	})

	t.Run("meta_cache filter properly applies with overlay path", func(t *testing.T) {
		repoClient := client.NewMock()
		repoClient.Files = map[string][]byte{
			"my-overlay/metadata/layout.conf": []byte("cache-formats = pms\n"),
		}

		ctx := testctx.WrapWithCfg(t.Context(), config.Project{})

		raw := config.Gentoo{
			Name:        "foo",
			Category:    "app-misc",
			MetaCache:   true,
			OverlayPath: "my-overlay",
			CommitAuthor: config.CommitAuthor{
				Name:  "Test",
				Email: "test@test.com",
			},
			CommitMessageTemplate: "test",
		}
		cfg := &GentooConfig{raw: raw}
		publisher, err := NewPublisher(ctx, cfg, nil, repoClient)
		require.NoError(t, err)
		files := publisher.filterMetaCache(NewChangeSet(
			client.RepoFile{Path: "my-overlay/metadata/md5-cache/app-misc/foo-1.0.0", Content: []byte("cache")},
			client.RepoFile{Path: "my-overlay/app-misc/foo-bin/foo-bin-1.0.0.ebuild", Content: []byte("ebuild")},
		), Layout{hasCacheFormatsConfigured: true, cacheFormats: []string{"pms"}}).Files()
		require.Len(t, files, 1)
		require.Equal(t, "my-overlay/app-misc/foo-bin/foo-bin-1.0.0.ebuild", files[0].Path)
	})
}

func TestEbuildData(t *testing.T) {
	t.Run("Validate invalid dosym", func(t *testing.T) {
		data := Ebuild{
			Description: "foo",
			License:     "MIT",
			Plan:        &InstallProgram{Body: []installStmt{actionStmt{Op: OpDosym, Source: "foo"}}},
		}
		require.EqualError(t, data.Validate(), "dosym requires a destination")
	})

	t.Run("Validate valid dosym", func(t *testing.T) {
		data := Ebuild{
			Description: "foo",
			License:     "MIT",
			Plan:        &InstallProgram{Body: []installStmt{actionStmt{Op: OpDosym, Source: "foo", Target: "bar"}}},
		}
		require.NoError(t, data.Validate())
	})

	t.Run("SortedUseFlags", func(t *testing.T) {
		data := Ebuild{
			UseFlags: []config.GentooUseFlag{
				{Flag: "systemd"},
				{Flag: "doc"},
				{Flag: "systemd"},
				{Flag: ""},
			},
		}
		flags := data.SortedUseFlags()
		require.Equal(t, []string{"doc", "systemd"}, flags)
	})

	t.Run("FormattedSrcURIs", func(t *testing.T) {
		data := Ebuild{
			Archs: []archData{
				{Keyword: "amd64", URIs: []archItem{{File: "foo.tar.gz", URI: "https://example.com/foo.tar.gz"}}},
				{Keyword: "arm64", URIs: []archItem{{File: "foo-arm64.tar.gz", URI: "https://example.com/foo-arm64.tar.gz"}}},
				{Keyword: "", URIs: []archItem{{File: "invalid", URI: "invalid"}}},
			},
		}
		uris := data.FormattedSrcURIs()
		require.Equal(t, []string{
			"amd64? ( https://example.com/foo.tar.gz -> foo.tar.gz )",
			"arm64? ( https://example.com/foo-arm64.tar.gz -> foo-arm64.tar.gz )",
		}, uris)
	})

	t.Run("RenderEbuild", func(t *testing.T) {
		data := Ebuild{
			Name:        "foo",
			Description: "Foo package",
			Homepage:    "https://example.com",
			License:     "MIT",
			Keywords:    "amd64",
		}
		content, err := data.Render()
		require.NoError(t, err)
		require.Contains(t, content, `DESCRIPTION="Foo package"`)
		require.Contains(t, content, `HOMEPAGE="https://example.com"`)
	})

	t.Run("RenderEbuild with custom eclasses", func(t *testing.T) {
		data := Ebuild{
			Name:        "foo",
			Description: "Foo package",
			License:     "MIT",
			Eclasses:    []string{"systemd", "desktop", "systemd"},
		}
		content, err := data.Render()
		require.NoError(t, err)
		require.Contains(t, content, "inherit systemd desktop")
	})

	t.Run("RenderMetaCache", func(t *testing.T) {
		data := Ebuild{
			Description: "Foo package",
			Homepage:    "https://example.com",
			License:     "MIT",
			Keywords:    "amd64",
			UseFlags:    []config.GentooUseFlag{{Flag: "systemd"}},
			Archs: []archData{
				{Keyword: "amd64", URIs: []archItem{{File: "foo.tar.gz", URI: "https://example.com/foo.tar.gz"}}},
			},
		}
		meta, err := data.RenderMetaCache("ebuild content sample")
		require.NoError(t, err)
		require.Contains(t, meta, "DESCRIPTION=Foo package")
		require.Contains(t, meta, "IUSE=systemd")
		require.Contains(t, meta, "SRC_URI=amd64? ( https://example.com/foo.tar.gz -> foo.tar.gz )")
		require.Contains(t, meta, "_md5_=")
	})
}

func TestInstallExtraFiles(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "foo.conf")
	require.NoError(t, os.WriteFile(srcFile, []byte("conf content"), 0o644))

	ebuildPath := filepath.Join(tmpDir, "app-misc", "foo", "foo-bin-1.0.0.ebuild")
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist: tmpDir,
	})

	ef := newExtraFilesProcessor(config.Gentoo{
		Category: "app-misc",
		Name:     "foo",
	}, nil, map[string]string{
		"files/foo.conf": srcFile,
	})

	err := ef.InstallExtraFiles(ctx, ebuildPath)
	require.NoError(t, err)

	destFile := filepath.Join(tmpDir, "app-misc", "foo", "files", "foo.conf")
	content, err := os.ReadFile(destFile)
	require.NoError(t, err)
	require.Equal(t, "conf content", string(content))

	arts := ctx.Artifacts.List()
	require.Len(t, arts, 1)
	require.Equal(t, "files/foo.conf", arts[0].Name)
	require.Equal(t, artifact.GentooFile, arts[0].Type)
}

func TestGentooMetadata(t *testing.T) {
	t.Run("AddMaintainers valid and empty email", func(t *testing.T) {
		var meta Metadata
		err := meta.AddMaintainers([]config.GentooMaintainer{
			{Name: "Alice", Email: "alice@example.com"},
		})
		require.NoError(t, err)
		require.Equal(t, []string{"alice@example.com"}, meta.MaintainerEmails())

		err = meta.AddMaintainers([]config.GentooMaintainer{{Name: "Invalid"}})
		require.EqualError(t, err, "maintainer email is required")
	})

	t.Run("AddUseFlags and SetUpstream and Marshal", func(t *testing.T) {
		var meta Metadata
		meta.AddUseFlags([]config.GentooUseFlag{
			{Flag: "systemd", Description: "Enable systemd"},
		})
		require.NoError(t, meta.SetUpstream("https://bugs.example.com", "", nil))

		content, err := meta.Marshal()
		require.NoError(t, err)
		require.Contains(t, string(content), `<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">`)
		require.Contains(t, string(content), `<flag name="systemd">Enable systemd</flag>`)
		require.Contains(t, string(content), `<bugs-to>https://bugs.example.com</bugs-to>`)
	})

	t.Run("AddUseFlags modifies existing", func(t *testing.T) {
		var meta Metadata
		meta.AddUseFlags([]config.GentooUseFlag{
			{Flag: "systemd", Description: "Enable systemd old"},
		})
		meta.AddUseFlags([]config.GentooUseFlag{
			{Flag: "systemd", Description: "Enable systemd new"},
		})

		content, err := meta.Marshal()
		require.NoError(t, err)
		require.Contains(t, string(content), `<flag name="systemd">Enable systemd new</flag>`)
		require.NotContains(t, string(content), `<flag name="systemd">Enable systemd old</flag>`)
	})
}

func TestHandleGentooManifestAndMetadataMalformedXML(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{})
	cfg := config.Gentoo{
		Category: "app-misc",
		Name:     "foo",
		BugsTo:   "https://bugs.example.com",
	}

	cli := client.NewMock()
	cli.Files = map[string][]byte{
		"app-misc/foo-bin/metadata.xml": []byte("<malformed xml"),
	}

	var files []client.RepoFile
	err := handleGentooManifestAndMetadata(ctx, cfg, cli, &files, nil)
	require.ErrorContains(t, err, "failed to parse metadata.xml")
}

func TestGentooSrcIDAndMultiArchiveSupport(t *testing.T) {
	t.Run("src_id without src derives binary and suppresses default install", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "program1",
			Gentoos: []config.Gentoo{{
				Category: "app-misc",
				Name:     "program1",
				Bin:      true,
				License:  "MIT",
				UseFlags: []config.GentooUseFlag{
					{Flag: "plugin", Description: "Install plugin executable"},
				},
				Doexe: []config.GentooInstallItem{
					{
						SrcID: "default",
						Dst:   "/opt/bin/program1",
					},
					{
						SrcID: "plugin",
						Dst:   "/var/www/cgi-bin/program2",
						Use:   []string{"plugin"},
					},
				},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "default_linux_amd64.tar.gz",
			Path:   "dist/default_linux_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "default",
				artifact.ExtraBinaries: []string{"program1"},
			},
		})
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "plugin_linux_amd64.tar.gz",
			Path:   "dist/plugin_linux_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "plugin",
				artifact.ExtraBinaries: []string{"program2-bin"},
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "program1-bin", "program1-bin-1.0.0.ebuild")
		content, err := os.ReadFile(ebuildPath)
		require.NoError(t, err)
		str := string(content)

		require.Contains(t, str, "amd64? (")
		require.Contains(t, str, "default_linux_amd64.tar.gz")
		require.Contains(t, str, "plugin_linux_amd64.tar.gz")

		require.Contains(t, str, `exeinto /opt/bin`)
		require.Contains(t, str, `doexe "program1"`)
		require.Contains(t, str, `if use plugin; then`)
		require.Contains(t, str, `exeinto /var/www/cgi-bin`)
		require.Contains(t, str, `newexe "program2-bin" "program2"`)
	})

	t.Run("src_id with src and partial suppression", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "myapp",
			Gentoos: []config.Gentoo{{
				Category: "app-misc",
				Name:     "myapp",
				IDs:      []string{"server", "client", "helper"},
				Bin:      true,
				License:  "MIT",
				UseFlags: []config.GentooUseFlag{{Flag: "tools"}},
				Doexe: []config.GentooInstallItem{{
					SrcID: "helper",
					Src:   "helpers/bar",
					Dst:   "/opt/myapp/helper",
					Use:   []string{"tools"},
				}},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "server_linux_amd64.tar.gz",
			Path:   "dist/server_linux_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "server",
				artifact.ExtraBinaries: []string{"myapp-server"},
			},
		})
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "client_linux_amd64.tar.gz",
			Path:   "dist/client_linux_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "client",
				artifact.ExtraBinaries: []string{"myapp-client"},
			},
		})
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "helper_linux_amd64.tar.gz",
			Path:   "dist/helper_linux_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "helper",
				artifact.ExtraBinaries: []string{"myapp-helper"},
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "myapp-bin", "myapp-bin-1.0.0.ebuild")
		content, err := os.ReadFile(ebuildPath)
		require.NoError(t, err)
		str := string(content)

		require.Contains(t, str, `doexe "myapp-server"`)
		require.Contains(t, str, `doexe "myapp-client"`)

		require.NotContains(t, str, `doexe "myapp-helper"`)
		require.Contains(t, str, `if use tools; then`)
		require.Contains(t, str, `exeinto /opt/myapp`)
		require.Contains(t, str, `newexe "helpers/bar" "helper"`)
	})

	t.Run("duplicate ID for same architecture is rejected", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "myapp",
			Gentoos: []config.Gentoo{{
				Category: "app-misc",
				Name:     "myapp",
				Bin:      true,
				License:  "MIT",
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "server1.tar.gz",
			Path:   "dist/server1.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID: "default",
			},
		})
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "server2.tar.gz",
			Path:   "dist/server2.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID: "default",
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		err := doRun(ctx, ctx.Config.Gentoos[0], client.NewMock())
		require.ErrorContains(t, err, `multiple linux archives map to Gentoo architecture "amd64" for ID "default"`)
	})

	t.Run("unknown src_id returns error", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "myapp",
			Gentoos: []config.Gentoo{{
				Category:    "app-misc",
				Name:        "myapp",
				Bin:         true,
				License:     "MIT",
				Description: "foo",
				Doexe: []config.GentooInstallItem{{
					SrcID: "cgi_typo",
				}},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "myapp.tar.gz",
			Path:   "dist/myapp.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID: "default",
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		err := doRun(ctx, ctx.Config.Gentoos[0], client.NewMock())
		require.ErrorContains(t, err, `gentoo doexe: src_id "cgi_typo" does not match a selected archive`)
	})

	t.Run("missing archive for one of the requested architectures returns error", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "myapp",
			Gentoos: []config.Gentoo{{
				Category:    "app-misc",
				Name:        "myapp",
				Bin:         true,
				License:     "MIT",
				Description: "foo",
				Doexe: []config.GentooInstallItem{{
					SrcID: "partial",
					Archs: []string{"amd64", "arm64"},
				}},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "partial.tar.gz",
			Path:   "dist/partial.tar.gz",
			Goos:   "linux",
			Goarch: "amd64", // Present for amd64, but missing for arm64
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "partial",
				artifact.ExtraBinaries: []string{"myapp"},
			},
		})

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "other.tar.gz",
			Path:   "dist/other.tar.gz",
			Goos:   "linux",
			Goarch: "arm64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "other",
				artifact.ExtraBinaries: []string{"myapp"},
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		err := doRun(ctx, ctx.Config.Gentoos[0], client.NewMock())
		require.ErrorContains(t, err, `gentoo doexe: src_id "partial" does not match a selected archive for archs [amd64 arm64]`)
	})

	t.Run("multiple binaries with dst returns error", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "myapp",
			Gentoos: []config.Gentoo{{
				Category:    "app-misc",
				Name:        "myapp",
				Bin:         true,
				License:     "MIT",
				Description: "foo",
				Doexe: []config.GentooInstallItem{{
					SrcID: "tools",
					Dst:   "/opt/bin/tool",
				}},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "tools.tar.gz",
			Path:   "dist/tools.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "tools",
				artifact.ExtraBinaries: []string{"foo", "bar"},
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		err := doRun(ctx, ctx.Config.Gentoos[0], client.NewMock())
		require.ErrorContains(t, err, `gentoo doexe: dst "/opt/bin/tool" cannot be used with multiple binaries [foo bar] in src_id "tools"; specify explicit src for each binary`)
	})

	t.Run("mismatched archive layouts across architectures returns error", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "myapp",
			Gentoos: []config.Gentoo{{
				Category:    "app-misc",
				Name:        "myapp",
				Bin:         true,
				License:     "MIT",
				Description: "foo",
				Doexe: []config.GentooInstallItem{{
					SrcID: "default",
				}},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "myapp_amd64.tar.gz",
			Path:   "dist/myapp_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:        "default",
				artifact.ExtraWrappedIn: "dir_amd64",
			},
		})
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "myapp_arm64.tar.gz",
			Path:   "dist/myapp_arm64.tar.gz",
			Goos:   "linux",
			Goarch: "arm64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:        "default",
				artifact.ExtraWrappedIn: "dir_arm64",
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		err := doRun(ctx, ctx.Config.Gentoos[0], client.NewMock())
		require.ErrorContains(t, err, `gentoo doexe: src_id "default" has mismatched archive layouts across architectures; specify explicit src`)
	})

	t.Run("doexe with archs avoids mismatched layouts error and generates conditionals", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "myapp",
			Gentoos: []config.Gentoo{{
				Category:    "app-misc",
				Name:        "myapp",
				Bin:         true,
				License:     "MIT",
				Description: "foo",
				Doexe: []config.GentooInstallItem{
					{SrcID: "default", Archs: []string{"amd64"}},
					{SrcID: "default", Archs: []string{"arm64"}},
				},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "myapp_amd64.tar.gz",
			Path:   "dist/myapp_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:        "default",
				artifact.ExtraWrappedIn: "dir_amd64",
			},
		})
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "myapp_arm64.tar.gz",
			Path:   "dist/myapp_arm64.tar.gz",
			Goos:   "linux",
			Goarch: "arm64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:        "default",
				artifact.ExtraWrappedIn: "dir_arm64",
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "myapp-bin", "myapp-bin-1.0.0.ebuild")
		content, err := os.ReadFile(ebuildPath)
		require.NoError(t, err)
		str := string(content)

		require.Contains(t, str, "if use amd64; then\n    exeinto /opt/bin\n    newexe \"dir_amd64/myapp\" \"myapp\" || die \"Failed to install dir_amd64/myapp\"\n  fi")
		require.Contains(t, str, "if use arm64; then\n    exeinto /opt/bin\n    newexe \"dir_arm64/myapp\" \"myapp\" || die \"Failed to install dir_arm64/myapp\"\n  fi")
	})

	t.Run("plain src stays literal even with wrappedIn archive", func(t *testing.T) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "myapp",
			Gentoos: []config.Gentoo{{
				Category:    "app-misc",
				Name:        "myapp",
				Bin:         true,
				License:     "MIT",
				Description: "foo",
				Doexe: []config.GentooInstallItem{{
					Src: "special/foo",
				}},
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "myapp_amd64.tar.gz",
			Path:   "dist/myapp_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:        "default",
				artifact.ExtraWrappedIn: "myapp-1.0.0",
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "myapp-bin", "myapp-bin-1.0.0.ebuild")
		content, err := os.ReadFile(ebuildPath)
		require.NoError(t, err)
		str := string(content)

		require.Contains(t, str, `newexe "special/foo" "foo"`)
		require.NotContains(t, str, `doexe "myapp-1.0.0/special/foo"`)
	})
}

func TestEbuildGenerationDeterminism(t *testing.T) {
	generateEbuild := func(reverseArtifactOrder bool) string {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "myapp",
			Gentoos: []config.Gentoo{{
				Category:    "app-misc",
				Name:        "myapp",
				Bin:         true,
				License:     "MIT",
				Description: "foo",
			}},
		}, testctx.WithVersion("1.0.0"))

		arts := []*artifact.Artifact{
			{
				Name:   "myapp_arm64.tar.gz",
				Path:   "dist/myapp_arm64.tar.gz",
				Goos:   "linux",
				Goarch: "arm64",
				Type:   artifact.UploadableArchive,
				Extra: map[string]any{
					artifact.ExtraID:       "default",
					artifact.ExtraBinaries: []string{"myapp-cli", "myapp-srv"},
				},
			},
			{
				Name:   "myapp_amd64.tar.gz",
				Path:   "dist/myapp_amd64.tar.gz",
				Goos:   "linux",
				Goarch: "amd64",
				Type:   artifact.UploadableArchive,
				Extra: map[string]any{
					artifact.ExtraID:       "default",
					artifact.ExtraBinaries: []string{"myapp-cli", "myapp-srv"},
				},
			},
			{
				Name:   "myapp_386.tar.gz",
				Path:   "dist/myapp_386.tar.gz",
				Goos:   "linux",
				Goarch: "386",
				Type:   artifact.UploadableArchive,
				Extra: map[string]any{
					artifact.ExtraID:       "default",
					artifact.ExtraBinaries: []string{"myapp-cli", "myapp-srv"},
				},
			},
		}

		if reverseArtifactOrder {
			slices.Reverse(arts)
		}

		for _, art := range arts {
			ctx.Artifacts.Add(art)
		}

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "myapp-bin", "myapp-bin-1.0.0.ebuild")
		content, err := os.ReadFile(ebuildPath)
		require.NoError(t, err)
		return string(content)
	}

	content1 := generateEbuild(false)
	content2 := generateEbuild(true)
	require.Equal(t, content1, content2)
}

func TestHandleGentooManifestAndMetadataPrunesOnlyFullyDeletedBaseVersions(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		ProjectName: "foo",
	})
	cfg := config.Gentoo{
		Category: "app-misc",
		Name:     "foo",
	}

	artPath := filepath.Join(dist, "foo_1.0.0_linux_amd64.tar.gz")
	require.NoError(t, os.WriteFile(artPath, []byte("test content"), 0o644))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_1.0.0_linux_amd64.tar.gz",
		Path:   artPath,
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	artPath2 := filepath.Join(dist, "foo_2.0.0_linux_amd64.tar.gz")
	require.NoError(t, os.WriteFile(artPath2, []byte("test content 2"), 0o644))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_2.0.0_linux_amd64.tar.gz",
		Path:   artPath2,
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	downloader := mockFileDownloader{
		content: []byte("thin-manifests = false\n"),
		contents: map[string][]byte{
			"app-misc/foo-bin/Manifest": []byte("DIST foo_1.0.0_linux_amd64.tar.gz 1 BLAKE2B deadbeef\nDIST foo_2.0.0_linux_amd64.tar.gz 1 BLAKE2B deadbeef\nEBUILD foo-bin-1.0.0.ebuild 1 BLAKE2B deadbeef\nEBUILD foo-bin-1.0.0-r1.ebuild 1 BLAKE2B deadbeef\nEBUILD foo-bin-2.0.0.ebuild 1 BLAKE2B deadbeef\n"),
		},
	}

	files := []client.RepoFile{
		{Content: []byte("ebuild content"), Path: "app-misc/foo-bin/foo-bin-1.0.0-r1.ebuild"},
		{Content: []byte("ebuild content"), Path: "app-misc/foo-bin/foo-bin-2.0.0.ebuild"},
	}

	deletedEbuilds := []string{"foo-bin-1.0.0.ebuild"}

	err := handleGentooManifestAndMetadata(ctx, cfg, downloader, &files, deletedEbuilds)
	require.NoError(t, err)

	var manifestContent string
	for _, f := range files {
		if f.Path == "app-misc/foo-bin/Manifest" {
			manifestContent = string(f.Content)
			break
		}
	}

	require.NotEmpty(t, manifestContent)
	// Base version 1.0.0 has a retained revision (foo-1.0.0-r1.ebuild), so its DIST should not be pruned.
	require.Contains(t, manifestContent, "DIST foo_1.0.0_linux_amd64.tar.gz")
	require.Contains(t, manifestContent, "DIST foo_2.0.0_linux_amd64.tar.gz")
	// The specific EBUILD 1.0.0 should be pruned though.
	require.NotContains(t, manifestContent, "EBUILD foo-bin-1.0.0.ebuild")
	require.Contains(t, manifestContent, "EBUILD foo-bin-1.0.0-r1.ebuild")
	require.Contains(t, manifestContent, "EBUILD foo-bin-2.0.0.ebuild")
}

func TestHandleGentooManifestAndMetadataPrunesFullyDeletedBaseVersions(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		ProjectName: "foo",
	})
	cfg := config.Gentoo{
		Category: "app-misc",
		Name:     "foo",
	}

	// Artifact for version 2.0.0 still exists
	artPath2 := filepath.Join(dist, "foo_2.0.0_linux_amd64.tar.gz")
	require.NoError(t, os.WriteFile(artPath2, []byte("test content 2"), 0o644))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "foo_2.0.0_linux_amd64.tar.gz",
		Path:   artPath2,
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	downloader := mockFileDownloader{
		content: []byte("thin-manifests = false\n"),
		contents: map[string][]byte{
			// Manifest starts with both 1.0.0 and 2.0.0
			"app-misc/foo-bin/Manifest": []byte("DIST foo_1.0.0_linux_amd64.tar.gz 1 BLAKE2B deadbeef\nDIST foo_2.0.0_linux_amd64.tar.gz 1 BLAKE2B deadbeef\nEBUILD foo-bin-1.0.0.ebuild 1 BLAKE2B deadbeef\nEBUILD foo-bin-2.0.0.ebuild 1 BLAKE2B deadbeef\n"),
		},
	}

	// Only 2.0.0 is retained
	files := []client.RepoFile{
		{Content: []byte("ebuild content"), Path: "app-misc/foo-bin/foo-bin-2.0.0.ebuild"},
	}

	// 1.0.0 is deleted
	deletedEbuilds := []string{"foo-bin-1.0.0.ebuild"}

	err := handleGentooManifestAndMetadata(ctx, cfg, downloader, &files, deletedEbuilds)
	require.NoError(t, err)

	var manifestContent string
	for _, f := range files {
		if f.Path == "app-misc/foo-bin/Manifest" {
			manifestContent = string(f.Content)
			break
		}
	}

	require.NotEmpty(t, manifestContent)
	// Base version 1.0.0 has no retained revisions, so its DIST should be pruned.
	require.NotContains(t, manifestContent, "DIST foo_1.0.0_linux_amd64.tar.gz")
	require.Contains(t, manifestContent, "DIST foo_2.0.0_linux_amd64.tar.gz")
	require.NotContains(t, manifestContent, "EBUILD foo-bin-1.0.0.ebuild")
	require.Contains(t, manifestContent, "EBUILD foo-bin-2.0.0.ebuild")
}

func TestGentooArchSpecificSuppression(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "myapp",
		Gentoos: []config.Gentoo{{
			Category: "app-misc",
			Name:     "myapp",
			Bin:      true,
			License:  "MIT",
			Doexe: []config.GentooInstallItem{{
				SrcID: "default",
				Archs: []string{"amd64"},
				Dst:   "/opt/bin/foo",
			}},
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "default_linux_amd64.tar.gz",
		Path:   "dist/default_linux_amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
		Extra: map[string]any{
			artifact.ExtraID:       "default",
			artifact.ExtraBinaries: []string{"myapp"},
		},
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "default_linux_arm64.tar.gz",
		Path:   "dist/default_linux_arm64.tar.gz",
		Goos:   "linux",
		Goarch: "arm64",
		Type:   artifact.UploadableArchive,
		Extra: map[string]any{
			artifact.ExtraID:       "default",
			artifact.ExtraBinaries: []string{"myapp"},
		},
	})

	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

	ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "myapp-bin", "myapp-bin-1.0.0.ebuild")
	content, err := os.ReadFile(ebuildPath)
	require.NoError(t, err)

	golden.RequireEqual(t, content)
}

func TestGentooSrcValidation(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "myapp",
		Gentoos: []config.Gentoo{{
			Category: "app-misc",
			Name:     "myapp",
			Bin:      true,
			License:  "MIT",
			Doexe: []config.GentooInstallItem{{
				Dst: "/opt/bin/foo",
			}},
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "default_linux_amd64.tar.gz",
		Path:   "dist/default_linux_amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
		Extra: map[string]any{
			artifact.ExtraID:       "default",
			artifact.ExtraBinaries: []string{"myapp"},
		},
	})

	require.NoError(t, Pipe{}.Default(ctx))
	err := doRun(ctx, ctx.Config.Gentoos[0], client.NewMock())
	require.ErrorContains(t, err, "gentoo doexe: either src or src_id is required")
}

func TestGentooMismatchedArchiveBypass(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "myapp",
		Gentoos: []config.Gentoo{{
			Category: "app-misc",
			Name:     "myapp",
			Bin:      true,
			License:  "MIT",
			Doins: []config.GentooInstallItem{{
				SrcID: "default",
				Src:   "config.yaml",
				Dst:   "/etc/myapp/config.yaml",
				Archs: []string{"amd64", "arm64"},
			}},
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "default_linux_amd64.tar.gz",
		Path:   "dist/default_linux_amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
		Extra: map[string]any{
			artifact.ExtraID:       "default",
			artifact.ExtraBinaries: []string{"myapp-amd64"},
		},
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "default_linux_arm64.tar.gz",
		Path:   "dist/default_linux_arm64.tar.gz",
		Goos:   "linux",
		Goarch: "arm64",
		Type:   artifact.UploadableArchive,
		Extra: map[string]any{
			artifact.ExtraID:       "default",
			artifact.ExtraBinaries: []string{"myapp-arm64"},
		},
	})

	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

	ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "myapp-bin", "myapp-bin-1.0.0.ebuild")
	content, err := os.ReadFile(ebuildPath)
	require.NoError(t, err)

	golden.RequireEqual(t, content)
}

func TestRenameArchiveMissingVersion(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		ProjectName: "app",
		Dist:        dist,
		Gentoos: []config.Gentoo{
			{
				ID:                       "default",
				Bin:                      true,
				License:                  "MIT",
				Description:              "foo",
				KeepVersions:             1,
				VersionRetentionStrategy: config.VersionRetentionStrategyKeepLatest,
			},
		},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "app_linux_amd64.tar.gz",
		Path:   "dist/app_linux_amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
	})

	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

	ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "app-bin", "app-bin-1.0.0.ebuild")
	content, err := os.ReadFile(ebuildPath)
	require.NoError(t, err)
	str := string(content)
	require.Contains(t, str, "-> app-1.0.0-app_linux_amd64.tar.gz )")
}

func TestGentooThreeArchSpecificSuppression(t *testing.T) {
	dist := t.TempDir()
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Dist:        dist,
		ProjectName: "myapp",
		Gentoos: []config.Gentoo{{
			Category: "app-misc",
			Name:     "myapp",
			Bin:      true,
			License:  "MIT",
			Doexe: []config.GentooInstallItem{
				{
					SrcID: "default",
					Archs: []string{"amd64"},
					Dst:   "/opt/bin/foo-amd64",
				},
				{
					SrcID: "default",
					Archs: []string{"arm64"},
					Dst:   "/opt/bin/foo-arm64",
				},
			},
		}},
	}, testctx.WithVersion("1.0.0"))

	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "default_linux_amd64.tar.gz",
		Path:   "dist/default_linux_amd64.tar.gz",
		Goos:   "linux",
		Goarch: "amd64",
		Type:   artifact.UploadableArchive,
		Extra: map[string]any{
			artifact.ExtraID:       "default",
			artifact.ExtraBinaries: []string{"myapp"},
		},
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "default_linux_arm64.tar.gz",
		Path:   "dist/default_linux_arm64.tar.gz",
		Goos:   "linux",
		Goarch: "arm64",
		Type:   artifact.UploadableArchive,
		Extra: map[string]any{
			artifact.ExtraID:       "default",
			artifact.ExtraBinaries: []string{"myapp"},
		},
	})
	ctx.Artifacts.Add(&artifact.Artifact{
		Name:   "default_linux_arm.tar.gz",
		Path:   "dist/default_linux_arm.tar.gz",
		Goos:   "linux",
		Goarch: "arm",
		Type:   artifact.UploadableArchive,
		Extra: map[string]any{
			artifact.ExtraID:       "default",
			artifact.ExtraBinaries: []string{"myapp"},
		},
	})

	require.NoError(t, Pipe{}.Default(ctx))
	require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

	ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "myapp-bin", "myapp-bin-1.0.0.ebuild")
	content, err := os.ReadFile(ebuildPath)
	require.NoError(t, err)

	golden.RequireEqual(t, content)
}

func TestGentooArchSuppressionPrecedence(t *testing.T) {
	newCtx := func(items []config.GentooInstallItem) (*import_context.Context, string) {
		dist := t.TempDir()
		ctx := testctx.WrapWithCfg(t.Context(), config.Project{
			Dist:        dist,
			ProjectName: "myapp",
			Gentoos: []config.Gentoo{{
				Category: "app-misc",
				Name:     "myapp",
				Bin:      true,
				License:  "MIT",
				Doexe:    items,
			}},
		}, testctx.WithVersion("1.0.0"))

		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "default_linux_amd64.tar.gz",
			Path:   "dist/default_linux_amd64.tar.gz",
			Goos:   "linux",
			Goarch: "amd64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "default",
				artifact.ExtraBinaries: []string{"myapp"},
			},
		})
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "default_linux_arm64.tar.gz",
			Path:   "dist/default_linux_arm64.tar.gz",
			Goos:   "linux",
			Goarch: "arm64",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "default",
				artifact.ExtraBinaries: []string{"myapp"},
			},
		})
		ctx.Artifacts.Add(&artifact.Artifact{
			Name:   "default_linux_arm.tar.gz",
			Path:   "dist/default_linux_arm.tar.gz",
			Goos:   "linux",
			Goarch: "arm",
			Type:   artifact.UploadableArchive,
			Extra: map[string]any{
				artifact.ExtraID:       "default",
				artifact.ExtraBinaries: []string{"myapp"},
			},
		})
		return ctx, dist
	}

	t.Run("two arch-specific entries union suppressed architectures and fallback only on remaining arch", func(t *testing.T) {
		ctx, dist := newCtx([]config.GentooInstallItem{
			{
				SrcID: "default",
				Archs: []string{"amd64"},
				Dst:   "/opt/bin/foo-amd64",
			},
			{
				SrcID: "default",
				Archs: []string{"arm64"},
				Dst:   "/opt/bin/foo-arm64",
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "myapp-bin", "myapp-bin-1.0.0.ebuild")
		content, err := os.ReadFile(ebuildPath)
		require.NoError(t, err)
		str := string(content)

		// Fallback binary 'doexe "myapp"' is generated ONLY for arm
		require.Contains(t, str, "if use arm; then\n    doexe \"myapp\" || die \"Failed to install binary\"\n  fi")
		require.Contains(t, str, "if use amd64; then\n    newexe \"myapp\" \"foo-amd64\" || die \"Failed to install myapp\"\n  fi")
		require.Contains(t, str, "if use arm64; then\n    newexe \"myapp\" \"foo-arm64\" || die \"Failed to install myapp\"\n  fi")
	})

	t.Run("arch-specific entry followed by global entry suppresses fallback on all architectures", func(t *testing.T) {
		ctx, dist := newCtx([]config.GentooInstallItem{
			{
				SrcID: "default",
				Archs: []string{"amd64"},
				Dst:   "/opt/bin/foo-amd64",
			},
			{
				SrcID: "default",
				Dst:   "/opt/bin/foo-global",
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "myapp-bin", "myapp-bin-1.0.0.ebuild")
		content, err := os.ReadFile(ebuildPath)
		require.NoError(t, err)
		str := string(content)

		// Fallback binary 'doexe "myapp"' should not be present for any architecture
		require.NotContains(t, str, `doexe "myapp"`)
		require.Contains(t, str, `newexe "myapp" "foo-global"`)
		require.Contains(t, str, `newexe "myapp" "foo-amd64"`)
	})

	t.Run("global entry followed by arch-specific entry maintains global suppression", func(t *testing.T) {
		ctx, dist := newCtx([]config.GentooInstallItem{
			{
				SrcID: "default",
				Dst:   "/opt/bin/foo-global",
			},
			{
				SrcID: "default",
				Archs: []string{"amd64"},
				Dst:   "/opt/bin/foo-amd64",
			},
		})

		require.NoError(t, Pipe{}.Default(ctx))
		require.NoError(t, doRun(ctx, ctx.Config.Gentoos[0], client.NewMock()))

		ebuildPath := filepath.Join(dist, "gentoo", "default", "app-misc", "myapp-bin", "myapp-bin-1.0.0.ebuild")
		content, err := os.ReadFile(ebuildPath)
		require.NoError(t, err)
		str := string(content)

		// Fallback binary 'doexe "myapp"' should not be present for any architecture
		require.NotContains(t, str, `doexe "myapp"`)
		require.Contains(t, str, `newexe "myapp" "foo-global"`)
		require.Contains(t, str, `newexe "myapp" "foo-amd64"`)
	})
}
