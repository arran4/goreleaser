package gentoo

import (
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
