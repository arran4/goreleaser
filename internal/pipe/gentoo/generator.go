package gentoo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/caarlos0/log"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/extrafiles"
	"github.com/goreleaser/goreleaser/v2/internal/tmpl"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

// Generator coordinates side-effecting generation boundaries. Release,
// install planning, and rendering remain independently testable domain steps.
type Generator struct {
	ctx       *context.Context
	cfg       *GentooConfig
	urlClient client.ReleaseURLTemplater
}

func NewGenerator(ctx *context.Context, cfg *GentooConfig, urlClient client.ReleaseURLTemplater) *Generator {
	return &Generator{ctx: ctx, cfg: cfg, urlClient: urlClient}
}

func (g *Generator) Generate() error {
	release, err := NewRelease(g.ctx, g.cfg, g.urlClient)
	if err != nil {
		return err
	}
	extras, err := g.resolveExtraFiles(release)
	if err != nil {
		return err
	}
	extraInstall, err := g.resolveExtraInstall()
	if err != nil {
		return err
	}
	plan, err := NewInstallPlanner(g.cfg, release, extras, extraInstall).Plan()
	if err != nil {
		return err
	}
	ebuild := g.newEbuild(release, plan, extraInstall)
	content, err := ebuild.Render()
	if err != nil {
		return err
	}
	generated, err := g.write(ebuild, content, extras)
	if err != nil {
		return err
	}
	for _, file := range generated {
		g.ctx.Artifacts.Add(file.Artifact())
	}
	return nil
}

func (g *Generator) resolveExtraFiles(release *Release) (*ExtraFiles, error) {
	files, err := extrafiles.Find(g.ctx, g.cfg.Files())
	if err != nil {
		return nil, err
	}
	extras := NewExtraFiles(g.cfg, release, files)
	if err := extras.Prepare(); err != nil {
		return nil, err
	}
	return extras, nil
}

func (g *Generator) resolveExtraInstall() (string, error) {
	return tmpl.New(g.ctx).WithExtraFields(tmpl.Fields{
		"GentooVersion": g.cfg.Version(),
		"Version":       g.cfg.Version(),
		"Name":          g.cfg.Name(),
		"Category":      g.cfg.Category(),
	}).Apply(g.cfg.ExtraInstall())
}

func (g *Generator) newEbuild(release *Release, plan *InstallProgram, extraInstall string) Ebuild {
	keywords := []string(g.cfg.Keywords())
	if len(keywords) == 0 {
		keywords = release.Keywords()
	}
	var eclasses []string
	for _, eclass := range g.cfg.Eclasses() {
		if !slices.Contains(eclasses, eclass) {
			eclasses = append(eclasses, eclass)
		}
	}
	return Ebuild{
		Name: g.cfg.Name(), Description: g.cfg.Description(), Homepage: g.cfg.Homepage(), License: g.cfg.License(),
		Keywords: strings.Join(keywords, " "), Bindir: g.cfg.Bindir(), ExtraInstall: extraInstall,
		Archs: release.templateArchitectures(), UseFlags: g.cfg.UseFlags(), Eclasses: eclasses, Plan: plan,
	}
}

func (g *Generator) write(ebuild Ebuild, content string, extras *ExtraFiles) ([]GeneratedFile, error) {
	diskPath := filepath.Join(g.ctx.Config.Dist, "gentoo", g.cfg.ID(), g.cfg.EbuildPath())
	if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
		return nil, err
	}
	if err := ebuild.Validate(); err != nil {
		return nil, err
	}
	log.WithField("ebuild", diskPath).Info("writing")
	if err := os.WriteFile(diskPath, []byte(content), 0o644); err != nil {
		return nil, err
	}
	generated := []GeneratedFile{{ConfigID: g.cfg.ID(), RepoPath: g.cfg.EbuildPath(), Kind: GeneratedEbuild, Path: diskPath}}
	aux, err := extras.Write(diskPath)
	if err != nil {
		return nil, err
	}
	generated = append(generated, aux...)
	cache, ok, err := g.writeMetaCache(ebuild, content)
	if err != nil {
		return nil, err
	}
	if ok {
		generated = append(generated, cache)
	}
	return generated, nil
}

func (g *Generator) writeMetaCache(ebuild Ebuild, content string) (GeneratedFile, bool, error) {
	if !g.cfg.MetaCache() {
		return GeneratedFile{}, false, nil
	}
	if ebuild.HasEclasses() {
		// A synthetic cache cannot reproduce metadata contributed by inherited
		// eclasses, so cache generation remains best-effort and conservative.
		log.Warnf("gentoo: meta_cache is enabled for %q, but ebuild %q inherits eclasses; skipping metadata cache generation", g.cfg.ID(), strings.TrimSuffix(filepath.Base(g.cfg.EbuildPath()), ".ebuild"))
		return GeneratedFile{}, false, nil
	}
	cacheContent := generateMetaCacheContent(ebuild, content)
	if cacheContent == "" {
		return GeneratedFile{}, false, nil
	}
	repoPath := g.cfg.MetaCachePath()
	diskPath := filepath.Join(g.ctx.Config.Dist, "gentoo", g.cfg.ID(), repoPath)
	if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
		return GeneratedFile{}, false, err
	}
	if err := os.WriteFile(diskPath, []byte(cacheContent), 0o644); err != nil {
		return GeneratedFile{}, false, err
	}
	return GeneratedFile{ConfigID: g.cfg.ID(), RepoPath: repoPath, Kind: GeneratedMetaCache, Path: diskPath}, true, nil
}
