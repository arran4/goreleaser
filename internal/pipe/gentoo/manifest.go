package gentoo

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	"golang.org/x/crypto/blake2b"
)

type ManifestRecord struct {
	Type   string
	Name   string
	Size   int64
	Hashes map[string]string
}

// Manifest models package records independently of repository I/O. Thin
// manifests contain only DIST records; thick manifests additionally require
// every retained EBUILD, AUX, and MISC file to be supplied by RepositoryState.
type Manifest struct {
	records []ManifestRecord
}

func ParseManifest(content []byte) (*Manifest, error) {
	manifest := &Manifest{}
	for line := range strings.Lines(string(content)) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 3 || (len(fields)-3)%2 != 0 {
			return nil, fmt.Errorf("invalid Manifest record %q", strings.TrimSpace(line))
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid Manifest size in %q: %w", strings.TrimSpace(line), err)
		}
		record := ManifestRecord{Type: fields[0], Name: fields[1], Size: size, Hashes: map[string]string{}}
		for i := 3; i < len(fields); i += 2 {
			record.Hashes[strings.ToUpper(fields[i])] = fields[i+1]
		}
		manifest.records = append(manifest.records, record)
	}
	return manifest, nil
}

func (m *Manifest) Records() []ManifestRecord {
	result := make([]ManifestRecord, 0, len(m.records))
	for _, record := range m.records {
		result = append(result, cloneManifestRecord(record))
	}
	return result
}

func (m *Manifest) Clone() *Manifest { return &Manifest{records: m.Records()} }

func (m *Manifest) Replace(record ManifestRecord) {
	m.Remove(record.Type, record.Name)
	m.records = append(m.records, cloneManifestRecord(record))
}

func (m *Manifest) ReplaceDist(record ManifestRecord)         { record.Type = "DIST"; m.Replace(record) }
func (m *Manifest) RemoveDist(name string)                    { m.Remove("DIST", name) }
func (m *Manifest) ReplacePackageFile(record ManifestRecord)  { m.Replace(record) }
func (m *Manifest) RemovePackageFile(recordType, name string) { m.Remove(recordType, name) }

func (m *Manifest) Remove(recordType, name string) {
	result := m.records[:0]
	for _, record := range m.records {
		if record.Type == recordType && record.Name == name {
			continue
		}
		result = append(result, record)
	}
	m.records = result
}

func (m *Manifest) RemovePackageRecords() {
	result := m.records[:0]
	for _, record := range m.records {
		if record.Type == "EBUILD" || record.Type == "AUX" || record.Type == "MISC" {
			continue
		}
		result = append(result, record)
	}
	m.records = result
}

func (m *Manifest) RemoveDistfilesForVersions(versions []string) {
	result := m.records[:0]
	for _, record := range m.records {
		if record.Type != "DIST" || !distfileMatchesAnyVersion(record.Name, versions) {
			result = append(result, record)
		}
	}
	m.records = result
}

func distfileMatchesAnyVersion(filename string, versions []string) bool {
	for _, version := range versions {
		index := strings.Index(filename, version)
		if index < 0 {
			continue
		}
		if index > 0 && filename[index-1] != '_' && filename[index-1] != '-' {
			continue
		}
		end := index + len(version)
		if end == len(filename) || filename[end] == '_' || filename[end] == '-' {
			return true
		}
		if filename[end] == '.' && (end+1 == len(filename) || filename[end+1] < '0' || filename[end+1] > '9') {
			return true
		}
	}
	return false
}

func (m *Manifest) Render() []byte {
	records := m.Records()
	slices.SortFunc(records, func(a, b ManifestRecord) int {
		if c := strings.Compare(a.Type, b.Type); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	var lines []string
	for _, record := range records {
		var line strings.Builder
		fmt.Fprintf(&line, "%s %s %d", record.Type, record.Name, record.Size)
		algorithms := make([]string, 0, len(record.Hashes))
		for algorithm := range record.Hashes {
			algorithms = append(algorithms, algorithm)
		}
		slices.Sort(algorithms)
		for _, algorithm := range algorithms {
			fmt.Fprintf(&line, " %s %s", algorithm, record.Hashes[algorithm])
		}
		lines = append(lines, line.String())
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

type ManifestHasher struct {
	algorithms []string
}

func NewManifestHasher(algorithms []string) *ManifestHasher {
	normalized := make([]string, 0, len(algorithms))
	for _, algorithm := range algorithms {
		normalized = append(normalized, strings.ToUpper(algorithm))
	}
	return &ManifestHasher{algorithms: normalized}
}

func (h *ManifestHasher) HashBytes(recordType, name string, content []byte) (ManifestRecord, error) {
	return h.hash(recordType, name, int64(len(content)), bytes.NewReader(content))
}

func (h *ManifestHasher) HashFile(recordType, name, filename string) (ManifestRecord, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return ManifestRecord{}, err
	}
	file, err := os.Open(filename)
	if err != nil {
		return ManifestRecord{}, err
	}
	defer file.Close()
	return h.hash(recordType, name, info.Size(), file)
}

func (h *ManifestHasher) hash(recordType, name string, size int64, reader io.Reader) (ManifestRecord, error) {
	hashes := map[string]hash.Hash{}
	var writers []io.Writer
	for _, algorithm := range h.algorithms {
		var sum hash.Hash
		switch algorithm {
		case "BLAKE2B":
			sum, _ = blake2b.New512(nil)
		case "SHA512":
			sum = sha512.New()
		case "SHA256":
			sum = sha256.New()
		default:
			return ManifestRecord{}, fmt.Errorf("unsupported manifest hash algorithm: %s", algorithm)
		}
		hashes[algorithm] = sum
		writers = append(writers, sum)
	}
	if len(writers) > 0 {
		if _, err := io.Copy(io.MultiWriter(writers...), reader); err != nil {
			return ManifestRecord{}, err
		}
	}
	encoded := map[string]string{}
	for _, algorithm := range h.algorithms {
		encoded[algorithm] = hex.EncodeToString(hashes[algorithm].Sum(nil))
	}
	return ManifestRecord{Type: recordType, Name: name, Size: size, Hashes: encoded}, nil
}

// ManifestPlanner applies publication changes to the parsed Manifest domain.
// Thin manifests retain only DIST records. Thick manifests are rebuilt from
// the complete retained package tree plus the planned writes and deletions.
type ManifestPlanner struct {
	cfg             *GentooConfig
	layout          Layout
	existing        *Manifest
	retainedFiles   []client.RepoFile
	distfiles       []DistfileSource
	retainedEbuilds []string
	deletedEbuilds  []string
}

func NewManifestPlanner(cfg *GentooConfig, layout Layout, existing *Manifest) *ManifestPlanner {
	return &ManifestPlanner{cfg: cfg, layout: layout, existing: existing}
}

func (p *ManifestPlanner) WithPackageState(files []client.RepoFile, retainedEbuilds, deletedEbuilds []string) *ManifestPlanner {
	p.retainedFiles = append([]client.RepoFile(nil), files...)
	p.retainedEbuilds = slices.Clone(retainedEbuilds)
	p.deletedEbuilds = slices.Clone(deletedEbuilds)
	return p
}

func (p *ManifestPlanner) WithDistfiles(distfiles []DistfileSource) *ManifestPlanner {
	p.distfiles = slices.Clone(distfiles)
	return p
}

func (p *ManifestPlanner) Apply(changes *ChangeSet) error {
	manifest := p.existing.Clone()
	p.removeDeletedDistfiles(manifest)
	if err := p.replaceDistfiles(manifest); err != nil {
		return err
	}
	manifest.RemovePackageRecords()
	if !p.layout.ThinManifests() {
		if err := p.replacePackageFiles(manifest, changes); err != nil {
			return err
		}
	}
	content := manifest.Render()
	if len(content) > 0 {
		changes.Write(p.cfg.ManifestPath(), content)
	}
	return nil
}

func (p *ManifestPlanner) removeDeletedDistfiles(manifest *Manifest) {
	retainedBases := map[string]struct{}{}
	for _, name := range p.retainedEbuilds {
		if version := parseGentooVersion(name, p.cfg.PackageName()+"-"); version != nil {
			retainedBases[version.WithoutRevision().String()] = struct{}{}
		}
	}
	var removedBases []string
	for _, name := range p.deletedEbuilds {
		version := parseGentooVersion(name, p.cfg.PackageName()+"-")
		if version == nil {
			continue
		}
		base := version.WithoutRevision().String()
		if _, retained := retainedBases[base]; !retained {
			removedBases = append(removedBases, base)
		}
	}
	manifest.RemoveDistfilesForVersions(removedBases)
}

func (p *ManifestPlanner) replaceDistfiles(manifest *Manifest) error {
	hasher := NewManifestHasher(p.layout.ManifestHashes())
	for _, distfile := range p.distfiles {
		record, err := hasher.HashFile("DIST", distfile.Name, distfile.Path)
		if err != nil {
			return err
		}
		manifest.ReplaceDist(record)
	}
	return nil
}

func (p *ManifestPlanner) replacePackageFiles(manifest *Manifest, changes *ChangeSet) error {
	files := map[string][]byte{}
	for _, file := range p.retainedFiles {
		if file.Path != p.cfg.ManifestPath() && isInsidePackageDir(file.Path, p.cfg.PackageDir()) {
			files[file.Path] = file.Content
		}
	}
	for _, file := range changes.Files() {
		if file.Path == p.cfg.ManifestPath() || !isInsidePackageDir(file.Path, p.cfg.PackageDir()) {
			continue
		}
		if file.Delete {
			delete(files, file.Path)
			continue
		}
		files[file.Path] = file.Content
	}
	paths := make([]string, 0, len(files))
	for filename := range files {
		paths = append(paths, filename)
	}
	slices.Sort(paths)
	hasher := NewManifestHasher(p.layout.ManifestHashes())
	for _, filename := range paths {
		recordType, name := manifestFileInfo(filename, p.cfg.PackageDir())
		record, err := hasher.HashBytes(recordType, name, files[filename])
		if err != nil {
			return err
		}
		manifest.ReplacePackageFile(record)
	}
	return nil
}

func manifestFileInfo(filePath, packageDir string) (string, string) {
	pathStr := filepath.ToSlash(filePath)
	filesDir := path.Join(packageDir, "files")
	if pathStr == filesDir || strings.HasPrefix(pathStr, filesDir+"/") {
		return "AUX", strings.TrimPrefix(pathStr, filesDir+"/")
	}
	if strings.HasSuffix(pathStr, ".ebuild") {
		return "EBUILD", path.Base(pathStr)
	}
	return "MISC", path.Base(pathStr)
}

func isInsidePackageDir(filePath, packageDir string) bool {
	p := filepath.ToSlash(filePath)
	d := filepath.ToSlash(packageDir)
	return p == d || strings.HasPrefix(p, d+"/")
}

func cloneManifestRecord(record ManifestRecord) ManifestRecord {
	record.Hashes = maps.Clone(record.Hashes)
	return record
}
