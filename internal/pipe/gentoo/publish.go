package gentoo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/caarlos0/log"
	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/commitauthor"
	"github.com/goreleaser/goreleaser/v2/internal/tmpl"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

type publicationInput struct {
	cfg   *GentooConfig
	files []GeneratedFile
}

func collectPublicationInputs(ctx *context.Context) ([]publicationInput, error) {
	artifacts := ctx.Artifacts.Filter(artifact.Or(artifact.ByType(artifact.GentooEbuild), artifact.ByType(artifact.GentooFile))).List()
	byID := map[string]*publicationInput{}
	var result []publicationInput
	for _, art := range artifacts {
		generated, err := GeneratedFileFromArtifact(*art)
		if err != nil {
			return nil, err
		}
		raw, err := gentooConfigByID(ctx, generated.ConfigID)
		if err != nil {
			return nil, err
		}
		skip, err := tmpl.New(ctx).Apply(raw.SkipUpload)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(skip) == "true" || strings.TrimSpace(skip) == "auto" && ctx.Semver.Prerelease != "" {
			continue
		}
		input := byID[generated.ConfigID]
		if input == nil {
			cfg, err := NewGentooConfig(ctx, raw)
			if err != nil {
				return nil, err
			}
			result = append(result, publicationInput{cfg: cfg})
			input = &result[len(result)-1]
			byID[generated.ConfigID] = input
		}
		input.files = append(input.files, generated)
	}
	return result, nil
}

// Publisher separates the publication lifecycle into synchronization,
// read-only planning, repository writes, and pull-request creation.
type Publisher struct {
	cfg     *GentooConfig
	files   []GeneratedFile
	target  *Repository
	state   *RepositoryState
	author  config.CommitAuthor
	message string
	base    client.Client
}

func NewPublisher(ctx *context.Context, cfg *GentooConfig, files []GeneratedFile, base client.Client) (*Publisher, error) {
	message, err := tmpl.New(ctx).Apply(cfg.raw.CommitMessageTemplate)
	if err != nil {
		return nil, err
	}
	author, err := commitauthor.Get(ctx, cfg.raw.CommitAuthor)
	if err != nil {
		return nil, err
	}
	provider, err := client.NewIfToken(ctx, base, cfg.raw.Repository.Token)
	if err != nil {
		return nil, err
	}
	target := NewRepository(provider, cfg.TargetRepository())
	var stateProvider any = provider
	if cfg.raw.Repository.Git.URL != "" {
		gitClient := client.NewGitUploadClient(cfg.StateRepository().Branch)
		stateProvider = gitClient
	}
	state := NewRepositoryState(NewRepository(stateProvider, cfg.StateRepository()), cfg)
	return &Publisher{cfg: cfg, files: files, target: target, state: state, author: author, message: message, base: base}, nil
}

func (p *Publisher) Publish(ctx *context.Context) error {
	if err := p.Sync(ctx); err != nil {
		return err
	}
	changes, err := p.Prepare(ctx)
	if err != nil {
		return err
	}
	if err := p.Write(ctx, changes); err != nil {
		return err
	}
	return p.OpenPullRequest(ctx)
}

func (p *Publisher) Sync(ctx *context.Context) error {
	if !p.cfg.raw.Repository.PullRequest.Enabled {
		return nil
	}
	if err := p.target.Sync(ctx, p.state.Repository()); err != nil {
		log.WithError(err).Warn("could not sync fork")
	}
	return nil
}

func (p *Publisher) Prepare(ctx *context.Context) (*ChangeSet, error) {
	changes, err := p.incomingChanges()
	if err != nil {
		return nil, err
	}
	retention := &retentionCoordinator{cfg: p.cfg.raw, files: changes.Files()}
	deleted, err := retention.applyVersionRetention(ctx, p.state.Repository().Client(), p.state.Repository().Repo())
	if err != nil {
		return nil, err
	}
	settings, err := loadOverlaySettings(ctx, p.cfg.raw, p.state.Repository().Client(), p.state.Repository().Repo())
	if err != nil {
		return nil, err
	}
	if !settings.ThinManifests() {
		reconstruct, reconstructErr := p.state.NeedsThickReconstruction(ctx)
		if reconstructErr != nil {
			return nil, reconstructErr
		}
		if reconstruct {
			retained, retainedErr := p.state.PackageFiles(ctx)
			if retainedErr != nil {
				return nil, fmt.Errorf("cannot reconstruct thick Manifest from retained package files: %w", retainedErr)
			}
			retention.files = appendMissingPackageFiles(retention.files, retained)
		}
	}
	retention.files = p.filterMetaCache(retention.files, settings)
	planned := NewChangeSet(retention.files...)
	if err := prepareManifestAndMetadata(ctx, p.cfg.raw, p.state.Repository().Client(), p.state.Repository().Repo(), planned, deleted); err != nil {
		return nil, err
	}
	return NewChangeSet(withoutRetainedFiles(planned.Files())...), nil
}

func appendMissingPackageFiles(files, retained []client.RepoFile) []client.RepoFile {
	for _, candidate := range retained {
		found := false
		for _, file := range files {
			if file.Path == candidate.Path {
				found = true
				break
			}
		}
		if !found {
			files = append(files, candidate)
		}
	}
	return files
}

func withoutRetainedFiles(files []client.RepoFile) []client.RepoFile {
	result := files[:0]
	for _, file := range files {
		if file.Identifier == "gentoo-retained" {
			continue
		}
		result = append(result, file)
	}
	return result
}

func (p *Publisher) incomingChanges() (*ChangeSet, error) {
	changes := NewChangeSet()
	for _, generated := range p.files {
		content, err := generated.Content()
		if err != nil {
			return nil, err
		}
		changes.Write(generated.RepoPath, content)
	}
	return changes, nil
}

func (p *Publisher) filterMetaCache(files []client.RepoFile, settings Layout) []client.RepoFile {
	allowed := settings.SupportsMetaCache()
	if p.cfg.MetaCache() && !allowed {
		log.Warnf("gentoo.meta_cache is true for %q, but overlay metadata/layout.conf disables cache-formats", p.cfg.ID())
	}
	prefix := filepath.ToSlash(filepath.Join(p.cfg.OverlayPath(), "metadata", "md5-cache")) + "/"
	result := files[:0]
	for _, file := range files {
		isCacheWrite := strings.HasPrefix(filepath.ToSlash(file.Path), prefix) && !file.Delete
		if isCacheWrite && (!p.cfg.MetaCache() || !allowed) {
			continue
		}
		result = append(result, file)
	}
	return result
}

func (p *Publisher) Write(ctx *context.Context, changes *ChangeSet) error {
	if p.cfg.raw.Repository.Git.URL == "" {
		return p.target.Write(ctx, p.author, p.message, changes)
	}
	gitClient := client.NewGitUploadClient(p.target.Repo().Branch)
	return gitClient.CreateFiles(ctx, p.author, p.target.Repo(), p.message, changes.Files())
}

func (p *Publisher) OpenPullRequest(ctx *context.Context) error {
	pr := p.cfg.raw.Repository.PullRequest
	if !pr.Enabled {
		return nil
	}
	provider, err := client.NewIfToken(ctx, p.base, pr.Token)
	if err != nil {
		return err
	}
	opener, ok := any(provider).(client.PullRequestOpener)
	if !ok {
		return errors.New("client does not support pull requests")
	}
	return opener.OpenPullRequest(ctx, p.state.Repository().Repo(), p.target.Repo(), p.message, pr.Draft)
}
