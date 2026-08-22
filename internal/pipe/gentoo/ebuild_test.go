package gentoo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEbuildRenderConsumesReducedProgram(t *testing.T) {
	program := (&InstallProgram{Body: []installStmt{
		stateStmt{Family: StateFamilyExe, Value: "/opt/bin"},
		actionStmt{Op: OpDoexe, Source: "foo", RequiredState: StateRequirement{Family: StateFamilyExe, Value: "/opt/bin"}},
	}}).Reduce()
	ebuild := Ebuild{
		Description: "Foo", License: "MIT", Keywords: "~amd64",
		Archs: []archData{{Keyword: "amd64", URIs: []archItem{{URI: "https://example.test/foo.tar.gz", File: "foo.tar.gz"}}}},
		Plan:  program,
	}
	content, err := ebuild.Render()
	require.NoError(t, err)
	require.Contains(t, content, "amd64? ( https://example.test/foo.tar.gz -> foo.tar.gz )")
	require.Contains(t, content, "exeinto /opt/bin")
	require.Contains(t, content, `doexe "foo"`)
}

func TestEbuildMetaCacheRejectsInheritedEclasses(t *testing.T) {
	_, err := (Ebuild{Eclasses: []string{"systemd"}}).RenderMetaCache("content")
	require.EqualError(t, err, "cannot render metadata cache for ebuild with inherited eclasses")
}
