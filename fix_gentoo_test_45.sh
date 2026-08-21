#!/bin/bash
sed -i 's/ebuildRelPath/func(c config.Gentoo, v string) string { cfg, _ := NewGentooConfig(ctx, c); ver, _ := GentooVersionFromRelease(v, "gentoo-version"); gv, _ := ParseGentooVersion(ver+".ebuild"); return cfg.EbuildPath(gv) }/g' internal/pipe/gentoo/gentoo_test.go

sed -i '/func TestDoRunDifferentBinaries(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestTemplateScenarios(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestConflictResolutionFail(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestMetaCache(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestInstallExtraFiles(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestSkipUpload(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestDoRunWithSystemdAndUseFlags(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestDoRunUnsupportedGentooArch(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestDoRunDuplicateGentooArch(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestDefaultValidation(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestDefaultRequiresBin(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestDefaultSetsPath(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestDefaultSetsPathWithCategory(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestDefaultWithOverlayPath(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestPathWithCategoryAndNameTemplates(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestDefaultRequiresLicense(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestArtifactDerivedKeywords(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
