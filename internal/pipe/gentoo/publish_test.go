package gentoo

import (
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/git"
	"github.com/goreleaser/goreleaser/v2/internal/testctx"
	"github.com/goreleaser/goreleaser/v2/internal/testlib"
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
	require.False(t, recorder.CreatedFile, "Prepare must not mutate the target repository")
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

func TestPublisherPrepareRevisionUsesExactMetadataCache(t *testing.T) {
	tests := []struct {
		name          string
		existing      string
		incoming      string
		wantVersion   string
		rejectVersion string
	}{
		{name: "identical r1 remains r1", existing: "EAPI=8\n", incoming: "# regenerated\nEAPI=8\n", wantVersion: "1.0-r1", rejectVersion: "1.0-r2"},
		{name: "changed r1 becomes r2", existing: "EAPI=8\nDESCRIPTION=old\n", incoming: "EAPI=8\nDESCRIPTION=new\n", wantVersion: "1.0-r2", rejectVersion: "1.0-r1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			ebuildPath := filepath.Join(directory, "foo-bin-1.0.ebuild")
			cachePath := filepath.Join(directory, "foo-bin-1.0.cache")
			archivePath := filepath.Join(directory, "foo-1.0.tar.gz")
			require.NoError(t, os.WriteFile(ebuildPath, []byte(tt.incoming), 0o644))
			require.NoError(t, os.WriteFile(cachePath, []byte("exact cache"), 0o644))
			require.NoError(t, os.WriteFile(archivePath, []byte("archive"), 0o644))

			ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("1.0"))
			ctx.Artifacts.Add(&artifact.Artifact{Name: "foo-1.0.tar.gz", Path: archivePath, Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
			thin := true
			cfg := &GentooConfig{raw: config.Gentoo{
				ID: "default", Name: "foo", Category: "app-misc", Type: "bin", MetaCache: true,
				ConflictResolution: config.ConflictResolutionRevision, ThinManifests: &thin,
				CommitAuthor: config.CommitAuthor{Name: "Test", Email: "test@example.com"}, CommitMessageTemplate: "publish",
			}, version: "1.0"}
			existingEbuild := path.Join(cfg.PackageDir(), "foo-bin-1.0-r1.ebuild")
			exactCache := cfg.MetaCachePathForVersion("1.0-r1")
			repo := client.NewMock()
			repo.DirFiles = map[string][]string{
				cfg.PackageDir():   {"foo-bin-1.0-r1.ebuild"},
				cfg.MetaCacheDir(): {"foo-bin-1.0-r1"},
			}
			repo.Files = map[string][]byte{
				existingEbuild:      []byte(tt.existing),
				exactCache:          []byte("exact cache"),
				cfg.MetaCachePath(): []byte("different base cache"),
			}
			publisher, err := NewPublisher(ctx, cfg, []GeneratedFile{
				{ConfigID: cfg.ID(), RepoPath: cfg.EbuildPath(), Kind: GeneratedEbuild, Path: ebuildPath},
				{ConfigID: cfg.ID(), RepoPath: cfg.MetaCachePath(), Kind: GeneratedMetaCache, Path: cachePath},
			}, repo)
			require.NoError(t, err)

			changes, err := publisher.Prepare(ctx)
			require.NoError(t, err)
			require.True(t, changes.Contains(path.Join(cfg.PackageDir(), "foo-bin-"+tt.wantVersion+".ebuild")), "%+v", changes.Files())
			require.True(t, changes.Contains(cfg.MetaCachePathForVersion(tt.wantVersion)))
			require.False(t, changes.Contains(path.Join(cfg.PackageDir(), "foo-bin-"+tt.rejectVersion+".ebuild")))
		})
	}
}

func TestPublisherPrepareThickManifestFailsWithoutCompletePackageState(t *testing.T) {
	directory := t.TempDir()
	ebuildPath := filepath.Join(directory, "foo-bin-2.0.ebuild")
	archivePath := filepath.Join(directory, "foo-2.0.tar.gz")
	require.NoError(t, os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0o644))
	require.NoError(t, os.WriteFile(archivePath, []byte("archive"), 0o644))

	ctx := testctx.WrapWithCfg(t.Context(), config.Project{}, testctx.WithVersion("2.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "foo-2.0.tar.gz", Path: archivePath, Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
	cfg := &GentooConfig{raw: config.Gentoo{
		ID: "default", Name: "foo", Category: "app-misc", Type: "bin", ConflictResolution: config.ConflictResolutionOverwrite,
		CommitAuthor: config.CommitAuthor{Name: "Test", Email: "test@example.com"}, CommitMessageTemplate: "publish",
	}, version: "2.0"}
	repo := client.NewMock()
	repo.DirFiles = map[string][]string{cfg.PackageDir(): {"foo-bin-1.0.ebuild", "files"}}
	repo.Files = map[string][]byte{path.Join(cfg.PackageDir(), "foo-bin-1.0.ebuild"): []byte("EAPI=8\n")}
	publisher, err := NewPublisher(ctx, cfg, []GeneratedFile{{ConfigID: cfg.ID(), RepoPath: cfg.EbuildPath(), Kind: GeneratedEbuild, Path: ebuildPath}}, repo)
	require.NoError(t, err)

	_, err = publisher.Prepare(ctx)
	require.ErrorContains(t, err, "cannot construct thick Manifest from retained package files")
}

func TestPublisherPrepareGitRepositoryReadsRemoteStateOnFirstUse(t *testing.T) {
	remote, privateKey := gentooGitStateRepository(t, map[string][]byte{
		"metadata/layout.conf":                  []byte("thin-manifests = true\nmanifest-hashes = SHA256\n"),
		"app-misc/foo-bin/foo-bin-1.0.ebuild":   []byte("EAPI=8\n# retained\n"),
		"app-misc/foo-bin/Manifest":             []byte("DIST foo-1.0.tar.gz 3 SHA256 old\n"),
		"app-misc/foo-bin/files/retained.patch": []byte("patch\n"),
	})
	directory := t.TempDir()
	ebuildPath := filepath.Join(directory, "foo-bin-2.0.ebuild")
	archivePath := filepath.Join(directory, "foo-2.0.tar.gz")
	require.NoError(t, os.WriteFile(ebuildPath, []byte("EAPI=8\n"), 0o644))
	require.NoError(t, os.WriteFile(archivePath, []byte("archive"), 0o644))

	ctx := testctx.WrapWithCfg(t.Context(), config.Project{Dist: t.TempDir()}, testctx.WithVersion("2.0"))
	ctx.Artifacts.Add(&artifact.Artifact{Name: "foo-2.0.tar.gz", Path: archivePath, Goos: "linux", Goarch: "amd64", Type: artifact.UploadableArchive})
	cfg := &GentooConfig{raw: config.Gentoo{
		ID: "default", Name: "foo", Category: "app-misc", Type: "bin",
		ConflictResolution: config.ConflictResolutionOverwrite,
		KeepVersions:       1, VersionRetentionStrategy: config.VersionRetentionStrategyKeepLatest,
		CommitAuthor: config.CommitAuthor{Name: "Test", Email: "test@example.com"}, CommitMessageTemplate: "publish",
		Repository: config.RepoRef{Name: "overlay", Branch: "main", Git: config.GitRepoRef{URL: remote, PrivateKey: privateKey}},
	}, version: "2.0"}
	checkout := filepath.Join(ctx.Config.Dist, "git", "overlay-main")
	require.NoDirExists(t, checkout)
	publisher, err := NewPublisher(ctx, cfg, []GeneratedFile{{ConfigID: cfg.ID(), RepoPath: cfg.EbuildPath(), Kind: GeneratedEbuild, Path: ebuildPath}}, client.NewMock())
	require.NoError(t, err)

	changes, err := publisher.Prepare(ctx)
	require.NoError(t, err)
	require.DirExists(t, checkout)
	deleted, ok := changes.Find("app-misc/foo-bin/foo-bin-1.0.ebuild")
	require.True(t, ok, "the existing remote ebuild must participate in retention")
	require.True(t, deleted.Delete)
	manifest, ok := changes.Find(cfg.ManifestPath())
	require.True(t, ok)
	require.NotContains(t, string(manifest.Content), "foo-1.0.tar.gz", "the existing remote Manifest must participate in planning")
	require.Contains(t, string(manifest.Content), "foo-2.0.tar.gz")
}

func TestCollectPublicationInputsInterleavedArtifacts(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{Gentoos: []config.Gentoo{
		{ID: "a", Name: "alpha", Category: "app-misc"},
		{ID: "b", Name: "beta", Category: "app-misc"},
	}}, testctx.WithVersion("1.0"))

	add := func(id, repoPath string, kind GeneratedFileKind) {
		t.Helper()
		ctx.Artifacts.Add(GeneratedFile{ConfigID: id, RepoPath: repoPath, Kind: kind, Path: filepath.Join(t.TempDir(), filepath.Base(repoPath))}.Artifact())
	}
	add("a", "app-misc/alpha-bin/alpha-bin-1.0.ebuild", GeneratedEbuild)
	add("b", "app-misc/beta-bin/beta-bin-1.0.ebuild", GeneratedEbuild)
	add("a", "app-misc/alpha-bin/files/alpha.conf", GeneratedAux)
	add("b", "app-misc/beta-bin/files/beta.conf", GeneratedAux)
	add("a", "metadata/md5-cache/app-misc/alpha-bin-1.0", GeneratedMetaCache)

	inputs, err := collectPublicationInputs(ctx)
	require.NoError(t, err)
	require.Len(t, inputs, 2)
	require.Equal(t, "a", inputs[0].cfg.ID())
	require.Equal(t, "b", inputs[1].cfg.ID())
	require.Equal(t, []string{
		"app-misc/alpha-bin/alpha-bin-1.0.ebuild",
		"app-misc/alpha-bin/files/alpha.conf",
		"metadata/md5-cache/app-misc/alpha-bin-1.0",
	}, generatedRepoPaths(inputs[0].files))
	require.Equal(t, []string{
		"app-misc/beta-bin/beta-bin-1.0.ebuild",
		"app-misc/beta-bin/files/beta.conf",
	}, generatedRepoPaths(inputs[1].files))
}

func generatedRepoPaths(files []GeneratedFile) []string {
	result := make([]string, 0, len(files))
	for _, file := range files {
		result = append(result, file.RepoPath)
	}
	return result
}

func gentooGitStateRepository(t *testing.T, files map[string][]byte) (string, string) {
	t.Helper()
	remote := testlib.GitMakeBareRepository(t)
	seed := t.TempDir()
	for name, content := range files {
		filename := filepath.Join(seed, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o755))
		require.NoError(t, os.WriteFile(filename, content, 0o644))
	}
	run := func(directory string, args ...string) {
		t.Helper()
		command := append([]string{"-C", directory}, args...)
		_, err := git.Clean(git.Run(t.Context(), command...))
		require.NoError(t, err)
	}
	run(seed, "init", "-b", "main")
	run(seed, "config", "user.name", "Test")
	run(seed, "config", "user.email", "test@example.com")
	run(seed, "config", "commit.gpgSign", "false")
	run(seed, "add", "-A", ".")
	run(seed, "commit", "-m", "seed")
	run(seed, "remote", "add", "origin", remote)
	run(seed, "push", "origin", "main")
	run(remote, "symbolic-ref", "HEAD", "refs/heads/main")
	return remote, testlib.MakeNewSSHKey(t, "")
}
