package gentoo

import (
	"cmp"
	"fmt"
	"path/filepath"
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

func (c *GentooConfig) Raw() config.Gentoo  { return c.raw }
func (c *GentooConfig) ID() string          { return c.raw.ID }
func (c *GentooConfig) Name() string        { return c.raw.Name }
func (c *GentooConfig) Category() string    { return c.raw.Category }
func (c *GentooConfig) Version() string     { return c.version }
func (c *GentooConfig) OverlayPath() string { return c.raw.OverlayPath }

func (c *GentooConfig) PackageName() string { return c.Name() + "-bin" }

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
	r := c.raw.Repository
	if r.PullRequest.Enabled {
		if r.PullRequest.Base.Owner != "" {
			r.Owner = r.PullRequest.Base.Owner
		}
		if r.PullRequest.Base.Name != "" {
			r.Name = r.PullRequest.Base.Name
		}
		if r.PullRequest.Base.Branch != "" {
			r.Branch = r.PullRequest.Base.Branch
		}
	}
	return strings.Join([]string{r.Git.URL, r.Owner, r.Name, r.Branch, c.OverlayPath(), c.Category(), c.PackageName()}, "\x00")
}

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
