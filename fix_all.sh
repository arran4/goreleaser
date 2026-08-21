#!/bin/bash
# Apply fixes to all files to get them to compile!

# Ebuild.go has issue with `gentooArch` not defined? Yes, let's put it in `version.go` since it is a common utility, or `utils.go`.
cat << 'EOT' > internal/pipe/gentoo/utils.go
package gentoo

import (
	"errors"
	"fmt"
)

func gentooArch(goarch string) (string, error) {
	switch goarch {
	case "386":
		return "x86", nil
	case "amd64":
		return "amd64", nil
	case "arm":
		return "arm", nil
	case "arm64":
		return "arm64", nil
	case "mips":
		return "mips", nil
	case "mips64", "mips64le":
		return "mips64", nil
	case "ppc":
		return "ppc", nil
	case "ppc64":
		return "ppc64", nil
	case "ppc64le":
		return "", errors.New("ppc64le is not supported by gentoo pipe due to ambiguity with ppc64")
	case "riscv64":
		return "riscv", nil
	case "s390x":
		return "s390", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
}
EOT

# Delete old procedural code from gentoo.go
sed -i '/func collectPublishGroups(/,$d' internal/pipe/gentoo/gentoo.go

# Add Generator, Pipe.Run, Pipe.Publish to gentoo.go
cat << 'EOT' >> internal/pipe/gentoo/gentoo.go

type Generator struct {
	ctx       *context.Context
	cfg       *GentooConfig
	urlClient client.ReleaseURLTemplater
}

func NewGenerator(ctx *context.Context, cfg *GentooConfig, urlClient client.ReleaseURLTemplater) *Generator {
	return &Generator{
		ctx:       ctx,
		cfg:       cfg,
		urlClient: urlClient,
	}
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

	generatedFiles = append(generatedFiles, GeneratedFile{
		Path:    ebuildPath,
		Content: ebuildContent,
		Kind:    GeneratedEbuild,
	})

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

	return generatedFiles, nil
}

func writeGeneratedFiles(ctx *context.Context, cfg *GentooConfig, files []GeneratedFile) error {
	for _, f := range files {
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
			Type: artifact.GentooEbuild,
			Extra: map[string]interface{}{
				"GentooConfig": cfg.Raw(),
			},
		}
		ctx.Artifacts.Add(&art)
	}
	return nil
}
EOT
