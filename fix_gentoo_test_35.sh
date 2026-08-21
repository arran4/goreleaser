#!/bin/bash
# Remove gentooArch from ebuild.go
sed -i '/func gentooArch(/,$d' internal/pipe/gentoo/ebuild.go
# Remove old version stuff from retention.go
sed -i '/var gentooPrereleaseRe/d' internal/pipe/gentoo/retention.go
sed -i '/func convertToGentooVersion(/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/type suffixKind int/,/val  int\n}/d' internal/pipe/gentoo/retention.go
sed -i '/type parsedGentooVersion struct/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/func (v \*parsedGentooVersion) Compare/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/func (v \*parsedGentooVersion) GreaterThan/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/func (v \*parsedGentooVersion) baseEqual/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/func compareGentooSuffixes/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/var gentooSuffixTokenRe/d' internal/pipe/gentoo/retention.go
sed -i '/func parseGentooVersion/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/func getVersionBucket/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/type ebuildDeleter struct/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/func (d \*ebuildDeleter) Delete/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/func countNewEbuilds/,/^}/d' internal/pipe/gentoo/retention.go
sed -i '/func determineKeepLatestDeletions/,/^}/d' internal/pipe/gentoo/retention.go
