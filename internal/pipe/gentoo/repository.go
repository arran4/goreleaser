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

func (r *Repository) OpenPullRequest(ctx *context.Context, head *Repository, title string, draft bool) error {
	opener, ok := r.client.(client.PullRequestOpener)
	if !ok {
		return errors.New("client does not support pull requests")
	}
	return opener.OpenPullRequest(ctx, r.repo, head.repo, title, draft)
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
		return client.ErrNotImplemented
	}
	return deleter.DeleteFile(ctx, author, r.repo, name, message)
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
	if errors.Is(err, client.ErrNotImplemented) || errors.Is(err, client.ErrNotFound) {
		names = nil
	} else if err != nil {
		return nil, err
	}
	prefix := s.cfg.PackageName() + "-"
	result := names[:0]
	for _, name := range names {
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".ebuild") {
			result = append(result, name)
		}
	}
	if len(result) > 0 {
		return result, nil
	}
	manifest, manifestErr := s.Manifest(ctx)
	if manifestErr != nil {
		return nil, manifestErr
	}
	for _, record := range manifest.Records() {
		if record.Type == "EBUILD" && strings.HasPrefix(record.Name, prefix) && strings.HasSuffix(record.Name, ".ebuild") {
			result = append(result, record.Name)
		}
	}
	return result, nil
}

func (s *RepositoryState) Retention(ctx *context.Context, incoming *ChangeSet) (RetentionState, error) {
	state := RetentionState{Files: map[string][]byte{}, MetaCacheFiles: map[string][]byte{}}
	names, err := s.Ebuilds(ctx)
	if errors.Is(err, client.ErrNotImplemented) {
		names = nil
	} else if err != nil {
		return state, err
	}
	for _, name := range names {
		version := parseGentooVersion(name, s.cfg.PackageName()+"-")
		if version == nil {
			continue
		}
		filename := path.Join(s.cfg.PackageDir(), name)
		content, readErr := s.repo.Read(ctx, filename)
		contentAvailable := readErr == nil
		if readErr != nil && !errors.Is(readErr, client.ErrNotFound) && !errors.Is(readErr, client.ErrNotImplemented) {
			return state, readErr
		}
		state.Ebuilds = append(state.Ebuilds, ExistingEbuild{
			Name: name, Path: filename, Version: version, Content: content, ContentAvailable: contentAvailable,
		})
		if contentAvailable {
			state.Files[filename] = content
		}
	}
	for _, file := range incoming.Files() {
		if file.Delete {
			continue
		}
		if err := s.readOptional(ctx, file.Path, state.Files); err != nil {
			return state, err
		}
	}
	if !s.cfg.MetaCache() {
		return state, nil
	}
	cacheNames, err := s.repo.List(ctx, s.cfg.MetaCacheDir())
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrNotImplemented) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	for _, name := range cacheNames {
		filename := path.Join(s.cfg.MetaCacheDir(), name)
		content, readErr := s.repo.Read(ctx, filename)
		if errors.Is(readErr, client.ErrNotFound) || errors.Is(readErr, client.ErrNotImplemented) {
			continue
		}
		if readErr != nil {
			return state, readErr
		}
		state.Files[filename] = content
		state.MetaCacheFiles[filename] = content
	}
	return state, nil
}

func (s *RepositoryState) readOptional(ctx *context.Context, filename string, destination map[string][]byte) error {
	content, err := s.repo.Read(ctx, filename)
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrNotImplemented) {
		return nil
	}
	if err != nil {
		return err
	}
	destination[filename] = content
	return nil
}

func (s *RepositoryState) Layout(ctx *context.Context) (Layout, error) {
	content, err := s.repo.Read(ctx, path.Join(s.cfg.OverlayPath(), "metadata/layout.conf"))
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrNotImplemented) {
		return ParseLayout(nil).WithConfig(s.cfg.manifest()), nil
	}
	if err != nil {
		return Layout{}, fmt.Errorf("failed to download layout.conf: %w", err)
	}
	return ParseLayout(content).WithConfig(s.cfg.manifest()), nil
}

func (s *RepositoryState) Metadata(ctx *context.Context) (*Metadata, error) {
	content, err := s.repo.Read(ctx, s.cfg.MetadataPath())
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrNotImplemented) {
		return NewMetadata(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to download metadata.xml: %w", err)
	}
	metadata, err := ParseMetadata(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metadata.xml: %w", err)
	}
	return metadata, nil
}

func (s *RepositoryState) Manifest(ctx *context.Context) (*Manifest, error) {
	content, err := s.repo.Read(ctx, s.cfg.ManifestPath())
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrNotImplemented) {
		return &Manifest{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to download Manifest: %w", err)
	}
	manifest, err := ParseManifest(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Manifest: %w", err)
	}
	return manifest, nil
}

// PackageFiles enumerates retained package content needed to construct a thick
// Manifest. Publication fails when a backend cannot enumerate the complete
// tree; silently omitting retained AUX files would create an invalid Manifest.
func (s *RepositoryState) PackageFiles(ctx *context.Context) ([]client.RepoFile, error) {
	return s.readTree(ctx, s.cfg.PackageDir())
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
		return nil, fmt.Errorf("cannot read retained package entry %s as a file (%v) or directory: %w", filename, readErr, listErr)
	}
	return result, nil
}
