package gentoo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestParseManipulateRender(t *testing.T) {
	manifest, err := ParseManifest([]byte("DIST foo.tar.gz 3 SHA256 abc\nAUX systemd/foo.service 2 SHA512 def\n"))
	require.NoError(t, err)
	manifest.RemoveDist("foo.tar.gz")
	manifest.ReplaceDist(ManifestRecord{Name: "bar.tar.gz", Size: 4, Hashes: map[string]string{"SHA256": "123"}})
	manifest.ReplacePackageFile(ManifestRecord{Type: "EBUILD", Name: "foo-1.ebuild", Size: 5, Hashes: map[string]string{"SHA512": "456"}})
	require.Equal(t, "AUX systemd/foo.service 2 SHA512 def\nDIST bar.tar.gz 4 SHA256 123\nEBUILD foo-1.ebuild 5 SHA512 456\n", string(manifest.Render()))
}

func TestManifestHasher(t *testing.T) {
	record, err := NewManifestHasher([]string{"BLAKE2B", "SHA512", "SHA256"}).HashBytes("AUX", "nested/foo.conf", []byte("content"))
	require.NoError(t, err)
	require.EqualValues(t, 7, record.Size)
	require.Len(t, record.Hashes["BLAKE2B"], 128)
	require.Len(t, record.Hashes["SHA512"], 128)
	require.Len(t, record.Hashes["SHA256"], 64)
}

func TestManifestRejectsUnsupportedHash(t *testing.T) {
	_, err := NewManifestHasher([]string{"MD5"}).HashBytes("DIST", "foo", []byte("content"))
	require.EqualError(t, err, "unsupported manifest hash algorithm: MD5")
}
