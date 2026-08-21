package gentoo

import (
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
	automatic := p.automaticBinaries()
	raw := p.cfg.raw
	program := buildInstallPlan(
		p.cfg.Bindir(),
		p.release.Architectures(),
		p.extraInstall,
		automatic,
		p.installers,
		raw.Dodir,
		p.extras.ResolveAll(raw.Doman),
		p.extras.ResolveAll(raw.Dodoc),
	)
	return program.Reduce(), program.Validate()
}

func (p *InstallPlanner) sections() []installSection {
	raw := p.cfg.raw
	return []installSection{
		{name: "dobin", items: raw.Dobin},
		{name: "doconfd", items: raw.Doconfd},
		{name: "doenvd", items: raw.Doenvd},
		{name: "doexe", items: raw.Doexe, defaultDir: p.cfg.Bindir()},
		{name: "doheader", items: raw.Doheader},
		{name: "doinitd", items: raw.Doinitd},
		{name: "doins", items: raw.Doins, defaultDir: "/"},
		{name: "dosbin", items: raw.Dosbin},
		{name: "dosym", items: raw.Dosym},
		{name: "systemd", items: raw.Systemd},
	}
}

func (p *InstallPlanner) resolveExplicit() error {
	for _, section := range p.sections() {
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

func buildInstallPlan(
	bindir string,
	keywordsList []string,
	extraInstall string,
	installByKw map[string][]installData,
	installers []installItemData,
	dodir []string,
	doman []string,
	dodoc []string,
) *InstallProgram {
	var stmts []installStmt

	if extraInstall != "" {
		stmts = append(stmts, rawStmt{Content: extraInstall})
	}

	for _, dir := range dodir {
		stmts = append(stmts, actionStmt{
			Op:     OpDodir,
			Source: dir,
		})
	}

	if bindir != "" && len(installByKw) > 0 {
		stmts = append(stmts, stateStmt{
			Family: StateFamilyExe,
			Value:  bindir,
		})
	}

	if len(installByKw) > 0 {
		groupMap := make(map[string][]string)
		installItemsMap := make(map[string][]installData)

		for _, kw := range keywordsList {
			installs := installByKw[kw]
			if len(installs) == 0 {
				continue
			}
			var keyParts []string
			for _, inst := range installs {
				keyParts = append(keyParts, inst.Source+":"+inst.Target)
			}
			groupKey := strings.Join(keyParts, ";")
			groupMap[groupKey] = append(groupMap[groupKey], kw)
			installItemsMap[groupKey] = installs
		}

		groupKeys := make([]string, 0, len(groupMap))
		for groupKey := range groupMap {
			groupKeys = append(groupKeys, groupKey)
		}
		slices.Sort(groupKeys)

		for _, groupKey := range groupKeys {
			kws := groupMap[groupKey]
			slices.Sort(kws)
			installs := installItemsMap[groupKey]

			var body []installStmt
			for _, inst := range installs {
				isRename := inst.Source != inst.Target && !strings.HasSuffix(inst.Source, "/"+inst.Target)
				op := OpDoexe
				if isRename {
					op = OpNewexe
				}
				dieMsg := "Failed to install binary"
				target := ""
				if isRename {
					dieMsg = "Failed to install " + inst.Target
					target = inst.Target
				}
				body = append(body, actionStmt{
					Op:            op,
					Source:        inst.Source,
					Target:        target,
					Die:           dieMsg,
					RequiredState: StateRequirement{Family: StateFamilyExe, Value: bindir},
				})
			}

			if len(kws) > 0 {
				stmts = append(stmts, conditionStmt{
					Expr: newArchsAndUseExpr(kws, nil),
					Body: body,
				})
			} else {
				stmts = append(stmts, body...)
			}
		}
	}

	for _, inst := range installers {
		var condBody []installStmt
		if inst.StateFamily != StateFamilyNone && inst.Dir != "" {
			condBody = append(condBody, stateStmt{
				Family: inst.StateFamily,
				Value:  inst.Dir,
			})
		}

		isRename := inst.Source != inst.Base && inst.Section != "dosym"
		op := resolveInstallOp(inst.Section, isRename)

		dieMsg := "Failed to install " + inst.Source
		target := ""
		if inst.Section == "dosym" {
			target = inst.Target
		} else if isRename {
			target = inst.Base
		}

		condBody = append(condBody, actionStmt{
			Op:            op,
			Source:        inst.Source,
			Target:        target,
			Die:           dieMsg,
			RequiredState: StateRequirement{Family: inst.StateFamily, Value: inst.Dir},
		})

		if len(inst.Keywords) > 0 || len(inst.Use) > 0 {
			stmts = append(stmts, conditionStmt{
				Expr: newArchsAndUseExpr(inst.Keywords, inst.Use),
				Body: condBody,
			})
		} else {
			stmts = append(stmts, condBody...)
		}
	}

	for _, man := range doman {
		stmts = append(stmts, actionStmt{
			Op:            OpDoman,
			Source:        man,
			RequiredState: StateRequirement{Family: StateFamilyNone},
		})
	}
	for _, doc := range dodoc {
		stmts = append(stmts, actionStmt{
			Op:            OpDodoc,
			Source:        doc,
			RequiredState: StateRequirement{Family: StateFamilyDoc, Value: ""},
		})
	}
	return &InstallProgram{UniverseArchitectures: keywordsList, Body: stmts}
}
