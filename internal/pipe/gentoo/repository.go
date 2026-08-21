package gentoo

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

// Repository binds a provider client to one complete owner/name/branch
// identity and centralizes optional SCM capability adaptation.
type Repository struct {
	client any
	repo   client.Repo
}

func NewRepository(provider any, repo client.Repo) *Repository {
	return &Repository{client: provider, repo: repo}
}

func (r *Repository) Repo() client.Repo { return r.repo }
func (r *Repository) Client() any       { return r.client }

func (r *Repository) Read(ctx *context.Context, name string) ([]byte, error) {
	downloader, ok := r.client.(client.FileDownloader)
	if !ok {
		return nil, client.ErrNotImplemented
	}
	return downloader.DownloadFile(ctx, r.repo, name)
}

func (r *Repository) List(ctx *context.Context, directory string) ([]string, error) {
	lister, ok := r.client.(client.DirectoryLister)
	if !ok {
		return nil, client.ErrNotImplemented
	}
	return lister.ListDir(ctx, r.repo, directory)
}

func (r *Repository) Sync(ctx *context.Context, base *Repository) error {
	syncer, ok := r.client.(client.ForkSyncer)
	if !ok {
		return nil
	}
	return syncer.SyncFork(ctx, r.repo, base.repo)
}

func (r *Repository) Write(ctx *context.Context, author config.CommitAuthor, message string, changes *ChangeSet) error {
	files := changes.Files()
	if creator, ok := r.client.(client.FilesCreator); ok {
		return creator.CreateFiles(ctx, author, r.repo, message, files)
	}
	for _, file := range files {
		if file.Delete {
			if err := r.delete(ctx, author, message, file.Path); err != nil {
				return err
			}
			continue
		}
		creator, ok := r.client.(client.FileCreator)
		if !ok {
			return client.ErrNotImplemented
		}
		if err := creator.CreateFile(ctx, author, r.repo, file.Content, file.Path, message); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) delete(ctx *context.Context, author config.CommitAuthor, message, name string) error {
	deleter, ok := r.client.(client.FileDeleter)
	if !ok {
		return nil
	}
	err := deleter.DeleteFile(ctx, author, r.repo, name, message)
	if errors.Is(err, client.ErrNotImplemented) {
		return nil
	}
	return err
}

// RepositoryState owns all reads of the existing package. The same resolved
// repository identity is used for retention, revisions, metadata, Manifest,
// and cache inspection.
type RepositoryState struct {
	repo *Repository
	cfg  *GentooConfig
}

func NewRepositoryState(repo *Repository, cfg *GentooConfig) *RepositoryState {
	return &RepositoryState{repo: repo, cfg: cfg}
}

func (s *RepositoryState) Repository() *Repository { return s.repo }

func (s *RepositoryState) Ebuilds(ctx *context.Context) ([]string, error) {
	names, err := s.repo.List(ctx, s.cfg.PackageDir())
	if err != nil {
		return nil, err
	}
	prefix := s.cfg.PackageName() + "-"
	result := names[:0]
	for _, name := range names {
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".ebuild") {
			result = append(result, name)
		}
	}
	return result, nil
}

// PackageFiles enumerates retained package content needed to construct a thick
// Manifest. Callers may skip ErrNotImplemented for providers that cannot list;
// a thin-to-thick transition must otherwise use these bytes rather than emit an
// incomplete Manifest.
func (s *RepositoryState) PackageFiles(ctx *context.Context) ([]client.RepoFile, error) {
	return s.readTree(ctx, s.cfg.PackageDir())
}

func (s *RepositoryState) NeedsThickReconstruction(ctx *context.Context) (bool, error) {
	content, err := s.repo.Read(ctx, s.cfg.ManifestPath())
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrNotImplemented) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	manifest, err := ParseManifest(content)
	if err != nil {
		return false, fmt.Errorf("parse existing Manifest: %w", err)
	}
	records := manifest.Records()
	if len(records) == 0 {
		return false, nil
	}
	for _, record := range records {
		if record.Type != "DIST" {
			return false, nil
		}
	}
	return true, nil
}

func (s *RepositoryState) readTree(ctx *context.Context, directory string) ([]client.RepoFile, error) {
	names, err := s.repo.List(ctx, directory)
	if err != nil {
		return nil, err
	}
	var result []client.RepoFile
	for _, name := range names {
		filename := path.Join(directory, name)
		if filename == s.cfg.ManifestPath() {
			continue
		}
		content, readErr := s.repo.Read(ctx, filename)
		if readErr == nil {
			result = append(result, client.RepoFile{Path: filename, Content: content, Identifier: "gentoo-retained"})
			continue
		}
		children, listErr := s.readTree(ctx, filename)
		if listErr == nil {
			result = append(result, children...)
			continue
		}
		if errors.Is(readErr, client.ErrNotFound) || errors.Is(readErr, client.ErrNotImplemented) {
			continue
		}
		return nil, readErr
	}
	return result, nil
}
