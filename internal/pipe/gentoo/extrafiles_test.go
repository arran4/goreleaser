package gentoo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestExtraFilesRejectsTraversal(t *testing.T) {
	file := filepath.Join(t.TempDir(), "foo")
	require.NoError(t, os.WriteFile(file, []byte("content"), 0o644))
	cfg := &GentooConfig{raw: config.Gentoo{}}
	err := NewExtraFiles(cfg, &Release{}, map[string]string{"../foo": file}).Prepare()
	require.ErrorContains(t, err, "must remain within the files directory")
}

func TestExtraFilesMapsFileDirSources(t *testing.T) {
	extra := NewExtraFiles(&GentooConfig{}, &Release{}, map[string]string{"files/foo.conf": "source"})
	require.Equal(t, "${FILESDIR}/foo.conf", extra.EbuildSource("files/foo.conf"))
	require.Equal(t, "literal.conf", extra.EbuildSource("literal.conf"))
}
