package gentoo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGentooVersionValueOperations(t *testing.T) {
	version, err := ParseGentooVersion("1.2.3_rc1-r2")
	require.NoError(t, err)
	require.Equal(t, "1.2.3_rc1-r2", version.String())
	require.Equal(t, 2, version.Revision())
	require.Equal(t, "1.2.3_rc1", version.WithoutRevision().String())
	require.Equal(t, "1.2.3_rc1-r10", version.WithRevision(10).String())
	require.Equal(t, "rc", version.Bucket())
}

func TestGentooVersionFromRelease(t *testing.T) {
	version, err := GentooVersionFromRelease("1.2.3-beta2", "gentoo-version")
	require.NoError(t, err)
	require.Equal(t, "1.2.3_beta2", version.String())
	_, err = GentooVersionFromRelease("1.0.0-nightly", "gentoo-version")
	require.Error(t, err)
}
