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
	tests := []struct {
		input string
		want  string
	}{
		{input: "1.2.3-rc1", want: "1.2.3_rc1"},
		{input: "1.2.3-beta2", want: "1.2.3_beta2"},
	}
	for _, tt := range tests {
		version, err := GentooVersionFromRelease(tt.input, "gentoo-version")
		require.NoError(t, err)
		require.Equal(t, tt.want, version.String())
	}
	for _, invalid := range []string{"1.0.0-dev.1", "1.0.0-preview.2", "1.0.0-nightly"} {
		_, err := GentooVersionFromRelease(invalid, "gentoo-version")
		require.Error(t, err)
	}
}

func TestGentooVersionPMSOrdering(t *testing.T) {
	ordered := []string{
		"1_alpha", "1_alpha1", "1_beta", "1_pre", "1_rc", "1", "1_p1",
	}
	for index := 0; index < len(ordered)-1; index++ {
		left, err := ParseGentooVersion(ordered[index])
		require.NoError(t, err)
		right, err := ParseGentooVersion(ordered[index+1])
		require.NoError(t, err)
		require.Negative(t, left.Compare(right), "%s should precede %s", left, right)
	}
	revisions := []string{"1-r1", "1-r2", "1-r10"}
	for index := 0; index < len(revisions)-1; index++ {
		left, err := ParseGentooVersion(revisions[index])
		require.NoError(t, err)
		right, err := ParseGentooVersion(revisions[index+1])
		require.NoError(t, err)
		require.Negative(t, left.Compare(right))
	}
}

func TestGentooVersionBaseComponentsAndChainedSuffixes(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "1", right: "1.0", want: -1},
		{left: "1.01", right: "1.1", want: -1},
		{left: "1.0", right: "1.0a", want: -1},
		{left: "1_alpha1_p1", right: "1_alpha1_p2", want: -1},
		{left: "1.0", right: "1.0", want: 0},
	}
	for _, tt := range tests {
		left, err := ParseGentooVersion(tt.left)
		require.NoError(t, err)
		right, err := ParseGentooVersion(tt.right)
		require.NoError(t, err)
		require.Equal(t, tt.want, left.Compare(right), "%s vs %s", tt.left, tt.right)
	}
}

func TestGentooVersionBuckets(t *testing.T) {
	for version, bucket := range map[string]string{
		"1_alpha1": "alpha", "1_beta2": "beta", "1_pre3": "pre", "1_rc1": "rc", "1": "stable", "1_p1": "stable",
	} {
		parsed, err := ParseGentooVersion(version)
		require.NoError(t, err)
		require.Equal(t, bucket, parsed.Bucket())
	}
}
