package gentoo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/testctx"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
	"github.com/stretchr/testify/require"
)

type recordingRepositoryClient struct {
	*client.Mock
	reads []client.Repo
}

func (c *recordingRepositoryClient) ListDir(ctx *context.Context, repo client.Repo, dir string) ([]string, error) {
	c.reads = append(c.reads, repo)
	return c.Mock.ListDir(ctx, repo, dir)
}

func (c *recordingRepositoryClient) DownloadFile(ctx *context.Context, repo client.Repo, name string) ([]byte, error) {
	c.reads = append(c.reads, repo)
	return c.Mock.DownloadFile(ctx, repo, name)
}

func TestPublisherAllStateReadsUseCrossRepositoryPRBase(t *testing.T) {
	directory := t.TempDir()
	ebuildPath := filepath.Join(directory, "foo-bin-1.0.ebuild")
	archivePath := filepath.Join(directory, "foo-1.0.tar.gz")
	require.NoError(t, os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0o644))
	require.NoError(t, os.WriteFile(archivePath, []byte("archive"), 0o644))
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("1.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "foo-1.0.tar.gz", Path: archivePath, Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
	raw := config.Gentoo{
		ID: "default", Name: "foo", Category: "app-misc", Type: "bin", ConflictResolution: config.ConflictResolutionOverwrite,
		CommitAuthor: config.CommitAuthor{Name: "Test", Email: "test@example.com"}, CommitMessageTemplate: "publish",
		ThinManifests: func() *bool { value := true; return &value }(),
		Repository: config.RepoRef{Owner: "target", Name: "fork-overlay", Branch: "release", PullRequest: config.PullRequest{
			Enabled: true, Base: config.PullRequestBase{Owner: "upstream", Name: "overlay", Branch: "main"},
		}},
	}
	cfg := &GentooConfig{raw: raw, version: "1.0"}
	recorder := &recordingRepositoryClient{Mock: client.NewMock()}
	publisher, err := NewPublisher(ctx, cfg, []GeneratedFile{{ConfigID: "default", RepoPath: cfg.EbuildPath(), Kind: GeneratedEbuild, Path: ebuildPath}}, recorder)
	require.NoError(t, err)
	_, err = publisher.Prepare(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, recorder.reads)
	for _, repo := range recorder.reads {
		require.Equal(t, "upstream", repo.Owner)
		require.Equal(t, "overlay", repo.Name)
		require.Equal(t, "main", repo.Branch)
	}
}

func TestPublisherPrepareThinToThickIncludesRetainedPackageFiles(t *testing.T) {
	directory := t.TempDir()
	ebuildPath := filepath.Join(directory, "foo-bin-2.0.ebuild")
	archivePath := filepath.Join(directory, "foo-2.0.tar.gz")
	require.NoError(t, os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0o644))
	require.NoError(t, os.WriteFile(archivePath, []byte("archive"), 0o644))

	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("2.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "foo-2.0.tar.gz", Path: archivePath, Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
	raw := config.Gentoo{
		ID: "default", Name: "foo", Category: "app-misc", Type: "bin", ConflictResolution: config.ConflictResolutionOverwrite,
		CommitAuthor: config.CommitAuthor{Name: "Test", Email: "test@example.com"}, CommitMessageTemplate: "publish",
	}
	cfg := &GentooConfig{raw: raw, version: "2.0"}
	repo := client.NewMock()
	repo.DirFiles = map[string][]string{
		"app-misc/foo-bin":       {"foo-bin-1.0.ebuild", "files", "Manifest"},
		"app-misc/foo-bin/files": {"old.patch"},
	}
	repo.Files = map[string][]byte{
		"metadata/layout.conf":                []byte("thin-manifests = false\nmanifest-hashes = SHA256\n"),
		"app-misc/foo-bin/Manifest":           []byte("DIST foo-1.0.tar.gz 1 SHA256 old\n"),
		"app-misc/foo-bin/foo-bin-1.0.ebuild": []byte("EAPI=8\n# retained\n"),
		"app-misc/foo-bin/files/old.patch":    []byte("patch"),
	}
	publisher, err := NewPublisher(ctx, cfg, []GeneratedFile{{ConfigID: "default", RepoPath: cfg.EbuildPath(), Kind: GeneratedEbuild, Path: ebuildPath}}, repo)
	require.NoError(t, err)
	changes, err := publisher.Prepare(ctx)
	require.NoError(t, err)

	var manifest []byte
	for _, file := range changes.Files() {
		if file.Path == cfg.ManifestPath() {
			manifest = file.Content
		}
		require.NotEqual(t, "gentoo-retained", file.Identifier)
	}
	require.Contains(t, string(manifest), "EBUILD foo-bin-1.0.ebuild")
	require.Contains(t, string(manifest), "EBUILD foo-bin-2.0.ebuild")
	require.Contains(t, string(manifest), "AUX old.patch")
}
