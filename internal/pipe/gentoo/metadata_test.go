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
  <doc lang="de">https://example.com/de</doc>
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
	require.NoError(t, metadata.SetUpstream("https://bugs.example.com", "https://docs.example.com", []config.GentooUpstreamRemoteID{{Type: "pypi", ID: "mypackage"}}))

	output, err := metadata.Render()
	require.NoError(t, err)
	text := string(output)
	for _, expected := range []string{
		`custom="root"`, `<!-- before maintainer -->`, `<!-- between elements -->`,
		`<maintainer type="person" custom="x">`, `<name>Alice Updated</name>`,
		`<description>Lead maintainer</description>`, `maintainer-attr="kept"`,
		`<doc lang="de">https://example.com/de</doc>`,
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
		{Email: "a@example.com", Name: "A"}, // omitted defaults to person
		{Email: "b@example.com", Name: "B", Type: "project"},
	}))
	content, err := metadata.Render()
	require.NoError(t, err)
	require.Contains(t, string(content), `<unknown>keep</unknown>`)
	require.Contains(t, string(content), `<maintainer custom="x" type="person">`)
	require.Contains(t, string(content), `<maintainer type="project">`)
	require.ElementsMatch(t, []string{"a@example.com", "b@example.com"}, metadata.MaintainerEmails())

	err = metadata.AddMaintainers([]config.GentooMaintainer{
		{Email: "invalid@example.com", Name: "Invalid", Type: "invalid"},
	})
	require.ErrorContains(t, err, "invalid gentoo maintainer type \"invalid\"")
}

func TestMetadataUpstreamRemoteIDs(t *testing.T) {
	metadata := NewMetadata()
	require.NoError(t, metadata.SetUpstream("", "", []config.GentooUpstreamRemoteID{
		{Type: " github ", ID: " owner/repo "},
		{Type: " github ", ID: " owner/repo "}, // duplicate should be ignored
		{Type: " pypi ", ID: " mypackage "},
	}))
	content, err := metadata.Render()
	require.NoError(t, err)
	text := string(content)
	require.Equal(t, 1, strings.Count(text, `<remote-id type="github">owner/repo</remote-id>`))
	require.Equal(t, 1, strings.Count(text, `<remote-id type="pypi">mypackage</remote-id>`))

	// Add another existing
	require.NoError(t, metadata.SetUpstream("", "", []config.GentooUpstreamRemoteID{
		{Type: "github", ID: "owner/repo"},
		{Type: "gitlab", ID: "owner/repo"},
	}))
	content, err = metadata.Render()
	require.NoError(t, err)
	text = string(content)
	require.Equal(t, 1, strings.Count(text, `<remote-id type="github">owner/repo</remote-id>`))
	require.Equal(t, 1, strings.Count(text, `<remote-id type="pypi">mypackage</remote-id>`))
	require.Equal(t, 1, strings.Count(text, `<remote-id type="gitlab">owner/repo</remote-id>`))

	err = metadata.SetUpstream("", "", []config.GentooUpstreamRemoteID{{ID: "foo"}})
	require.ErrorContains(t, err, "remote_ids[0] is invalid: type is required for id \"foo\"")
	err = metadata.SetUpstream("", "", []config.GentooUpstreamRemoteID{{Type: "foo"}})
	require.ErrorContains(t, err, "remote_ids[0] is invalid: id is required for type \"foo\"")

	err = metadata.SetUpstream("", "", []config.GentooUpstreamRemoteID{{Type: "   ", ID: "foo"}})
	require.ErrorContains(t, err, "remote_ids[0] is invalid: type is required for id \"foo\"")

	err = metadata.SetUpstream("", "", []config.GentooUpstreamRemoteID{{Type: "foo", ID: "   "}})
	require.ErrorContains(t, err, "remote_ids[0] is invalid: id is required for type \"foo\"")

	// Ensure nothing was committed to the metadata node from the failed calls
	content2, _ := metadata.Render()
	text = string(content2)
	require.Equal(t, 0, strings.Count(text, `<remote-id type="foo">`))

	require.NoError(t, metadata.SetUpstream("", "", []config.GentooUpstreamRemoteID{
		{Type: "github", ID: "new/repo"},
	}))
	content, _ = metadata.Render()
	require.Equal(t, 1, strings.Count(string(content), `<remote-id type="github">new/repo</remote-id>`))
	require.Equal(t, 1, strings.Count(string(content), `<remote-id type="github">owner/repo</remote-id>`))
}

func TestMetadataUpstreamDoc(t *testing.T) {
	metadata, err := ParseMetadata([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata>
  <upstream>
    <doc lang="de">https://example.com/de</doc>
    <doc lang="fr">https://example.com/fr</doc>
  </upstream>
</pkgmetadata>`))
	require.NoError(t, err)
	require.NoError(t, metadata.SetUpstream("", "https://example.com/generic", nil))
	content, err := metadata.Render()
	require.NoError(t, err)
	text := string(content)
	require.Contains(t, text, `<doc lang="de">https://example.com/de</doc>`)
	require.Contains(t, text, `<doc lang="fr">https://example.com/fr</doc>`)
	require.Contains(t, text, `<doc>https://example.com/generic</doc>`)

	require.NoError(t, metadata.SetUpstream("", "https://example.com/new", nil))
	content, err = metadata.Render()
	require.NoError(t, err)
	text = string(content)
	require.Contains(t, text, `<doc lang="de">https://example.com/de</doc>`)
	require.Contains(t, text, `<doc lang="fr">https://example.com/fr</doc>`)
	require.Contains(t, text, `<doc>https://example.com/new</doc>`)
	require.NotContains(t, text, `<doc>https://example.com/generic</doc>`)
}

func TestMetadataSetUpstreamAtomicity(t *testing.T) {
	original := `<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata>
  <upstream custom="keep">
    <bugs-to>old-bugs</bugs-to>
    <doc>old-doc</doc>
    <doc lang="de">old-localized-doc</doc>
    <remote-id type="github">existing/repo</remote-id>
    <unknown>keep-me</unknown>
  </upstream>
</pkgmetadata>`
	metadata, err := ParseMetadata([]byte(original))
	require.NoError(t, err)
	renderedOriginal, _ := metadata.Render()

	err = metadata.SetUpstream("new-bugs", "new-doc", []config.GentooUpstreamRemoteID{{Type: "github", ID: "valid/repo"}, {Type: "   ", ID: "invalid"}})
	require.ErrorContains(t, err, "remote_ids[1] is invalid: type is required for id \"invalid\"")
	content, _ := metadata.Render()
	require.Equal(t, string(renderedOriginal), string(content))
}

func TestMetadataLongDescription(t *testing.T) {
	metadata, err := ParseMetadata([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata>
  <longdescription>old</longdescription>
  <longdescription lang="de">localized</longdescription>
</pkgmetadata>`))
	require.NoError(t, err)
	metadata.SetLongDescription("new")
	content, err := metadata.Render()
	require.NoError(t, err)
	text := string(content)
	require.Contains(t, text, `<longdescription lang="de">localized</longdescription>`)
	require.Contains(t, text, `<longdescription>new</longdescription>`)
	require.NotContains(t, text, `<longdescription>old</longdescription>`)

	// Ensure no duplicate unqualified nodes are added
	require.Equal(t, 1, strings.Count(text, `<longdescription>`))
	// 1 open tag without attributes + 2 close tags (one for localized, one for unqualified)
	require.Equal(t, 2, strings.Count(text, `</longdescription>`))
}

func TestMetadataIdempotency(t *testing.T) {
	metadata, err := ParseMetadata([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<pkgmetadata>
  <!-- existing comment -->
  <longdescription lang="de">localized</longdescription>
  <unknown-element />
  <upstream>
    <doc lang="de">docs-de</doc>
    <remote-id type="github">existing/repo</remote-id>
  </upstream>
</pkgmetadata>`))
	require.NoError(t, err)
	metadata.SetLongDescription("long")
	require.NoError(t, metadata.AddMaintainers([]config.GentooMaintainer{
		{Email: "a@example.com", Name: "A", Type: "person"},
	}))
	require.NoError(t, metadata.SetUpstream("bugs", "docs", []config.GentooUpstreamRemoteID{
		{Type: "github", ID: "owner/repo"},
	}))
	content, err := metadata.Render()
	require.NoError(t, err)

	parsed, err := ParseMetadata(content)
	require.NoError(t, err)
	parsed.SetLongDescription("long")
	require.NoError(t, parsed.AddMaintainers([]config.GentooMaintainer{
		{Email: "a@example.com", Name: "A", Type: "person"},
	}))
	require.NoError(t, parsed.SetUpstream("bugs", "docs", []config.GentooUpstreamRemoteID{
		{Type: "github", ID: "owner/repo"},
	}))
	content2, err := parsed.Render()
	require.NoError(t, err)
	require.Equal(t, string(content), string(content2))
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
