package gentoo

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

type gentooInnerNode struct {
	XMLName xml.Name
	Content string            `xml:",chardata"`
	Attrs   []xml.Attr        `xml:",any,attr"`
	Nodes   []gentooInnerNode `xml:",any"`
}

type gentooMaintainer struct {
	Type  string `xml:"type,attr,omitempty"`
	Email string `xml:"email"`
	Name  string `xml:"name,omitempty"`
}

type gentooUpstream struct {
	BugsTo string            `xml:"bugs-to,omitempty"`
	Doc    string            `xml:"doc,omitempty"`
	Attrs  []xml.Attr        `xml:",any,attr"`
	Nodes  []gentooInnerNode `xml:",any"`
}

type gentooUseFlag struct {
	XMLName xml.Name   `xml:"flag"`
	Name    string     `xml:"name,attr"`
	Value   string     `xml:",chardata"`
	Attrs   []xml.Attr `xml:",any,attr"`
}

type gentooUse struct {
	XMLName xml.Name          `xml:"use"`
	Flags   []gentooUseFlag   `xml:"flag"`
	Attrs   []xml.Attr        `xml:",any,attr"`
	Nodes   []gentooInnerNode `xml:",any"`
}

// Metadata preserves unknown XML nodes, attributes, comments, and ordering
// while applying the Gentoo fields managed by this publisher.
type Metadata struct {
	XMLName     xml.Name           `xml:"pkgmetadata"`
	Attrs       []xml.Attr         `xml:",any,attr"`
	Maintainers []gentooMaintainer `xml:"maintainer"`
	Use         *gentooUse         `xml:"use,omitempty"`
	Upstream    *gentooUpstream    `xml:"upstream,omitempty"`
	InnerNodes  []gentooInnerNode  `xml:",any"`
}

func (m *Metadata) AddMaintainers(maintainers []config.GentooMaintainer) error {
	for _, main := range maintainers {
		if main.Email == "" {
			return errors.New("maintainer email is required")
		}
		exists := false
		for _, em := range m.Maintainers {
			if em.Email == main.Email {
				exists = true
				break
			}
		}
		if !exists {
			m.Maintainers = append(m.Maintainers, gentooMaintainer{
				Type:  "person",
				Email: main.Email,
				Name:  main.Name,
			})
		}
	}
	return nil
}

func (m *Metadata) AddUseFlags(flags []config.GentooUseFlag) {
	if len(flags) == 0 {
		return
	}
	if m.Use == nil {
		m.Use = &gentooUse{}
	}
	configuredFlags := make(map[string]string)
	for _, flag := range flags {
		if flag.Description != "" {
			configuredFlags[strings.TrimLeft(flag.Flag, "+-")] = flag.Description
		}
	}

	var configuredFlagNames []string
	for k := range configuredFlags {
		configuredFlagNames = append(configuredFlagNames, k)
	}
	slices.Sort(configuredFlagNames)

	for _, k := range configuredFlagNames {
		v := configuredFlags[k]
		exists := false
		for i, ef := range m.Use.Flags {
			if ef.Name == k {
				m.Use.Flags[i].Value = v
				exists = true
				break
			}
		}
		if !exists {
			m.Use.Flags = append(m.Use.Flags, gentooUseFlag{
				Name:  k,
				Value: v,
			})
		}
	}
}

func (m *Metadata) SetUpstream(bugsTo string) {
	if bugsTo == "" {
		return
	}
	if m.Upstream == nil {
		m.Upstream = &gentooUpstream{}
	}
	m.Upstream.BugsTo = bugsTo
}

func (m *Metadata) Marshal() ([]byte, error) {
	content, err := xml.MarshalIndent(m, "", "\t")
	if err != nil {
		return nil, err
	}
	header := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE pkgmetadata SYSTEM \"https://www.gentoo.org/dtd/metadata.dtd\">\n")
	return append(header, append(content, '\n')...), nil
}

func ParseMetadata(content []byte) (*Metadata, error) {
	metadata := &Metadata{}
	if err := xml.Unmarshal(content, metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

func (m *Metadata) Render() ([]byte, error) { return m.Marshal() }

// Layout is the effective overlay layout.conf policy after applying explicit
// publisher overrides.
type Layout struct {
	hashes                    []string
	thin                      bool
	cacheFormats              []string
	hasCacheFormatsConfigured bool
}

func (l Layout) ManifestHashes() []string { return slices.Clone(l.hashes) }
func (l Layout) ThinManifests() bool      { return l.thin }
func (l Layout) SupportsMetaCache() bool {
	return !l.hasCacheFormatsConfigured || slices.Contains(l.cacheFormats, "md5-dict") || slices.Contains(l.cacheFormats, "md5-cache")
}

func loadOverlaySettings(ctx *context.Context, cfg config.Gentoo, repoClient any, repo client.Repo) (Layout, error) {
	settings := Layout{
		hashes: []string{"BLAKE2B", "SHA512"},
		thin:   false,
	}
	if len(cfg.ManifestHashes) > 0 {
		settings.hashes = cfg.ManifestHashes
	}
	if cfg.ThinManifests != nil {
		settings.thin = *cfg.ThinManifests
	}

	dl, ok := repoClient.(client.FileDownloader)
	if !ok {
		return settings, nil
	}
	content, err := dl.DownloadFile(ctx, repo, path.Join(cfg.OverlayPath, "metadata/layout.conf"))
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrNotImplemented) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("failed to download layout.conf: %w", err)
	}
	for lineB := range bytes.SplitSeq(content, []byte{'\n'}) {
		key, value, ok := strings.Cut(strings.TrimSpace(string(lineB)), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "manifest-hashes":
			if len(cfg.ManifestHashes) == 0 {
				settings.hashes = strings.Fields(value)
			}
		case "thin-manifests":
			if cfg.ThinManifests == nil {
				settings.thin = strings.TrimSpace(value) == "true"
			}
		case "cache-formats":
			settings.hasCacheFormatsConfigured = true
			settings.cacheFormats = strings.Fields(value)
		}
	}
	return settings, nil
}

func prepareManifestAndMetadata(ctx *context.Context, cfg config.Gentoo, repoClient any, repo client.Repo, changes *ChangeSet, deletedEbuilds []string) error {
	dir := packageDir(cfg)

	metadataPath := path.Join(dir, "metadata.xml")
	manifestPath := path.Join(dir, "Manifest")

	if len(cfg.Maintainers) > 0 || cfg.BugsTo != "" || len(cfg.UseFlags) > 0 {
		meta := Metadata{}
		if dl, ok := repoClient.(client.FileDownloader); ok {
			content, err := dl.DownloadFile(ctx, repo, metadataPath)
			if err == nil {
				if err := xml.Unmarshal(content, &meta); err != nil {
					return fmt.Errorf("failed to parse metadata.xml: %w", err)
				}
			} else if !errors.Is(err, client.ErrNotFound) && !errors.Is(err, client.ErrNotImplemented) {
				return fmt.Errorf("failed to download metadata.xml: %w", err)
			}
		}

		meta.AddUseFlags(cfg.UseFlags)
		if err := meta.AddMaintainers(cfg.Maintainers); err != nil {
			return err
		}
		meta.SetUpstream(cfg.BugsTo)

		content, err := meta.Marshal()
		if err != nil {
			return err
		}
		changes.files = append(changes.files, client.RepoFile{
			Content: content,
			Path:    metadataPath,
		})
	}

	settings, err := loadOverlaySettings(ctx, cfg, repoClient, repo)
	if err != nil {
		return err
	}
	manifestHashes, thinManifests := settings.hashes, settings.thin
	manifestLines, err := loadManifestLines(ctx, repoClient, repo, manifestPath)
	if err != nil {
		return err
	}

	prefix := filepath.Base(dir) + "-"

	retainedBaseVersions := make(map[string]bool)
	for _, f := range changes.files {
		if f.Delete || !strings.HasSuffix(f.Path, ".ebuild") || !isInsidePackageDir(f.Path, dir) {
			continue
		}

		filename := filepath.Base(f.Path)
		if v, ok := strings.CutPrefix(filename, prefix); ok {
			v = strings.TrimSuffix(v, ".ebuild")
			if idx := strings.LastIndex(v, "-r"); idx != -1 {
				if _, err := strconv.Atoi(v[idx+2:]); err == nil {
					v = v[:idx]
				}
			}
			retainedBaseVersions[v] = true
		}
	}

	var deletedVersions []string
	var deletedBaseVersions []string
	for _, e := range deletedEbuilds {
		v, _ := strings.CutPrefix(e, prefix)
		v = strings.TrimSuffix(v, ".ebuild")
		deletedVersions = append(deletedVersions, v)

		baseV := v
		if idx := strings.LastIndex(v, "-r"); idx != -1 {
			if _, err := strconv.Atoi(v[idx+2:]); err == nil {
				baseV = v[:idx]
			}
		}
		if !retainedBaseVersions[baseV] {
			deletedBaseVersions = append(deletedBaseVersions, baseV)
		}
	}

	newManifestFiles := map[string]struct{}{}
	if !thinManifests {
		for _, f := range changes.files {
			if !f.Delete && isInsidePackageDir(f.Path, dir) {
				recordType, filename := manifestFileInfo(f.Path, dir)
				newManifestFiles[recordType+":"+filename] = struct{}{}
			}
		}
	}
	filters := []artifact.Filter{
		artifact.ByGoos("linux"),
		artifact.ByType(artifact.UploadableArchive),
		artifact.OnlyReplacingUnibins,
	}
	if len(cfg.IDs) > 0 {
		filters = append(filters, artifact.ByIDs(cfg.IDs...))
	}
	arches := ctx.Artifacts.Filter(artifact.And(filters...)).List()
	var resolved *GentooConfig
	if ctx.Version != "" {
		version, versionErr := convertToGentooVersion(ctx.Version, "gentoo-version")
		if versionErr != nil {
			return versionErr
		}
		resolved = &GentooConfig{raw: cfg, version: version}
	}
	currentDists := make(map[string]struct{}, len(arches))
	for _, art := range arches {
		name := art.Name
		if resolved != nil {
			name = archiveDistfile(resolved, ctx.Version, art.Name)
		}
		currentDists[name] = struct{}{}
	}

	var newManifestLines []string
	for _, line := range manifestLines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			newManifestLines = append(newManifestLines, line)
			continue
		}

		recordType := fields[0]
		filename := fields[1]

		switch recordType {
		case "DIST":
			_, removed := currentDists[filename]
			for _, dv := range deletedBaseVersions {
				if idx := strings.Index(filename, dv); idx != -1 {
					isMatch := true
					if idx > 0 && filename[idx-1] != '_' && filename[idx-1] != '-' {
						isMatch = false
					}
					endIdx := idx + len(dv)
					if endIdx < len(filename) {
						next := filename[endIdx]
						if next == '.' {
							if endIdx+1 < len(filename) && filename[endIdx+1] >= '0' && filename[endIdx+1] <= '9' {
								isMatch = false
							}
						} else if next != '_' && next != '-' {
							isMatch = false
						}
					}
					if isMatch {
						removed = true
						break
					}
				}
			}
			if !removed {
				newManifestLines = append(newManifestLines, line)
			}
		case "EBUILD", "AUX", "MISC":
			if thinManifests {
				continue
			}
			removed := false
			for _, dv := range deletedVersions {
				if recordType == "EBUILD" && filename == filepath.Base(dir)+"-"+dv+".ebuild" {
					removed = true
					break
				}
			}
			if !removed {
				_, removed = newManifestFiles[recordType+":"+filename]
			}
			if !removed {
				newManifestLines = append(newManifestLines, line)
			}
		default:
			newManifestLines = append(newManifestLines, line)
		}
	}

	for _, art := range arches {
		distfile := art.Name
		if resolved != nil {
			distfile = archiveDistfile(resolved, ctx.Version, art.Name)
		}
		line, err := NewManifestHasher(manifestHashes).Line("DIST", distfile, art.Path, nil)
		if err != nil {
			return err
		}
		newManifestLines = append(newManifestLines, line)
	}

	if !thinManifests {
		for _, f := range changes.files {
			if f.Delete || !isInsidePackageDir(f.Path, dir) {
				continue
			}

			recordType, filename := manifestFileInfo(f.Path, dir)

			line, err := NewManifestHasher(manifestHashes).Line(recordType, filename, f.Path, f.Content)
			if err != nil {
				return err
			}
			newManifestLines = append(newManifestLines, line)
		}
	}

	if len(newManifestLines) > 0 {
		slices.Sort(newManifestLines)
		newManifestLines = slices.Compact(newManifestLines)
		changes.files = append(changes.files, client.RepoFile{
			Content: []byte(strings.Join(newManifestLines, "\n") + "\n"),
			Path:    manifestPath,
		})
	}
	return nil
}

func loadManifestLines(ctx *context.Context, repoClient any, repo client.Repo, manifestPath string) ([]string, error) {
	if dl, ok := repoClient.(client.FileDownloader); ok {
		content, err := dl.DownloadFile(ctx, repo, manifestPath)
		if err == nil {
			return strings.FieldsFunc(string(content), func(r rune) bool { return r == '\n' || r == '\r' }), nil
		}
		if !errors.Is(err, client.ErrNotFound) && !errors.Is(err, client.ErrNotImplemented) {
			return nil, fmt.Errorf("failed to download Manifest: %w", err)
		}
	}
	return nil, nil
}

func manifestFileInfo(filePath, packageDir string) (string, string) {
	pathStr := filepath.ToSlash(filePath)
	filesDir := path.Join(packageDir, "files")
	if pathStr == filesDir || strings.HasPrefix(pathStr, filesDir+"/") {
		return "AUX", strings.TrimPrefix(pathStr, filesDir+"/")
	}
	if strings.HasSuffix(pathStr, ".ebuild") {
		return "EBUILD", path.Base(pathStr)
	}
	return "MISC", path.Base(pathStr)
}

func isInsidePackageDir(filePath, packageDir string) bool {
	p := filepath.ToSlash(filePath)
	d := filepath.ToSlash(packageDir)
	return p == d || strings.HasPrefix(p, d+"/")
}
