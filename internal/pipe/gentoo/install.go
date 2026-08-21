package gentoo

import (
	"errors"
	"fmt"
	"path"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
)

type BinaryKey struct {
	ArchiveID string
	Arch      string
	Binary    string
}

type BinaryClaims struct {
	claimed map[BinaryKey]struct{}
}

func NewBinaryClaims() *BinaryClaims {
	return &BinaryClaims{
		claimed: make(map[BinaryKey]struct{}),
	}
}

func (c *BinaryClaims) Claim(key BinaryKey) {
	c.claimed[key] = struct{}{}
}

func (c *BinaryClaims) IsClaimed(key BinaryKey) bool {
	_, ok := c.claimed[key]
	return ok
}

type Install struct {
	source string
	target string

	command       string
	renameCommand string
	intoCommand   string
	directory     string

	use   []string
	archs []string
}

func (i Install) Source() string { return i.source }
func (i Install) Target() string { return i.target }
func (i Install) Architectures() []string { return i.archs }
func (i Install) UseFlags() []string { return i.use }

type InstallerSpec struct {
	Command       string
	RenameCommand string
	IntoCommand   string
	DefaultDir    string
}

func Installer(section string, cfg *GentooConfig) (InstallerSpec, error) {
	switch section {
	case "doexe":
		return InstallerSpec{
			Command:       "doexe",
			RenameCommand: "newexe",
			IntoCommand:   "exeinto",
			DefaultDir:    cfg.Bindir(),
		}, nil
	case "dobin":
		return InstallerSpec{
			Command:       "dobin",
			RenameCommand: "newbin",
			IntoCommand:   "into",
			DefaultDir:    "/usr",
		}, nil
	case "dosbin":
		return InstallerSpec{
			Command:       "dosbin",
			RenameCommand: "newsbin",
			IntoCommand:   "into",
			DefaultDir:    "/usr",
		}, nil
	case "doins":
		return InstallerSpec{
			Command:       "doins",
			RenameCommand: "newins",
			IntoCommand:   "insinto",
			DefaultDir:    "/",
		}, nil
	case "doinitd":
		return InstallerSpec{
			Command:       "doinitd",
			RenameCommand: "newinitd",
			IntoCommand:   "exeinto",
			DefaultDir:    "/etc/init.d",
		}, nil
	case "doconfd":
		return InstallerSpec{
			Command:       "doconfd",
			RenameCommand: "newconfd",
			IntoCommand:   "insinto",
			DefaultDir:    "/etc/conf.d",
		}, nil
	case "doenvd":
		return InstallerSpec{
			Command:       "doenvd",
			RenameCommand: "newenvd",
			IntoCommand:   "insinto",
			DefaultDir:    "/etc/env.d",
		}, nil
	case "doheader":
		return InstallerSpec{
			Command:       "doheader",
			RenameCommand: "newheader",
			IntoCommand:   "insinto",
			DefaultDir:    "/usr/include",
		}, nil
	case "dosym":
		return InstallerSpec{
			Command:       "dosym",
			RenameCommand: "dosym",
			IntoCommand:   "",
			DefaultDir:    "",
		}, nil
	case "systemd":
		return InstallerSpec{
			Command:       "doins",
			RenameCommand: "newins",
			IntoCommand:   "insinto",
			DefaultDir:    "/usr/lib/systemd/system",
		}, nil
	default:
		return InstallerSpec{}, fmt.Errorf("unknown installer section: %s", section)
	}
}

type InstallPlan struct {
	installs []Install
}

type InstallPlanner struct {
	cfg     *GentooConfig
	release *Release
	claims  *BinaryClaims
	result  []Install
}

func NewInstallPlanner(cfg *GentooConfig, release *Release) *InstallPlanner {
	return &InstallPlanner{
		cfg:     cfg,
		release: release,
		claims:  NewBinaryClaims(),
	}
}

func (p *InstallPlanner) Plan() (*InstallPlan, error) {
	if err := p.resolveExplicit(); err != nil {
		return nil, err
	}
	if err := p.addAutomatic(); err != nil {
		return nil, err
	}
	return &InstallPlan{installs: p.result}, nil
}

func (p *InstallPlanner) resolveExplicit() error {
	sections := []struct {
		name  string
		items []config.GentooInstallItem
	}{
		{"dobin", p.cfg.Raw().Dobin},
		{"doconfd", p.cfg.Raw().Doconfd},
		{"doenvd", p.cfg.Raw().Doenvd},
		{"doexe", p.cfg.Raw().Doexe},
		{"doheader", p.cfg.Raw().Doheader},
		{"doinitd", p.cfg.Raw().Doinitd},
		{"doins", p.cfg.Raw().Doins},
		{"dosbin", p.cfg.Raw().Dosbin},
		{"dosym", p.cfg.Raw().Dosym},
		{"systemd", p.cfg.Raw().Systemd},
	}

	for _, s := range sections {
		if err := p.resolveSection(s.name, s.items); err != nil {
			return err
		}
	}
	return nil
}

func (p *InstallPlanner) resolveSection(name string, items []config.GentooInstallItem) error {
	spec, err := Installer(name, p.cfg)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := p.resolveItem(spec, item); err != nil {
			return err
		}
	}
	return nil
}

func (p *InstallPlanner) resolveItem(spec InstallerSpec, item config.GentooInstallItem) error {
	if item.Src == "" && item.SrcID == "" {
		return errors.New("install item must specify either src or src_id")
	}

	var install UseInstallItem
	install.item = item
	install.spec = spec

	dir := spec.DefaultDir
	target := item.Dst

	if target != "" && spec.Command != "dosym" {
		if target[len(target)-1] == '/' {
			dir = target[:len(target)-1]
			target = ""
		} else if d, f := path.Split(target); d != "" {
			dir = d
			target = f
			if dir != "/" {
				dir = dir[:len(dir)-1]
			}
		}
	}

	if item.Src != "" {
		install.source = item.Src
		if target == "" {
			target = path.Base(install.source)
		}

		if spec.Command != "dosym" && item.SrcID != "" {
			archives := p.release.ArchivesByID(item.SrcID)
			if len(archives) == 0 {
				return fmt.Errorf("no archives found for src_id %q", item.SrcID)
			}

			archs := item.Archs
			if len(archs) == 0 {
				for _, a := range archives {
					archs = append(archs, a.GoArch())
				}
			}

			for _, goarch := range archs {
				gentooArch, err := gentooArch(goarch)
				if err != nil {
					return err
				}
				a, ok := p.release.Archive(item.SrcID, "~"+gentooArch)
				if !ok {
					a, ok = p.release.Archive(item.SrcID, gentooArch)
				}
				if !ok {
					return fmt.Errorf("no archive found for src_id %q and arch %q", item.SrcID, goarch)
				}

				if !a.Contains(item.Src) {
					return fmt.Errorf("archive %q does not contain source file %q", a.Filename(), item.Src)
				}
				install.source = a.Path(item.Src)

				for _, bin := range a.Binaries() {
					if bin == install.source {
						p.claims.Claim(BinaryKey{ArchiveID: item.SrcID, Arch: gentooArch, Binary: bin})
					}
				}

				install.archs = append(install.archs, gentooArch)
			}
		}

		install.target = target
		install.directory = dir
		p.result = append(p.result, install.ToInstall())
		return nil
	}

	archives := p.release.ArchivesByID(item.SrcID)
	if len(archives) == 0 {
		return fmt.Errorf("no archives found for src_id %q", item.SrcID)
	}

	archs := item.Archs
	if len(archs) == 0 {
		for _, a := range archives {
			archs = append(archs, a.GoArch())
		}
	}

	var firstBinaries []string
	var firstArch string
	var resolvedArchs []string

	for i, goarch := range archs {
		gentooArch, err := gentooArch(goarch)
		if err != nil {
			return err
		}
		a, ok := p.release.Archive(item.SrcID, "~"+gentooArch)
		if !ok {
			a, ok = p.release.Archive(item.SrcID, gentooArch)
		}
		if !ok {
			return fmt.Errorf("no archive found for src_id %q and arch %q", item.SrcID, goarch)
		}
		resolvedArchs = append(resolvedArchs, gentooArch)

		bins := a.Binaries()
		if i == 0 {
			firstBinaries = bins
			firstArch = gentooArch
		} else {
			if len(firstBinaries) != len(bins) {
				return fmt.Errorf("binary count mismatch between architectures %s and %s for src_id %q", firstArch, gentooArch, item.SrcID)
			}
		}

		for _, bin := range bins {
			p.claims.Claim(BinaryKey{ArchiveID: item.SrcID, Arch: gentooArch, Binary: bin})
		}
	}

	if len(firstBinaries) > 1 && item.Dst != "" {
		return fmt.Errorf("cannot specify dst with multiple binaries for src_id %q", item.SrcID)
	}

	if len(firstBinaries) == 0 {
		return fmt.Errorf("no binaries found for src_id %q", item.SrcID)
	}

	for _, bin := range firstBinaries {
		install.source = bin
		install.target = target
		if install.target == "" {
			install.target = path.Base(install.source)
		}
		install.directory = dir
		install.archs = resolvedArchs
		p.result = append(p.result, install.ToInstall())
	}

	return nil
}

func (p *InstallPlanner) addAutomatic() error {
	spec, _ := Installer("doexe", p.cfg)

	for _, a := range p.release.Archives() {
		for _, bin := range a.Binaries() {
			key := BinaryKey{ArchiveID: a.ID(), Arch: a.GentooArch(), Binary: bin}
			if p.claims.IsClaimed(key) {
				continue
			}

			install := Install{
				source:        bin,
				target:        path.Base(bin),
				command:       spec.Command,
				renameCommand: spec.RenameCommand,
				intoCommand:   spec.IntoCommand,
				directory:     spec.DefaultDir,
				archs:         []string{a.GentooArch()},
			}
			p.result = append(p.result, install)
		}
	}

	return nil
}

type UseInstallItem struct {
	item      config.GentooInstallItem
	spec      InstallerSpec
	source    string
	target    string
	directory string
	archs     []string
}

func (u UseInstallItem) ToInstall() Install {
	return Install{
		source:        u.source,
		target:        u.target,
		command:       u.spec.Command,
		renameCommand: u.spec.RenameCommand,
		intoCommand:   u.spec.IntoCommand,
		directory:     u.directory,
		use:           u.item.Use,
		archs:         u.archs,
	}
}
