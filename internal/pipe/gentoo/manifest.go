package gentoo

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/crypto/blake2b"
)

type ManifestRecordType string

const (
	ManifestRecordDIST   ManifestRecordType = "DIST"
	ManifestRecordEBUILD ManifestRecordType = "EBUILD"
	ManifestRecordAUX    ManifestRecordType = "AUX"
	ManifestRecordMISC   ManifestRecordType = "MISC"
)

type ManifestRecord struct {
	Type   ManifestRecordType
	Name   string
	Size   int64
	Hashes map[string]string
}

func (r ManifestRecord) String() string {
	hashes := []string{}
	for algo, val := range r.Hashes {
		hashes = append(hashes, fmt.Sprintf("%s %s", algo, val))
	}
	slices.Sort(hashes)
	return fmt.Sprintf("%s %s %d %s", r.Type, r.Name, r.Size, strings.Join(hashes, " "))
}

type Manifest struct {
	records []ManifestRecord
}

func ParseManifest(content []byte) (*Manifest, error) {
	var records []ManifestRecord
	lines := strings.FieldsFunc(string(content), func(r rune) bool { return r == '\n' || r == '\r' })
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		recordType := ManifestRecordType(fields[0])
		name := fields[1]
		var size int64
		fmt.Sscanf(fields[2], "%d", &size)

		hashes := make(map[string]string)
		for i := 3; i < len(fields)-1; i += 2 {
			hashes[fields[i]] = fields[i+1]
		}

		records = append(records, ManifestRecord{
			Type:   recordType,
			Name:   name,
			Size:   size,
			Hashes: hashes,
		})
	}
	return &Manifest{records: records}, nil
}

func (m *Manifest) Render() []byte {
	var lines []string
	for _, r := range m.records {
		lines = append(lines, r.String())
	}
	slices.Sort(lines)
	lines = slices.Compact(lines)
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func (m *Manifest) ReplaceDist(record ManifestRecord) {
	for i, r := range m.records {
		if r.Type == ManifestRecordDIST && r.Name == record.Name {
			m.records[i] = record
			return
		}
	}
	m.records = append(m.records, record)
}

func (m *Manifest) RemoveDist(name string) {
	var keep []ManifestRecord
	for _, r := range m.records {
		if r.Type == ManifestRecordDIST && r.Name == name {
			continue
		}
		keep = append(keep, r)
	}
	m.records = keep
}

func (m *Manifest) ReplacePackageFile(record ManifestRecord) {
	for i, r := range m.records {
		if r.Type == record.Type && r.Name == record.Name {
			m.records[i] = record
			return
		}
	}
	m.records = append(m.records, record)
}

func (m *Manifest) RemovePackageFile(kind ManifestRecordType, name string) {
	var keep []ManifestRecord
	for _, r := range m.records {
		if r.Type == kind && r.Name == name {
			continue
		}
		keep = append(keep, r)
	}
	m.records = keep
}

func (m *Manifest) Records() []ManifestRecord {
	return m.records
}

type ManifestHasher struct {
	algorithms []string
}

func NewManifestHasher(algorithms []string) *ManifestHasher {
	return &ManifestHasher{algorithms: algorithms}
}

func (h *ManifestHasher) HashBytes(kind ManifestRecordType, name string, content []byte) (ManifestRecord, error) {
	return h.hashReader(kind, name, bytes.NewReader(content), int64(len(content)))
}

func (h *ManifestHasher) HashFile(kind ManifestRecordType, name string, pathStr string) (ManifestRecord, error) {
	info, err := os.Stat(pathStr)
	if err != nil {
		return ManifestRecord{}, err
	}
	f, err := os.Open(pathStr)
	if err != nil {
		return ManifestRecord{}, err
	}
	defer f.Close()
	return h.hashReader(kind, name, f, info.Size())
}

func (h *ManifestHasher) hashReader(kind ManifestRecordType, name string, r io.Reader, size int64) (ManifestRecord, error) {
	var writers []io.Writer
	var b2b hash.Hash
	var s512 hash.Hash
	var s256 hash.Hash

	for _, algo := range h.algorithms {
		algo = strings.ToUpper(algo)
		switch algo {
		case "BLAKE2B":
			b2b, _ = blake2b.New512(nil)
			writers = append(writers, b2b)
		case "SHA512":
			s512 = sha512.New()
			writers = append(writers, s512)
		case "SHA256":
			s256 = sha256.New()
			writers = append(writers, s256)
		default:
			return ManifestRecord{}, fmt.Errorf("unsupported manifest hash algorithm: %s", algo)
		}
	}

	if len(writers) > 0 {
		if _, err := io.Copy(io.MultiWriter(writers...), r); err != nil {
			return ManifestRecord{}, err
		}
	}

	hashes := make(map[string]string)
	for _, algo := range h.algorithms {
		algo = strings.ToUpper(algo)
		switch algo {
		case "BLAKE2B":
			if b2b != nil {
				hashes["BLAKE2B"] = fmt.Sprintf("%x", b2b.Sum(nil))
			}
		case "SHA512":
			if s512 != nil {
				hashes["SHA512"] = fmt.Sprintf("%x", s512.Sum(nil))
			}
		case "SHA256":
			if s256 != nil {
				hashes["SHA256"] = fmt.Sprintf("%x", s256.Sum(nil))
			}
		}
	}

	return ManifestRecord{
		Type:   kind,
		Name:   name,
		Size:   size,
		Hashes: hashes,
	}, nil
}

type Layout struct {
	manifestHashes []string
	thinManifests  bool
	cacheFormats   []string
}

func DefaultLayout() Layout {
	return Layout{
		manifestHashes: []string{"BLAKE2B", "SHA512"},
		thinManifests:  false,
	}
}

func ParseLayout(content []byte) (Layout, error) {
	l := DefaultLayout()
	for lineB := range bytes.SplitSeq(content, []byte{'\n'}) {
		key, value, ok := strings.Cut(strings.TrimSpace(string(lineB)), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "manifest-hashes":
			l.manifestHashes = strings.Fields(value)
		case "thin-manifests":
			l.thinManifests = strings.TrimSpace(value) == "true"
		case "cache-formats":
			l.cacheFormats = strings.Fields(value)
		}
	}
	return l, nil
}

func (l Layout) WithConfig(cfg *GentooConfig) Layout {
	if len(cfg.Raw().ManifestHashes) > 0 {
		l.manifestHashes = cfg.Raw().ManifestHashes
	}
	if cfg.Raw().ThinManifests != nil {
		l.thinManifests = *cfg.Raw().ThinManifests
	}
	return l
}

func (l Layout) ManifestHashes() []string {
	return l.manifestHashes
}

func (l Layout) ThinManifests() bool {
	return l.thinManifests
}

func (l Layout) SupportsMetaCache() bool {
	return slices.Contains(l.cacheFormats, "md5-dict") || slices.Contains(l.cacheFormats, "md5-cache")
}

func ManifestFileInfo(filePath, packageDir string) (ManifestRecordType, string) {
	pathStr := filepath.ToSlash(filePath)
	filesDir := path.Join(packageDir, "files")
	if pathStr == filesDir || strings.HasPrefix(pathStr, filesDir+"/") {
		return ManifestRecordAUX, strings.TrimPrefix(pathStr, filesDir+"/")
	}
	if strings.HasSuffix(pathStr, ".ebuild") {
		return ManifestRecordEBUILD, path.Base(pathStr)
	}
	return ManifestRecordMISC, path.Base(pathStr)
}
