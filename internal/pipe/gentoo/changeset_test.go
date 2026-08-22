package gentoo

import (
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/stretchr/testify/require"
)

func TestChangeSetReplacesPathsAndReturnsCopies(t *testing.T) {
	changes := NewChangeSet()
	changes.Write("file", []byte("one"))
	changes.Write("file", []byte("two"))
	file, ok := changes.Find("file")
	require.True(t, ok)
	require.Equal(t, []byte("two"), file.Content)
	require.Len(t, changes.Files(), 1)

	files := changes.Files()
	files[0].Path = "mutated"
	files[0].Content[0] = 'x'
	require.True(t, changes.Contains("file"))
	require.False(t, changes.Contains("mutated"))
	file, ok = changes.Find("file")
	require.True(t, ok)
	require.Equal(t, []byte("two"), file.Content)
}

func TestChangeSetDeleteRenameAndRemove(t *testing.T) {
	changes := NewChangeSet(client.RepoFile{Path: "old", Content: []byte("content")})
	changes.Rename("old", "new", []byte("replacement"))
	require.False(t, changes.Contains("old"))
	require.True(t, changes.Contains("new"))
	changes.Delete("new")
	file, ok := changes.Find("new")
	require.True(t, ok)
	require.True(t, file.Delete)
	changes.Remove("new")
	require.False(t, changes.HasChanges())
}
