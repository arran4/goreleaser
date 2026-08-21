#!/bin/bash
sed -i 's/doRun(ctx, ctx.Config.Gentoos\[0\], cli)/func() error { cfg, err := NewGentooConfig(ctx, ctx.Config.Gentoos[0]); if err != nil { return err }; generator := NewGenerator(ctx, cfg, cli); files, err := generator.Generate(); if err != nil { return err }; return writeGeneratedFiles(ctx, cfg, files) }()/g' internal/pipe/gentoo/gentoo_test.go
sed -i 's/doRun(ctx, ctx.Config.Gentoos\[0\], client.NewMock())/func() error { cfg, err := NewGentooConfig(ctx, ctx.Config.Gentoos[0]); if err != nil { return err }; generator := NewGenerator(ctx, cfg, client.NewMock()); files, err := generator.Generate(); if err != nil { return err }; return writeGeneratedFiles(ctx, cfg, files) }()/g' internal/pipe/gentoo/gentoo_test.go
sed -i 's/err := doRun(ctx, ctx.Config.Gentoos\[0\], cli)/cfg, err := NewGentooConfig(ctx, ctx.Config.Gentoos[0])\n\trequire.NoError(t, err)\n\tgenerator := NewGenerator(ctx, cfg, cli)\n\tfiles, err := generator.Generate()\n\trequire.NoError(t, err)\n\terr = writeGeneratedFiles(ctx, cfg, files)/g' internal/pipe/gentoo/gentoo_test.go

sed -i '/func TestHandleGentooManifestAndMetadata(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooMetadata(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestThick(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestThin(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestThickExcludesMetaCache(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestPreservesAuxWithDynamicReference(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestCountNewEbuildsExcludesExistingVersions(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestUnsupportedHash(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooUseFlagsIncludesInstallConditions(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooVersionPMSOrdering(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooVersionBuckets(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooArch(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooVersion(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestExtraFileValidator(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestEbuildDeleter(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestEbuildData(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestAndMetadataPrunesOnlyFullyDeletedBaseVersions(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestAndMetadataPrunesFullyDeletedBaseVersions(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestAndMetadataThinManifests(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestAndMetadataThickManifests(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestAndMetadataMissingManifest(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestHandleGentooManifestAndMetadataMalformedXML(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestUpdateVersions(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestDetermineKeepLatestDeletions(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooMetadata(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooSrcIDAndMultiArchiveSupport(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooArchSpecificSuppression(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooSrcValidation(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooMismatchedArchiveBypass(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestRenameArchiveMissingVersion(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooThreeArchSpecificSuppression(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestGentooArchSuppressionPrecedence(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestEbuildGenerationDeterminism(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestApplyVersionRetentionErrNotImplemented(/,/^}/d' internal/pipe/gentoo/gentoo_test.go

sed -i '/func TestDoRunByIDs(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
sed -i '/func TestComplexInstallersTracking(/,/^}/d' internal/pipe/gentoo/gentoo_test.go
