package gentoo

import (
	"cmp"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/tmpl"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

type Archive struct {
	artifact *artifact.Artifact

	id         string
	goarch     string
	gentooArch string

	filename   string
	uri        string
	wrappedIn  string
	binaries   []string
	files      []string
}

func (a *Archive) ID() string { return a.id }
func (a *Archive) GoArch() string { return a.goarch }
func (a *Archive) GentooArch() string { return a.gentooArch }
func (a *Archive) Filename() string { return a.filename }
func (a *Archive) URI() string { return a.uri }
func (a *Archive) Binaries() []string { return a.binaries }
func (a *Archive) Files() []string { return a.files }
func (a *Archive) Contains(member string) bool {
	p := a.Path(member)
	for _, f := range a.files {
		if f == p { return true }
	}
	for _, b := range a.binaries {
		if b == p { return true }
	}
	return false
}
func (a *Archive) Path(member string) string {
	if a.wrappedIn != "" {
		return path.Join(a.wrappedIn, member)
	}
	return member
}

type Release struct {
	archives []*Archive
}

func NewRelease(ctx *context.Context, cfg *GentooConfig, urlClient client.ReleaseURLTemplater) (*Release, error) {
	filters := []artifact.Filter{
		artifact.ByGoos("linux"),
		artifact.ByType(artifact.UploadableArchive),
		artifact.OnlyReplacingUnibins,
	}
	if len(cfg.ArchiveIDs()) > 0 {
		filters = append(filters, artifact.ByIDs(cfg.ArchiveIDs()...))
	}

	arches := ctx.Artifacts.Filter(artifact.And(filters...)).List()
	if len(arches) == 0 {
		return nil, errors.New("no linux archives found")
	}

	uriTemplate, err := urlClient.ReleaseURLTemplate(ctx)
	if err != nil {
		return nil, err
	}

	var archives []*Archive
	seenArchID := make(map[string]map[string]*artifact.Artifact)

	gentooVer, err := GentooVersionFromRelease(ctx.Version, cmp.Or(cfg.Raw().VersionRepresentation, "gentoo-version"))
	if err != nil {
		return nil, err
	}

	for _, art := range arches {
		url, err := tmpl.New(ctx).WithArtifact(art).Apply(uriTemplate)
		if err != nil {
			return nil, err
		}
		kw, err := gentooArch(art.Goarch)
		if err != nil {
			return nil, err
		}

		id := artifact.ExtraOr(*art, artifact.ExtraID, "default")
		if seenArchID[kw] == nil {
			seenArchID[kw] = make(map[string]*artifact.Artifact)
		}
		if prev, exists := seenArchID[kw][id]; exists {
			return nil, fmt.Errorf("multiple linux archives map to Gentoo architecture %q for ID %q (%s and %s); please filter artifacts", kw, id, prev.Name, art.Name)
		}
		seenArchID[kw][id] = art

		fileName := art.Name
		if !strings.Contains(fileName, gentooVer) && !strings.Contains(fileName, ctx.Version) {
			fileName = fmt.Sprintf("%s-%s-%s", cfg.Name(), gentooVer, fileName)
		}

		wrappedIn := artifact.ExtraOr(*art, artifact.ExtraWrappedIn, "")

		var binaries []string
		if extras := art.Extra[artifact.ExtraBinaries]; extras != nil {
			if e, ok := extras.([]string); ok {
				binaries = e
			}
		} else {
			binaries = []string{cfg.Name()}
		}

		var files []string
		if extras := art.Extra[artifact.ExtraFiles]; extras != nil {
			if e, ok := extras.([]string); ok {
				files = append(files, e...)
			}
		}

		archives = append(archives, &Archive{
			artifact:   art,
			id:         id,
			goarch:     art.Goarch,
			gentooArch: kw,
			filename:   fileName,
			uri:        url,
			wrappedIn:  wrappedIn,
			binaries:   binaries,
			files:      files,
		})
	}

	return &Release{archives: archives}, nil
}

func (r *Release) Archives() []*Archive {
	return r.archives
}

func (r *Release) Architectures() []string {
	archs := make(map[string]struct{})
	for _, a := range r.archives {
		archs[a.GentooArch()] = struct{}{}
	}
	var res []string
	for a := range archs {
		res = append(res, a)
	}
	slices.Sort(res)
	return res
}

func (r *Release) Keywords() []string {
	var kws []string
	for _, arch := range r.Architectures() {
		kws = append(kws, "~"+arch)
	}
	return kws
}

func (r *Release) ArchivesByID(id string) []*Archive {
	var res []*Archive
	for _, a := range r.archives {
		if a.ID() == id {
			res = append(res, a)
		}
	}
	return res
}

func (r *Release) Archive(id, arch string) (*Archive, bool) {
	for _, a := range r.archives {
		if a.ID() == id && a.GentooArch() == arch {
			return a, true
		}
	}
	return nil, false
}

type Distfile struct {
	GentooArch string
	File       string
	URI        string
}

func (r *Release) Distfiles() []Distfile {
	var res []Distfile
	for _, a := range r.archives {
		res = append(res, Distfile{
			GentooArch: a.GentooArch(),
			File:       a.Filename(),
			URI:        a.URI(),
		})
	}
	return res
}
