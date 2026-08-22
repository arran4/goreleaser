package gentoo

import "github.com/goreleaser/goreleaser/v2/internal/client"

// ChangeSet is a publication plan that can be inspected before any provider
// mutation occurs.
type ChangeSet struct {
	files []client.RepoFile
}

func NewChangeSet(files ...client.RepoFile) *ChangeSet {
	changes := &ChangeSet{}
	for _, file := range files {
		changes.Add(file)
	}
	return changes
}

func (c *ChangeSet) Write(path string, content []byte) {
	c.Replace(client.RepoFile{Path: path, Content: content})
}

func (c *ChangeSet) Delete(path string) {
	c.Replace(client.RepoFile{Path: path, Delete: true})
}

func (c *ChangeSet) Rename(oldPath, newPath string, content []byte) {
	c.Remove(oldPath)
	c.Write(newPath, content)
}

func (c *ChangeSet) Add(file client.RepoFile) { c.Replace(file) }

func (c *ChangeSet) Replace(file client.RepoFile) {
	c.Remove(file.Path)
	c.files = append(c.files, cloneRepoFile(file))
}

func (c *ChangeSet) Remove(path string) {
	result := c.files[:0]
	for _, file := range c.files {
		if file.Path != path {
			result = append(result, file)
		}
	}
	c.files = result
}

func (c *ChangeSet) Find(path string) (client.RepoFile, bool) {
	for _, file := range c.files {
		if file.Path == path {
			return cloneRepoFile(file), true
		}
	}
	return client.RepoFile{}, false
}

func (c *ChangeSet) Contains(path string) bool {
	_, ok := c.Find(path)
	return ok
}

func (c *ChangeSet) Clone() *ChangeSet { return NewChangeSet(c.Files()...) }

func (c *ChangeSet) Files() []client.RepoFile {
	result := make([]client.RepoFile, 0, len(c.files))
	for _, file := range c.files {
		result = append(result, cloneRepoFile(file))
	}
	return result
}

func (c *ChangeSet) HasChanges() bool { return len(c.files) > 0 }

func cloneRepoFile(file client.RepoFile) client.RepoFile {
	file.Content = append([]byte(nil), file.Content...)
	return file
}
