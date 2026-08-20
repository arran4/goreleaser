package gentoo

import (
	"errors"
	"fmt"
	"maps"
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
			HasKnownInitialState:        false,
			DefaultState:                "/",
			RequiresStateInitialization: true,
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
	for _, f := range []StateFamily{StateFamilyBin, StateFamilyDoc} {
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
		if dst == "" {
			return StateFamilyBin, "/usr", path.Base(filepath.ToSlash(src)), nil
		}
		cleanedDst := path.Clean(filepath.ToSlash(dst))
		dir := path.Dir(cleanedDst)
		base := path.Base(cleanedDst)
		if dir == "." || dir == "" {
			return StateFamilyBin, "/usr", base, nil
		}
		var root string
		switch {
		case dir == "bin" || dir == "/bin":
			root = "/"
		case strings.HasSuffix(dir, "/bin"):
			root, _ = strings.CutSuffix(dir, "/bin")
			if root == "" {
				root = "/"
			}
		default:
			return StateFamilyNone, "", "", fmt.Errorf("gentoo %s: destination %q is incompatible with %s; directory must end in /bin", d.Command, dst, d.Command)
		}
		return StateFamilyBin, root, base, nil

	case DestinationModeIntoSbin:
		if dst == "" {
			return StateFamilyBin, "/usr", path.Base(filepath.ToSlash(src)), nil
		}
		cleanedDst := path.Clean(filepath.ToSlash(dst))
		dir := path.Dir(cleanedDst)
		base := path.Base(cleanedDst)
		if dir == "." || dir == "" {
			return StateFamilyBin, "/usr", base, nil
		}
		var root string
		switch {
		case dir == "sbin" || dir == "/sbin":
			root = "/"
		case strings.HasSuffix(dir, "/sbin"):
			root, _ = strings.CutSuffix(dir, "/sbin")
			if root == "" {
				root = "/"
			}
		default:
			return StateFamilyNone, "", "", fmt.Errorf("gentoo %s: destination %q is incompatible with %s; directory must end in /sbin", d.Command, dst, d.Command)
		}
		return StateFamilyBin, root, base, nil

	case DestinationModeExe:
		dirVal := defaultDir
		if dst == "" {
			return StateFamilyExe, dirVal, path.Base(filepath.ToSlash(src)), nil
		}
		cleanedDst := path.Clean(filepath.ToSlash(dst))
		dir := path.Dir(cleanedDst)
		base := path.Base(cleanedDst)
		if dir != "." && dir != "" {
			dirVal = dir
		}
		return StateFamilyExe, dirVal, base, nil

	case DestinationModeIns:
		dirVal := defaultDir
		if dirVal == "" {
			dirVal = "/"
		}
		if dst == "" {
			return StateFamilyIns, dirVal, path.Base(filepath.ToSlash(src)), nil
		}
		cleanedDst := path.Clean(filepath.ToSlash(dst))
		dir := path.Dir(cleanedDst)
		base := path.Base(cleanedDst)
		if dir != "." && dir != "" {
			dirVal = dir
		}
		return StateFamilyIns, dirVal, base, nil

	case DestinationModeDoc:
		if dst == "" {
			return StateFamilyDoc, "", path.Base(filepath.ToSlash(src)), nil
		}
		cleanedDst := path.Clean(filepath.ToSlash(dst))
		dir := path.Dir(cleanedDst)
		base := path.Base(cleanedDst)
		docDir := ""
		if dir != "." && dir != "" {
			docDir = strings.TrimPrefix(dir, "/")
		}
		return StateFamilyDoc, docDir, base, nil

	case DestinationModeFixed:
		if dst == "" {
			return StateFamilyNone, "", path.Base(filepath.ToSlash(src)), nil
		}
		cleanedDst := path.Clean(filepath.ToSlash(dst))
		dir := path.Dir(cleanedDst)
		base := path.Base(cleanedDst)
		if dir == "." || dir == "" {
			return StateFamilyNone, "", base, nil
		}
		if !slices.Contains(d.ValidFixedDirs, dir) {
			return StateFamilyNone, "", "", fmt.Errorf("gentoo %s: destination %q is incompatible with %s; expected %s/<name>", d.Command, dst, d.Command, d.FixedDir)
		}
		return StateFamilyNone, dir, base, nil

	case DestinationModeMan:
		if dst == "" {
			return StateFamilyNone, "", path.Base(filepath.ToSlash(src)), nil
		}
		cleanedDst := path.Clean(filepath.ToSlash(dst))
		dir := path.Dir(cleanedDst)
		base := path.Base(cleanedDst)
		if dir != "." && dir != "" {
			trimmed := strings.TrimPrefix(dir, "/")
			if !strings.HasPrefix(trimmed, "usr/share/man/man") && !strings.HasPrefix(trimmed, "share/man/man") {
				return StateFamilyNone, "", "", fmt.Errorf("gentoo %s: destination %q is incompatible with %s; expected /usr/share/man/manX/<name>", d.Command, dst, d.Command)
			}
		}
		return StateFamilyNone, "", base, nil

	case DestinationModeSymlink, DestinationModeDir:
		return StateFamilyNone, "", dst, nil

	default:
		return StateFamilyNone, "", path.Base(filepath.ToSlash(src)), nil
	}
}

func (op InstallOp) Descriptor() OpDescriptor {
	switch op {
	case OpDoexe:
		return OpDescriptor{
			Op:                          OpDoexe,
			Command:                     "doexe",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpNewexe,
			StateFamily:                 StateFamilyExe,
			RequiresStateInitialization: true,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeExe,
			AppendDie:                   true,
		}
	case OpNewexe:
		return OpDescriptor{
			Op:                          OpNewexe,
			Command:                     "newexe",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyExe,
			RequiresStateInitialization: true,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeExe,
			AppendDie:                   true,
		}
	case OpDoins:
		return OpDescriptor{
			Op:                          OpDoins,
			Command:                     "doins",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpNewins,
			StateFamily:                 StateFamilyIns,
			RequiresStateInitialization: true,
			DefaultState:                "/",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeIns,
			AppendDie:                   true,
		}
	case OpNewins:
		return OpDescriptor{
			Op:                          OpNewins,
			Command:                     "newins",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyIns,
			RequiresStateInitialization: true,
			DefaultState:                "/",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeIns,
			AppendDie:                   true,
		}
	case OpDobin:
		return OpDescriptor{
			Op:                          OpDobin,
			Command:                     "dobin",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpNewbin,
			StateFamily:                 StateFamilyBin,
			RequiresStateInitialization: false,
			DefaultState:                "/usr",
			HasKnownInitialState:        true,
			DestinationMode:             DestinationModeIntoBin,
			AppendDie:                   true,
		}
	case OpNewbin:
		return OpDescriptor{
			Op:                          OpNewbin,
			Command:                     "newbin",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyBin,
			RequiresStateInitialization: false,
			DefaultState:                "/usr",
			HasKnownInitialState:        true,
			DestinationMode:             DestinationModeIntoBin,
			AppendDie:                   true,
		}
	case OpDosbin:
		return OpDescriptor{
			Op:                          OpDosbin,
			Command:                     "dosbin",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpNewsbin,
			StateFamily:                 StateFamilyBin,
			RequiresStateInitialization: false,
			DefaultState:                "/usr",
			HasKnownInitialState:        true,
			DestinationMode:             DestinationModeIntoSbin,
			AppendDie:                   true,
		}
	case OpNewsbin:
		return OpDescriptor{
			Op:                          OpNewsbin,
			Command:                     "newsbin",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyBin,
			RequiresStateInitialization: false,
			DefaultState:                "/usr",
			HasKnownInitialState:        true,
			DestinationMode:             DestinationModeIntoSbin,
			AppendDie:                   true,
		}
	case OpDoconfd:
		return OpDescriptor{
			Op:                          OpDoconfd,
			Command:                     "doconfd",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpNewconfd,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeFixed,
			FixedDir:                    "/etc/conf.d",
			ValidFixedDirs:              []string{"/etc/conf.d", "etc/conf.d"},
			AppendDie:                   true,
		}
	case OpNewconfd:
		return OpDescriptor{
			Op:                          OpNewconfd,
			Command:                     "newconfd",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeFixed,
			FixedDir:                    "/etc/conf.d",
			ValidFixedDirs:              []string{"/etc/conf.d", "etc/conf.d"},
			AppendDie:                   true,
		}
	case OpDoenvd:
		return OpDescriptor{
			Op:                          OpDoenvd,
			Command:                     "doenvd",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpNewenvd,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeFixed,
			FixedDir:                    "/etc/env.d",
			ValidFixedDirs:              []string{"/etc/env.d", "etc/env.d"},
			AppendDie:                   true,
		}
	case OpNewenvd:
		return OpDescriptor{
			Op:                          OpNewenvd,
			Command:                     "newenvd",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeFixed,
			FixedDir:                    "/etc/env.d",
			ValidFixedDirs:              []string{"/etc/env.d", "etc/env.d"},
			AppendDie:                   true,
		}
	case OpDoheader:
		return OpDescriptor{
			Op:                          OpDoheader,
			Command:                     "doheader",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpNewheader,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeFixed,
			FixedDir:                    "/usr/include",
			ValidFixedDirs:              []string{"/usr/include", "usr/include"},
			AppendDie:                   true,
		}
	case OpNewheader:
		return OpDescriptor{
			Op:                          OpNewheader,
			Command:                     "newheader",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeFixed,
			FixedDir:                    "/usr/include",
			ValidFixedDirs:              []string{"/usr/include", "usr/include"},
			AppendDie:                   true,
		}
	case OpDoinitd:
		return OpDescriptor{
			Op:                          OpDoinitd,
			Command:                     "doinitd",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpNewinitd,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeFixed,
			FixedDir:                    "/etc/init.d",
			ValidFixedDirs:              []string{"/etc/init.d", "etc/init.d"},
			AppendDie:                   true,
		}
	case OpNewinitd:
		return OpDescriptor{
			Op:                          OpNewinitd,
			Command:                     "newinitd",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeFixed,
			FixedDir:                    "/etc/init.d",
			ValidFixedDirs:              []string{"/etc/init.d", "etc/init.d"},
			AppendDie:                   true,
		}
	case OpSystemdDounit:
		return OpDescriptor{
			Op:                          OpSystemdDounit,
			Command:                     "systemd_dounit",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpSystemdNewunit,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeFixed,
			FixedDir:                    "/usr/lib/systemd/system",
			ValidFixedDirs:              []string{"/usr/lib/systemd/system", "usr/lib/systemd/system", "/lib/systemd/system", "lib/systemd/system"},
			AppendDie:                   true,
		}
	case OpSystemdNewunit:
		return OpDescriptor{
			Op:                          OpSystemdNewunit,
			Command:                     "systemd_newunit",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeFixed,
			FixedDir:                    "/usr/lib/systemd/system",
			ValidFixedDirs:              []string{"/usr/lib/systemd/system", "usr/lib/systemd/system", "/lib/systemd/system", "lib/systemd/system"},
			AppendDie:                   true,
		}
	case OpDosym:
		return OpDescriptor{
			Op:                          OpDosym,
			Command:                     "dosym",
			ArgMode:                     ArgModeTwoArgs,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeSymlink,
			AppendDie:                   true,
		}
	case OpDodoc:
		return OpDescriptor{
			Op:                          OpDodoc,
			Command:                     "dodoc",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpNewdoc,
			StateFamily:                 StateFamilyDoc,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        true,
			DestinationMode:             DestinationModeDoc,
			AppendDie:                   false,
		}
	case OpNewdoc:
		return OpDescriptor{
			Op:                          OpNewdoc,
			Command:                     "newdoc",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyDoc,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        true,
			DestinationMode:             DestinationModeDoc,
			AppendDie:                   false,
		}
	case OpDoman:
		return OpDescriptor{
			Op:                          OpDoman,
			Command:                     "doman",
			ArgMode:                     ArgModeSingle,
			SupportsRename:              true,
			RenameOp:                    OpNewman,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeMan,
			AppendDie:                   false,
		}
	case OpNewman:
		return OpDescriptor{
			Op:                          OpNewman,
			Command:                     "newman",
			ArgMode:                     ArgModeRename,
			IsRename:                    true,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeMan,
			AppendDie:                   false,
		}
	case OpDodir:
		return OpDescriptor{
			Op:                          OpDodir,
			Command:                     "dodir",
			ArgMode:                     ArgModeSingle,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeDir,
			AppendDie:                   false,
		}
	default:
		return OpDescriptor{
			Op:                          op,
			Command:                     string(op),
			ArgMode:                     ArgModeSingle,
			StateFamily:                 StateFamilyNone,
			RequiresStateInitialization: false,
			DefaultState:                "",
			HasKnownInitialState:        false,
			DestinationMode:             DestinationModeNone,
			AppendDie:                   true,
		}
	}
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

type installStmt interface {
	isInstallStmt()
	String(indent string) string
	Validate() error
	Equals(other installStmt) bool
}

type stateStmt struct {
	Family StateFamily
	Value  string
}

func (s stateStmt) isInstallStmt() {}

func (s stateStmt) String(indent string) string {
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString(string(s.Family))
	if s.Value != "" {
		sb.WriteString(" ")
		sb.WriteString(s.Value)
	}
	sb.WriteString("\n")
	return sb.String()
}

func (s stateStmt) Validate() error {
	if s.Family == "" {
		return errors.New("stateStmt requires a state family")
	}
	desc := s.Family.Descriptor()
	if desc.RequiresStateInitialization && s.Value == "" {
		return fmt.Errorf("stateStmt %s requires a directory value", s.Family)
	}
	return nil
}

func (s stateStmt) Equals(other installStmt) bool {
	o, ok := other.(stateStmt)
	return ok && s.Family == o.Family && s.Value == o.Value
}

type actionStmt struct {
	Op            InstallOp
	Source        string
	Target        string
	Die           string
	RequiredState StateRequirement
}

func (a actionStmt) isInstallStmt() {}

func (a actionStmt) String(indent string) string {
	desc := a.Op.Descriptor()
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString(desc.Command)
	sb.WriteString(" \"")
	sb.WriteString(a.Source)
	sb.WriteString("\"")

	if desc.TakesTwoArgs() && a.Target != "" {
		sb.WriteString(" \"")
		sb.WriteString(a.Target)
		sb.WriteString("\"")
	}

	if desc.AppendDie {
		dieMsg := a.Die
		if dieMsg == "" {
			if a.Target != "" {
				dieMsg = "Failed to install " + a.Target
			} else {
				dieMsg = "Failed to install " + a.Source
			}
		}
		sb.WriteString(" || die \"")
		sb.WriteString(dieMsg)
		sb.WriteString("\"")
	}
	sb.WriteString("\n")
	return sb.String()
}

func (a actionStmt) Validate() error {
	desc := a.Op.Descriptor()
	if a.Source == "" {
		return fmt.Errorf("%s requires a source argument", desc.Command)
	}
	if a.Op == OpDosym && a.Target == "" {
		return errors.New("dosym requires a destination")
	}
	if desc.IsRename && a.Target == "" {
		return fmt.Errorf("%s requires a destination", a.Op)
	}
	if desc.ArgMode == ArgModeSingle && a.Target != "" {
		return fmt.Errorf("%s does not accept a second argument", desc.Command)
	}
	return nil
}

func (a actionStmt) Equals(other installStmt) bool {
	o, ok := other.(actionStmt)
	return ok && a.Op == o.Op && a.Source == o.Source && a.Target == o.Target && a.Die == o.Die && a.RequiredState == o.RequiredState
}

type rawStmt struct {
	Content string
}

func (r rawStmt) isInstallStmt() {}

func (r rawStmt) String(indent string) string {
	if r.Content == "" {
		return ""
	}
	var sb strings.Builder
	for line := range strings.SplitSeq(r.Content, "\n") {
		if line != "" {
			sb.WriteString(indent)
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (r rawStmt) Validate() error { return nil }

func (r rawStmt) Equals(other installStmt) bool {
	o, ok := other.(rawStmt)
	return ok && r.Content == o.Content
}

type conditionStmt struct {
	Expr conditionExpr
	Body []installStmt
}

func (c conditionStmt) isInstallStmt() {}

func (c conditionStmt) String(indent string) string {
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString("if ")
	if c.Expr != nil {
		sb.WriteString(c.Expr.Shell())
	}
	sb.WriteString("; then\n")
	for _, stmt := range c.Body {
		sb.WriteString(stmt.String(indent + "  "))
	}
	sb.WriteString(indent)
	sb.WriteString("fi\n")
	return sb.String()
}

func (c conditionStmt) Validate() error {
	if c.Expr == nil {
		return errors.New("condition requires an expression")
	}
	for _, s := range c.Body {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c conditionStmt) Equals(other installStmt) bool {
	o, ok := other.(conditionStmt)
	if !ok {
		return false
	}
	if (c.Expr == nil) != (o.Expr == nil) {
		return false
	}
	if c.Expr != nil && !c.Expr.Equals(o.Expr) {
		return false
	}
	if len(c.Body) != len(o.Body) {
		return false
	}
	for i := range c.Body {
		if !c.Body[i].Equals(o.Body[i]) {
			return false
		}
	}
	return true
}

// conditionExpr represents a boolean condition AST node.
type conditionExpr interface {
	isConditionExpr()
	String() string
	Shell() string
	Equals(other conditionExpr) bool
}

// TrueExpr represents a boolean constant true condition AST node.
type TrueExpr struct{}

func (TrueExpr) isConditionExpr() {}

func (TrueExpr) String() string {
	return "True"
}

func (TrueExpr) Shell() string {
	return "true"
}

func (TrueExpr) Equals(other conditionExpr) bool {
	_, ok := other.(TrueExpr)
	return ok
}

// FalseExpr represents a boolean constant false condition AST node.
type FalseExpr struct{}

func (FalseExpr) isConditionExpr() {}

func (FalseExpr) String() string {
	return "False"
}

func (FalseExpr) Shell() string {
	return "false"
}

func (FalseExpr) Equals(other conditionExpr) bool {
	_, ok := other.(FalseExpr)
	return ok
}

// ArchExpr represents an architecture condition predicate, e.g. Arch(amd64).
type ArchExpr struct {
	Arch string
}

func (a ArchExpr) isConditionExpr() {}

func (a ArchExpr) String() string {
	return fmt.Sprintf("Arch(%s)", a.Arch)
}

func (a ArchExpr) Shell() string {
	return "use " + a.Arch
}

func (a ArchExpr) Equals(other conditionExpr) bool {
	o, ok := other.(ArchExpr)
	return ok && a.Arch == o.Arch
}

// UseExpr represents a Gentoo USE flag condition predicate, e.g. Use(extended).
type UseExpr struct {
	Flag string
}

func (u UseExpr) isConditionExpr() {}

func (u UseExpr) String() string {
	return fmt.Sprintf("Use(%s)", u.Flag)
}

func (u UseExpr) Shell() string {
	return "use " + u.Flag
}

func (u UseExpr) Equals(other conditionExpr) bool {
	o, ok := other.(UseExpr)
	return ok && u.Flag == o.Flag
}

// NotExpr represents a boolean NOT operation, e.g. NOT(Use(extended)).
type NotExpr struct {
	Expr conditionExpr
}

func (n NotExpr) isConditionExpr() {}

func (n NotExpr) String() string {
	if n.Expr == nil {
		return "NOT()"
	}
	return fmt.Sprintf("NOT(%s)", n.Expr.String())
}

func (n NotExpr) Shell() string {
	if n.Expr == nil {
		return ""
	}
	switch inner := n.Expr.(type) {
	case TrueExpr:
		return "false"
	case FalseExpr:
		return "true"
	case ArchExpr:
		return "! use " + inner.Arch
	case UseExpr:
		return "! use " + inner.Flag
	case NotExpr:
		return inner.Expr.Shell()
	default:
		return "! { " + n.Expr.Shell() + "; }"
	}
}

func (n NotExpr) Equals(other conditionExpr) bool {
	o, ok := other.(NotExpr)
	if !ok {
		return false
	}
	if (n.Expr == nil) != (o.Expr == nil) {
		return false
	}
	if n.Expr != nil && !n.Expr.Equals(o.Expr) {
		return false
	}
	return true
}

// AndExpr represents a boolean AND operation, e.g. AND(Arch(amd64), Use(extended)).
type AndExpr struct {
	Exprs []conditionExpr
}

func (a AndExpr) isConditionExpr() {}

func (a AndExpr) String() string {
	var parts []string
	for _, expr := range a.Exprs {
		parts = append(parts, expr.String())
	}
	return fmt.Sprintf("AND(%s)", strings.Join(parts, ", "))
}

func (a AndExpr) Shell() string {
	var parts []string
	for _, expr := range a.Exprs {
		if _, isOr := expr.(OrExpr); isOr {
			parts = append(parts, "{ "+expr.Shell()+"; }")
		} else {
			parts = append(parts, expr.Shell())
		}
	}
	return strings.Join(parts, " && ")
}

func (a AndExpr) Equals(other conditionExpr) bool {
	o, ok := other.(AndExpr)
	if !ok || len(a.Exprs) != len(o.Exprs) {
		return false
	}
	for i, e := range a.Exprs {
		if !e.Equals(o.Exprs[i]) {
			return false
		}
	}
	return true
}

// OrExpr represents a boolean OR operation, e.g. OR(Arch(amd64), Arch(arm64)).
type OrExpr struct {
	Exprs []conditionExpr
}

func (o OrExpr) isConditionExpr() {}

func (o OrExpr) String() string {
	var parts []string
	for _, expr := range o.Exprs {
		parts = append(parts, expr.String())
	}
	return fmt.Sprintf("OR(%s)", strings.Join(parts, ", "))
}

func (o OrExpr) Shell() string {
	var parts []string
	for _, expr := range o.Exprs {
		if _, isAnd := expr.(AndExpr); isAnd {
			parts = append(parts, "{ "+expr.Shell()+"; }")
		} else {
			parts = append(parts, expr.Shell())
		}
	}
	return strings.Join(parts, " || ")
}

func (o OrExpr) Equals(other conditionExpr) bool {
	otherOr, ok := other.(OrExpr)
	if !ok || len(o.Exprs) != len(otherOr.Exprs) {
		return false
	}
	for i, e := range o.Exprs {
		if !e.Equals(otherOr.Exprs[i]) {
			return false
		}
	}
	return true
}

// NewNotExpr creates a normalized NOT expression.
func NewNotExpr(expr conditionExpr) conditionExpr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case TrueExpr:
		return FalseExpr{}
	case FalseExpr:
		return TrueExpr{}
	case NotExpr:
		return e.Expr // NOT(NOT(x)) -> x
	default:
		return NotExpr{Expr: expr}
	}
}

// NewAndExpr creates a canonicalized, flattened, deduplicated AND expression.
func NewAndExpr(exprs ...conditionExpr) conditionExpr {
	var flattened []conditionExpr
	for _, e := range exprs {
		if e == nil {
			continue
		}
		if a, ok := e.(AndExpr); ok {
			for _, sub := range a.Exprs {
				if sub != nil {
					flattened = append(flattened, sub)
				}
			}
		} else {
			flattened = append(flattened, e)
		}
	}

	if len(flattened) == 0 {
		return nil
	}

	hasTrue := false
	var filtered []conditionExpr
	for _, e := range flattened {
		if _, isFalse := e.(FalseExpr); isFalse {
			return FalseExpr{}
		}
		if _, isTrue := e.(TrueExpr); isTrue {
			hasTrue = true
			continue
		}
		filtered = append(filtered, e)
	}

	if len(filtered) == 0 {
		if hasTrue {
			return TrueExpr{}
		}
		return nil
	}

	// Deduplicate using Equals
	var unique []conditionExpr
	for _, e := range filtered {
		if !slices.ContainsFunc(unique, func(u conditionExpr) bool {
			return e.Equals(u)
		}) {
			unique = append(unique, e)
		}
	}

	// Canonical sort
	slices.SortFunc(unique, func(a, b conditionExpr) int {
		return strings.Compare(a.String(), b.String())
	})

	if len(unique) == 1 {
		return unique[0]
	}
	return AndExpr{Exprs: unique}
}

// NewOrExpr creates a canonicalized, flattened, deduplicated OR expression.
func NewOrExpr(exprs ...conditionExpr) conditionExpr {
	var flattened []conditionExpr
	for _, e := range exprs {
		if e == nil {
			continue
		}
		if o, ok := e.(OrExpr); ok {
			for _, sub := range o.Exprs {
				if sub != nil {
					flattened = append(flattened, sub)
				}
			}
		} else {
			flattened = append(flattened, e)
		}
	}

	if len(flattened) == 0 {
		return nil
	}

	hasFalse := false
	var filtered []conditionExpr
	for _, e := range flattened {
		if _, isTrue := e.(TrueExpr); isTrue {
			return TrueExpr{}
		}
		if _, isFalse := e.(FalseExpr); isFalse {
			hasFalse = true
			continue
		}
		filtered = append(filtered, e)
	}

	if len(filtered) == 0 {
		if hasFalse {
			return FalseExpr{}
		}
		return nil
	}

	// Deduplicate using Equals
	var unique []conditionExpr
	for _, e := range filtered {
		if !slices.ContainsFunc(unique, func(u conditionExpr) bool {
			return e.Equals(u)
		}) {
			unique = append(unique, e)
		}
	}

	// Canonical sort
	slices.SortFunc(unique, func(a, b conditionExpr) int {
		return strings.Compare(a.String(), b.String())
	})

	if len(unique) == 1 {
		return unique[0]
	}
	return OrExpr{Exprs: unique}
}

func newArchsAndUseExpr(archs []string, uses []string) conditionExpr {
	var terms []conditionExpr

	if len(archs) > 0 {
		var archTerms []conditionExpr
		for _, arch := range archs {
			archTerms = append(archTerms, ArchExpr{Arch: arch})
		}
		if len(archTerms) == 1 {
			terms = append(terms, archTerms[0])
		} else {
			terms = append(terms, NewOrExpr(archTerms...))
		}
	}

	for _, use := range uses {
		if rest, ok := strings.CutPrefix(use, "!"); ok {
			terms = append(terms, NewNotExpr(UseExpr{Flag: rest}))
		} else {
			terms = append(terms, UseExpr{Flag: use})
		}
	}

	if len(terms) == 0 {
		return nil
	}
	if len(terms) == 1 {
		return terms[0]
	}
	return NewAndExpr(terms...)
}

func isUniversalArchExpr(expr conditionExpr, universe []string) bool {
	if len(universe) == 0 || expr == nil {
		return false
	}
	switch e := expr.(type) {
	case ArchExpr:
		return len(universe) == 1 && universe[0] == e.Arch
	case OrExpr:
		var archs []string
		for _, term := range e.Exprs {
			if arch, ok := term.(ArchExpr); ok {
				archs = append(archs, arch.Arch)
			} else {
				return false
			}
		}
		for _, u := range universe {
			if !slices.Contains(archs, u) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// simplifyExpr recursively simplifies universal architecture predicates anywhere in an expression tree.
func simplifyExpr(expr conditionExpr, universe []string) conditionExpr {
	if expr == nil {
		return nil
	}
	if len(universe) == 0 {
		return expr
	}
	if isUniversalArchExpr(expr, universe) {
		return TrueExpr{}
	}
	switch e := expr.(type) {
	case TrueExpr, FalseExpr:
		return e
	case ArchExpr:
		if len(universe) == 1 && universe[0] == e.Arch {
			return TrueExpr{}
		}
		return e
	case NotExpr:
		simplified := simplifyExpr(e.Expr, universe)
		if simplified == nil {
			return nil
		}
		return NewNotExpr(simplified)
	case AndExpr:
		var terms []conditionExpr
		for _, sub := range e.Exprs {
			s := simplifyExpr(sub, universe)
			if s != nil {
				terms = append(terms, s)
			}
		}
		return NewAndExpr(terms...)
	case OrExpr:
		// Check if the arch terms in OrExpr cover the universe
		var archs []string
		for _, sub := range e.Exprs {
			if arch, ok := sub.(ArchExpr); ok {
				archs = append(archs, arch.Arch)
			}
		}
		if len(archs) > 0 && len(universe) > 0 {
			coversAll := true
			for _, u := range universe {
				if !slices.Contains(archs, u) {
					coversAll = false
					break
				}
			}
			if coversAll {
				return TrueExpr{}
			}
		}

		var terms []conditionExpr
		for _, sub := range e.Exprs {
			if isUniversalArchExpr(sub, universe) {
				return TrueExpr{}
			}
			s := simplifyExpr(sub, universe)
			if s != nil {
				terms = append(terms, s)
			}
		}
		return NewOrExpr(terms...)
	default:
		return expr
	}
}

type installPlan struct {
	UniverseArchitectures []string
	Body                  []installStmt
}

func (p *installPlan) String(indent string) string {
	if p == nil {
		return ""
	}
	return formatStmts(p.Body, indent)
}

func (p *installPlan) reducePlan() *installPlan {
	if p == nil {
		return nil
	}
	current := p
	const maxIterations = 100
	for range maxIterations {
		initialState := InitialState()
		next := &installPlan{
			UniverseArchitectures: current.UniverseArchitectures,
			Body:                  reduceStmts(current.Body, current.UniverseArchitectures, initialState),
		}
		if planEqual(current, next) {
			return next
		}
		current = next
	}
	panic(fmt.Sprintf("reducer failed to converge after %d iterations", maxIterations))
}

func (p *installPlan) Validate() error {
	if p == nil {
		return nil
	}
	for _, stmt := range p.Body {
		if err := stmt.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func planEqual(a, b *installPlan) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !slices.Equal(a.UniverseArchitectures, b.UniverseArchitectures) {
		return false
	}
	if len(a.Body) != len(b.Body) {
		return false
	}
	for i := range a.Body {
		if !a.Body[i].Equals(b.Body[i]) {
			return false
		}
	}
	return true
}

func reduceStmts(stmts []installStmt, universe []string, state map[StateFamily]string) []installStmt {
	var reduced []installStmt

	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case conditionStmt:
			simplifiedExpr := simplifyExpr(s.Expr, universe)
			if simplifiedExpr == nil || simplifiedExpr.Equals(TrueExpr{}) {
				sBody := reduceStmts(s.Body, universe, state)
				reduced = append(reduced, sBody...)
				continue
			}
			if simplifiedExpr.Equals(FalseExpr{}) {
				continue
			}
			s.Expr = simplifiedExpr

			stateBefore := make(map[StateFamily]string)
			maps.Copy(stateBefore, state)

			branchState := make(map[StateFamily]string)
			maps.Copy(branchState, stateBefore)

			reducedBody := reduceStmts(s.Body, universe, branchState)
			if len(reducedBody) == 0 {
				continue
			}

			s.Body = reducedBody
			reduced = append(reduced, s)

			// Join state: any family whose value diverged in the branch becomes uncertain
			for k, vBefore := range stateBefore {
				vBranch, ok := branchState[k]
				if !ok || vBranch != vBefore {
					delete(state, k)
				}
			}
			for k := range branchState {
				if _, ok := stateBefore[k]; !ok {
					delete(state, k)
				}
			}

		case stateStmt:
			desc := s.Family.Descriptor()
			if val, ok := state[s.Family]; ok && val == s.Value {
				continue // redundant
			}
			if _, ok := state[s.Family]; !ok && desc.HasKnownInitialState && desc.DefaultState == s.Value {
				state[s.Family] = s.Value
				continue // redundant with Portage initial state
			}
			state[s.Family] = s.Value
			reduced = append(reduced, s)

		case actionStmt:
			if s.RequiredState.Family != StateFamilyNone && s.RequiredState.Value != "" {
				fam := s.RequiredState.Family
				val := s.RequiredState.Value
				currentVal, isSet := state[fam]
				if !isSet || currentVal != val {
					// State is not set to what this action requires. Emit state setter!
					reduced = append(reduced, stateStmt{
						Family: fam,
						Value:  val,
					})
					state[fam] = val
				}
			} else if s.RequiredState.Family == StateFamilyDoc && s.RequiredState.Value == "" {
				// docinto default reset
				currentVal, isSet := state[StateFamilyDoc]
				if isSet && currentVal != "" {
					reduced = append(reduced, stateStmt{
						Family: StateFamilyDoc,
						Value:  "",
					})
					state[StateFamilyDoc] = ""
				}
			}
			reduced = append(reduced, s)

		default:
			reduced = append(reduced, s)
		}
	}

	// Merge and factor sibling conditions
	reduced = mergeAndFactorSiblingConditions(reduced)

	return reduced
}

func mergeAndFactorSiblingConditions(stmts []installStmt) []installStmt {
	var result []installStmt
	i := 0
	for i < len(stmts) {
		_, isCond := stmts[i].(conditionStmt)
		if !isCond {
			if raw, isRaw := stmts[i].(rawStmt); isRaw && raw.Content == "" {
				i++
				continue
			}
			result = append(result, stmts[i])
			i++
			continue
		}

		// Collect contiguous conditionStmts
		var conds []conditionStmt
		j := i
		for j < len(stmts) {
			c, ok := stmts[j].(conditionStmt)
			if !ok {
				break
			}
			conds = append(conds, c)
			j++
		}

		factored := factorConditionsSlice(conds)
		result = append(result, factored...)
		i = j
	}
	return result
}

func factorConditionsSlice(conds []conditionStmt) []installStmt {
	if len(conds) == 0 {
		return nil
	}
	if len(conds) == 1 {
		return []installStmt{conds[0]}
	}

	var result []installStmt
	i := 0
	for i < len(conds) {
		bestJ := i
		var bestFactor conditionExpr

		for j := len(conds) - 1; j > i; j-- {
			factors := findCommonFactors(conds[i : j+1])
			if len(factors) > 0 {
				bestJ = j
				bestFactor = selectBestFactor(factors)
				break
			}
		}

		if bestJ > i && bestFactor != nil {
			var innerBody []installStmt
			for k := i; k <= bestJ; k++ {
				rem := removeTerm(conds[k].Expr, bestFactor)
				if rem == nil || rem.Equals(TrueExpr{}) {
					innerBody = append(innerBody, conds[k].Body...)
				} else {
					innerBody = append(innerBody, conditionStmt{
						Expr: rem,
						Body: conds[k].Body,
					})
				}
			}
			factoredCond := conditionStmt{
				Expr: bestFactor,
				Body: mergeAndFactorSiblingConditions(innerBody),
			}
			result = append(result, factoredCond)
			i = bestJ + 1
		} else {
			result = append(result, conds[i])
			i++
		}
	}
	return result
}

func getTerms(expr conditionExpr) []conditionExpr {
	if expr == nil {
		return nil
	}
	if and, ok := expr.(AndExpr); ok {
		return and.Exprs
	}
	return []conditionExpr{expr}
}

func findCommonFactors(slice []conditionStmt) []conditionExpr {
	if len(slice) == 0 {
		return nil
	}
	common := getTerms(slice[0].Expr)
	for _, c := range slice[1:] {
		cTerms := getTerms(c.Expr)
		var nextCommon []conditionExpr
		for _, f := range common {
			if slices.ContainsFunc(cTerms, func(ct conditionExpr) bool {
				return f.Equals(ct)
			}) {
				nextCommon = append(nextCommon, f)
			}
		}
		common = nextCommon
		if len(common) == 0 {
			break
		}
	}
	return common
}

func selectBestFactor(factors []conditionExpr) conditionExpr {
	if len(factors) == 0 {
		return nil
	}
	slices.SortFunc(factors, func(a, b conditionExpr) int {
		pA := factorPriority(a)
		pB := factorPriority(b)
		if pA != pB {
			return pA - pB
		}
		return strings.Compare(a.String(), b.String())
	})
	return factors[0]
}

func factorPriority(e conditionExpr) int {
	switch e.(type) {
	case ArchExpr, OrExpr:
		return 1
	case UseExpr:
		return 2
	case NotExpr:
		return 3
	default:
		return 4
	}
}

func removeTerm(expr conditionExpr, term conditionExpr) conditionExpr {
	if expr == nil || term == nil {
		return expr
	}
	if expr.Equals(term) {
		return TrueExpr{}
	}
	if and, ok := expr.(AndExpr); ok {
		var remaining []conditionExpr
		for _, t := range and.Exprs {
			if !t.Equals(term) {
				remaining = append(remaining, t)
			}
		}
		if len(remaining) == 0 {
			return TrueExpr{}
		}
		if len(remaining) == 1 {
			return remaining[0]
		}
		return NewAndExpr(remaining...)
	}
	return expr
}

func formatStmts(stmts []installStmt, indent string) string {
	var sb strings.Builder
	for _, stmt := range stmts {
		sb.WriteString(stmt.String(indent))
	}
	return strings.TrimRight(sb.String(), "\n")
}
