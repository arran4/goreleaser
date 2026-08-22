package gentoo

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/tmpl"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

// Archive is the normalized Gentoo view of one release archive. Artifact Extra
// is decoded during construction so downstream planning never depends on its
// weakly typed map representation.
type Archive struct {
	name       string
	sourcePath string
	id         string
	goarch     string
	gentooArch string
	uri        string
	distfile   string
	wrappedIn  string
	binaries   []string
	files      []string
}

func (a *Archive) ID() string         { return a.id }
func (a *Archive) Name() string       { return a.name }
func (a *Archive) SourcePath() string { return a.sourcePath }
func (a *Archive) GoArch() string     { return a.goarch }
func (a *Archive) GentooArch() string { return a.gentooArch }
func (a *Archive) URI() string        { return a.uri }
func (a *Archive) Distfile() string   { return a.distfile }
func (a *Archive) Binaries() []string { return slices.Clone(a.binaries) }
func (a *Archive) Files() []string    { return slices.Clone(a.files) }

func (a *Archive) Path(member string) string {
	if a.wrappedIn == "" {
		return normalizeArchivePath(member)
	}
	return normalizeArchivePath(path.Join(a.wrappedIn, member))
}

func (a *Archive) Contains(member string) bool {
	want := normalizeArchivePath(member)
	for _, candidate := range append(a.Files(), a.Binaries()...) {
		if a.Path(candidate) == want {
			return true
		}
	}
	return false
}

// Release owns archive selection, architecture normalization, URI expansion,
// and distfile naming for one Gentoo package version.
type Release struct {
	archives []*Archive
}

func NewRelease(ctx *context.Context, cfg *GentooConfig, cl client.ReleaseURLTemplater) (*Release, error) {
	selected := selectReleaseArtifacts(ctx, cfg)
	if len(selected) == 0 {
		return nil, errors.New("no linux archives found")
	}
	uriTemplate, err := cl.ReleaseURLTemplate(ctx)
	if err != nil {
		return nil, err
	}

	release := &Release{}
	seen := map[string]*Archive{}
	for _, art := range selected {
		archive, err := newArchive(ctx, cfg, art, uriTemplate)
		if err != nil {
			return nil, err
		}
		key := archive.ID() + "\x00" + archive.GentooArch()
		if previous := seen[key]; previous != nil {
			return nil, fmt.Errorf("multiple linux archives map to Gentoo architecture %q for ID %q (%s and %s); please filter artifacts", archive.GentooArch(), archive.ID(), previous.Name(), archive.Name())
		}
		seen[key] = archive
		release.archives = append(release.archives, archive)
	}
	slices.SortFunc(release.archives, func(a, b *Archive) int {
		if c := strings.Compare(a.GentooArch(), b.GentooArch()); c != 0 {
			return c
		}
		if c := strings.Compare(a.ID(), b.ID()); c != 0 {
			return c
		}
		return strings.Compare(a.Distfile(), b.Distfile())
	})
	return release, nil
}

func selectReleaseArtifacts(ctx *context.Context, cfg *GentooConfig) []*artifact.Artifact {
	filters := []artifact.Filter{
		artifact.ByGoos("linux"),
		artifact.ByType(artifact.UploadableArchive),
		artifact.OnlyReplacingUnibins,
	}
	if ids := cfg.ArchiveIDs(); len(ids) > 0 {
		filters = append(filters, artifact.ByIDs(ids...))
	}
	return ctx.Artifacts.Filter(artifact.And(filters...)).List()
}

// DistfileSource is the normalized local input used to hash a DIST record.
type DistfileSource struct {
	Name string
	Path string
}

// ReleaseDistfiles applies the same archive boundary used by generation but
// retains only the normalized values needed to hash publication DIST records.
func ReleaseDistfiles(ctx *context.Context, cfg *GentooConfig) []DistfileSource {
	selected := selectReleaseArtifacts(ctx, cfg)
	result := make([]DistfileSource, 0, len(selected))
	for _, archive := range selected {
		result = append(result, DistfileSource{Name: archiveDistfile(cfg, ctx.Version, archive.Name), Path: archive.Path})
	}
	slices.SortFunc(result, func(a, b DistfileSource) int { return strings.Compare(a.Name, b.Name) })
	return result
}

func newArchive(ctx *context.Context, cfg *GentooConfig, art *artifact.Artifact, uriTemplate string) (*Archive, error) {
	gentooArchitecture, err := gentooArch(art.Goarch)
	if err != nil {
		return nil, err
	}
	uri, err := tmpl.New(ctx).WithArtifact(art).Apply(uriTemplate)
	if err != nil {
		return nil, err
	}
	distfile := archiveDistfile(cfg, ctx.Version, art.Name)
	binaries := artifact.ExtraOr(*art, artifact.ExtraBinaries, []string{})
	if len(binaries) == 0 {
		binaries = []string{cfg.Name()}
	}
	return &Archive{
		name:       art.Name,
		sourcePath: art.Path,
		id:         artifact.ExtraOr(*art, artifact.ExtraID, "default"),
		goarch:     art.Goarch,
		gentooArch: gentooArchitecture,
		uri:        uri,
		distfile:   distfile,
		wrappedIn:  artifact.ExtraOr(*art, artifact.ExtraWrappedIn, ""),
		binaries:   slices.Clone(binaries),
		files:      slices.Clone(artifact.ExtraOr(*art, artifact.ExtraFiles, []string{})),
	}, nil
}

func archiveDistfile(cfg *GentooConfig, releaseVersion, filename string) string {
	if strings.Contains(filename, cfg.Version()) || strings.Contains(filename, releaseVersion) {
		return filename
	}
	return fmt.Sprintf("%s-%s-%s", cfg.Name(), cfg.Version(), filename)
}

func (r *Release) Archives() []*Archive { return slices.Clone(r.archives) }

func (r *Release) ArchivesByID(id string) []*Archive {
	var result []*Archive
	for _, archive := range r.archives {
		if archive.ID() == id {
			result = append(result, archive)
		}
	}
	return result
}

func (r *Release) Archive(id, arch string) *Archive {
	for _, archive := range r.archives {
		if archive.ID() == id && archive.GentooArch() == arch {
			return archive
		}
	}
	return nil
}

func (r *Release) Architectures() []string {
	var result []string
	for _, archive := range r.archives {
		result = append(result, archive.GentooArch())
	}
	return slices.Compact(result)
}

func (r *Release) Keywords() []string {
	result := r.Architectures()
	for i := range result {
		result[i] = "~" + result[i]
	}
	return result
}

func (r *Release) Distfiles() []string {
	var result []string
	for _, archive := range r.archives {
		result = append(result, archive.Distfile())
	}
	return result
}

func (r *Release) templateArchitectures() []archData {
	byArch := map[string][]archItem{}
	for _, archive := range r.archives {
		byArch[archive.GentooArch()] = append(byArch[archive.GentooArch()], archItem{File: archive.Distfile(), URI: archive.URI()})
	}
	var result []archData
	for _, arch := range r.Architectures() {
		items := byArch[arch]
		slices.SortFunc(items, func(a, b archItem) int { return strings.Compare(a.File, b.File) })
		result = append(result, archData{Keyword: arch, URIs: items})
	}
	return result
}

func normalizeArchivePath(value string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(value)), "./")
}
