package gentoo

import (
	"testing"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestMetadataPreservesUnknownContent(t *testing.T) {
	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata custom="value">
  <!-- keep me -->
  <longdescription lang="en">A long description.</longdescription>
  <upstream><remote-id type="github">owner/repo</remote-id><unknown attr="x">value</unknown></upstream>
</pkgmetadata>`)
	metadata, err := ParseMetadata(input)
	require.NoError(t, err)
	metadata.AddUseFlags([]config.GentooUseFlag{{Flag: "systemd", Description: "Install service unit"}})
	output, err := metadata.Render()
	require.NoError(t, err)
	require.Contains(t, string(output), `custom="value"`)
	require.Contains(t, string(output), `longdescription`)
	require.Contains(t, string(output), `remote-id type="github"`)
	require.Contains(t, string(output), `unknown attr="x"`)
}

func TestParseMetadataRejectsMalformedXML(t *testing.T) {
	_, err := ParseMetadata([]byte("<pkgmetadata>"))
	require.Error(t, err)
}
