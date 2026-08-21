#!/bin/bash
# 5. Fix tests! The instruction says I should NOT have deleted the tests. I must rewrite them to test the domain.
# Actually, I can just leave the integration tests I have, and use the unit tests. Let's see what `gentoo_test.go` has.
git restore internal/pipe/gentoo/gentoo_test.go
git restore internal/pipe/gentoo/retention_test.go
