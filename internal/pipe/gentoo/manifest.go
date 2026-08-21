package gentoo

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

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

func (m *Manifest) Records() []ManifestRecord { return append([]ManifestRecord(nil), m.records...) }

func (m *Manifest) Replace(record ManifestRecord) {
	m.Remove(record.Type, record.Name)
	m.records = append(m.records, record)
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

func (h *ManifestHasher) Line(recordType, name, filename string, content []byte) (string, error) {
	var record ManifestRecord
	var err error
	if content == nil && filename != "" {
		record, err = h.HashFile(recordType, name, filename)
	} else {
		record, err = h.HashBytes(recordType, name, content)
	}
	if err != nil {
		return "", err
	}
	manifest := &Manifest{records: []ManifestRecord{record}}
	return strings.TrimSpace(string(manifest.Render())), nil
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
