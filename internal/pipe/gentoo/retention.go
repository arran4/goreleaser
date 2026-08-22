package gentoo

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
)

// ExistingEbuild is normalized package state. ContentAvailable distinguishes
// an unreadable file from a valid empty ebuild; revision comparison requires
// exact content while name-only conflict and retention policies do not.
type ExistingEbuild struct {
	Name             string
	Path             string
	Version          *GentooVersion
	Content          []byte
	ContentAvailable bool
}

// RetentionState is the repository-independent input to RetentionPlanner.
// RepositoryState resolves the exact bytes needed for conflict and cache
// comparison before constructing this value.
type RetentionState struct {
	Ebuilds        []ExistingEbuild
	Files          map[string][]byte
	MetaCacheFiles map[string][]byte
}

// RetentionPlan contains the complete repository-independent result of
// revision and retention planning. Deletes names existing ebuilds removed by
// policy; Changes contains both revised writes and corresponding deletions.
type RetentionPlan struct {
	Changes         *ChangeSet
	Deletes         []string
	RetainedEbuilds []string
}

// RetentionPlanner ranks existing and incoming Gentoo versions together.
// Incoming releases never consume an artificial reserved slot, so backfilling
// an older version cannot evict a newer version already in the overlay.
type RetentionPlanner struct {
	cfg      *GentooConfig
	policy   retentionPolicy
	state    RetentionState
	incoming *ChangeSet
	prefix   string
}

func NewRetentionPlanner(cfg *GentooConfig, state RetentionState, incoming *ChangeSet) *RetentionPlanner {
	return &RetentionPlanner{
		cfg: cfg, policy: cfg.retention(), state: state, incoming: incoming.Clone(), prefix: cfg.PackageName() + "-",
	}
}

func (p *RetentionPlanner) Plan() (RetentionPlan, error) {
	changes := p.incoming.Clone()
	if err := p.resolveConflicts(changes); err != nil {
		return RetentionPlan{}, err
	}
	incoming := p.incomingEbuildNames(changes)
	deletes := p.planRetention(incoming)
	for _, name := range deletes {
		p.deleteExisting(changes, name)
	}
	return RetentionPlan{
		Changes: changes, Deletes: deletes, RetainedEbuilds: p.retainedEbuilds(incoming, deletes),
	}, nil
}

func (p *RetentionPlanner) resolveConflicts(changes *ChangeSet) error {
	for _, file := range changes.Files() {
		if file.Delete || !p.isEbuild(file.Path) {
			continue
		}
		switch p.policy.conflictResolution {
		case config.ConflictResolutionFail:
			if p.existingByName(filepath.Base(file.Path)) != nil {
				return fmt.Errorf("ebuild %s already exists in %s", filepath.Base(file.Path), p.cfg.PackageDir())
			}
		case config.ConflictResolutionRevision:
			if err := p.resolveRevision(changes, file); err != nil {
				return err
			}
		case config.ConflictResolutionOverwrite, "":
			continue
		}
	}
	return nil
}

func (p *RetentionPlanner) resolveRevision(changes *ChangeSet, incoming client.RepoFile) error {
	version := parseGentooVersion(filepath.Base(incoming.Path), p.prefix)
	if version == nil {
		return nil
	}
	existing := p.latestBaseVersion(version)
	if existing == nil {
		return nil
	}
	if !existing.ContentAvailable {
		return fmt.Errorf("cannot compare existing ebuild %s for revision planning: content is unavailable", existing.Path)
	}
	if p.contentMatches(*existing, incoming, changes) {
		p.renameRevision(changes, incoming.Path, existing.Version)
		return nil
	}
	p.renameRevision(changes, incoming.Path, version.WithRevision(existing.Version.Revision()+1))
	return nil
}

func (p *RetentionPlanner) contentMatches(existing ExistingEbuild, incoming client.RepoFile, changes *ChangeSet) bool {
	if !bytes.Equal(stripComments(existing.Content), stripComments(incoming.Content)) {
		return false
	}
	for _, file := range changes.Files() {
		if file.Delete || file.Path == incoming.Path {
			continue
		}
		existingPath := file.Path
		if file.Path == p.cfg.MetaCachePath() {
			existingPath = p.cfg.MetaCachePathForVersion(existing.Version.String())
		}
		content, ok := p.state.Files[existingPath]
		if !ok || !bytes.Equal(content, file.Content) {
			return false
		}
	}
	return true
}

func (p *RetentionPlanner) renameRevision(changes *ChangeSet, oldPath string, version *GentooVersion) {
	file, ok := changes.Find(oldPath)
	if !ok {
		return
	}
	newPath := path.Join(p.cfg.PackageDir(), p.prefix+version.String()+".ebuild")
	file.Path = newPath
	changes.Remove(oldPath)
	changes.Add(file)
	cache, ok := changes.Find(p.cfg.MetaCachePath())
	if !ok {
		return
	}
	changes.Remove(cache.Path)
	cache.Path = p.cfg.MetaCachePathForVersion(version.String())
	changes.Add(cache)
}

func (p *RetentionPlanner) latestBaseVersion(version *GentooVersion) *ExistingEbuild {
	var latest *ExistingEbuild
	for i := range p.state.Ebuilds {
		candidate := &p.state.Ebuilds[i]
		if !candidate.Version.BaseEqual(version) {
			continue
		}
		if latest == nil || candidate.Version.GreaterThan(latest.Version) {
			latest = candidate
		}
	}
	return latest
}

func (p *RetentionPlanner) planRetention(incoming []string) []string {
	if p.policy.keepVersions <= 0 || p.policy.strategy == "" {
		return nil
	}
	switch p.policy.strategy {
	case config.VersionRetentionStrategyKeepLatest:
		return p.keepLatest(incoming)
	case config.VersionRetentionStrategyKeepPrereleases:
		return p.keepPrereleases(incoming)
	default:
		return nil
	}
}

func (p *RetentionPlanner) keepLatest(incoming []string) []string {
	all := p.combinedNames(incoming)
	slices.SortFunc(all, p.compareNames)
	kept := all[:min(len(all), p.policy.keepVersions)]
	return p.existingNotKept(kept, incoming, nil)
}

func (p *RetentionPlanner) keepPrereleases(incoming []string) []string {
	all := p.combinedNames(incoming)
	maxima := p.bucketMaxima(all)
	kept := map[string]struct{}{}
	for _, bucket := range []string{"alpha", "beta", "pre", "rc", "stable"} {
		var candidates []string
		for _, name := range all {
			version := parseGentooVersion(name, p.prefix)
			if p.versionBucket(version) != bucket || p.supersededByLaterBucket(version, maxima) {
				continue
			}
			candidates = append(candidates, name)
		}
		slices.SortFunc(candidates, p.compareNames)
		for _, name := range candidates[:min(len(candidates), p.policy.keepVersions)] {
			kept[name] = struct{}{}
		}
	}
	return p.existingNotKept(nil, incoming, kept)
}

func (p *RetentionPlanner) existingNotKept(keptNames, incoming []string, keptSet map[string]struct{}) []string {
	if keptSet == nil {
		keptSet = map[string]struct{}{}
		for _, name := range keptNames {
			keptSet[name] = struct{}{}
		}
	}
	var deletes []string
	for _, existing := range p.state.Ebuilds {
		if slices.Contains(incoming, existing.Name) {
			continue
		}
		if _, ok := keptSet[existing.Name]; !ok {
			deletes = append(deletes, existing.Name)
		}
	}
	slices.Sort(deletes)
	return deletes
}

func (p *RetentionPlanner) bucketMaxima(names []string) map[string]*GentooVersion {
	result := map[string]*GentooVersion{}
	for _, name := range names {
		version := parseGentooVersion(name, p.prefix)
		bucket := p.versionBucket(version)
		if result[bucket] == nil || version.GreaterThan(result[bucket]) {
			result[bucket] = version
		}
	}
	return result
}

func (p *RetentionPlanner) supersededByLaterBucket(version *GentooVersion, maxima map[string]*GentooVersion) bool {
	if version == nil {
		return false
	}
	order := []string{"alpha", "beta", "pre", "rc", "stable"}
	index := slices.Index(order, version.Bucket())
	for _, bucket := range order[index+1:] {
		if maxima[bucket] != nil && maxima[bucket].Compare(version) >= 0 {
			return true
		}
	}
	return false
}

func (p *RetentionPlanner) versionBucket(version *GentooVersion) string {
	if version == nil {
		return "stable"
	}
	return version.Bucket()
}

func (p *RetentionPlanner) combinedNames(incoming []string) []string {
	result := slices.Clone(incoming)
	for _, existing := range p.state.Ebuilds {
		if !slices.Contains(result, existing.Name) {
			result = append(result, existing.Name)
		}
	}
	return result
}

func (p *RetentionPlanner) incomingEbuildNames(changes *ChangeSet) []string {
	var result []string
	for _, file := range changes.Files() {
		if !file.Delete && p.isEbuild(file.Path) {
			result = append(result, filepath.Base(file.Path))
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func (p *RetentionPlanner) retainedEbuilds(incoming, deletes []string) []string {
	result := slices.Clone(incoming)
	for _, existing := range p.state.Ebuilds {
		if !slices.Contains(deletes, existing.Name) && !slices.Contains(result, existing.Name) {
			result = append(result, existing.Name)
		}
	}
	slices.SortFunc(result, p.compareNames)
	return result
}

func (p *RetentionPlanner) deleteExisting(changes *ChangeSet, name string) {
	changes.Delete(path.Join(p.cfg.PackageDir(), name))
	version := strings.TrimSuffix(strings.TrimPrefix(name, p.prefix), ".ebuild")
	cachePath := p.cfg.MetaCachePathForVersion(version)
	if _, ok := p.state.MetaCacheFiles[cachePath]; ok {
		changes.Delete(cachePath)
	}
}

func (p *RetentionPlanner) existingByName(name string) *ExistingEbuild {
	for i := range p.state.Ebuilds {
		if p.state.Ebuilds[i].Name == name {
			return &p.state.Ebuilds[i]
		}
	}
	return nil
}

func (p *RetentionPlanner) isEbuild(filename string) bool {
	return path.Dir(filepath.ToSlash(filename)) == p.cfg.PackageDir() && strings.HasPrefix(filepath.Base(filename), p.prefix) && strings.HasSuffix(filename, ".ebuild")
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

func stripComments(content []byte) []byte {
	var result []byte
	for line := range bytes.SplitSeq(content, []byte{'\n'}) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		result = append(result, line...)
		result = append(result, '\n')
	}
	return result
}
