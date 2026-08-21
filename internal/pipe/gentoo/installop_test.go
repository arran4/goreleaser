package gentoo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpDescriptorDecomposesDestinationFamilies(t *testing.T) {
	tests := []struct {
		name       string
		op         InstallOp
		source     string
		dest       string
		defaultDir string
		family     StateFamily
		dir        string
		base       string
	}{
		{name: "bin", op: OpDobin, source: "foo", dest: "/usr/bin/bar", family: StateFamilyBin, dir: "/usr", base: "bar"},
		{name: "sbin", op: OpDosbin, source: "foo", dest: "/opt/sbin/foo", family: StateFamilyBin, dir: "/opt", base: "foo"},
		{name: "exe", op: OpDoexe, source: "foo", dest: "/opt/libexec/bar", defaultDir: "/opt/bin", family: StateFamilyExe, dir: "/opt/libexec", base: "bar"},
		{name: "ins", op: OpDoins, source: "foo", dest: "/etc/foo/config", family: StateFamilyIns, dir: "/etc/foo", base: "config"},
		{name: "doc", op: OpDodoc, source: "README", dest: "guide/README", family: StateFamilyDoc, dir: "guide", base: "README"},
		{name: "fixed", op: OpDoinitd, source: "foo.init", dest: "/etc/init.d/foo", dir: "/etc/init.d", base: "foo"},
		{name: "man", op: OpDoman, source: "foo.1", dest: "/usr/share/man/man1/bar.1", base: "bar.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			family, dir, base, err := tt.op.Descriptor().DecomposeDestination(tt.source, tt.dest, tt.defaultDir)
			require.NoError(t, err)
			require.Equal(t, tt.family, family)
			require.Equal(t, tt.dir, dir)
			require.Equal(t, tt.base, base)
		})
	}
}

func TestOpDescriptorRejectsIncompatibleFixedDestination(t *testing.T) {
	_, _, _, err := OpDoinitd.Descriptor().DecomposeDestination("foo.init", "/tmp/foo", "")
	require.ErrorContains(t, err, "expected /etc/init.d/<name>")
}

func TestInstallOpDescriptorTableIsCompleteAndConsistent(t *testing.T) {
	known := map[InstallOp]struct{}{}
	for _, op := range allInstallOps {
		_, duplicate := known[op]
		require.False(t, duplicate, "allInstallOps contains %q more than once", op)
		known[op] = struct{}{}

		descriptor, ok := installOpDescriptors[op]
		require.True(t, ok, "%q has no descriptor", op)
		require.Equal(t, op, descriptor.Op)
		require.Equal(t, descriptor.ArgMode == ArgModeRename, descriptor.IsRename, "%q has inconsistent rename markers", op)
		if !descriptor.SupportsRename {
			continue
		}
		_, renameKnown := installOpDescriptors[descriptor.RenameOp]
		require.True(t, renameKnown, "%q references unknown rename operation %q", op, descriptor.RenameOp)
		rename := descriptor.RenameOp.Descriptor()
		require.True(t, rename.IsRename)
		require.Equal(t, ArgModeRename, rename.ArgMode)
	}
	require.Len(t, installOpDescriptors, len(allInstallOps), "descriptor table and allInstallOps must describe the same operation set")
}
