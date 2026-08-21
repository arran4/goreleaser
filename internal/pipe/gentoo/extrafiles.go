package gentoo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/caarlos0/log"
)

// ExtraFiles owns validation and AUX-file path mapping. Archive selection and
// install-command lowering are deliberately outside this component.
type ExtraFiles struct {
	cfg     *GentooConfig
	release *Release
	files   map[string]string
}

func NewExtraFiles(cfg *GentooConfig, release *Release, files map[string]string) *ExtraFiles {
	return &ExtraFiles{cfg: cfg, release: release, files: files}
}

func (f *ExtraFiles) Prepare() error {
	for name, source := range f.files {
		if f.inEveryArchive(name) {
			log.Warnf("file %s is already in all archives, skipping upload to Gentoo files/ directory", name)
			delete(f.files, name)
			continue
		}
		if err := f.validate(name, source); err != nil {
			return err
		}
	}
	return nil
}

func (f *ExtraFiles) inEveryArchive(name string) bool {
	archives := f.release.Archives()
	if len(archives) == 0 {
		return false
	}
	for _, archive := range archives {
		if !archive.Contains(name) {
			return false
		}
	}
	return true
}

func (f *ExtraFiles) validate(name, source string) error {
	if _, err := gentooExtraFilePath(name); err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("failed to stat extra file %s: %w", name, err)
	}
	if f.cfg.SkipFilesValidation() {
		return nil
	}
	if info.Size() > 20*1024 {
		return fmt.Errorf("extra file %s is larger than 20KB. Gentoo policy forbids large files in the files/ directory. Please add it to a release asset instead", name)
	}
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("failed to open extra file %s: %w", name, err)
	}
	defer file.Close()
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to read extra file %s: %w", name, err)
	}
	if bytes.IndexByte(buffer[:n], 0) != -1 {
		return fmt.Errorf("extra file %s appears to be a binary file. Gentoo policy forbids binary files in the files/ directory", name)
	}
	return nil
}

func (f *ExtraFiles) Contains(name string) bool {
	_, ok := f.files[name]
	return ok
}

func (f *ExtraFiles) EbuildSource(name string) string {
	if !f.Contains(name) {
		return name
	}
	return "${FILESDIR}/" + strings.TrimPrefix(name, "files/")
}

func (f *ExtraFiles) ResolveAll(names []string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, f.EbuildSource(name))
	}
	return result
}

func (f *ExtraFiles) Write(ebuildPath string) ([]GeneratedFile, error) {
	var generated []GeneratedFile
	for name, source := range f.files {
		relativePath, err := gentooExtraFilePath(name)
		if err != nil {
			return nil, err
		}
		destination := filepath.Join(filepath.Dir(ebuildPath), relativePath)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return nil, err
		}
		if err := copyFile(source, destination); err != nil {
			return nil, err
		}
		generated = append(generated, GeneratedFile{
			ConfigID: f.cfg.ID(),
			RepoPath: path.Join(f.cfg.PackageDir(), filepath.ToSlash(relativePath)),
			Kind:     GeneratedAux,
			Path:     destination,
		})
	}
	return generated, nil
}

func gentooExtraFilePath(name string) (string, error) {
	value := filepath.ToSlash(name)
	value = strings.TrimPrefix(value, "files/")
	if value == "" || path.IsAbs(value) || value != path.Clean(value) || strings.HasPrefix(value, "../") || value == ".." {
		return "", fmt.Errorf("extra file name %q must remain within the files directory", name)
	}
	return path.Join("files", value), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = out.ReadFrom(in)
	return err
}
