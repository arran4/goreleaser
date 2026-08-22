package gentoo

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
)

type BinaryKey struct {
	ArchiveID string
	Arch      string
	Binary    string
}

// BinaryClaims records binaries handled by explicit install rules. Keys carry
// architecture so an amd64 override cannot suppress arm64 automatic installs.
type BinaryClaims struct {
	claimed map[BinaryKey]struct{}
}

func NewBinaryClaims() *BinaryClaims { return &BinaryClaims{claimed: map[BinaryKey]struct{}{}} }

func (c *BinaryClaims) Claim(key BinaryKey) { c.claimed[key] = struct{}{} }

func (c *BinaryClaims) ClaimArchive(archive *Archive) {
	for _, binary := range archive.Binaries() {
		c.Claim(BinaryKey{ArchiveID: archive.ID(), Arch: archive.GentooArch(), Binary: binary})
	}
}

func (c *BinaryClaims) IsClaimed(key BinaryKey) bool {
	_, ok := c.claimed[key]
	return ok
}

type installSection struct {
	name       string
	items      []config.GentooInstallItem
	defaultDir string
}

type installItemData struct {
	Source      string
	Target      string
	Dir         string
	Base        string
	Use         []string
	Keywords    []string
	Section     string
	StateFamily StateFamily
}

func (d installItemData) Validate() error {
	if d.Section == "" {
		return errors.New("section is required")
	}
	if d.Source == "" {
		return errors.New("source is required")
	}
	isRename := d.Source != d.Base && d.Section != "dosym"
	descriptor := resolveInstallOp(d.Section, isRename).Descriptor()
	if descriptor.ArgMode == ArgModeRename && d.Base == "" {
		return fmt.Errorf("%s requires a destination base name", resolveInstallOp(d.Section, isRename))
	}
	return nil
}

// InstallPlanner lowers resolved config and normalized archives into the
// existing install AST. The reducer remains responsible only for semantic
// simplification and install-state elimination.
type InstallPlanner struct {
	cfg          *GentooConfig
	release      *Release
	extras       *ExtraFiles
	claims       *BinaryClaims
	extraInstall string
	installers   []installItemData
}

func NewInstallPlanner(cfg *GentooConfig, release *Release, extras *ExtraFiles, extraInstall string) *InstallPlanner {
	return &InstallPlanner{cfg: cfg, release: release, extras: extras, claims: NewBinaryClaims(), extraInstall: extraInstall}
}

func (p *InstallPlanner) Plan() (*InstallProgram, error) {
	if err := p.resolveExplicit(); err != nil {
		return nil, err
	}
	builder := installProgramBuilder{
		bindir:        p.cfg.Bindir(),
		architectures: p.release.Architectures(),
		extraInstall:  p.extraInstall,
		automatic:     p.automaticBinaries(),
		explicit:      p.installers,
		directories:   p.cfg.Directories(),
		manpages:      p.extras.ResolveAll(p.cfg.Manpages()),
		docs:          p.extras.ResolveAll(p.cfg.Docs()),
	}
	reduced := builder.Build().Reduce()
	if err := reduced.Validate(); err != nil {
		return nil, err
	}
	return reduced, nil
}

func (p *InstallPlanner) resolveExplicit() error {
	for _, section := range p.cfg.installSections() {
		for _, item := range section.items {
			resolved, err := p.resolveItem(section, item)
			if err != nil {
				return err
			}
			p.installers = append(p.installers, resolved...)
		}
	}
	return nil
}

func (p *InstallPlanner) resolveItem(section installSection, item config.GentooInstallItem) ([]installItemData, error) {
	if item.Src == "" && item.SrcID == "" {
		return nil, fmt.Errorf("gentoo %s: either src or src_id is required", section.name)
	}
	archs, err := gentooArchitectures(item.Archs)
	if err != nil {
		return nil, fmt.Errorf("gentoo %s: %w", section.name, err)
	}
	if item.SrcID == "" {
		return p.lowerLiteral(section, item, archs)
	}
	return p.lowerArchiveItem(section, item, archs)
}

func (p *InstallPlanner) lowerLiteral(section installSection, item config.GentooInstallItem, archs []string) ([]installItemData, error) {
	return p.lowerSources(section, item, archs, []string{p.extras.EbuildSource(item.Src)})
}

func (p *InstallPlanner) lowerArchiveItem(section installSection, item config.GentooInstallItem, archs []string) ([]installItemData, error) {
	archives := p.release.ArchivesByID(item.SrcID)
	archives = selectArchitectures(archives, archs)
	if len(archives) == 0 {
		if len(archs) > 0 {
			return nil, fmt.Errorf("gentoo %s: src_id %q does not match a selected archive for archs %v", section.name, item.SrcID, item.Archs)
		}
		return nil, fmt.Errorf("gentoo %s: src_id %q does not match a selected archive", section.name, item.SrcID)
	}
	for _, arch := range archs {
		if p.release.Archive(item.SrcID, arch) == nil {
			return nil, fmt.Errorf("gentoo %s: src_id %q does not match a selected archive for archs %v", section.name, item.SrcID, item.Archs)
		}
	}
	if err := validateArchiveLayouts(section.name, item, archives); err != nil {
		return nil, err
	}

	if item.Src != "" {
		for _, archive := range archives {
			p.claims.ClaimArchive(archive)
		}
		return p.lowerSources(section, item, archs, []string{archives[0].Path(item.Src)})
	}

	binaries := archives[0].Binaries()
	if len(binaries) > 1 && item.Dst != "" {
		return nil, fmt.Errorf("gentoo %s: dst %q cannot be used with multiple binaries %v in src_id %q; specify explicit src for each binary", section.name, item.Dst, binaries, item.SrcID)
	}
	for _, archive := range archives {
		for _, binary := range archive.Binaries() {
			p.claims.Claim(BinaryKey{ArchiveID: archive.ID(), Arch: archive.GentooArch(), Binary: binary})
		}
	}
	sources := make([]string, 0, len(binaries))
	for _, binary := range binaries {
		sources = append(sources, archives[0].Path(binary))
	}
	return p.lowerSources(section, item, archs, sources)
}

func (p *InstallPlanner) lowerSources(section installSection, item config.GentooInstallItem, archs, sources []string) ([]installItemData, error) {
	result := make([]installItemData, 0, len(sources))
	for _, source := range sources {
		name, family, dir, base, err := p.decomposeDestination(section.name, source, item.Dst, section.defaultDir)
		if err != nil {
			return nil, err
		}
		resolved := installItemData{Source: source, Target: item.Dst, Dir: dir, Base: base, Use: item.Use, Keywords: archs, Section: name, StateFamily: family}
		if err := resolved.Validate(); err != nil {
			return nil, err
		}
		result = append(result, resolved)
	}
	return result, nil
}

func (p *InstallPlanner) decomposeDestination(section, source, destination, defaultDir string) (string, StateFamily, string, string, error) {
	if section != "systemd" {
		family, dir, base, err := resolveSectionOp(section).Descriptor().DecomposeDestination(source, destination, defaultDir)
		return section, family, dir, base, err
	}
	hasEclass := slices.Contains(p.cfg.Eclasses(), "systemd")
	if destination == "" && hasEclass {
		return "systemd", StateFamilyNone, "", path.Base(filepath.ToSlash(source)), nil
	}
	if destination == "" {
		return "doins", StateFamilyIns, "/usr/lib/systemd/system", path.Base(filepath.ToSlash(source)), nil
	}
	cleaned := path.Clean(filepath.ToSlash(destination))
	dir, base := path.Dir(cleaned), path.Base(cleaned)
	if hasEclass && (dir == "." || dir == "" || dir == "/usr/lib/systemd/system" || dir == "usr/lib/systemd/system") {
		return "systemd", StateFamilyNone, "", base, nil
	}
	if dir == "." || dir == "" {
		dir = "/usr/lib/systemd/system"
	}
	return "doins", StateFamilyIns, dir, base, nil
}

func (p *InstallPlanner) automaticBinaries() map[string][]installData {
	result := map[string][]installData{}
	for _, archive := range p.release.Archives() {
		for _, binary := range archive.Binaries() {
			key := BinaryKey{ArchiveID: archive.ID(), Arch: archive.GentooArch(), Binary: binary}
			if p.claims.IsClaimed(key) {
				continue
			}
			result[archive.GentooArch()] = append(result[archive.GentooArch()], installData{
				Source: archive.Path(binary), Target: filepath.Base(binary), Keywords: []string{archive.GentooArch()},
			})
		}
	}
	for arch := range result {
		slices.SortFunc(result[arch], func(a, b installData) int {
			if c := strings.Compare(a.Source, b.Source); c != 0 {
				return c
			}
			return strings.Compare(a.Target, b.Target)
		})
	}
	return result
}

func gentooArchitectures(goarchs []string) ([]string, error) {
	result := make([]string, 0, len(goarchs))
	for _, goarch := range goarchs {
		arch, err := gentooArch(goarch)
		if err != nil {
			return nil, err
		}
		result = append(result, arch)
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}

func selectArchitectures(archives []*Archive, archs []string) []*Archive {
	if len(archs) == 0 {
		return archives
	}
	var result []*Archive
	for _, archive := range archives {
		if slices.Contains(archs, archive.GentooArch()) {
			result = append(result, archive)
		}
	}
	return result
}

func validateArchiveLayouts(section string, item config.GentooInstallItem, archives []*Archive) error {
	first := archives[0]
	for _, archive := range archives[1:] {
		wrappedMatches := archive.Path("") == first.Path("")
		binariesMatch := slices.Equal(archive.Binaries(), first.Binaries())
		if wrappedMatches && (item.Src != "" || binariesMatch) {
			continue
		}
		return fmt.Errorf("gentoo %s: src_id %q has mismatched archive layouts across architectures; specify explicit src", section, item.SrcID)
	}
	return nil
}

type installData struct {
	Source   string
	Target   string
	Keywords []string
}

type installProgramBuilder struct {
	bindir        string
	architectures []string
	extraInstall  string
	automatic     map[string][]installData
	explicit      []installItemData
	directories   []string
	manpages      []string
	docs          []string
	statements    []installStmt
}

func (b *installProgramBuilder) Build() *InstallProgram {
	b.addExtraInstall()
	b.addDirectories()
	b.addAutomaticBinaries()
	b.addExplicitInstalls()
	b.addManpages()
	b.addDocs()
	return &InstallProgram{UniverseArchitectures: b.architectures, Body: b.statements}
}

func (b *installProgramBuilder) addExtraInstall() {
	if b.extraInstall != "" {
		b.statements = append(b.statements, rawStmt{Content: b.extraInstall})
	}
}

func (b *installProgramBuilder) addDirectories() {
	for _, directory := range b.directories {
		b.statements = append(b.statements, actionStmt{Op: OpDodir, Source: directory})
	}
}

func (b *installProgramBuilder) addAutomaticBinaries() {
	if len(b.automatic) == 0 {
		return
	}
	if b.bindir != "" {
		b.statements = append(b.statements, stateStmt{Family: StateFamilyExe, Value: b.bindir})
	}
	groups := b.automaticGroups()
	for _, group := range groups {
		body := make([]installStmt, 0, len(group.installs))
		for _, install := range group.installs {
			body = append(body, automaticInstallStatement(install, b.bindir))
		}
		b.statements = append(b.statements, conditionStmt{Expr: newArchsAndUseExpr(group.architectures, nil), Body: body})
	}
}

type automaticInstallGroup struct {
	architectures []string
	installs      []installData
}

func (b *installProgramBuilder) automaticGroups() []automaticInstallGroup {
	byKey := map[string]*automaticInstallGroup{}
	for _, architecture := range b.architectures {
		installs := b.automatic[architecture]
		if len(installs) == 0 {
			continue
		}
		parts := make([]string, 0, len(installs))
		for _, install := range installs {
			parts = append(parts, install.Source+":"+install.Target)
		}
		key := strings.Join(parts, ";")
		if byKey[key] == nil {
			byKey[key] = &automaticInstallGroup{installs: installs}
		}
		byKey[key].architectures = append(byKey[key].architectures, architecture)
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]automaticInstallGroup, 0, len(keys))
	for _, key := range keys {
		slices.Sort(byKey[key].architectures)
		result = append(result, *byKey[key])
	}
	return result
}

func automaticInstallStatement(install installData, bindir string) actionStmt {
	rename := install.Source != install.Target && !strings.HasSuffix(install.Source, "/"+install.Target)
	if !rename {
		return actionStmt{Op: OpDoexe, Source: install.Source, Die: "Failed to install binary", RequiredState: StateRequirement{Family: StateFamilyExe, Value: bindir}}
	}
	return actionStmt{Op: OpNewexe, Source: install.Source, Target: install.Target, Die: "Failed to install " + install.Target, RequiredState: StateRequirement{Family: StateFamilyExe, Value: bindir}}
}

func (b *installProgramBuilder) addExplicitInstalls() {
	for _, install := range b.explicit {
		body := explicitInstallStatements(install)
		if len(install.Keywords) == 0 && len(install.Use) == 0 {
			b.statements = append(b.statements, body...)
			continue
		}
		b.statements = append(b.statements, conditionStmt{Expr: newArchsAndUseExpr(install.Keywords, install.Use), Body: body})
	}
}

func explicitInstallStatements(install installItemData) []installStmt {
	var body []installStmt
	if install.StateFamily != StateFamilyNone && install.Dir != "" {
		body = append(body, stateStmt{Family: install.StateFamily, Value: install.Dir})
	}
	rename := install.Source != install.Base && install.Section != "dosym"
	target := ""
	if install.Section == "dosym" {
		target = install.Target
	} else if rename {
		target = install.Base
	}
	return append(body, actionStmt{
		Op: resolveInstallOp(install.Section, rename), Source: install.Source, Target: target,
		Die:           "Failed to install " + install.Source,
		RequiredState: StateRequirement{Family: install.StateFamily, Value: install.Dir},
	})
}

func (b *installProgramBuilder) addManpages() {
	for _, manpage := range b.manpages {
		b.statements = append(b.statements, actionStmt{Op: OpDoman, Source: manpage, RequiredState: StateRequirement{Family: StateFamilyNone}})
	}
}

func (b *installProgramBuilder) addDocs() {
	for _, doc := range b.docs {
		b.statements = append(b.statements, actionStmt{Op: OpDodoc, Source: doc, RequiredState: StateRequirement{Family: StateFamilyDoc}})
	}
}
