package gentoo

import (
	"fmt"
	"path/filepath"
	"slices"
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
	byID := map[string]int{}
	var result []publicationInput
	for _, art := range artifacts {
		generated, err := GeneratedFileFromArtifact(*art)
		if err != nil {
			return nil, err
		}
		index, ok := byID[generated.ConfigID]
		if !ok {
			raw, err := gentooConfigByID(ctx, generated.ConfigID)
			if err != nil {
				return nil, err
			}
			skip, err := tmpl.New(ctx).Apply(publicationConfigFrom(raw).skipUpload)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(skip) == "true" || strings.TrimSpace(skip) == "auto" && ctx.Semver.Prerelease != "" {
				byID[generated.ConfigID] = -1
				continue
			}
			cfg, err := NewGentooConfig(ctx, raw)
			if err != nil {
				return nil, err
			}
			result = append(result, publicationInput{cfg: cfg})
			index = len(result) - 1
			byID[generated.ConfigID] = index
		}
		if index < 0 {
			continue
		}
		result[index].files = append(result[index].files, generated)
	}
	for i := range result {
		slices.SortFunc(result[i].files, func(a, b GeneratedFile) int { return strings.Compare(a.RepoPath, b.RepoPath) })
	}
	slices.SortFunc(result, func(a, b publicationInput) int { return strings.Compare(a.cfg.ID(), b.cfg.ID()) })
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
	config  publicationConfig
}

func NewPublisher(ctx *context.Context, cfg *GentooConfig, files []GeneratedFile, base client.Client) (*Publisher, error) {
	publication := cfg.publication()
	message, err := tmpl.New(ctx).Apply(publication.commitMessage)
	if err != nil {
		return nil, err
	}
	author, err := commitauthor.Get(ctx, publication.commitAuthor)
	if err != nil {
		return nil, err
	}
	provider, err := client.NewIfToken(ctx, base, publication.repository.Token)
	if err != nil {
		return nil, err
	}
	target := NewRepository(provider, cfg.TargetRepository())
	var stateProvider any = provider
	stateRepo := cfg.StateRepository()
	if publication.repository.Git.URL != "" && sameRepository(target.Repo(), stateRepo) {
		gitClient := client.NewGitUploadClient(cfg.StateRepository().Branch)
		stateProvider = gitClient
	}
	state := NewRepositoryState(NewRepository(stateProvider, stateRepo), cfg)
	return &Publisher{cfg: cfg, files: files, target: target, state: state, author: author, message: message, base: base, config: publication}, nil
}

func sameRepository(a, b client.Repo) bool {
	return a.Owner == b.Owner && a.Name == b.Name
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
	if !p.config.repository.PullRequest.Enabled {
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
	retentionState, err := p.state.Retention(ctx, changes)
	if err != nil {
		return nil, err
	}
	retention, err := NewRetentionPlanner(p.cfg, retentionState, changes).Plan()
	if err != nil {
		return nil, err
	}
	layout, err := p.state.Layout(ctx)
	if err != nil {
		return nil, err
	}
	planned := p.filterMetaCache(retention.Changes, layout)
	metadataConfig := p.cfg.metadata()
	if !metadataConfig.Empty() {
		metadata, metadataErr := p.state.Metadata(ctx)
		if metadataErr != nil {
			return nil, metadataErr
		}
		if err := prepareMetadata(metadata, metadataConfig, planned, p.cfg.MetadataPath()); err != nil {
			return nil, err
		}
	}
	manifest, err := p.state.Manifest(ctx)
	if err != nil {
		return nil, err
	}
	var packageFiles []client.RepoFile
	if !layout.ThinManifests() {
		packageFiles, err = p.state.PackageFiles(ctx)
		if err != nil {
			return nil, fmt.Errorf("cannot construct thick Manifest from retained package files: %w", err)
		}
	}
	planner := NewManifestPlanner(p.cfg, layout, manifest).
		WithPackageState(packageFiles, retention.RetainedEbuilds, retention.Deletes).
		WithDistfiles(ReleaseDistfiles(ctx, p.cfg))
	if err := planner.Apply(planned); err != nil {
		return nil, err
	}
	return planned, nil
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

func (p *Publisher) filterMetaCache(changes *ChangeSet, settings Layout) *ChangeSet {
	allowed := settings.SupportsMetaCache()
	if p.cfg.MetaCache() && !allowed {
		log.Warnf("gentoo.meta_cache is true for %q, but overlay metadata/layout.conf disables cache-formats", p.cfg.ID())
	}
	prefix := filepath.ToSlash(filepath.Join(p.cfg.OverlayPath(), "metadata", "md5-cache")) + "/"
	result := changes.Clone()
	for _, file := range changes.Files() {
		isCacheWrite := strings.HasPrefix(filepath.ToSlash(file.Path), prefix) && !file.Delete
		if isCacheWrite && (!p.cfg.MetaCache() || !allowed) {
			result.Remove(file.Path)
		}
	}
	return result
}

func (p *Publisher) Write(ctx *context.Context, changes *ChangeSet) error {
	if p.config.repository.Git.URL == "" {
		return p.target.Write(ctx, p.author, p.message, changes)
	}
	gitClient := client.NewGitUploadClient(p.target.Repo().Branch)
	return gitClient.CreateFiles(ctx, p.author, p.target.Repo(), p.message, changes.Files())
}

func (p *Publisher) OpenPullRequest(ctx *context.Context) error {
	pr := p.config.repository.PullRequest
	if !pr.Enabled {
		return nil
	}
	provider, err := client.NewIfToken(ctx, p.base, pr.Token)
	if err != nil {
		return err
	}
	base := NewRepository(provider, p.state.Repository().Repo())
	return base.OpenPullRequest(ctx, p.target, p.message, pr.Draft)
}
