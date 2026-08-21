package gentoo

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/tmpl"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

// GentooConfig is the validated, template-expanded configuration used by a
// single generation or publication operation. Keeping the raw configuration
// private makes this the boundary at which template values and package paths
// become stable domain values.
type GentooConfig struct {
	raw     config.Gentoo
	version string
}

// GentooConfigs owns invariants that span configured publication targets.
type GentooConfigs struct {
	entries []*GentooConfig
}

func NewGentooConfigs(ctx *context.Context, raw []config.Gentoo) (*GentooConfigs, error) {
	result := &GentooConfigs{}
	destinations := map[string]string{}
	for _, entry := range raw {
		resolved, err := NewGentooConfig(ctx, entry)
		if err != nil {
			return nil, err
		}
		if previous, ok := destinations[resolved.DestinationKey()]; ok {
			return nil, fmt.Errorf("gentoo configs %q and %q publish to the same package destination", previous, resolved.ID())
		}
		destinations[resolved.DestinationKey()] = resolved.ID()
		result.entries = append(result.entries, resolved)
	}
	return result, nil
}

func (c *GentooConfigs) Entries() []*GentooConfig { return slices.Clone(c.entries) }

func NewGentooConfig(ctx *context.Context, raw config.Gentoo) (*GentooConfig, error) {
	version, err := convertToGentooVersion(ctx.Version, cmp.Or(raw.VersionRepresentation, "gentoo-version"))
	if err != nil {
		return nil, err
	}

	tp := tmpl.New(ctx).WithExtraFields(tmpl.Fields{
		"GentooVersion": version,
		"Version":       version,
		"Name":          raw.Name,
		"Category":      raw.Category,
	})
	if err := tp.ApplyAll(&raw.Name, &raw.Category, &raw.OverlayPath, &raw.Description, &raw.Homepage, &raw.BugsTo, &raw.License); err != nil {
		return nil, err
	}
	if raw.Repository, err = client.TemplateRef(tp.Apply, raw.Repository); err != nil {
		return nil, err
	}

	cfg := &GentooConfig{raw: raw, version: version}
	if err := cfg.validatePackage(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *GentooConfig) ID() string          { return c.raw.ID }
func (c *GentooConfig) Name() string        { return c.raw.Name }
func (c *GentooConfig) Category() string    { return c.raw.Category }
func (c *GentooConfig) Version() string     { return c.version }
func (c *GentooConfig) OverlayPath() string { return c.raw.OverlayPath }
func (c *GentooConfig) Bindir() string      { return c.raw.Bindir }

func (c *GentooConfig) PackageName() string {
	if strings.HasSuffix(c.Name(), "-bin") {
		return c.Name()
	}
	return c.Name() + "-bin"
}

func (c *GentooConfig) TargetRepository() client.Repo {
	return client.RepoFromRef(c.raw.Repository)
}

// StateRepository is the repository whose current overlay contents are read.
// For pull requests this is the complete base identity, not the fork with only
// its branch changed.
func (c *GentooConfig) StateRepository() client.Repo {
	target := c.TargetRepository()
	if !c.raw.Repository.PullRequest.Enabled {
		return target
	}
	base := c.raw.Repository.PullRequest.Base
	if base.Owner != "" {
		target.Owner = base.Owner
	}
	if base.Name != "" {
		target.Name = base.Name
	}
	if base.Branch != "" {
		target.Branch = base.Branch
	}
	return target
}

func (c *GentooConfig) PackageDir() string {
	p := filepath.ToSlash(filepath.Join(c.Category(), c.PackageName()))
	if c.OverlayPath() != "" {
		return filepath.ToSlash(filepath.Join(c.OverlayPath(), p))
	}
	return p
}

func (c *GentooConfig) EbuildPath() string {
	return filepath.ToSlash(filepath.Join(c.PackageDir(), fmt.Sprintf("%s-%s.ebuild", c.PackageName(), c.Version())))
}

func (c *GentooConfig) MetadataPath() string {
	return filepath.ToSlash(filepath.Join(c.PackageDir(), "metadata.xml"))
}

func (c *GentooConfig) ManifestPath() string {
	return filepath.ToSlash(filepath.Join(c.PackageDir(), "Manifest"))
}

func (c *GentooConfig) MetaCachePath() string {
	return filepath.ToSlash(filepath.Join(c.OverlayPath(), "metadata", "md5-cache", c.Category(), c.PackageName()+"-"+c.Version()))
}

func (c *GentooConfig) DestinationKey() string {
	r := c.StateRepository()
	return strings.Join([]string{c.raw.Repository.Git.URL, r.Owner, r.Name, r.Branch, c.OverlayPath(), c.Category(), c.PackageName()}, "\x00")
}

func (c *GentooConfig) ArchiveIDs() []string         { return slices.Clone(c.raw.IDs) }
func (c *GentooConfig) Keywords() config.StringArray { return slices.Clone(c.raw.Keywords) }
func (c *GentooConfig) Description() string          { return c.raw.Description }
func (c *GentooConfig) Homepage() string             { return c.raw.Homepage }
func (c *GentooConfig) License() string              { return c.raw.License }
func (c *GentooConfig) Eclasses() []string           { return slices.Clone(c.raw.Eclasses) }
func (c *GentooConfig) MetaCache() bool              { return c.raw.MetaCache }
func (c *GentooConfig) SkipFilesValidation() bool    { return c.raw.SkipFilesValidation }
func (c *GentooConfig) Files() []config.ExtraFile    { return slices.Clone(c.raw.Files) }
func (c *GentooConfig) ExtraInstall() string         { return c.raw.ExtraInstall }

func (c *GentooConfig) validatePackage() error {
	for _, p := range []string{c.PackageDir(), c.EbuildPath()} {
		clean := filepath.ToSlash(filepath.Clean(p))
		if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
			return fmt.Errorf("path %q must remain within the overlay", p)
		}
	}
	return nil
}

// GentooArtifactRef is deliberately safe to serialize in artifacts.json. The
// publish phase resolves the original configuration again from ConfigID.
type GentooArtifactRef struct {
	ConfigID  string
	RepoPath  string
	MetaCache bool
}

func gentooConfigByID(ctx *context.Context, id string) (config.Gentoo, error) {
	for _, cfg := range ctx.Config.Gentoos {
		if cfg.ID == id {
			return cfg, nil
		}
	}
	return config.Gentoo{}, fmt.Errorf("gentoo artifact references unknown config ID %q", id)
}

func packageDir(cfg config.Gentoo) string {
	pkgName := cfg.Name
	if cfg.Type == "bin" && !strings.HasSuffix(pkgName, "-bin") {
		pkgName += "-bin"
	}
	dir := filepath.ToSlash(filepath.Join(cfg.Category, pkgName))
	if cfg.OverlayPath != "" {
		dir = filepath.ToSlash(filepath.Join(cfg.OverlayPath, dir))
	}
	return dir
}

func ebuildRelPath(cfg config.Gentoo, gentooVer string) string {
	pkgName := cfg.Name
	if cfg.Type == "bin" && !strings.HasSuffix(pkgName, "-bin") {
		pkgName += "-bin"
	}
	dir := packageDir(cfg)
	return filepath.ToSlash(filepath.Join(dir, fmt.Sprintf("%s-%s.ebuild", pkgName, gentooVer)))
}
