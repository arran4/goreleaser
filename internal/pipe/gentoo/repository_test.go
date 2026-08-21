package gentoo

import (
	"errors"
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/testctx"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
	"github.com/stretchr/testify/require"
)

type recordingLister struct{ repo client.Repo }

func (r *recordingLister) ListDir(_ *context.Context, repo client.Repo, _ string) ([]string, error) {
	r.repo = repo
	return []string{"foo-bin-1.0.ebuild"}, nil
}

func TestRepositoryReportsMissingReadCapabilities(t *testing.T) {
	repo := NewRepository(struct{}{}, client.Repo{})
	_, err := repo.Read(testctx.Wrap(t.Context()), "file")
	require.ErrorIs(t, err, client.ErrNotImplemented)
	_, err = repo.List(testctx.Wrap(t.Context()), "dir")
	require.ErrorIs(t, err, client.ErrNotImplemented)
	err = repo.Write(testctx.Wrap(t.Context()), config.CommitAuthor{}, "delete", NewChangeSet(client.RepoFile{Path: "old", Delete: true}))
	require.ErrorIs(t, err, client.ErrNotImplemented)
}

type failingRepository struct{ err error }

func (r failingRepository) ListDir(_ *context.Context, _ client.Repo, _ string) ([]string, error) {
	return nil, r.err
}

func (r failingRepository) DownloadFile(_ *context.Context, _ client.Repo, _ string) ([]byte, error) {
	return nil, r.err
}

func TestRepositoryStatePropagatesProviderErrors(t *testing.T) {
	want := errors.New("provider failed")
	cfg := resolvedConfig(t, config.Gentoo{Name: "foo", Category: "app-misc"})
	state := NewRepositoryState(NewRepository(failingRepository{err: want}, client.Repo{}), cfg)
	_, err := state.Ebuilds(testctx.Wrap(t.Context()))
	require.ErrorIs(t, err, want)
	_, err = state.Manifest(testctx.Wrap(t.Context()))
	require.ErrorIs(t, err, want)
}

func TestRepositoryStateUsesCompletePRBaseIdentity(t *testing.T) {
	cfg := resolvedConfig(t, config.Gentoo{Name: "foo", Category: "app-misc", Repository: config.RepoRef{
		Owner: "target", Name: "fork-overlay", Branch: "release",
		PullRequest: config.PullRequest{Enabled: true, Base: config.PullRequestBase{Owner: "upstream", Name: "overlay", Branch: "main"}},
	}})
	recorder := &recordingLister{}
	state := NewRepositoryState(NewRepository(recorder, cfg.StateRepository()), cfg)
	names, err := state.Ebuilds(testctx.Wrap(t.Context()))
	require.NoError(t, err)
	require.Equal(t, []string{"foo-bin-1.0.ebuild"}, names)
	require.Equal(t, "upstream", recorder.repo.Owner)
	require.Equal(t, "overlay", recorder.repo.Name)
	require.Equal(t, "main", recorder.repo.Branch)
}

func TestRepositoryStatePackageFilesReadsNestedTree(t *testing.T) {
	cfg := resolvedConfig(t, config.Gentoo{Name: "foo", Category: "app-misc"})
	repo := client.NewMock()
	repo.DirFiles = map[string][]string{
		"app-misc/foo-bin":              {"foo-bin-1.0.ebuild", "files"},
		"app-misc/foo-bin/files":        {"foo.patch", "nested"},
		"app-misc/foo-bin/files/nested": {"bar.patch"},
	}
	repo.Files = map[string][]byte{
		"app-misc/foo-bin/foo-bin-1.0.ebuild":     []byte("EAPI=8\n"),
		"app-misc/foo-bin/files/foo.patch":        []byte("foo"),
		"app-misc/foo-bin/files/nested/bar.patch": []byte("bar"),
	}
	state := NewRepositoryState(NewRepository(repo, client.Repo{}), cfg)

	files, err := state.PackageFiles(testctx.Wrap(t.Context()))
	require.NoError(t, err)
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	require.ElementsMatch(t, []string{
		"app-misc/foo-bin/foo-bin-1.0.ebuild",
		"app-misc/foo-bin/files/foo.patch",
		"app-misc/foo-bin/files/nested/bar.patch",
	}, paths)
}

func TestRepositoryStateRetentionMarksUnavailableEbuildContent(t *testing.T) {
	cfg := resolvedConfig(t, config.Gentoo{Name: "foo", Category: "app-misc"})
	repo := client.NewMock()
	repo.DirFiles = map[string][]string{cfg.PackageDir(): {"foo-bin-1.0.ebuild"}}
	state := NewRepositoryState(NewRepository(repo, client.Repo{}), cfg)

	retention, err := state.Retention(testctx.Wrap(t.Context()), NewChangeSet())
	require.NoError(t, err)
	require.Len(t, retention.Ebuilds, 1)
	require.False(t, retention.Ebuilds[0].ContentAvailable)
}
