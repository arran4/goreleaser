#!/bin/bash
# Remove duplicate import "bytes" in utils.go which causes a syntax error.
sed -i '/import "bytes"/d' internal/pipe/gentoo/utils.go
sed -i 's/import (/import (\n\t"bytes"\n/g' internal/pipe/gentoo/utils.go

# Overwrite retention.go
cat << 'EOT' > internal/pipe/gentoo/retention.go
package gentoo

import (
	"bytes"
	"slices"
	"strings"

	"github.com/caarlos0/log"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	goreleaser_context "github.com/goreleaser/goreleaser/v2/pkg/context"
)

type RetentionPlan struct {
	Revisions []Revision
	Deletes   []ExistingEbuild
}

type Revision struct {
	From string
	To   string
}

type RetentionPlanner struct {
	cfg      *GentooConfig
	existing []ExistingEbuild
	incoming []GeneratedFile

	stateRepo *RepositoryState
}

func NewRetentionPlanner(cfg *GentooConfig, existing []ExistingEbuild, incoming []GeneratedFile) *RetentionPlanner {
	return &RetentionPlanner{
		cfg:      cfg,
		existing: existing,
		incoming: incoming,
	}
}

func (p *RetentionPlanner) Plan(ctx *goreleaser_context.Context) (*RetentionPlan, error) {
	plan := &RetentionPlan{}

	incomingEbuilds := []GeneratedFile{}
	for _, f := range p.incoming {
		if f.Kind == GeneratedEbuild {
			incomingEbuilds = append(incomingEbuilds, f)
		}
	}

	var activeEbuilds []ExistingEbuild

	for _, f := range incomingEbuilds {
		v, err := ParseGentooVersion(f.Path)
		if err != nil {
			continue
		}

		var maxRev *ExistingEbuild
		for i, ex := range p.existing {
			if ex.Version.BaseEqual(v) {
				if maxRev == nil || ex.Version.Revision() > maxRev.Version.Revision() {
					maxRev = &p.existing[i]
				}
			}
		}

		if maxRev == nil {
			activeEbuilds = append(activeEbuilds, ExistingEbuild{
				Name:    f.Path,
				Version: v,
				Content: f.Content,
			})
			continue
		}

		isDifferent := !slices.Equal(stripComments(maxRev.Content), stripComments(f.Content))

		switch p.cfg.Raw().ConflictResolution {
		case config.ConflictResolutionFail:
			activeEbuilds = append(activeEbuilds, ExistingEbuild{
				Name:    f.Path,
				Version: v,
				Content: f.Content,
			})
		case config.ConflictResolutionOverwrite:
			activeEbuilds = append(activeEbuilds, ExistingEbuild{
				Name:    f.Path,
				Version: v,
				Content: f.Content,
			})
		case config.ConflictResolutionRevision:
			if !isDifferent {
				log.WithField("file", f.Path).Debug("existing ebuild matches new ebuild content, not creating a new revision")
				activeEbuilds = append(activeEbuilds, *maxRev)
				plan.Revisions = append(plan.Revisions, Revision{
					From: f.Path,
					To:   maxRev.Name,
				})
			} else {
				newRev := maxRev.Version.Revision() + 1
				newVer := v.WithRevision(newRev)
				newPath := p.cfg.EbuildPath(newVer)
				log.WithField("file", f.Path).WithField("new_file", newPath).Info("ebuild content changed, bumping revision")
				plan.Revisions = append(plan.Revisions, Revision{
					From: f.Path,
					To:   newPath,
				})
				activeEbuilds = append(activeEbuilds, ExistingEbuild{
					Name:    newPath,
					Version: newVer,
					Content: f.Content,
				})
			}
		}
	}

	var allVersions []ExistingEbuild
	allVersions = append(allVersions, activeEbuilds...)
	for _, ex := range p.existing {
		found := false
		for _, a := range activeEbuilds {
			if a.Version.String() == ex.Version.String() {
				found = true
				break
			}
		}
		if !found {
			allVersions = append(allVersions, ex)
		}
	}

	if p.cfg.Raw().KeepVersions > 0 {
		if p.cfg.Raw().VersionRetentionStrategy == config.VersionRetentionStrategyKeepPrereleases {
			groups := make(map[VersionBucket][]ExistingEbuild)
			for _, e := range allVersions {
				b := e.Version.Bucket()
				groups[b] = append(groups[b], e)
			}

			for _, group := range groups {
				slices.SortFunc(group, func(a, b ExistingEbuild) int {
					return b.Version.Compare(a.Version)
				})
			}

			var maxStable, maxRc *ExistingEbuild
			if len(groups[BucketStable]) > 0 {
				maxStable = &groups[BucketStable][0]
			}
			if len(groups[BucketRc]) > 0 {
				maxRc = &groups[BucketRc][0]
			}

			newCounts := make(map[VersionBucket]int)
			for _, a := range activeEbuilds {
				newCounts[a.Version.Bucket()]++
			}

			for b, group := range groups {
				var toKeep []ExistingEbuild
				for _, e := range group {
					violates := false
					if b == BucketAlpha || b == BucketBeta || b == BucketPre {
						if (maxRc != nil && !e.Version.GreaterThan(maxRc.Version)) ||
							(maxStable != nil && !e.Version.GreaterThan(maxStable.Version)) {
							violates = true
						}
					} else if b == BucketRc {
						if maxStable != nil && !e.Version.GreaterThan(maxStable.Version) {
							violates = true
						}
					}

					if violates {
						plan.Deletes = append(plan.Deletes, e)
					} else {
						toKeep = append(toKeep, e)
					}
				}

				allowedToKeep := max(0, p.cfg.Raw().KeepVersions-newCounts[b])
				if len(toKeep) > allowedToKeep {
					for _, e := range toKeep[allowedToKeep:] {
						isNew := false
						for _, a := range activeEbuilds {
							if a.Name == e.Name {
								isNew = true
								break
							}
						}
						if !isNew {
							plan.Deletes = append(plan.Deletes, e)
						}
					}
				}
			}

		} else if p.cfg.Raw().VersionRetentionStrategy == config.VersionRetentionStrategyKeepLatest {
			slices.SortFunc(allVersions, func(a, b ExistingEbuild) int {
				return b.Version.Compare(a.Version)
			})

			if len(allVersions) > p.cfg.Raw().KeepVersions {
				for _, e := range allVersions[p.cfg.Raw().KeepVersions:] {
					isNew := false
					for _, a := range activeEbuilds {
						if a.Name == e.Name {
							isNew = true
							break
						}
					}
					if !isNew {
						plan.Deletes = append(plan.Deletes, e)
					}
				}
			}
		}
	}

	return plan, nil
}
EOT

# Fix open PR variable shadowing
sed -i 's/prClient, err =/prClient, err :=/g' internal/pipe/gentoo/publish.go
