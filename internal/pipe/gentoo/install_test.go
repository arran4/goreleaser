package gentoo

import (
	"strings"
	"testing"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func plannerRelease(archives ...*Archive) *Release { return &Release{archives: archives} }

func TestInstallPlannerArchSpecificClaimPreservesOtherArchitecture(t *testing.T) {
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Bindir: "/opt/bin", Doexe: []config.GentooInstallItem{{SrcID: "default", Archs: []string{"amd64"}}}}}
	release := plannerRelease(
		&Archive{id: "default", gentooArch: "amd64", binaries: []string{"foo"}},
		&Archive{id: "default", gentooArch: "arm64", binaries: []string{"foo"}},
	)
	program, err := NewInstallPlanner(cfg, release, NewExtraFiles(cfg, release, nil), "").Plan()
	require.NoError(t, err)
	script := program.String("")
	require.Contains(t, script, "amd64")
	require.Contains(t, script, "arm64")
	require.Equal(t, 2, strings.Count(script, `doexe "foo"`))
}

func TestInstallPlannerMultiBinarySrcID(t *testing.T) {
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Bindir: "/opt/bin", Doexe: []config.GentooInstallItem{{SrcID: "default"}}}}
	release := plannerRelease(&Archive{id: "default", gentooArch: "amd64", binaries: []string{"foo", "bar"}})
	program, err := NewInstallPlanner(cfg, release, NewExtraFiles(cfg, release, nil), "").Plan()
	require.NoError(t, err)
	require.Contains(t, program.String(""), `doexe "bar"`)
	require.Contains(t, program.String(""), `doexe "foo"`)
}

func TestInstallPlannerExplicitSourceAllowsDifferentBinaryLists(t *testing.T) {
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Doins: []config.GentooInstallItem{{SrcID: "default", Src: "config.yaml", Dst: "/etc/foo/config.yaml"}}}}
	release := plannerRelease(
		&Archive{id: "default", gentooArch: "amd64", binaries: []string{"foo-amd64"}},
		&Archive{id: "default", gentooArch: "arm64", binaries: []string{"foo-arm64"}},
	)
	_, err := NewInstallPlanner(cfg, release, NewExtraFiles(cfg, release, nil), "").Plan()
	require.NoError(t, err)
}

func TestBinaryClaimsAreArchitectureAndBinarySpecific(t *testing.T) {
	claims := NewBinaryClaims()
	claims.Claim(BinaryKey{ArchiveID: "default", Arch: "amd64", Binary: "foo"})
	require.True(t, claims.IsClaimed(BinaryKey{ArchiveID: "default", Arch: "amd64", Binary: "foo"}))
	require.False(t, claims.IsClaimed(BinaryKey{ArchiveID: "default", Arch: "arm64", Binary: "foo"}))
	require.False(t, claims.IsClaimed(BinaryKey{ArchiveID: "default", Arch: "amd64", Binary: "bar"}))
}

func TestInstallPlannerRejectsUnknownOrMissingSrcIDArchitecture(t *testing.T) {
	tests := []struct {
		name string
		item config.GentooInstallItem
		want string
	}{
		{name: "unknown id", item: config.GentooInstallItem{SrcID: "missing"}, want: `src_id "missing" does not match a selected archive`},
		{name: "missing architecture", item: config.GentooInstallItem{SrcID: "default", Archs: []string{"arm64"}}, want: `does not match a selected archive for archs [arm64]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Doexe: []config.GentooInstallItem{tt.item}}}
			release := plannerRelease(&Archive{id: "default", gentooArch: "amd64", binaries: []string{"foo"}})
			_, err := NewInstallPlanner(cfg, release, NewExtraFiles(cfg, release, nil), "").Plan()
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestInstallPlannerRejectsMultiBinaryDestination(t *testing.T) {
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Doexe: []config.GentooInstallItem{{SrcID: "default", Dst: "renamed"}}}}
	release := plannerRelease(&Archive{id: "default", gentooArch: "amd64", binaries: []string{"foo", "bar"}})
	_, err := NewInstallPlanner(cfg, release, NewExtraFiles(cfg, release, nil), "").Plan()
	require.ErrorContains(t, err, "cannot be used with multiple binaries")
}

func TestInstallPlannerWrappedArchiveAndLiteralFiledirSource(t *testing.T) {
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Bindir: "/opt/bin", Doexe: []config.GentooInstallItem{
		{SrcID: "default", Src: "bin/foo"},
		{Src: "foo-helper"},
	}}}
	release := plannerRelease(&Archive{id: "default", gentooArch: "amd64", wrappedIn: "release", binaries: []string{"foo"}})
	extras := NewExtraFiles(cfg, release, map[string]string{"foo-helper": "unused"})
	program, err := NewInstallPlanner(cfg, release, extras, "").Plan()
	require.NoError(t, err)
	script := program.String("")
	require.Contains(t, script, `"release/bin/foo"`)
	require.Contains(t, script, `"${FILESDIR}/foo-helper"`)
}

func TestInstallPlannerRejectsDifferingArchiveWrappers(t *testing.T) {
	cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Doins: []config.GentooInstallItem{{SrcID: "default", Src: "config.yaml"}}}}
	release := plannerRelease(
		&Archive{id: "default", gentooArch: "amd64", wrappedIn: "amd", binaries: []string{"foo"}},
		&Archive{id: "default", gentooArch: "arm64", wrappedIn: "arm", binaries: []string{"foo"}},
	)
	_, err := NewInstallPlanner(cfg, release, NewExtraFiles(cfg, release, nil), "").Plan()
	require.ErrorContains(t, err, "mismatched archive layouts")
}

func TestInstallPlannerSystemdEclassAndFallback(t *testing.T) {
	tests := []struct {
		name     string
		eclasses []string
		want     string
	}{
		{name: "eclass", eclasses: []string{"systemd"}, want: `systemd_dounit "foo.service"`},
		{name: "fallback", want: `insinto /usr/lib/systemd/system`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &GentooConfig{raw: config.Gentoo{Name: "foo", Eclasses: tt.eclasses, Systemd: []config.GentooInstallItem{{Src: "foo.service"}}}}
			release := plannerRelease(&Archive{id: "default", gentooArch: "amd64"})
			program, err := NewInstallPlanner(cfg, release, NewExtraFiles(cfg, release, nil), "").Plan()
			require.NoError(t, err)
			require.Contains(t, program.String(""), tt.want)
		})
	}
}

func TestInstallPlannerConditionsDirectoriesDocsAndManpages(t *testing.T) {
	cfg := &GentooConfig{raw: config.Gentoo{
		Name: "foo", Dodir: []string{"/var/lib/foo"}, Dodoc: []string{"README"}, Doman: []string{"foo.1"},
		Doins: []config.GentooInstallItem{{Src: "config", Use: []string{"feature"}, Archs: []string{"amd64"}}},
	}}
	release := plannerRelease(
		&Archive{id: "default", gentooArch: "amd64"},
		&Archive{id: "default", gentooArch: "arm64"},
	)
	program, err := NewInstallPlanner(cfg, release, NewExtraFiles(cfg, release, nil), "").Plan()
	require.NoError(t, err)
	script := program.String("")
	for _, expected := range []string{`dodir "/var/lib/foo"`, `feature`, `amd64`, `doman "foo.1"`, `dodoc "README"`} {
		require.Contains(t, script, expected)
	}
	require.NoError(t, program.Validate())
	require.Equal(t, script, program.Reduce().String(""))
}

func TestInstallProgramValidationUsesReducedProgram(t *testing.T) {
	program := &InstallProgram{Body: []installStmt{conditionStmt{
		Expr: FalseExpr{},
		Body: []installStmt{actionStmt{Op: OpDosym, Source: "invalid-without-destination"}},
	}}}
	require.Error(t, program.Validate(), "the unreduced unreachable action is invalid")
	require.NoError(t, program.Reduce().Validate(), "the reduced program returned by InstallPlanner is valid")
}
