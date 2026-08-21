// Package gentoo implements the gentoo ebuild pipe.
package gentoo

import (
	"errors"
	"fmt"
	"strings"

	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/commitauthor"
	"github.com/goreleaser/goreleaser/v2/internal/ids"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

const (
	ebuildExtra     = "GentooArtifactRef"
	ebuildPathExtra = "GentooPath"
	ebuildMetaCache = "GentooMetaCache"
)

// Pipe builds and publishes gentoo ebuilds.
type Pipe struct{}

func (Pipe) String() string        { return "gentoo ebuild" }
func (Pipe) ContinueOnError() bool { return true }
func (Pipe) Skip(ctx *context.Context) bool {
	return len(ctx.Config.Gentoos) == 0
}

func (Pipe) Default(ctx *context.Context) error {
	ids := ids.New("gentoo_overlays")
	for i := range ctx.Config.Gentoos {
		if err := defaultGentooConfig(ctx, &ctx.Config.Gentoos[i]); err != nil {
			return err
		}
		ids.Inc(ctx.Config.Gentoos[i].ID)
	}
	return ids.Validate()
}

func defaultGentooConfig(ctx *context.Context, g *config.Gentoo) error {
	g.CommitAuthor = commitauthor.Default(g.CommitAuthor)
	if g.ID == "" {
		g.ID = "default"
	}
	if !g.Bin {
		return errors.New("bin must be true")
	}
	if g.CommitMessageTemplate == "" {
		g.CommitMessageTemplate = "{{ .ProjectName }}: bump to {{ .Tag }}"
	}
	if g.Type == "" {
		g.Type = "bin"
	}
	if g.Type != "bin" {
		return fmt.Errorf("invalid gentoo type %q: currently only \"bin\" is supported", g.Type)
	}
	if g.Bindir == "" {
		g.Bindir = "/opt/bin"
	}
	if g.License == "" {
		return errors.New("license is required")
	}
	if strings.TrimSpace(g.Description) == "" {
		g.Description = ctx.Config.ProjectName
	}
	if strings.TrimSpace(g.Description) == "" {
		return errors.New("description is required")
	}
	if g.ConflictResolution == "" {
		g.ConflictResolution = config.ConflictResolutionRevision
	}
	if g.ConflictResolution != config.ConflictResolutionFail && g.ConflictResolution != config.ConflictResolutionOverwrite && g.ConflictResolution != config.ConflictResolutionRevision {
		return fmt.Errorf("conflict_resolution %q is not valid, must be one of [Fail, Overwrite, Revision]", g.ConflictResolution)
	}
	if g.KeepVersions < 0 {
		return errors.New("keep_versions must be greater than or equal to 0")
	}
	if g.VersionRetentionStrategy != "" && g.VersionRetentionStrategy != config.VersionRetentionStrategyKeepLatest && g.VersionRetentionStrategy != config.VersionRetentionStrategyKeepPrereleases {
		return fmt.Errorf("version_retention_strategy %q is not valid, must be one of [keep_latest, keep_prereleases]", g.VersionRetentionStrategy)
	}
	if g.KeepVersions > 0 && g.VersionRetentionStrategy == "" {
		return errors.New("version_retention_strategy must be provided if keep_versions > 0")
	}
	if g.Name == "" {
		g.Name = ctx.Config.ProjectName
	}
	if g.Category == "" {
		g.Category = "app-misc"
	}
	return nil
}

func (Pipe) Run(ctx *context.Context) error {
	cl, err := client.NewReleaseClient(ctx)
	if err != nil {
		return err
	}
	return runAll(ctx, cl)
}

func runAll(ctx *context.Context, cl client.ReleaseURLTemplater) error {
	configs, err := NewGentooConfigs(ctx, ctx.Config.Gentoos)
	if err != nil {
		return err
	}
	for _, cfg := range configs.Entries() {
		if err := NewGenerator(ctx, cfg, cl).Generate(); err != nil {
			return err
		}
	}
	return nil
}

func (Pipe) Publish(ctx *context.Context) error {
	inputs, err := collectPublicationInputs(ctx)
	if err != nil || len(inputs) == 0 {
		return err
	}
	cl, err := client.New(ctx)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		publisher, err := NewPublisher(ctx, input.cfg, input.files, cl)
		if err != nil {
			return err
		}
		if err := publisher.Publish(ctx); err != nil {
			return err
		}
	}
	return nil
}
