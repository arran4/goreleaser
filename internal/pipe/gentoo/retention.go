package gentoo

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/caarlos0/log"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

var gentooPrereleaseRe = regexp.MustCompile(`(?i)-(alpha|beta|pre|rc|p)[.\-]?(\d*)`)

func convertToGentooVersion(v string, from string) (string, error) {
	switch from {
	case "gentoo-version":
		converted := gentooPrereleaseRe.ReplaceAllStringFunc(v, func(m string) string {
			match := gentooPrereleaseRe.FindStringSubmatch(m)
			return "_" + strings.ToLower(match[1]) + match[2]
		})
		if parseGentooVersion(converted+".ebuild", "") == nil {
			return "", fmt.Errorf("version %q cannot be naturally represented in Gentoo", v)
		}
		return converted, nil
	default:
		return "", fmt.Errorf("unsupported version representation %v", from)
	}
}

type suffixKind int

const (
	suffixAlpha suffixKind = 1
	suffixBeta  suffixKind = 2
	suffixPre   suffixKind = 3
	suffixRc    suffixKind = 4
	suffixP     suffixKind = 5
)

type gentooSuffix struct {
	kind suffixKind
	val  int
}

// GentooVersion is the parsed Gentoo version value used for ordering,
// retention buckets, and revision planning.
type GentooVersion struct {
	raw        string
	value      string
	baseNum    []int
	baseNumStr []string
	baseLetter rune
	suffixes   []gentooSuffix
	revision   int
}

func (v *GentooVersion) Compare(other *GentooVersion) int {
	if v == nil && other == nil {
		return 0
	}
	if v == nil {
		return -1
	}
	if other == nil {
		return 1
	}

	// Algorithm 3.2 & 3.3: Numeric components comparison
	if len(v.baseNum) > 0 && len(other.baseNum) > 0 {
		if v.baseNum[0] < other.baseNum[0] {
			return -1
		}
		if v.baseNum[0] > other.baseNum[0] {
			return 1
		}
	}

	minLen := min(len(v.baseNum), len(other.baseNum))
	for i := 1; i < minLen; i++ {
		s1 := v.baseNumStr[i]
		s2 := other.baseNumStr[i]
		if strings.HasPrefix(s1, "0") || strings.HasPrefix(s2, "0") {
			s1Trim := strings.TrimRight(s1, "0")
			s2Trim := strings.TrimRight(s2, "0")
			if s1Trim < s2Trim {
				return -1
			}
			if s1Trim > s2Trim {
				return 1
			}
		} else {
			if v.baseNum[i] < other.baseNum[i] {
				return -1
			}
			if v.baseNum[i] > other.baseNum[i] {
				return 1
			}
		}
	}

	if len(v.baseNum) < len(other.baseNum) {
		return -1
	}
	if len(v.baseNum) > len(other.baseNum) {
		return 1
	}

	// Algorithm 3.4: Letter components comparison
	if v.baseLetter < other.baseLetter {
		return -1
	}
	if v.baseLetter > other.baseLetter {
		return 1
	}

	// Algorithm 3.5 & 3.6: Suffixes comparison
	cmpSuffix := compareGentooSuffixes(v.suffixes, other.suffixes)
	if cmpSuffix != 0 {
		return cmpSuffix
	}

	// Algorithm 3.7: Revision comparison
	if v.revision < other.revision {
		return -1
	}
	if v.revision > other.revision {
		return 1
	}

	return 0
}

func (v *GentooVersion) GreaterThan(other *GentooVersion) bool {
	return v.Compare(other) > 0
}

func (v *GentooVersion) BaseEqual(other *GentooVersion) bool {
	if v == nil || other == nil {
		return v == other
	}
	if len(v.baseNum) != len(other.baseNum) || v.baseLetter != other.baseLetter || len(v.suffixes) != len(other.suffixes) {
		return false
	}
	for i := range v.baseNum {
		if v.baseNum[i] != other.baseNum[i] || v.baseNumStr[i] != other.baseNumStr[i] {
			return false
		}
	}
	for i := range v.suffixes {
		if v.suffixes[i] != other.suffixes[i] {
			return false
		}
	}
	return true
}

func compareGentooSuffixes(s1, s2 []gentooSuffix) int {
	if len(s1) == 0 && len(s2) == 0 {
		return 0
	}
	if len(s1) == 0 {
		if s2[0].kind == suffixP {
			return -1 // release < _p
		}
		return 1 // release > _alpha, _beta, _pre, _rc
	}
	if len(s2) == 0 {
		if s1[0].kind == suffixP {
			return 1 // _p > release
		}
		return -1 // _alpha, _beta, _pre, _rc < release
	}

	maxLen := max(len(s1), len(s2))
	for i := range maxLen {
		if i >= len(s1) {
			if s2[i].kind == suffixP {
				return -1
			}
			return 1
		}
		if i >= len(s2) {
			if s1[i].kind == suffixP {
				return 1
			}
			return -1
		}
		if s1[i].kind < s2[i].kind {
			return -1
		}
		if s1[i].kind > s2[i].kind {
			return 1
		}
		if s1[i].val < s2[i].val {
			return -1
		}
		if s1[i].val > s2[i].val {
			return 1
		}
	}
	return 0
}

var gentooSuffixTokenRe = regexp.MustCompile(`_(alpha|beta|pre|rc|p)(\d*)$`)

// ParseGentooVersion parses a standalone Gentoo version. It rejects values
// that cannot be represented by the ebuild version grammar.
func ParseGentooVersion(version string) (*GentooVersion, error) {
	v := parseGentooVersion(version+".ebuild", "")
	if v == nil {
		return nil, fmt.Errorf("invalid Gentoo version %q", version)
	}
	return v, nil
}

func GentooVersionFromRelease(version, representation string) (*GentooVersion, error) {
	converted, err := convertToGentooVersion(version, representation)
	if err != nil {
		return nil, err
	}
	return ParseGentooVersion(converted)
}

func (v *GentooVersion) String() string {
	if v == nil {
		return ""
	}
	return v.value
}

func (v *GentooVersion) Revision() int {
	if v == nil {
		return 0
	}
	return v.revision
}

func (v *GentooVersion) WithoutRevision() *GentooVersion {
	if v == nil {
		return nil
	}
	clone := *v
	clone.revision = 0
	if index := strings.LastIndex(clone.value, "-r"); index >= 0 {
		clone.value = clone.value[:index]
	}
	return &clone
}

func (v *GentooVersion) WithRevision(revision int) *GentooVersion {
	if v == nil {
		return nil
	}
	clone := *v
	clone.revision = revision
	clone.value = clone.WithoutRevision().value
	if revision > 0 {
		clone.value += fmt.Sprintf("-r%d", revision)
	}
	return &clone
}

func parseGentooVersion(n, prefix string) *GentooVersion {
	vStr := strings.TrimSuffix(strings.TrimPrefix(n, prefix), ".ebuild")
	if vStr == "" || vStr == n {
		return nil
	}

	value := vStr
	var rev int
	if idx := strings.LastIndex(vStr, "-r"); idx != -1 {
		if parsedRev, err := strconv.Atoi(vStr[idx+2:]); err == nil {
			rev = parsedRev
			vStr = vStr[:idx]
		}
	}

	var suffixes []gentooSuffix
	for {
		loc := gentooSuffixTokenRe.FindStringSubmatchIndex(vStr)
		if loc == nil {
			break
		}
		kindStr := vStr[loc[2]:loc[3]]
		valStr := vStr[loc[4]:loc[5]]
		val := 0
		if valStr != "" {
			var err error
			val, err = strconv.Atoi(valStr)
			if err != nil {
				return nil
			}
		}

		var kind suffixKind
		switch kindStr {
		case "alpha":
			kind = suffixAlpha
		case "beta":
			kind = suffixBeta
		case "pre":
			kind = suffixPre
		case "rc":
			kind = suffixRc
		case "p":
			kind = suffixP
		default:
			return nil
		}

		suffixes = append([]gentooSuffix{{kind: kind, val: val}}, suffixes...)
		vStr = vStr[:loc[0]]
	}

	if vStr == "" {
		return nil
	}

	var letter rune
	lastChar := vStr[len(vStr)-1]
	if lastChar >= 'a' && lastChar <= 'z' {
		letter = rune(lastChar)
		vStr = vStr[:len(vStr)-1]
	}

	if vStr == "" {
		return nil
	}

	parts := strings.Split(vStr, ".")
	var baseNum []int
	var baseNumStr []string
	for _, p := range parts {
		if p == "" {
			return nil
		}
		num, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		baseNum = append(baseNum, num)
		baseNumStr = append(baseNumStr, p)
	}

	return &GentooVersion{
		raw:        n,
		value:      value,
		baseNum:    baseNum,
		baseNumStr: baseNumStr,
		baseLetter: letter,
		suffixes:   suffixes,
		revision:   rev,
	}
}

func (v *GentooVersion) Bucket() string {
	if v == nil || len(v.suffixes) == 0 {
		return "stable"
	}
	last := v.suffixes[len(v.suffixes)-1]
	switch last.kind {
	case suffixAlpha:
		return "alpha"
	case suffixBeta:
		return "beta"
	case suffixPre:
		return "pre"
	case suffixRc:
		return "rc"
	default:
		return "stable"
	}
}

type ebuildDeleter struct {
	dir            string
	metaCacheDir   string
	metaCacheFiles map[string]struct{}
	changes        *ChangeSet
	deletedEbuilds *[]string
}

func (d *ebuildDeleter) Delete(ebuildName string) {
	d.changes.Delete(path.Join(d.dir, ebuildName))
	*d.deletedEbuilds = append(*d.deletedEbuilds, ebuildName)
	md5Name := strings.TrimSuffix(ebuildName, ".ebuild")
	if _, ok := d.metaCacheFiles[md5Name]; !ok {
		return
	}
	md5CachePath := path.Join(d.metaCacheDir, md5Name)
	d.changes.Delete(md5CachePath)
}

func countNewEbuilds(ebuilds, newFiles []string, bucket func(string) string) map[string]int {
	counts := map[string]int{}
	for _, file := range newFiles {
		if !slices.Contains(ebuilds, file) {
			counts[bucket(file)]++
		}
	}
	return counts
}

func determineKeepLatestDeletions(ebuilds, newFiles []string, prefix string, keepVersions int) []string {
	planner := &RetentionPlanner{prefix: prefix, keepVersions: keepVersions, existing: slices.Clone(ebuilds), incoming: slices.Clone(newFiles)}
	return planner.Plan().Deletes
}

type RetentionPlan struct {
	Deletes []string
}

// RetentionPlanner combines existing and incoming versions before ranking
// them. This prevents an older backfill from consuming a slot ahead of a newer
// version already present in the overlay.
type RetentionPlanner struct {
	prefix       string
	keepVersions int
	existing     []string
	incoming     []string
}

func NewRetentionPlanner(cfg *GentooConfig, existing, incoming []string) *RetentionPlanner {
	return &RetentionPlanner{prefix: cfg.PackageName() + "-", keepVersions: cfg.raw.KeepVersions, existing: slices.Clone(existing), incoming: slices.Clone(incoming)}
}

func (p *RetentionPlanner) Plan() RetentionPlan {
	all := slices.Clone(p.existing)
	for _, name := range p.incoming {
		if !slices.Contains(all, name) {
			all = append(all, name)
		}
	}
	slices.SortFunc(all, p.compareNames)
	if p.keepVersions <= 0 || len(all) <= p.keepVersions {
		return RetentionPlan{}
	}
	kept := all[:p.keepVersions]
	var deletes []string
	for _, name := range p.existing {
		if !slices.Contains(kept, name) {
			deletes = append(deletes, name)
		}
	}
	return RetentionPlan{Deletes: deletes}
}

func (p *RetentionPlanner) compareNames(left, right string) int {
	a := parseGentooVersion(left, p.prefix)
	b := parseGentooVersion(right, p.prefix)
	if a == nil && b == nil {
		return strings.Compare(right, left)
	}
	if a == nil {
		return 1
	}
	if b == nil {
		return -1
	}
	return -a.Compare(b)
}

type retentionCoordinator struct {
	cfg   config.Gentoo
	files []client.RepoFile
}

func (g *retentionCoordinator) applyVersionRetention(ctx *context.Context, repoClient any, repo client.Repo) ([]string, error) {
	dir := packageDir(g.cfg)
	stateRepo := repo

	var ebuilds []string
	prefix := filepath.Base(dir) + "-"

	lister, ok := repoClient.(client.DirectoryLister)
	if ok {
		names, err := lister.ListDir(ctx, stateRepo, dir)
		if err != nil && !errors.Is(err, client.ErrNotImplemented) {
			return nil, err
		}
		for _, n := range names {
			if strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ".ebuild") {
				ebuilds = append(ebuilds, n)
			}
		}
	}

	if len(ebuilds) == 0 {
		settings, err := loadOverlaySettings(ctx, g.cfg, repoClient, stateRepo)
		if err == nil && !settings.thin {
			manifestPath := filepath.ToSlash(filepath.Join(dir, "Manifest"))
			manifestLines, err := loadManifestLines(ctx, repoClient, stateRepo, manifestPath)
			if err == nil {
				for _, line := range manifestLines {
					fields := strings.Fields(line)
					if len(fields) >= 2 && fields[0] == "EBUILD" {
						ebuilds = append(ebuilds, fields[1])
					}
				}
			}
		}
	}

	if len(ebuilds) > 0 {
		switch g.cfg.ConflictResolution {
		case config.ConflictResolutionRevision:
			dl, ok := repoClient.(client.FileDownloader)
			if ok {
				g.updateVersions(ctx, dl, stateRepo, dir, prefix, ebuilds)
			}
		case config.ConflictResolutionOverwrite:
			// overwrites by default, no specific action required
		case config.ConflictResolutionFail:
			var newFiles []string
			for _, f := range g.files {
				name := filepath.Base(f.Path)
				if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".ebuild") {
					newFiles = append(newFiles, name)
				}
			}
			for _, nf := range newFiles {
				if slices.Contains(ebuilds, nf) {
					return nil, fmt.Errorf("ebuild %s already exists in %s", nf, dir)
				}
			}
		}
	}

	if !ok || g.cfg.KeepVersions <= 0 || g.cfg.VersionRetentionStrategy == "" {
		return nil, nil
	}

	slices.SortFunc(ebuilds, func(i, j string) int {
		vI := parseGentooVersion(i, prefix)
		vJ := parseGentooVersion(j, prefix)
		if vI != nil && vJ != nil {
			if vI.GreaterThan(vJ) {
				return -1
			}
			if vJ.GreaterThan(vI) {
				return 1
			}
			return 0
		}
		if vI != nil {
			return -1
		}
		if vJ != nil {
			return 1
		}
		return strings.Compare(j, i)
	})

	var newFiles []string
	for _, f := range g.files {
		name := filepath.Base(f.Path)
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".ebuild") {
			newFiles = append(newFiles, name)
		}
	}

	metaCacheDir := filepath.ToSlash(filepath.Join("metadata", "md5-cache", g.cfg.Category))
	if g.cfg.OverlayPath != "" {
		metaCacheDir = filepath.ToSlash(filepath.Join(g.cfg.OverlayPath, metaCacheDir))
	}
	metaCacheFiles := map[string]struct{}{}
	if g.cfg.MetaCache {
		cacheNames, err := lister.ListDir(ctx, stateRepo, metaCacheDir)
		if err != nil && !errors.Is(err, client.ErrNotFound) && !errors.Is(err, client.ErrNotImplemented) {
			return nil, err
		}
		for _, name := range cacheNames {
			metaCacheFiles[name] = struct{}{}
		}
	}

	var deletedEbuilds []string
	changes := NewChangeSet(g.files...)
	deleter := &ebuildDeleter{
		dir:            dir,
		metaCacheDir:   metaCacheDir,
		metaCacheFiles: metaCacheFiles,
		changes:        changes,
		deletedEbuilds: &deletedEbuilds,
	}
	defer func() { g.files = changes.Files() }()

	if g.cfg.VersionRetentionStrategy == config.VersionRetentionStrategyKeepPrereleases {
		var allEbuilds []string
		allEbuilds = append(allEbuilds, ebuilds...)
		allEbuilds = append(allEbuilds, newFiles...)

		maxVersions := map[string]*GentooVersion{}
		for _, n := range allEbuilds {
			v := parseGentooVersion(n, prefix)
			if v == nil {
				continue
			}
			b := v.Bucket()
			if maxVersions[b] == nil || v.GreaterThan(maxVersions[b]) {
				maxVersions[b] = v
			}
		}

		groups := map[string][]string{}
		for _, n := range ebuilds {
			v := parseGentooVersion(n, prefix)
			if v == nil {
				groups["stable"] = append(groups["stable"], n)
				continue
			}
			b := v.Bucket()

			violates := false
			switch b {
			case "alpha":
				if (maxVersions["beta"] != nil && !v.GreaterThan(maxVersions["beta"])) ||
					(maxVersions["pre"] != nil && !v.GreaterThan(maxVersions["pre"])) ||
					(maxVersions["rc"] != nil && !v.GreaterThan(maxVersions["rc"])) ||
					(maxVersions["stable"] != nil && !v.GreaterThan(maxVersions["stable"])) {
					violates = true
				}
			case "beta":
				if (maxVersions["pre"] != nil && !v.GreaterThan(maxVersions["pre"])) ||
					(maxVersions["rc"] != nil && !v.GreaterThan(maxVersions["rc"])) ||
					(maxVersions["stable"] != nil && !v.GreaterThan(maxVersions["stable"])) {
					violates = true
				}
			case "pre":
				if (maxVersions["rc"] != nil && !v.GreaterThan(maxVersions["rc"])) ||
					(maxVersions["stable"] != nil && !v.GreaterThan(maxVersions["stable"])) {
					violates = true
				}
			case "rc":
				if maxVersions["stable"] != nil && !v.GreaterThan(maxVersions["stable"]) {
					violates = true
				}
			}

			if violates {
				deleter.Delete(n)
			} else {
				groups[b] = append(groups[b], n)
			}
		}

		newCounts := countNewEbuilds(ebuilds, newFiles, func(f string) string {
			v := parseGentooVersion(f, prefix)
			if v == nil {
				return "stable"
			}
			return v.Bucket()
		})

		for b, bucketEbuilds := range groups {
			allowedToKeep := max(0, g.cfg.KeepVersions-newCounts[b])
			if len(bucketEbuilds) > allowedToKeep {
				for _, n := range bucketEbuilds[allowedToKeep:] {
					deleter.Delete(n)
				}
			}
		}
	} else if g.cfg.VersionRetentionStrategy == config.VersionRetentionStrategyKeepLatest {
		toDelete := determineKeepLatestDeletions(ebuilds, newFiles, prefix, g.cfg.KeepVersions)
		if len(toDelete) > 0 {
			log.WithField("keep_versions", g.cfg.KeepVersions).
				WithField("allowed_to_keep", g.cfg.KeepVersions).
				WithField("total_old", len(ebuilds)).
				Debug("keeping latest versions")

			for _, n := range toDelete {
				deleter.Delete(n)
			}
		}
	}
	return deletedEbuilds, nil
}

func stripComments(content []byte) []byte {
	var result []byte
	for line := range bytes.SplitSeq(content, []byte{'\n'}) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 && trimmed[0] != '#' {
			result = append(result, line...)
			result = append(result, '\n')
		}
	}
	return result
}

func (g *retentionCoordinator) updateVersions(ctx *context.Context, dl client.FileDownloader, stateRepo client.Repo, dir, prefix string, ebuilds []string) {
	for i := range g.files {
		if !strings.HasSuffix(g.files[i].Path, ".ebuild") || g.files[i].Delete {
			continue
		}

		fName := filepath.Base(g.files[i].Path)
		v := parseGentooVersion(fName, prefix)
		if v == nil {
			continue
		}

		maxR := -1
		var maxREbuild string
		for _, e := range ebuilds {
			ev := parseGentooVersion(e, prefix)
			if ev != nil && ev.BaseEqual(v) && ev.revision > maxR {
				maxR = ev.revision
				maxREbuild = e
			}
		}

		if maxR == -1 || maxREbuild == "" {
			continue
		}

		existingEbuildContent, err := dl.DownloadFile(ctx, stateRepo, filepath.ToSlash(filepath.Join(dir, maxREbuild)))
		if err != nil {
			continue
		}

		strippedExisting := stripComments(existingEbuildContent)
		strippedNew := stripComments(g.files[i].Content)

		isDifferent := !bytes.Equal(strippedExisting, strippedNew)
		vStr := strings.TrimSuffix(strings.TrimPrefix(fName, prefix), ".ebuild")
		baseCachePath := filepath.ToSlash(filepath.Join(g.cfg.OverlayPath, "metadata", "md5-cache", g.cfg.Category, prefix+vStr))
		existingCachePath := filepath.ToSlash(filepath.Join(g.cfg.OverlayPath, "metadata", "md5-cache", g.cfg.Category, strings.TrimSuffix(maxREbuild, ".ebuild")))

		if !isDifferent {
			for _, f := range g.files {
				if f.Path == g.files[i].Path || f.Delete {
					continue
				}
				existingPath := f.Path
				if existingPath == baseCachePath {
					existingPath = existingCachePath
				}
				existingContent, dErr := dl.DownloadFile(ctx, stateRepo, existingPath)
				if dErr != nil || !bytes.Equal(existingContent, f.Content) {
					isDifferent = true
					break
				}
			}
		}

		if !isDifferent {
			log.WithField("file", fName).Debug("existing ebuild matches new ebuild content, not creating a new revision")
			g.files[i].Path = filepath.ToSlash(filepath.Join(dir, maxREbuild))
			for j := range g.files {
				if g.files[j].Path == baseCachePath {
					g.files[j].Path = existingCachePath
				}
			}
			continue
		}

		newRev := maxR + 1
		newEbuildName := fmt.Sprintf("%s%s-r%d.ebuild", prefix, vStr, newRev)
		newEbuildPath := filepath.ToSlash(filepath.Join(dir, newEbuildName))
		log.WithField("file", fName).WithField("new_file", newEbuildName).Info("ebuild content changed, bumping revision")
		g.files[i].Path = newEbuildPath

		oldMetaCachePrefix := filepath.ToSlash(filepath.Join("metadata", "md5-cache", g.cfg.Category, fmt.Sprintf("%s%s", prefix, vStr)))
		newMetaCachePath := filepath.ToSlash(filepath.Join("metadata", "md5-cache", g.cfg.Category, fmt.Sprintf("%s%s-r%d", prefix, vStr, newRev)))
		if g.cfg.OverlayPath != "" {
			oldMetaCachePrefix = filepath.ToSlash(filepath.Join(g.cfg.OverlayPath, oldMetaCachePrefix))
			newMetaCachePath = filepath.ToSlash(filepath.Join(g.cfg.OverlayPath, newMetaCachePath))
		}
		for j := range g.files {
			if g.files[j].Path == oldMetaCachePrefix {
				g.files[j].Path = newMetaCachePath
			}
		}
	}
}
