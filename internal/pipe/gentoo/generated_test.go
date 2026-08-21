package gentoo

import (
	"encoding/json"
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/stretchr/testify/require"
)

func TestGeneratedFileArtifactRoundTripUsesSafeReference(t *testing.T) {
	generated := GeneratedFile{ConfigID: "default", RepoPath: "app-misc/foo-bin/foo-bin-1.0.ebuild", Kind: GeneratedEbuild, Path: "/tmp/foo.ebuild"}
	artifact := generated.Artifact()
	roundTrip, err := GeneratedFileFromArtifact(*artifact)
	require.NoError(t, err)
	require.Equal(t, generated, roundTrip)
	encoded, err := json.Marshal(artifact.Extra)
	require.NoError(t, err)
	text := string(encoded)
	require.NotContains(t, text, "repository.token")
	require.NotContains(t, text, "private_key")
	require.Contains(t, text, "default")
	require.Contains(t, text, "foo-bin-1.0.ebuild")
}

func TestGeneratedFileArtifactReferenceSurvivesJSONRoundTrip(t *testing.T) {
	generated := GeneratedFile{ConfigID: "default", RepoPath: "app-misc/foo-bin/files/foo.conf", Kind: GeneratedAux, Path: "/tmp/foo.conf"}
	encoded, err := json.Marshal(generated.Artifact().Extra)
	require.NoError(t, err)
	var extra map[string]any
	require.NoError(t, json.Unmarshal(encoded, &extra))
	decoded, err := GeneratedFileFromArtifact(artifact.Artifact{Name: "foo.conf", Path: generated.Path, Type: artifact.GentooFile, Extra: extra})
	require.NoError(t, err)
	require.Equal(t, generated, decoded)
}

func TestGeneratedFileFromArtifactRejectsUnsafeOrMissingReference(t *testing.T) {
	_, err := GeneratedFileFromArtifact(*GeneratedFile{Path: "/tmp/foo"}.Artifact())
	require.ErrorContains(t, err, "has no safe configuration reference")
}
