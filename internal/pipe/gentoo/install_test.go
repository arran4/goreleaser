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
