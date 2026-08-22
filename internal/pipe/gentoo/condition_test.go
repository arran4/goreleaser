package gentoo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConditionConstructorsNormalizeAndRender(t *testing.T) {
	amd64 := ArchExpr{Arch: "amd64"}
	feature := UseExpr{Flag: "feature"}
	require.Equal(t, "use amd64 && use feature", NewAndExpr(feature, amd64, feature).Shell())
	require.Equal(t, "! use feature", NewNotExpr(feature).Shell())
	require.True(t, NewNotExpr(NewNotExpr(feature)).Equals(feature))
	require.Equal(t, "use amd64 || use feature", NewOrExpr(feature, amd64, feature).Shell())
}

func TestConditionSimplifiesUniversalAndPartialArchitectures(t *testing.T) {
	universe := []string{"amd64", "arm64"}
	all := simplifyExpr(NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"}), universe)
	require.IsType(t, TrueExpr{}, all)
	partial := simplifyExpr(ArchExpr{Arch: "amd64"}, universe)
	require.True(t, partial.Equals(ArchExpr{Arch: "amd64"}))
}
