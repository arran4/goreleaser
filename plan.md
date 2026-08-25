1. **Extend `pkg/config/config.go`:**
   - Add `GentooUpstreamRemoteID` with `Type` and `ID`.
   - Add `GentooUpstream` with `BugsTo`, `Doc`, and `RemoteIDs`.
   - Update `Gentoo` struct to include `LongDescription` and `Upstream`.
   - Update `GentooMaintainer` to include `Type`.

2. **Update configuration validation in `internal/pipe/gentoo/gentoo.go`:**
   - In `defaultGentooConfig`, validate and default maintainer `Type` to "person". Reject invalid types.

3. **Update config resolution in `internal/pipe/gentoo/config.go`:**
   - In `NewGentooConfig`, apply `tmpl.ApplyAll` to `LongDescription`, `Upstream.BugsTo`, `Upstream.Doc`, and iterate over `Upstream.RemoteIDs` to apply it to their `ID` and `Type` fields.
   - Extend `metadataConfig` struct to hold `longDescription`, `upstreamDoc`, and `remoteIDs`.
   - Update `metadataConfig.Empty()` to consider the new fields.
   - Update `c.metadata()` to prefer top-level `BugsTo` if set, else fallback to `Upstream.BugsTo`.

4. **Update `internal/pipe/gentoo/metadata.go`:**
   - Enhance `AddMaintainers` to set the `type` attribute but preserve existing attributes.
   - Add `SetLongDescription` to `Metadata` tree to manage the unqualified `<longdescription>` while ignoring those with attributes.
   - Add `SetUpstream(bugsTo, doc string, remoteIDs []config.GentooUpstreamRemoteID)` to `Metadata` to manage `bugs-to`, `doc`, and `remote-id`s. Add logic to prevent duplicates based on `(type, value)`.
   - Update `prepareMetadata` to call these new methods and pass the updated `metadataConfig`.
   - Add a small helper `setAttr` to `metadataNode` to facilitate attribute preservation/updating.

5. **Update tests in `internal/pipe/gentoo/metadata_test.go` and `gentoo_test.go`:**
   - Add tests for default maintainer type, project maintainers, validation rejection.
   - Test `SetLongDescription` for preserving translated/restricted items.
   - Test `SetUpstream` for triggering writes on `doc` alone, remote IDs behavior (duplicate prevention, additive behavior).
   - Test precedence of legacy `bugs_to` vs `upstream.bugs_to`.
   - Test metadata generation for all these cases.

6. **Complete pre-commit steps to ensure proper testing, verification, review, and reflection are done.**

7. **Submit the changes.**
