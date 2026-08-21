#!/bin/bash
sed -i '/Name:    "foo",/a \\t\t\tBin:        true,' internal/pipe/gentoo/gentoo_test.go
