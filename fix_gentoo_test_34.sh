#!/bin/bash
sed -i 's/func TestDetermineKeepLatestDeletions(/func determineKeepLatestDeletions(/g' internal/pipe/gentoo/gentoo_test.go
# Wait, no. I should just delete the test because `determineKeepLatestDeletions` is gone! I already did delete it, but `git restore` brought it back.
# Instead of `sed` which was error prone because `^}` didn't delete the whole test if it had `t.Run()`, I'll use `go test` to find which tests fail to compile, and delete those lines manually or use a go parsing tool.
