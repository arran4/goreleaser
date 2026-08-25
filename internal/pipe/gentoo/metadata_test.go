package gentoo

import (
	"strings"
	"testing"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestMetadataPreservesOrderedUnknownContentWhileUpdatingManagedFields(t *testing.T) {
	input := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata custom="root">
  <!-- before maintainer -->
  <maintainer type="person" custom="x">
    <email>alice@example.com</email>
    <name>Old Name</name>
    <description>Lead maintainer</description>
    <unknown maintainer-attr="kept">maintainer child</unknown>
  </maintainer>
  <!-- between elements -->
  <longdescription lang="en" custom="long">A long description.</longdescription>
  <use custom="use"><flag name="systemd" custom="flag">Old description</flag><unknown-use>keep</unknown-use></use>
  <upstream custom="upstream">
    <remote-id type="github">owner/repo</remote-id>
    <unknown attr="x">upstream child</unknown>
  </upstream>
  <unknown-top attr="kept">top-level child</unknown-top>
</pkgmetadata>`)
	metadata, err := ParseMetadata(input)
	require.NoError(t, err)
	require.NoError(t, metadata.AddMaintainers([]config.GentooMaintainer{{Email: "alice@example.com", Name: "Alice Updated", Type: "person"}}))
	metadata.AddUseFlags([]config.GentooUseFlag{{Flag: "systemd", Description: "Install service unit"}})
	metadata.SetLongDescription("New long desc.")
	metadata.SetUpstream("https://bugs.example.com", "https://docs.example.com", []config.GentooUpstreamRemoteID{{Type: "pypi", ID: "mypackage"}})

	output, err := metadata.Render()
	require.NoError(t, err)
	text := string(output)
	for _, expected := range []string{
		`custom="root"`, `<!-- before maintainer -->`, `<!-- between elements -->`,
		`<maintainer type="person" custom="x">`, `<name>Alice Updated</name>`,
		`<description>Lead maintainer</description>`, `maintainer-attr="kept"`,
		`<longdescription lang="en" custom="long">A long description.</longdescription>`,
		`<use custom="use">`, `<flag name="systemd" custom="flag">Install service unit</flag>`, `<unknown-use>keep</unknown-use>`,
		`<upstream custom="upstream">`, `<remote-id type="github">owner/repo</remote-id>`,
		`<unknown attr="x">upstream child</unknown>`, `<unknown-top attr="kept">top-level child</unknown-top>`,
		`<longdescription>New long desc.</longdescription>`,
		`<bugs-to>https://bugs.example.com</bugs-to>`, `<doc>https://docs.example.com</doc>`,
		`<remote-id type="pypi">mypackage</remote-id>`,
	} {
		require.Contains(t, text, expected)
	}
	require.Less(t, strings.Index(text, "before maintainer"), strings.Index(text, "between elements"))
	require.Less(t, strings.Index(text, "between elements"), strings.Index(text, "longdescription"))
	require.Less(t, strings.Index(text, "longdescription"), strings.Index(text, "remote-id"))
}

func TestMetadataAddsMaintainerWithoutDiscardingExistingChildren(t *testing.T) {
	metadata, err := ParseMetadata([]byte(`<pkgmetadata><maintainer custom="x"><email>a@example.com</email><unknown>keep</unknown></maintainer></pkgmetadata>`))
	require.NoError(t, err)
	require.NoError(t, metadata.AddMaintainers([]config.GentooMaintainer{
		{Email: "a@example.com", Name: "A", Type: "person"},
		{Email: "b@example.com", Name: "B", Type: "project"},
	}))
	content, err := metadata.Render()
	require.NoError(t, err)
	require.Contains(t, string(content), `<unknown>keep</unknown>`)
	require.Contains(t, string(content), `<maintainer custom="x" type="person">`)
	require.Contains(t, string(content), `<maintainer type="project">`)
	require.ElementsMatch(t, []string{"a@example.com", "b@example.com"}, metadata.MaintainerEmails())
}

func TestMetadataUpstreamRemoteIDs(t *testing.T) {
	metadata := NewMetadata()
	metadata.SetUpstream("", "", []config.GentooUpstreamRemoteID{
		{Type: "github", ID: "owner/repo"},
		{Type: "github", ID: "owner/repo"}, // duplicate should be ignored
		{Type: "pypi", ID: "mypackage"},
	})
	content, err := metadata.Render()
	require.NoError(t, err)
	text := string(content)
	require.Equal(t, 1, strings.Count(text, `<remote-id type="github">owner/repo</remote-id>`))
	require.Equal(t, 1, strings.Count(text, `<remote-id type="pypi">mypackage</remote-id>`))

	// Add another existing
	metadata.SetUpstream("", "", []config.GentooUpstreamRemoteID{
		{Type: "github", ID: "owner/repo"},
		{Type: "gitlab", ID: "owner/repo"},
	})
	content, err = metadata.Render()
	require.NoError(t, err)
	text = string(content)
	require.Equal(t, 1, strings.Count(text, `<remote-id type="github">owner/repo</remote-id>`))
	require.Equal(t, 1, strings.Count(text, `<remote-id type="pypi">mypackage</remote-id>`))
	require.Equal(t, 1, strings.Count(text, `<remote-id type="gitlab">owner/repo</remote-id>`))
}

func TestParseMetadataRejectsMalformedXML(t *testing.T) {
	_, err := ParseMetadata([]byte("<pkgmetadata>"))
	require.Error(t, err)
}

func TestLayoutParsesPolicyAndAppliesConfigOverrides(t *testing.T) {
	layout := ParseLayout([]byte("manifest-hashes = SHA256 SHA512\nthin-manifests = true\ncache-formats = pms\n"))
	require.Equal(t, []string{"SHA256", "SHA512"}, layout.ManifestHashes())
	require.True(t, layout.ThinManifests())
	require.False(t, layout.SupportsMetaCache())

	thin := false
	overridden := layout.WithConfig(manifestConfig{hashes: []string{"BLAKE2B"}, thin: &thin})
	require.Equal(t, []string{"BLAKE2B"}, overridden.ManifestHashes())
	require.False(t, overridden.ThinManifests())
	require.False(t, overridden.SupportsMetaCache())
}

func TestLayoutRecognizesSupportedMetaCacheFormats(t *testing.T) {
	for _, format := range []string{"md5-dict", "md5-cache"} {
		layout := ParseLayout([]byte("cache-formats = " + format + "\n"))
		require.True(t, layout.SupportsMetaCache())
	}
	require.True(t, ParseLayout(nil).SupportsMetaCache())
}
