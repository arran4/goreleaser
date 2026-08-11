---
title: "Gentoo Ebuilds"
linkTitle: Gentoo
weight: 150
---

{{< g_version "v2.17" >}}

After releasing to GitHub, GitLab, or Gitea, GoReleaser can generate and publish
a _Gentoo Ebuild_ to an overlay repository.

The `gentoo_overlays` section specifies how the ebuilds should be created:

```yaml {filename=".goreleaser.yaml"}
gentoo_overlays:
  - # ID of the gentoo configuration, must be unique.
    #
    # Default: "default".
    id: myproject

    # Name of the package/ebuild.
    #
    # Default: the project name.
    # Templates: allowed.
    name: myproject

    # IDs of the archives to use.
    # Empty means all archives.
    ids:
      - foo
      - bar

    # Optional Gentoo category for the package (e.g. app-admin, dev-util).
    #
    # Default: "app-misc".
    # Templates: allowed.
    category: app-admin

    # The relative directory path of the package within the overlay repository.
    # GoReleaser creates the package directory, Manifest, metadata.xml, and files/ under this path.
    #
    # Default: "{{ .Category }}/{{ .Name }}-bin" (for type "bin") or "{{ .Category }}/{{ .Name }}".
    # Templates: allowed.
    overlay_path: "app-admin/myproject-bin"

    # The relative path of the ebuild file within the repository.
    #
    # Default: "{{ .OverlayPath }}/<package>-{{ .GentooVersion }}.ebuild".
    # Templates: allowed (supports {{ .GentooVersion }} and {{ .Version }}).
    path: "app-admin/myproject-bin/myproject-bin-{{ .GentooVersion }}.ebuild"

    # The ebuild type variant.
    #
    # Default: "bin".
    type: "bin"

    # Commit message template.
    #
    # Default: '{{ .ProjectName }}: bump to {{ .Tag }}'.
    # Templates: allowed.
    commit_msg_template: "{{ .ProjectName }}: bump to {{ .Tag }}"

    # Keywords to be applied to the package.
    #
    # Default: Derived from build artifacts (e.g. ["~amd64", "~arm64"]).
    keywords:
      - "~amd64"
      - "~arm64"

    # Extra files to add to the ebuild's files/ directory.
    files:
      - src: "init.d/myproject"
        dst: "myproject.init"

    # Skip size (< 20KB) and binary file checks for files in the files/ directory.
    #
    # Default: false.
    skip_files_validation: false

    # Whether the ebuild installs pre-compiled binary packages (unpacking binary release artifacts).
    #
    # Note: GoReleaser currently generates unpack-based binary ebuilds.
    # Required: must be true.
    bin: true

    # Keep past versions that were created by GoReleaser.
    # Requires an active SCM integration (e.g. GitHub/GitLab token) to work properly.
    #
    # Default: 0.
    keep_versions: 3

    # Strategy for resolving ebuild version conflicts when a file already exists.
    # Valid options: Fail, Overwrite, Revision.
    # When "Revision" is specified, the ebuild revision (e.g. from 1.0.0 to 1.0.0-r1)
    # will automatically bump when the content of the ebuild or its auxiliary files changes materially.
    #
    # Default: Revision.
    conflict_resolution: Revision

    # Retention strategy for old ebuild versions.
    #
    # Valid options: keep_latest, keep_prereleases.
    # Required if keep_versions > 0.
    version_retention_strategy: keep_latest

    # Setting this will prevent GoReleaser from committing and pushing the generated ebuild
    # to the repository — instead, the ebuild file will be saved in the dist directory only.
    #
    # If set to "auto", the ebuild will not be uploaded if the release tag is a prerelease (e.g. v1.0.0-rc1).
    #
    # Valid options: true, false, auto.
    # Default: false.
    # Templates: allowed.
    skip_upload: false

    # Destination binary installation directory.
    #
    # Default: "/opt/bin".
    bindir: "/usr/bin"

    # Enable Gentoo metadata cache generation (metadata/md5-cache/<category>/<package>-<version>).
    # Note: If the repository's metadata/layout.conf disables cache-formats (e.g. cache-formats is specified without md5-dict),
    # metadata cache generation will be disabled automatically.
    #
    # Default: false.
    meta_cache: true

    # Overrides for manifest hashes. Usually derived from metadata/layout.conf.
    #
    # Default: null (defers to layout.conf or ["BLAKE2B", "SHA512"]).
    # manifest_hashes:
    #   - BLAKE2B
    #   - SHA512

    # Overrides thin manifests generation. Usually derived from metadata/layout.conf.
    #
    # Default: null (defers to layout.conf or false).
    # thin_manifests: false

    # The Gentoo maintainers of the ebuild.
    maintainers:
      - name: "John Doe"
        email: "john@example.com"

    # Upstream bug tracker URL.
    #
    # Templates: allowed.
    bugs_to: "https://github.com/myorg/myproject/issues"

    # Project homepage.
    #
    # Templates: allowed.
    homepage: "https://myproject.com"

    # The ebuild's description.
    #
    # Templates: allowed.
    description: "Software to create fast and easy drum rolls."

    # The ebuild's license.
    #
    # Required.
    # Templates: allowed.
    license: "MIT"

    # Extra ebuild installation instructions.
    #
    # Templates: allowed.
    extra_install: |
      doins extra/stuff

    # USE flags defined for the ebuild.
    useflags:
      - flag: systemd
        description: "Enable systemd support"

    # Files to install with dobin.
    dobin:
      - src: "path/to/bin"
        dst: "mybin"
        use:
          - systemd

    # Files to install with doconfd.
    doconfd:
      - src: "path/to/conf"
        dst: "myconf"

    # Directories to create with dodir.
    dodir:
      - "/var/lib/myproject"

    # Documentation files to install with dodoc.
    dodoc:
      - "README.md"

    # Files to install with doenvd.
    doenvd:
      - src: "path/to/env"
        dst: "50myproject"

    # Executable files to install with doexe.
    doexe:
      - src: "path/to/script.sh"
        dst: "script.sh"

    # Header files to install with doheader.
    doheader:
      - src: "path/to/header.h"
        dst: "header.h"

    # Init scripts to install with doinitd.
    doinitd:
      - src: "path/to/init"
        dst: "myproject"

    # General files to install with doins.
    doins:
      - src: "path/to/file"
        dst: "file"

    # Man pages to install with doman.
    doman:
      - "man/myproject.1"

    # System binaries to install with dosbin.
    dosbin:
      - src: "path/to/sbin"
        dst: "mysbin"

    # Symlinks to create with dosym.
    dosym:
      - src: "/usr/bin/myproject"
        dst: "/usr/bin/myproject-alias"

    # Systemd service files to install.
    systemd:
      - src: "path/to/service.service"
        dst: "service.service"

{{% g_include file="includes/commit_author.md" %}}
{{% g_include file="includes/repository.md" %}}
```

{{< g_templates >}}

## Limitations

- If the target repository does not contain `metadata/layout.conf`, GoReleaser falls back to internal defaults (such as `BLAKE2B` and `SHA512` manifest hashes).

{{% g_include file="includes/prs.md" %}}

