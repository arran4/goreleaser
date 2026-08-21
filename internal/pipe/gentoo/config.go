package gentoo

import (
	"cmp"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/tmpl"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

type GentooConfig struct {
	raw config.Gentoo
}

func NewGentooConfig(ctx *context.Context, cfg config.Gentoo) (*GentooConfig, error) {
	c := &GentooConfig{raw: cfg}
	if err := c.applyDefaults(ctx); err != nil {
		return nil, err
	}
	if err := c.applyTemplates(ctx); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *GentooConfig) applyDefaults(ctx *context.Context) error {
	if c.raw.ID == "" {
		c.raw.ID = "default"
	}
	if !c.raw.Bin {
		return errors.New("bin must be true")
	}
	if c.raw.CommitMessageTemplate == "" {
		c.raw.CommitMessageTemplate = "{{ .ProjectName }}: bump to {{ .Tag }}"
	}
	if c.raw.Type == "" {
		c.raw.Type = "bin"
	}
	if c.raw.Type == "bin" && c.raw.Bindir == "" {
		c.raw.Bindir = "/opt/bin"
	} else if c.raw.Bindir == "" {
		c.raw.Bindir = "/usr/bin"
	}
	if strings.TrimSpace(c.raw.Description) == "" {
		c.raw.Description = ctx.Config.ProjectName
	}
	if c.raw.ConflictResolution == "" {
		c.raw.ConflictResolution = config.ConflictResolutionRevision
	}
	if c.raw.Name == "" {
		c.raw.Name = ctx.Config.ProjectName
	}
	if c.raw.Category == "" {
		c.raw.Category = "app-misc"
	}
	return nil
}

func (c *GentooConfig) applyTemplates(ctx *context.Context) error {
	gentooVer, err := GentooVersionFromRelease(ctx.Version, cmp.Or(c.raw.VersionRepresentation, "gentoo-version"))
	if err != nil {
		return err
	}

	tp := tmpl.New(ctx).WithExtraFields(tmpl.Fields{
		"GentooVersion": gentooVer,
		"Version":       gentooVer,
		"Name":          c.raw.Name,
		"Category":      c.raw.Category,
	})

	if err := tp.ApplyAll(
		&c.raw.Name,
		&c.raw.Category,
		&c.raw.OverlayPath,
		&c.raw.Description,
		&c.raw.Homepage,
		&c.raw.BugsTo,
		&c.raw.License,
		&c.raw.ExtraInstall,
	); err != nil {
		return err
	}

	c.raw.Repository, err = client.TemplateRef(tp.Apply, c.raw.Repository)
	if err != nil {
		return err
	}
	return nil
}

func (c *GentooConfig) validate() error {
	relPath := c.EbuildPath(GentooVersion{})
	if strings.HasPrefix(path.Clean(relPath), "../") || strings.Contains(path.Clean(relPath), "/../") {
		return fmt.Errorf("path %q must be a relative category/package/file.ebuild path", relPath)
	}
	return nil
}

func (c *GentooConfig) ID() string { return c.raw.ID }
func (c *GentooConfig) Name() string { return c.raw.Name }
func (c *GentooConfig) PackageName() string {
	pkgName := c.raw.Name
	if c.raw.Type == "bin" && !strings.HasSuffix(pkgName, "-bin") {
		pkgName += "-bin"
	}
	return pkgName
}
func (c *GentooConfig) Category() string { return c.raw.Category }
func (c *GentooConfig) PackageDir() string {
	dir := path.Join(c.Category(), c.PackageName())
	if c.raw.OverlayPath != "" {
		dir = path.Join(c.raw.OverlayPath, dir)
	}
	return dir
}
func (c *GentooConfig) OverlayPath() string { return c.raw.OverlayPath }
func (c *GentooConfig) EbuildPath(version GentooVersion) string {
	return path.Join(c.PackageDir(), fmt.Sprintf("%s-%s.ebuild", c.PackageName(), version.String()))
}
func (c *GentooConfig) MetadataPath() string { return path.Join(c.PackageDir(), "metadata.xml") }
func (c *GentooConfig) ManifestPath() string { return path.Join(c.PackageDir(), "Manifest") }
func (c *GentooConfig) MetaCacheDir() string {
	d := path.Join("metadata", "md5-cache", c.Category())
	if c.raw.OverlayPath != "" {
		d = path.Join(c.raw.OverlayPath, d)
	}
	return d
}
func (c *GentooConfig) ArchiveIDs() []string { return c.raw.IDs }
func (c *GentooConfig) Bindir() string { return c.raw.Bindir }
func (c *GentooConfig) Raw() config.Gentoo { return c.raw }
