package gentoo

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/ids"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

type Pipe struct{}

func (Pipe) String() string        { return "gentoo ebuild" }
func (Pipe) ContinueOnError() bool { return true }
func (Pipe) Skip(ctx *context.Context) bool { return len(ctx.Config.Gentoos) == 0 }

func (Pipe) Default(ctx *context.Context) error {
	ids := ids.New("gentoo_overlays")
	for i := range ctx.Config.Gentoos {
		raw := ctx.Config.Gentoos[i]
		cfg, err := NewGentooConfig(ctx, raw)
		if err != nil {
			return err
		}
		ctx.Config.Gentoos[i] = cfg.Raw()
		ids.Inc(cfg.ID())
	}
	return ids.Validate()
}

type Generator struct {
	ctx       *context.Context
	cfg       *GentooConfig
	urlClient client.ReleaseURLTemplater
}

func NewGenerator(ctx *context.Context, cfg *GentooConfig, urlClient client.ReleaseURLTemplater) *Generator {
	return &Generator{ctx: ctx, cfg: cfg, urlClient: urlClient}
}

func (g *Generator) Generate() ([]GeneratedFile, error) {
	release, err := NewRelease(g.ctx, g.cfg, g.urlClient)
	if err != nil {
		return nil, err
	}

	planner := NewInstallPlanner(g.cfg, release)
	installs, err := planner.Plan()
	if err != nil {
		return nil, err
	}

	extras := NewExtraFiles(g.cfg, release, g.cfg.Raw().Files)
	if err := extras.Validate(); err != nil {
		return nil, err
	}
	extras.RemoveArchived()

	ebuild, err := NewEbuild(g.cfg, release, installs, extras)
	if err != nil {
		return nil, err
	}

	var generatedFiles []GeneratedFile
	ebuildContent, err := ebuild.Render()
	if err != nil {
		return nil, err
	}

	gentooVer, err := GentooVersionFromRelease(g.ctx.Version, cmp.Or(g.cfg.Raw().VersionRepresentation, "gentoo-version"))
	if err != nil {
		return nil, err
	}
	ver, err := ParseGentooVersion(gentooVer + ".ebuild")
	if err != nil {
		return nil, err
	}

	ebuildPath := g.cfg.EbuildPath(ver)
	generatedFiles = append(generatedFiles, GeneratedFile{Path: ebuildPath, Content: ebuildContent, Kind: GeneratedEbuild})

	if g.cfg.Raw().MetaCache {
		mc := &MetaCache{ebuild: ebuild}
		mcContent, err := mc.Render(ebuildContent)
		if err == nil {
			generatedFiles = append(generatedFiles, GeneratedFile{
				Path:    g.cfg.MetaCacheDir() + "/" + fmt.Sprintf("%s-%s", g.cfg.PackageName(), ver.String()),
				Content: mcContent,
				Kind:    GeneratedMetaCache,
			})
		}
	}

	extraGenerated, err := extras.Write(g.ctx, g.cfg.PackageDir())
	if err != nil {
		return nil, err
	}
	generatedFiles = append(generatedFiles, extraGenerated...)

	for _, dist := range release.Distfiles() {
		generatedFiles = append(generatedFiles, GeneratedFile{
			Path:    dist.File,
			Content: []byte(dist.URI),
			Kind:    GeneratedDist,
		})
	}

	return generatedFiles, nil
}

func (Pipe) Run(ctx *context.Context) error {
	cl, err := client.NewReleaseClient(ctx)
	if err != nil {
		return err
	}

	for _, raw := range ctx.Config.Gentoos {
		cfg, err := NewGentooConfig(ctx, raw)
		if err != nil {
			return err
		}

		if checkSkip(ctx, cfg.Raw().SkipUpload) {
			continue
		}

		generator := NewGenerator(ctx, cfg, cl)
		files, err := generator.Generate()
		if err != nil {
			return err
		}

		if err := writeGeneratedFiles(ctx, cfg, files); err != nil {
			return err
		}
	}
	return nil
}

func (Pipe) Publish(ctx *context.Context) error {
	cl, err := client.New(ctx)
	if err != nil {
		return err
	}

	urlClient, err := client.NewReleaseClient(ctx)
	if err != nil {
		return err
	}

	for _, raw := range ctx.Config.Gentoos {
		cfg, err := NewGentooConfig(ctx, raw)
		if err != nil {
			return err
		}

		if checkSkip(ctx, cfg.Raw().SkipUpload) {
			continue
		}

		generator := NewGenerator(ctx, cfg, urlClient)
		files, err := generator.Generate()
		if err != nil {
			return err
		}

		publisher, err := NewPublisher(ctx, cfg, files, cl)
		if err != nil {
			return err
		}

		if err := publisher.Publish(ctx); err != nil {
			return err
		}
	}
	return nil
}

func writeGeneratedFiles(ctx *context.Context, cfg *GentooConfig, files []GeneratedFile) error {
	for _, f := range files {
		if f.Kind == GeneratedDist {
			continue
		}
		target := filepath.Join(ctx.Config.Dist, "gentoo", cfg.ID(), f.Path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, f.Content, 0o644); err != nil {
			return err
		}

		art := artifact.Artifact{
			Name: f.Path,
			Path: target,
			Type: func(k GeneratedFileKind) artifact.Type {
				if k == GeneratedEbuild {
					return artifact.GentooEbuild
				}
				return artifact.GentooFile
			}(f.Kind),
			Extra: map[string]interface{}{
				"GentooConfig": cfg.Raw(),
			},
		}
		ctx.Artifacts.Add(&art)
	}
	return nil
}
