#!/bin/bash
# Ah, git reset --hard restored retention.go to its original state so it has the version stuff.
# Let's delete the version stuff from retention.go
sed -i '/var gentooPrereleaseRe/,/func determineKeepLatestDeletions/d' internal/pipe/gentoo/retention.go
