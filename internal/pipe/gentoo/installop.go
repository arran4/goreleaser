package gentoo

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type StateFamily string

const (
	StateFamilyNone StateFamily = ""
	StateFamilyExe  StateFamily = "exeinto"
	StateFamilyIns  StateFamily = "insinto"
	StateFamilyBin  StateFamily = "into"
	StateFamilyDoc  StateFamily = "docinto"
)

type StateFamilyDescriptor struct {
	Family                      StateFamily
	Command                     string
	HasKnownInitialState        bool
	DefaultState                string
	RequiresStateInitialization bool
}

func (f StateFamily) Descriptor() StateFamilyDescriptor {
	switch f {
	case StateFamilyExe:
		return StateFamilyDescriptor{
			Family:                      StateFamilyExe,
			Command:                     "exeinto",
			HasKnownInitialState:        false,
			DefaultState:                "",
			RequiresStateInitialization: true,
		}
	case StateFamilyIns:
		return StateFamilyDescriptor{
			Family:                      StateFamilyIns,
			Command:                     "insinto",
			HasKnownInitialState:        true,
			DefaultState:                "/",
			RequiresStateInitialization: false,
		}
	case StateFamilyBin:
		return StateFamilyDescriptor{
			Family:                      StateFamilyBin,
			Command:                     "into",
			HasKnownInitialState:        true,
			DefaultState:                "/usr",
			RequiresStateInitialization: false,
		}
	case StateFamilyDoc:
		return StateFamilyDescriptor{
			Family:                      StateFamilyDoc,
			Command:                     "docinto",
			HasKnownInitialState:        true,
			DefaultState:                "",
			RequiresStateInitialization: false,
		}
	default:
		return StateFamilyDescriptor{
			Family:                      StateFamilyNone,
			Command:                     "",
			HasKnownInitialState:        false,
			DefaultState:                "",
			RequiresStateInitialization: false,
		}
	}
}

func InitialState() map[StateFamily]string {
	state := make(map[StateFamily]string)
	for _, f := range []StateFamily{StateFamilyBin, StateFamilyDoc, StateFamilyIns} {
		desc := f.Descriptor()
		if desc.HasKnownInitialState {
			state[f] = desc.DefaultState
		}
	}
	return state
}

type StateRequirement struct {
	Family StateFamily
	Value  string
}

type ArgMode int

const (
	ArgModeSingle ArgMode = iota
	ArgModeRename
	ArgModeTwoArgs
)

type DestinationMode int

const (
	DestinationModeNone DestinationMode = iota
	DestinationModeIntoBin
	DestinationModeIntoSbin
	DestinationModeExe
	DestinationModeIns
	DestinationModeDoc
	DestinationModeFixed
	DestinationModeMan
	DestinationModeSymlink
	DestinationModeDir
)

type InstallOp string

const (
	OpDoexe          InstallOp = "doexe"
	OpNewexe         InstallOp = "newexe"
	OpDoins          InstallOp = "doins"
	OpNewins         InstallOp = "newins"
	OpDobin          InstallOp = "dobin"
	OpNewbin         InstallOp = "newbin"
	OpDosbin         InstallOp = "dosbin"
	OpNewsbin        InstallOp = "newsbin"
	OpDoconfd        InstallOp = "doconfd"
	OpNewconfd       InstallOp = "newconfd"
	OpDoenvd         InstallOp = "doenvd"
	OpNewenvd        InstallOp = "newenvd"
	OpDoheader       InstallOp = "doheader"
	OpNewheader      InstallOp = "newheader"
	OpDoinitd        InstallOp = "doinitd"
	OpNewinitd       InstallOp = "newinitd"
	OpSystemdDounit  InstallOp = "systemd_dounit"
	OpSystemdNewunit InstallOp = "systemd_newunit"
	OpDosym          InstallOp = "dosym"
	OpDodoc          InstallOp = "dodoc"
	OpNewdoc         InstallOp = "newdoc"
	OpDoman          InstallOp = "doman"
	OpNewman         InstallOp = "newman"
	OpDodir          InstallOp = "dodir"
)

var allInstallOps = []InstallOp{
	OpDoexe,
	OpNewexe,
	OpDoins,
	OpNewins,
	OpDobin,
	OpNewbin,
	OpDosbin,
	OpNewsbin,
	OpDoconfd,
	OpNewconfd,
	OpDoenvd,
	OpNewenvd,
	OpDoheader,
	OpNewheader,
	OpDoinitd,
	OpNewinitd,
	OpSystemdDounit,
	OpSystemdNewunit,
	OpDosym,
	OpDodoc,
	OpNewdoc,
	OpDoman,
	OpNewman,
	OpDodir,
}

type OpDescriptor struct {
	Op      InstallOp
	Command string

	// arguments / rename semantics
	ArgMode        ArgMode
	SupportsRename bool
	RenameOp       InstallOp
	IsRename       bool

	// destination state semantics
	StateFamily                 StateFamily
	RequiresStateInitialization bool
	DefaultState                string
	HasKnownInitialState        bool

	// destination interpretation
	DestinationMode DestinationMode
	FixedDir        string
	ValidFixedDirs  []string

	// error handling
	AppendDie bool
}

func (d OpDescriptor) TakesTwoArgs() bool {
	return d.ArgMode == ArgModeRename || d.ArgMode == ArgModeTwoArgs
}

func (d OpDescriptor) DecomposeDestination(src, dst, defaultDir string) (StateFamily, string, string, error) {
	switch d.DestinationMode {
	case DestinationModeIntoBin:
		return d.decomposeBinDestination(src, dst, "bin")

	case DestinationModeIntoSbin:
		return d.decomposeBinDestination(src, dst, "sbin")

	case DestinationModeExe:
		return d.decomposeStateDestination(StateFamilyExe, src, dst, defaultDir)

	case DestinationModeIns:
		if defaultDir == "" {
			defaultDir = "/"
		}
		return d.decomposeStateDestination(StateFamilyIns, src, dst, defaultDir)

	case DestinationModeDoc:
		return d.decomposeDocDestination(src, dst)

	case DestinationModeFixed:
		return d.decomposeFixedDestination(src, dst)

	case DestinationModeMan:
		return d.decomposeManDestination(src, dst)

	case DestinationModeSymlink, DestinationModeDir:
		return StateFamilyNone, "", dst, nil

	default:
		return StateFamilyNone, "", path.Base(filepath.ToSlash(src)), nil
	}
}

func (d OpDescriptor) decomposeFixedDestination(src, dst string) (StateFamily, string, string, error) {
	if dst == "" {
		return StateFamilyNone, "", path.Base(filepath.ToSlash(src)), nil
	}
	cleaned := path.Clean(filepath.ToSlash(dst))
	dir, base := path.Dir(cleaned), path.Base(cleaned)
	if dir == "." || dir == "" {
		return StateFamilyNone, "", base, nil
	}
	if !slices.Contains(d.ValidFixedDirs, dir) {
		return StateFamilyNone, "", "", fmt.Errorf("gentoo %s: destination %q is incompatible with %s; expected %s/<name>", d.Command, dst, d.Command, d.FixedDir)
	}
	return StateFamilyNone, dir, base, nil
}

func (d OpDescriptor) decomposeManDestination(src, dst string) (StateFamily, string, string, error) {
	if dst == "" {
		return StateFamilyNone, "", path.Base(filepath.ToSlash(src)), nil
	}
	cleaned := path.Clean(filepath.ToSlash(dst))
	dir, base := path.Dir(cleaned), path.Base(cleaned)
	if dir == "." || dir == "" {
		return StateFamilyNone, "", base, nil
	}
	trimmed := strings.TrimPrefix(dir, "/")
	if !strings.HasPrefix(trimmed, "usr/share/man/man") && !strings.HasPrefix(trimmed, "share/man/man") {
		return StateFamilyNone, "", "", fmt.Errorf("gentoo %s: destination %q is incompatible with %s; expected /usr/share/man/manX/<name>", d.Command, dst, d.Command)
	}
	return StateFamilyNone, "", base, nil
}

func (d OpDescriptor) decomposeBinDestination(src, dst, directoryName string) (StateFamily, string, string, error) {
	if dst == "" {
		return StateFamilyBin, "/usr", path.Base(filepath.ToSlash(src)), nil
	}
	cleaned := path.Clean(filepath.ToSlash(dst))
	dir, base := path.Dir(cleaned), path.Base(cleaned)
	if dir == "." || dir == "" {
		return StateFamilyBin, "/usr", base, nil
	}
	if dir == directoryName || dir == "/"+directoryName {
		return StateFamilyBin, "/", base, nil
	}
	suffix := "/" + directoryName
	root, ok := strings.CutSuffix(dir, suffix)
	if !ok {
		return StateFamilyNone, "", "", fmt.Errorf("gentoo %s: destination %q is incompatible with %s; directory must end in /%s", d.Command, dst, d.Command, directoryName)
	}
	if root == "" {
		root = "/"
	}
	return StateFamilyBin, root, base, nil
}

func (d OpDescriptor) decomposeStateDestination(family StateFamily, src, dst, defaultDir string) (StateFamily, string, string, error) {
	if dst == "" {
		return family, defaultDir, path.Base(filepath.ToSlash(src)), nil
	}
	cleaned := path.Clean(filepath.ToSlash(dst))
	dir, base := path.Dir(cleaned), path.Base(cleaned)
	if dir != "." && dir != "" {
		defaultDir = dir
	}
	return family, defaultDir, base, nil
}

func (d OpDescriptor) decomposeDocDestination(src, dst string) (StateFamily, string, string, error) {
	if dst == "" {
		return StateFamilyDoc, "", path.Base(filepath.ToSlash(src)), nil
	}
	cleaned := path.Clean(filepath.ToSlash(dst))
	dir, base := path.Dir(cleaned), path.Base(cleaned)
	if dir == "." || dir == "" {
		return StateFamilyDoc, "", base, nil
	}
	return StateFamilyDoc, strings.TrimPrefix(dir, "/"), base, nil
}

var installOpDescriptors = map[InstallOp]OpDescriptor{
	OpDoexe:          {Op: OpDoexe, Command: "doexe", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpNewexe, StateFamily: StateFamilyExe, RequiresStateInitialization: true, DestinationMode: DestinationModeExe, AppendDie: true},
	OpNewexe:         {Op: OpNewexe, Command: "newexe", ArgMode: ArgModeRename, IsRename: true, StateFamily: StateFamilyExe, RequiresStateInitialization: true, DestinationMode: DestinationModeExe, AppendDie: true},
	OpDoins:          {Op: OpDoins, Command: "doins", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpNewins, StateFamily: StateFamilyIns, DefaultState: "/", HasKnownInitialState: true, DestinationMode: DestinationModeIns, AppendDie: true},
	OpNewins:         {Op: OpNewins, Command: "newins", ArgMode: ArgModeRename, IsRename: true, StateFamily: StateFamilyIns, DefaultState: "/", HasKnownInitialState: true, DestinationMode: DestinationModeIns, AppendDie: true},
	OpDobin:          {Op: OpDobin, Command: "dobin", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpNewbin, StateFamily: StateFamilyBin, DefaultState: "/usr", HasKnownInitialState: true, DestinationMode: DestinationModeIntoBin, AppendDie: true},
	OpNewbin:         {Op: OpNewbin, Command: "newbin", ArgMode: ArgModeRename, IsRename: true, StateFamily: StateFamilyBin, DefaultState: "/usr", HasKnownInitialState: true, DestinationMode: DestinationModeIntoBin, AppendDie: true},
	OpDosbin:         {Op: OpDosbin, Command: "dosbin", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpNewsbin, StateFamily: StateFamilyBin, DefaultState: "/usr", HasKnownInitialState: true, DestinationMode: DestinationModeIntoSbin, AppendDie: true},
	OpNewsbin:        {Op: OpNewsbin, Command: "newsbin", ArgMode: ArgModeRename, IsRename: true, StateFamily: StateFamilyBin, DefaultState: "/usr", HasKnownInitialState: true, DestinationMode: DestinationModeIntoSbin, AppendDie: true},
	OpDoconfd:        {Op: OpDoconfd, Command: "doconfd", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpNewconfd, DestinationMode: DestinationModeFixed, FixedDir: "/etc/conf.d", ValidFixedDirs: []string{"/etc/conf.d", "etc/conf.d"}, AppendDie: true},
	OpNewconfd:       {Op: OpNewconfd, Command: "newconfd", ArgMode: ArgModeRename, IsRename: true, DestinationMode: DestinationModeFixed, FixedDir: "/etc/conf.d", ValidFixedDirs: []string{"/etc/conf.d", "etc/conf.d"}, AppendDie: true},
	OpDoenvd:         {Op: OpDoenvd, Command: "doenvd", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpNewenvd, DestinationMode: DestinationModeFixed, FixedDir: "/etc/env.d", ValidFixedDirs: []string{"/etc/env.d", "etc/env.d"}, AppendDie: true},
	OpNewenvd:        {Op: OpNewenvd, Command: "newenvd", ArgMode: ArgModeRename, IsRename: true, DestinationMode: DestinationModeFixed, FixedDir: "/etc/env.d", ValidFixedDirs: []string{"/etc/env.d", "etc/env.d"}, AppendDie: true},
	OpDoheader:       {Op: OpDoheader, Command: "doheader", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpNewheader, DestinationMode: DestinationModeFixed, FixedDir: "/usr/include", ValidFixedDirs: []string{"/usr/include", "usr/include"}, AppendDie: true},
	OpNewheader:      {Op: OpNewheader, Command: "newheader", ArgMode: ArgModeRename, IsRename: true, DestinationMode: DestinationModeFixed, FixedDir: "/usr/include", ValidFixedDirs: []string{"/usr/include", "usr/include"}, AppendDie: true},
	OpDoinitd:        {Op: OpDoinitd, Command: "doinitd", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpNewinitd, DestinationMode: DestinationModeFixed, FixedDir: "/etc/init.d", ValidFixedDirs: []string{"/etc/init.d", "etc/init.d"}, AppendDie: true},
	OpNewinitd:       {Op: OpNewinitd, Command: "newinitd", ArgMode: ArgModeRename, IsRename: true, DestinationMode: DestinationModeFixed, FixedDir: "/etc/init.d", ValidFixedDirs: []string{"/etc/init.d", "etc/init.d"}, AppendDie: true},
	OpSystemdDounit:  {Op: OpSystemdDounit, Command: "systemd_dounit", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpSystemdNewunit, DestinationMode: DestinationModeFixed, FixedDir: "/usr/lib/systemd/system", ValidFixedDirs: []string{"/usr/lib/systemd/system", "usr/lib/systemd/system"}, AppendDie: true},
	OpSystemdNewunit: {Op: OpSystemdNewunit, Command: "systemd_newunit", ArgMode: ArgModeRename, IsRename: true, DestinationMode: DestinationModeFixed, FixedDir: "/usr/lib/systemd/system", ValidFixedDirs: []string{"/usr/lib/systemd/system", "usr/lib/systemd/system"}, AppendDie: true},
	OpDosym:          {Op: OpDosym, Command: "dosym", ArgMode: ArgModeTwoArgs, DestinationMode: DestinationModeSymlink, AppendDie: true},
	OpDodoc:          {Op: OpDodoc, Command: "dodoc", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpNewdoc, StateFamily: StateFamilyDoc, HasKnownInitialState: true, DestinationMode: DestinationModeDoc},
	OpNewdoc:         {Op: OpNewdoc, Command: "newdoc", ArgMode: ArgModeRename, IsRename: true, StateFamily: StateFamilyDoc, HasKnownInitialState: true, DestinationMode: DestinationModeDoc},
	OpDoman:          {Op: OpDoman, Command: "doman", ArgMode: ArgModeSingle, SupportsRename: true, RenameOp: OpNewman, DestinationMode: DestinationModeMan},
	OpNewman:         {Op: OpNewman, Command: "newman", ArgMode: ArgModeRename, IsRename: true, DestinationMode: DestinationModeMan},
	OpDodir:          {Op: OpDodir, Command: "dodir", ArgMode: ArgModeSingle, DestinationMode: DestinationModeDir},
}

func (op InstallOp) Descriptor() OpDescriptor {
	descriptor, ok := installOpDescriptors[op]
	if !ok {
		return OpDescriptor{Op: op, Command: string(op), ArgMode: ArgModeSingle, DestinationMode: DestinationModeNone, AppendDie: true}
	}
	descriptor.ValidFixedDirs = slices.Clone(descriptor.ValidFixedDirs)
	return descriptor
}

func resolveSectionOp(section string) InstallOp {
	switch section {
	case "dobin":
		return OpDobin
	case "dosbin":
		return OpDosbin
	case "doexe":
		return OpDoexe
	case "doins":
		return OpDoins
	case "doconfd":
		return OpDoconfd
	case "doenvd":
		return OpDoenvd
	case "doheader":
		return OpDoheader
	case "doinitd":
		return OpDoinitd
	case "systemd":
		return OpSystemdDounit
	case "dosym":
		return OpDosym
	case "dodoc":
		return OpDodoc
	case "doman":
		return OpDoman
	case "dodir":
		return OpDodir
	default:
		return InstallOp(section)
	}
}

func resolveInstallOp(section string, isRename bool) InstallOp {
	baseOp := resolveSectionOp(section)
	desc := baseOp.Descriptor()
	if isRename && desc.SupportsRename && desc.RenameOp != "" {
		return desc.RenameOp
	}
	return baseOp
}
