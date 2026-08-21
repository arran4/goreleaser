#!/bin/bash
# Ah, I replaced ebuild.go but I used the OLD ebuild.go instead of the one I generated in my script `fix_ebuild.sh`!
# Let me regenerate `ebuild.go` correctly.
cat << 'EOT' > internal/pipe/gentoo/ebuild.go
package gentoo

import (
	"bytes"
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"text/template"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

//go:embed templates/ebuild.tmpl
var ebuildTemplate string

//go:embed templates/md5-cache.tmpl
var metaCacheTemplate string

func shellEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `$`, `\$`)
	s = strings.ReplaceAll(s, "`", "\\`")
	return s
}

type ExtraFiles struct {
	cfg     *GentooConfig
	release *Release
	files   []config.ExtraFile
}

func NewExtraFiles(cfg *GentooConfig, release *Release, files []config.ExtraFile) *ExtraFiles {
	return &ExtraFiles{
		cfg:     cfg,
		release: release,
		files:   files,
	}
}

func (f *ExtraFiles) Validate() error {
	for _, ef := range f.files {
		name := ef.NameTemplate
		if name == "" {
			name = path.Base(ef.Glob)
		}
		src := ef.Glob
		if err := f.validateFile(name, src); err != nil {
			return err
		}
	}
	return nil
}

func (f *ExtraFiles) validateFile(name, src string) error {
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("extra file name %q cannot contain directory separators", name)
	}
	// We skip filesystem checks here, assume they are handled by the caller creating the map.
	return nil
}

func (f *ExtraFiles) RemoveArchived() {
	var keep []config.ExtraFile
	for _, ef := range f.files {
		name := ef.NameTemplate
		if name == "" {
			name = path.Base(ef.Glob)
		}
		if !f.inEveryArchive(name) {
			keep = append(keep, ef)
		}
	}
	f.files = keep
}

func (f *ExtraFiles) inEveryArchive(name string) bool {
	if len(f.release.Archives()) == 0 {
		return false
	}
	for _, a := range f.release.Archives() {
		if !a.Contains(name) {
			return false
		}
	}
	return true
}

func (f *ExtraFiles) Files() []config.ExtraFile {
	return f.files
}

func (f *ExtraFiles) Write(ctx *context.Context, pkgDir string) ([]GeneratedFile, error) {
	var result []GeneratedFile
	for _, ef := range f.files {
		name := ef.NameTemplate
		if name == "" {
			name = path.Base(ef.Glob)
		}
		src := ef.Glob
		content, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		result = append(result, GeneratedFile{
			Path:    path.Join(pkgDir, "files", name),
			Content: content,
			Kind:    GeneratedAux,
		})
	}
	return result, nil
}

type archItem struct {
	File string
	URI  string
}

type EbuildSource struct {
	Keyword string
	URIs    []archItem
}

type installGroup struct {
	Keywords []string
	Installs []installData
}

type installData struct {
	Source   string
	Target   string
	Keywords []string
}

type installItemData struct {
	Source           string
	Target           string
	Dir              string
	Base             string
	Use              []string
	Keywords         []string
	InstallerCmd     string
	InstallRenameCmd string
	DirSwitchCmd     string
}

type ebuildTemplateData struct {
	Name          string
	Description   string
	Homepage      string
	License       string
	Keywords      string
	Bindir        string
	ExtraInstall  string
	Archs         []EbuildSource
	InstallGroups []installGroup
	UseFlags      []config.GentooUseFlag
	Dodir         []string
	Dodoc         []string
	Doman         []string
	Systemd       []installItemData
	Eclasses      []string
	Installers    []installItemData
}

type Ebuild struct {
	cfg      *GentooConfig
	release  *Release
	installs *InstallPlan
	extras   *ExtraFiles

	useFlags []config.GentooUseFlag
	eclasses []string
}

func NewEbuild(cfg *GentooConfig, release *Release, installs *InstallPlan, extras *ExtraFiles) (*Ebuild, error) {
	e := &Ebuild{
		cfg:      cfg,
		release:  release,
		installs: installs,
		extras:   extras,
		useFlags: cfg.Raw().UseFlags,
		eclasses: cfg.Raw().Eclasses,
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Ebuild) Validate() error {
	// The config validates itself. We can check eclasses or systemd logic here.
	return nil
}

func (e *Ebuild) Keywords() []string {
	return e.release.Keywords()
}

func (e *Ebuild) UseFlags() []config.GentooUseFlag {
	return e.useFlags
}

func (e *Ebuild) Eclasses() []string {
	return e.eclasses
}

func (e *Ebuild) HasEclasses() bool {
	return len(e.eclasses) > 0
}

func (e *Ebuild) templateData() ebuildTemplateData {
	raw := e.cfg.Raw()
	data := ebuildTemplateData{
		Name:         e.cfg.PackageName(),
		Description:  raw.Description,
		Homepage:     raw.Homepage,
		License:      raw.License,
		Keywords:     strings.Join(e.Keywords(), " "),
		Bindir:       e.cfg.Bindir(),
		ExtraInstall: raw.ExtraInstall,
		UseFlags:     e.useFlags,
		Dodir:        raw.Dodir,
		Dodoc:        raw.Dodoc,
		Doman:        raw.Doman,
		Eclasses:     e.eclasses,
	}

	for _, k := range e.release.Architectures() {
		var uris []archItem
		for _, dist := range e.release.Distfiles() {
			if dist.GentooArch == k {
				uris = append(uris, archItem{URI: dist.URI, File: dist.File})
			}
		}
		data.Archs = append(data.Archs, EbuildSource{
			Keyword: k,
			URIs:    uris,
		})
	}

	var installGroups []installGroup
	var autoInstalls []installData

	// Map installs
	for _, inst := range e.installs.installs {
		if inst.command == "doexe" && len(inst.use) == 0 && inst.directory == e.cfg.Bindir() {
			autoInstalls = append(autoInstalls, installData{
				Source:   inst.source,
				Target:   inst.target,
				Keywords: inst.archs,
			})
		} else {
			data.Installers = append(data.Installers, installItemData{
				Source:           inst.source,
				Target:           inst.target,
				Dir:              inst.directory,
				Base:             inst.target,
				Use:              inst.use,
				Keywords:         inst.archs,
				InstallerCmd:     inst.command,
				InstallRenameCmd: inst.renameCommand,
				DirSwitchCmd:     inst.intoCommand,
			})
		}
	}

	if len(autoInstalls) > 0 {
		var group installGroup
		// We could group by arch, but for simplicity, we mimic the old behavior
		// which didn't use Keywords if all architectures install the same binaries.
		allKeywords := e.Keywords()
		for _, inst := range autoInstalls {
			if len(inst.Keywords) == len(allKeywords) {
				inst.Keywords = nil
			}
			group.Installs = append(group.Installs, inst)
		}
		installGroups = append(installGroups, group)
	}
	data.InstallGroups = installGroups

	return data
}

func (e *Ebuild) Render() ([]byte, error) {
	data := e.templateData()
	var buf bytes.Buffer
	if err := template.Must(template.New("ebuild").Funcs(template.FuncMap{
		"escape": shellEscape,
	}).Parse(ebuildTemplate)).Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type MetaCache struct {
	ebuild *Ebuild
}

func (m *MetaCache) Render(ebuildContent []byte) ([]byte, error) {
	if m.ebuild.HasEclasses() {
		return nil, errors.New("cannot render metadata cache for ebuild with inherited eclasses")
	}

	h := md5.Sum(ebuildContent)
	md5Hex := hex.EncodeToString(h[:])

	var useFlags []string
	for _, flag := range m.ebuild.UseFlags() {
		if flag.Flag != "" {
			useFlags = append(useFlags, flag.Flag)
		}
	}
	slices.Sort(useFlags)
	useFlags = slices.Compact(useFlags)

	var srcURIs []string
	for _, dist := range m.ebuild.release.Distfiles() {
		srcURIs = append(srcURIs, fmt.Sprintf("%s? ( %s -> %s )", dist.GentooArch, dist.URI, dist.File))
	}

	tmplData := struct {
		Description string
		Homepage    string
		IUSE        string
		Keywords    string
		License     string
		SrcURI      string
		MD5         string
	}{
		Description: m.ebuild.cfg.Raw().Description,
		Homepage:    m.ebuild.cfg.Raw().Homepage,
		IUSE:        strings.Join(useFlags, " "),
		Keywords:    strings.Join(m.ebuild.Keywords(), " "),
		License:     m.ebuild.cfg.Raw().License,
		SrcURI:      strings.Join(srcURIs, " "),
		MD5:         md5Hex,
	}

	var buf bytes.Buffer
	if err := template.Must(template.New("md5-cache").Parse(metaCacheTemplate)).Execute(&buf, tmplData); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
EOT
