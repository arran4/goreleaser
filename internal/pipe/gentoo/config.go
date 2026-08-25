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

// publicationConfig is the immutable publication view of a resolved Gentoo
// configuration. It keeps provider credentials at the publication boundary
// without exposing the complete config to domain planners.
type publicationConfig struct {
	repository    config.RepoRef
	commitAuthor  config.CommitAuthor
	commitMessage string
	skipUpload    string
}

type retentionPolicy struct {
	conflictResolution config.ConflictResolution
	strategy           config.VersionRetentionStrategy
	keepVersions       int
}

type metadataConfig struct {
	maintainers     []config.GentooMaintainer
	useFlags        []config.GentooUseFlag
	longDescription string
	upstream        config.GentooUpstream
}

func (c metadataConfig) Empty() bool {
	return len(c.maintainers) == 0 && len(c.useFlags) == 0 && c.longDescription == "" &&
		c.upstream.BugsTo == "" && c.upstream.Doc == "" && len(c.upstream.RemoteIDs) == 0
}

type manifestConfig struct {
	hashes []string
	thin   *bool
}

// GentooConfigs owns invariants that span configured publication targets.
type GentooConfigs struct {
	entries []*GentooConfig
}

func NewGentooConfigs(ctx *context.Context, raw []config.Gentoo) (*GentooConfigs, error) {
	result := &GentooConfigs{}
	configuredIDs := map[string]struct{}{}
	destinations := map[string]string{}
	for _, entry := range raw {
		resolved, err := NewGentooConfig(ctx, entry)
		if err != nil {
			return nil, err
		}
		if _, exists := configuredIDs[resolved.ID()]; exists {
			return nil, fmt.Errorf("gentoo config ID %q is duplicated", resolved.ID())
		}
		configuredIDs[resolved.ID()] = struct{}{}
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
	if err := tp.ApplyAll(&raw.LongDescription, &raw.Upstream.BugsTo, &raw.Upstream.Doc); err != nil {
		return nil, err
	}
	for i := range raw.Upstream.RemoteIDs {
		if err := tp.ApplyAll(&raw.Upstream.RemoteIDs[i].ID, &raw.Upstream.RemoteIDs[i].Type); err != nil {
			return nil, err
		}
		if strings.TrimSpace(raw.Upstream.RemoteIDs[i].Type) == "" {
			return nil, fmt.Errorf("remote-id type is required for id %q", raw.Upstream.RemoteIDs[i].ID)
		}
		if strings.TrimSpace(raw.Upstream.RemoteIDs[i].ID) == "" {
			return nil, fmt.Errorf("remote-id id is required for type %q", raw.Upstream.RemoteIDs[i].Type)
		}
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
	return c.MetaCachePathForVersion(c.Version())
}

func (c *GentooConfig) MetaCacheDir() string {
	return filepath.ToSlash(filepath.Join(c.OverlayPath(), "metadata", "md5-cache", c.Category()))
}

func (c *GentooConfig) MetaCachePathForVersion(version string) string {
	return filepath.ToSlash(filepath.Join(c.MetaCacheDir(), c.PackageName()+"-"+version))
}

func (c *GentooConfig) DestinationKey() string {
	r := c.TargetRepository()
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

func (c *GentooConfig) installSections() []installSection {
	return []installSection{
		{name: "dobin", items: slices.Clone(c.raw.Dobin)},
		{name: "doconfd", items: slices.Clone(c.raw.Doconfd)},
		{name: "doenvd", items: slices.Clone(c.raw.Doenvd)},
		{name: "doexe", items: slices.Clone(c.raw.Doexe), defaultDir: c.Bindir()},
		{name: "doheader", items: slices.Clone(c.raw.Doheader)},
		{name: "doinitd", items: slices.Clone(c.raw.Doinitd)},
		{name: "doins", items: slices.Clone(c.raw.Doins), defaultDir: "/"},
		{name: "dosbin", items: slices.Clone(c.raw.Dosbin)},
		{name: "dosym", items: slices.Clone(c.raw.Dosym)},
		{name: "systemd", items: slices.Clone(c.raw.Systemd)},
	}
}

func (c *GentooConfig) Directories() []string { return slices.Clone(c.raw.Dodir) }
func (c *GentooConfig) Manpages() []string    { return slices.Clone(c.raw.Doman) }
func (c *GentooConfig) Docs() []string        { return slices.Clone(c.raw.Dodoc) }
func (c *GentooConfig) UseFlags() []config.GentooUseFlag {
	return gentooUseFlags(c.raw)
}

func gentooUseFlags(cfg config.Gentoo) []config.GentooUseFlag {
	var flags []config.GentooUseFlag
	configured := map[string]struct{}{}
	for _, flag := range cfg.UseFlags {
		name := strings.TrimLeft(flag.Flag, "+-")
		if _, ok := configured[name]; ok {
			continue
		}
		configured[name] = struct{}{}
		flags = append(flags, flag)
	}
	groups := [][]config.GentooInstallItem{
		cfg.Dobin, cfg.Doconfd, cfg.Doenvd, cfg.Doexe, cfg.Doheader, cfg.Doinitd,
		cfg.Doins, cfg.Dosbin, cfg.Dosym, cfg.Systemd,
	}
	var additional []string
	for _, group := range groups {
		for _, item := range group {
			for _, condition := range item.Use {
				flag := strings.TrimLeft(condition, "!+-")
				if flag == "" {
					continue
				}
				if _, ok := configured[flag]; ok {
					continue
				}
				configured[flag] = struct{}{}
				additional = append(additional, flag)
			}
		}
	}
	slices.Sort(additional)
	for _, flag := range additional {
		flags = append(flags, config.GentooUseFlag{Flag: flag})
	}
	return flags
}

func (c *GentooConfig) publication() publicationConfig {
	return publicationConfigFrom(c.raw)
}

func publicationConfigFrom(raw config.Gentoo) publicationConfig {
	return publicationConfig{
		repository:    raw.Repository,
		commitAuthor:  raw.CommitAuthor,
		commitMessage: raw.CommitMessageTemplate,
		skipUpload:    raw.SkipUpload,
	}
}

func (c *GentooConfig) retention() retentionPolicy {
	return retentionPolicy{
		conflictResolution: c.raw.ConflictResolution,
		strategy:           c.raw.VersionRetentionStrategy,
		keepVersions:       c.raw.KeepVersions,
	}
}

func (c *GentooConfig) metadata() metadataConfig {
	bugsTo := c.raw.BugsTo
	if bugsTo == "" {
		bugsTo = c.raw.Upstream.BugsTo
	}
	upstream := config.GentooUpstream{BugsTo: bugsTo, Doc: c.raw.Upstream.Doc, RemoteIDs: slices.Clone(c.raw.Upstream.RemoteIDs)}
	return metadataConfig{
		maintainers:     slices.Clone(c.raw.Maintainers),
		useFlags:        slices.Clone(c.raw.UseFlags),
		longDescription: c.raw.LongDescription,
		upstream:        upstream,
	}
}

func (c *GentooConfig) manifest() manifestConfig {
	return manifestConfig{hashes: slices.Clone(c.raw.ManifestHashes), thin: c.raw.ThinManifests}
}

func (c *GentooConfig) validatePackage() error {
	fields := []struct {
		label string
		value string
	}{
		{label: "overlay_path", value: c.OverlayPath()},
		{label: "category", value: c.Category()},
		{label: "name", value: c.Name()},
	}
	for _, field := range fields {
		label, value := field.label, field.value
		if value == "" && label == "overlay_path" {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(value))
		if filepath.IsAbs(value) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
			return fmt.Errorf("%s %q must remain within the overlay", label, value)
		}
		if label == "name" && strings.Contains(clean, "/") {
			return fmt.Errorf("name %q must be a package name, not a path", value)
		}
	}
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
