package gentoo

import (
	"path"
	"slices"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

// These adapters keep older integration assertions readable while focused
// component tests exercise the new APIs directly.
func newExtraFilesProcessor(cfg config.Gentoo, artifacts []*artifact.Artifact, files map[string]string) *ExtraFiles {
	release := &Release{}
	for _, art := range artifacts {
		arch, _ := gentooArch(art.Goarch)
		binaries := artifact.ExtraOr(*art, artifact.ExtraBinaries, []string{})
		release.archives = append(release.archives, &Archive{
			artifact: art, id: artifact.ExtraOr(*art, artifact.ExtraID, "default"), goarch: art.Goarch, gentooArch: arch,
			wrappedIn: artifact.ExtraOr(*art, artifact.ExtraWrappedIn, ""), binaries: binaries,
			files: artifact.ExtraOr(*art, artifact.ExtraFiles, []string{}),
		})
	}
	return NewExtraFiles(&GentooConfig{raw: cfg}, release, files)
}

func doRun(ctx *context.Context, raw config.Gentoo, urlClient client.ReleaseURLTemplater) error {
	cfg, err := NewGentooConfig(ctx, raw)
	if err != nil {
		return err
	}
	return NewGenerator(ctx, cfg, urlClient).Generate()
}

func (f *ExtraFiles) Filter() error { return f.Prepare() }

func (f *ExtraFiles) inArchives(name string) bool {
	if len(f.release.archives) == 0 {
		return false
	}
	for _, archive := range f.release.archives {
		art := archive.artifact
		wrapped := artifact.ExtraOr(*art, artifact.ExtraWrappedIn, "")
		found := false
		candidates := append(artifact.ExtraOr(*art, artifact.ExtraFiles, []string{}), artifact.ExtraOr(*art, artifact.ExtraBinaries, []string{})...)
		for _, candidate := range candidates {
			if normalizeArchivePath(path.Join(wrapped, candidate)) == normalizeArchivePath(name) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (f *ExtraFiles) InstallExtraFiles(ctx *context.Context, ebuildPath string) error {
	generated, err := f.Write(ebuildPath)
	if err != nil {
		return err
	}
	for _, file := range generated {
		ctx.Artifacts.Add(file.Artifact())
	}
	return nil
}

func decomposeDestination(section, source, destination, defaultDir string) (StateFamily, string, string, error) {
	return resolveSectionOp(section).Descriptor().DecomposeDestination(source, destination, defaultDir)
}

func lowerInstallItemsFromConfig(section string, configured []config.GentooInstallItem, defaultDir string) ([]installItemData, error) {
	var result []installItemData
	for _, item := range configured {
		archs, err := gentooArchitectures(item.Archs)
		if err != nil {
			return nil, err
		}
		family, dir, base, err := decomposeDestination(section, item.Src, item.Dst, defaultDir)
		if err != nil {
			return nil, err
		}
		result = append(result, installItemData{Source: item.Src, Target: item.Dst, Dir: dir, Base: base, Use: slices.Clone(item.Use), Keywords: archs, Section: section, StateFamily: family})
	}
	return result, nil
}

func publishGenerated(ctx *context.Context, provider client.Client) error {
	inputs, err := collectPublicationInputs(ctx)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		publisher, err := NewPublisher(ctx, input.cfg, input.files, provider)
		if err != nil {
			return err
		}
		if err := publisher.Publish(ctx); err != nil {
			return err
		}
	}
	return nil
}

func gentooArtifactExtra(configID, repoPath string) map[string]any {
	return GeneratedFile{ConfigID: configID, RepoPath: repoPath, Kind: GeneratedAux, Path: "artifact"}.Artifact().Extra
}

func handleGentooManifestAndMetadata(ctx *context.Context, cfg config.Gentoo, repoClient any, files *[]client.RepoFile, deleted []string) error {
	changes := NewChangeSet((*files)...)
	if err := prepareManifestAndMetadata(ctx, cfg, repoClient, client.Repo{}, changes, deleted); err != nil {
		return err
	}
	*files = changes.Files()
	return nil
}
