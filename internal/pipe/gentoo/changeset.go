package gentoo

import "github.com/goreleaser/goreleaser/v2/internal/client"

// ChangeSet is a publication plan that can be inspected before any provider
// mutation occurs.
type ChangeSet struct {
	files []client.RepoFile
}

func NewChangeSet(files ...client.RepoFile) *ChangeSet {
	return &ChangeSet{files: append([]client.RepoFile(nil), files...)}
}

func (c *ChangeSet) Write(path string, content []byte) {
	c.files = append(c.files, client.RepoFile{Path: path, Content: content})
}

func (c *ChangeSet) Delete(path string) {
	c.files = append(c.files, client.RepoFile{Path: path, Delete: true})
}

func (c *ChangeSet) Rename(oldPath, newPath string, content []byte) {
	c.Delete(oldPath)
	c.Write(newPath, content)
}

func (c *ChangeSet) Files() []client.RepoFile {
	return append([]client.RepoFile(nil), c.files...)
}

func (c *ChangeSet) HasChanges() bool { return len(c.files) > 0 }
