package gentoo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
)

type GeneratedFileKind string

const (
	GeneratedEbuild    GeneratedFileKind = "ebuild"
	GeneratedAux       GeneratedFileKind = "aux"
	GeneratedMetaCache GeneratedFileKind = "meta-cache"
)

// GeneratedFile is the safe bridge between generation and publication. Its
// artifact metadata deliberately contains only identifiers and repository
// paths; resolved configuration and credentials remain in memory.
type GeneratedFile struct {
	ConfigID string
	RepoPath string
	Kind     GeneratedFileKind
	Path     string
}

func (f GeneratedFile) Artifact() *artifact.Artifact {
	typ := artifact.GentooFile
	if f.Kind == GeneratedEbuild {
		typ = artifact.GentooEbuild
	}
	name := filepath.Base(f.Path)
	if f.Kind == GeneratedAux {
		if index := strings.Index(filepath.ToSlash(f.RepoPath), "/files/"); index >= 0 {
			name = filepath.ToSlash(f.RepoPath)[index+1:]
		}
	}
	return &artifact.Artifact{
		Name: name,
		Path: f.Path,
		Type: typ,
		Extra: map[string]any{
			ebuildExtra:     GentooArtifactRef{ConfigID: f.ConfigID, RepoPath: f.RepoPath, MetaCache: f.Kind == GeneratedMetaCache},
			ebuildPathExtra: f.RepoPath,
			ebuildMetaCache: f.Kind == GeneratedMetaCache,
		},
	}
}

func GeneratedFileFromArtifact(art artifact.Artifact) (GeneratedFile, error) {
	ref, ok := decodeGentooArtifactRef(art.Extra[ebuildExtra])
	if !ok || ref.ConfigID == "" || ref.RepoPath == "" {
		return GeneratedFile{}, fmt.Errorf("gentoo artifact %q has no safe configuration reference", art.Name)
	}
	kind := GeneratedAux
	if art.Type == artifact.GentooEbuild {
		kind = GeneratedEbuild
	} else if ref.MetaCache {
		kind = GeneratedMetaCache
	}
	return GeneratedFile{ConfigID: ref.ConfigID, RepoPath: ref.RepoPath, Kind: kind, Path: art.Path}, nil
}

func decodeGentooArtifactRef(value any) (GentooArtifactRef, bool) {
	if ref, ok := value.(GentooArtifactRef); ok {
		return ref, true
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return GentooArtifactRef{}, false
	}
	configID, configOK := fields["ConfigID"].(string)
	repoPath, pathOK := fields["RepoPath"].(string)
	metaCache, _ := fields["MetaCache"].(bool)
	return GentooArtifactRef{ConfigID: configID, RepoPath: repoPath, MetaCache: metaCache}, configOK && pathOK
}

func (f GeneratedFile) Content() ([]byte, error) { return os.ReadFile(f.Path) }
