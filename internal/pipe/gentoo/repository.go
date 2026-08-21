package gentoo

import (
	"errors"
	"path"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	goreleaser_context "github.com/goreleaser/goreleaser/v2/pkg/context"
)

type Repository struct {
	client any
	repo   client.Repo
}

func NewRepository(cl any, repo client.Repo) *Repository {
	return &Repository{
		client: cl,
		repo:   repo,
	}
}

func (r *Repository) WithBranch(branch string) *Repository {
	if branch == "" {
		return r
	}
	repo := r.repo
	repo.Branch = branch
	return &Repository{
		client: r.client,
		repo:   repo,
	}
}

func (r *Repository) Client() any {
	return r.client
}

func (r *Repository) Repo() client.Repo {
	return r.repo
}

func (r *Repository) Read(ctx *goreleaser_context.Context, p string) ([]byte, error) {
	dl, ok := r.client.(client.FileDownloader)
	if !ok {
		return nil, client.ErrNotImplemented
	}
	return dl.DownloadFile(ctx, r.repo, p)
}

type GeneratedFileKind int

const (
	GeneratedEbuild GeneratedFileKind = iota
	GeneratedAux
	GeneratedMetaCache
	GeneratedManifest
	GeneratedMetadata
	GeneratedDist
)

type GeneratedFile struct {
	Path    string
	Content []byte
	Kind    GeneratedFileKind
}

type ChangeSet struct {
	Writes  []GeneratedFile
	Deletes []string
}

func NewChangeSet() *ChangeSet {
	return &ChangeSet{}
}

func (c *ChangeSet) Write(p string, content []byte, kind GeneratedFileKind) {
	c.Writes = append(c.Writes, GeneratedFile{Path: p, Content: content, Kind: kind})
}

func (c *ChangeSet) Delete(p string) {
	c.Deletes = append(c.Deletes, p)
}

func (c *ChangeSet) HasChanges() bool {
	return len(c.Writes) > 0 || len(c.Deletes) > 0
}

type ExistingEbuild struct {
	Name    string
	Version GentooVersion
	Content []byte
}

type RepositoryState struct {
	repo *Repository
	cfg  *GentooConfig
}

func NewRepositoryState(repo *Repository, cfg *GentooConfig) *RepositoryState {
	return &RepositoryState{
		repo: repo,
		cfg:  cfg,
	}
}

func (s *RepositoryState) Layout(ctx *goreleaser_context.Context) (*Layout, error) {
	layoutPath := path.Join(s.cfg.OverlayPath(), "metadata/layout.conf")
	content, err := s.repo.Read(ctx, layoutPath)
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrNotImplemented) {
		l := DefaultLayout()
		return &l, nil
	}
	if err != nil {
		return nil, err
	}
	l, err := ParseLayout(content)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *RepositoryState) Manifest(ctx *goreleaser_context.Context) (*Manifest, error) {
	content, err := s.repo.Read(ctx, s.cfg.ManifestPath())
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrNotImplemented) {
		return &Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseManifest(content)
}

func (s *RepositoryState) Metadata(ctx *goreleaser_context.Context) (*Metadata, error) {
	content, err := s.repo.Read(ctx, s.cfg.MetadataPath())
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrNotImplemented) {
		return NewMetadata(), nil
	}
	if err != nil {
		return nil, err
	}
	return ParseMetadata(content)
}

func (s *RepositoryState) Ebuilds(ctx *goreleaser_context.Context) ([]ExistingEbuild, error) {
	manifest, err := s.Manifest(ctx)
	if err != nil {
		return nil, err
	}
	var ebuilds []ExistingEbuild
	for _, rec := range manifest.Records() {
		if rec.Type == ManifestRecordEBUILD {
			v, err := ParseGentooVersion(rec.Name)
			if err != nil {
				continue
			}
			p := path.Join(s.cfg.PackageDir(), rec.Name)
			content, err := s.repo.Read(ctx, p)
			if err != nil {
				continue
			}
			ebuilds = append(ebuilds, ExistingEbuild{
				Name:    rec.Name,
				Version: v,
				Content: content,
			})
		}
	}
	return ebuilds, nil
}
