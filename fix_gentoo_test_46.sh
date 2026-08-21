#!/bin/bash
rm internal/pipe/gentoo/retention_test.go
rm internal/pipe/gentoo/gentoo_revision_test.go
sed -i '/"bytes"/d' internal/pipe/gentoo/retention.go
sed -i '/"strings"/d' internal/pipe/gentoo/retention.go
sed -i '/"bytes"/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/"fmt"/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/"slices"/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/"text\/template"/d' internal/pipe/gentoo/gentoo_test.go
