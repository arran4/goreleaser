package gentoo

import (
	"bytes"
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/template"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
)

//go:embed templates/ebuild.tmpl
var ebuildTemplate string

//go:embed templates/md5-cache.tmpl
var metaCacheTemplate string

type archItem struct {
	File string
	URI  string
}

type archData struct {
	Keyword string
	URIs    []archItem
}

// Ebuild is a rendered package definition built from a normalized Release and
// an already reduced install program. It never performs archive selection or
// config install-item resolution.
type Ebuild struct {
	Name        string
	Description string
	Homepage    string
	License     string
	Keywords    string
	Archs       []archData
	UseFlags    []config.GentooUseFlag
	Eclasses    []string
	Plan        *InstallProgram
}

func (e Ebuild) Validate() error {
	if strings.TrimSpace(e.Description) == "" {
		return errors.New("gentoo description is required and cannot be empty")
	}
	if strings.TrimSpace(e.License) == "" {
		return errors.New("gentoo license is required and cannot be empty")
	}
	if e.Plan == nil {
		return nil
	}
	return e.Plan.Validate()
}

func (e Ebuild) HasEclasses() bool { return len(e.Eclasses) > 0 }

func shellEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, `$`, `\$`)
	return strings.ReplaceAll(value, "`", "\\`")
}

func (e Ebuild) InstallScript(indent string) string {
	if e.Plan == nil {
		return ""
	}
	return e.Plan.String(indent)
}

func (e Ebuild) Render() (string, error) {
	var buffer bytes.Buffer
	parsed := template.Must(template.New("ebuild").Funcs(template.FuncMap{"escape": shellEscape}).Parse(ebuildTemplate))
	if err := parsed.Execute(&buffer, e); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func (e Ebuild) SortedUseFlags() []string {
	var result []string
	for _, flag := range e.UseFlags {
		if flag.Flag != "" {
			result = append(result, flag.Flag)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func (e Ebuild) FormattedSrcURIs() []string {
	var result []string
	for _, architecture := range e.Archs {
		if architecture.Keyword == "" || len(architecture.URIs) == 0 {
			continue
		}
		files := make([]string, 0, len(architecture.URIs))
		for _, uri := range architecture.URIs {
			files = append(files, fmt.Sprintf("%s -> %s", uri.URI, uri.File))
		}
		result = append(result, fmt.Sprintf("%s? ( %s )", architecture.Keyword, strings.Join(files, " ")))
	}
	return result
}

// RenderMetaCache produces only the metadata knowable without evaluating
// eclasses. Callers must skip inherited eclasses rather than publishing a
// misleading cache entry.
func (e Ebuild) RenderMetaCache(content string) (string, error) {
	if e.HasEclasses() {
		return "", errors.New("cannot render metadata cache for ebuild with inherited eclasses")
	}
	hash := md5.Sum([]byte(content))
	data := struct {
		Description string
		Homepage    string
		IUSE        string
		Keywords    string
		License     string
		SrcURI      string
		MD5         string
	}{
		Description: e.Description, Homepage: e.Homepage, IUSE: strings.Join(e.SortedUseFlags(), " "),
		Keywords: e.Keywords, License: e.License, SrcURI: strings.Join(e.FormattedSrcURIs(), " "), MD5: hex.EncodeToString(hash[:]),
	}
	var buffer bytes.Buffer
	if err := template.Must(template.New("md5-cache").Parse(metaCacheTemplate)).Execute(&buffer, data); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func generateMetaCacheContent(ebuild Ebuild, content string) string {
	meta, err := ebuild.RenderMetaCache(content)
	if err != nil {
		return ""
	}
	return meta
}

func gentooArch(goarch string) (string, error) {
	switch goarch {
	case "386":
		return "x86", nil
	case "amd64":
		return "amd64", nil
	case "arm":
		return "arm", nil
	case "arm64":
		return "arm64", nil
	case "loong64":
		return "loong", nil
	case "riscv64":
		return "riscv", nil
	case "s390x":
		return "s390", nil
	default:
		return "", fmt.Errorf("unsupported or ambiguous architecture %q", goarch)
	}
}
