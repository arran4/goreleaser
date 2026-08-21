package gentoo

import (
	"bytes"

	"errors"
	"fmt"
	"strconv"

	"github.com/goreleaser/goreleaser/v2/pkg/context"
)

func gentooArch(goarch string) (string, error) {
	switch goarch {
	case "386": return "x86", nil
	case "amd64": return "amd64", nil
	case "arm": return "arm", nil
	case "arm64": return "arm64", nil
	case "mips": return "mips", nil
	case "mips64", "mips64le": return "mips64", nil
	case "ppc": return "ppc", nil
	case "ppc64": return "ppc64", nil
	case "ppc64le": return "", errors.New("ppc64le is not supported by gentoo pipe due to ambiguity with ppc64")
	case "riscv64": return "riscv", nil
	case "s390x": return "s390", nil
	default: return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
}

func checkSkip(ctx *context.Context, skipUpload string) bool {
	if skipUpload == "auto" {
		return ctx.Snapshot
	}
	b, err := strconv.ParseBool(skipUpload)
	return err == nil && b
}


func stripComments(content []byte) []byte {
	var result []byte
	for line := range bytes.SplitSeq(content, []byte{'\n'}) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 && trimmed[0] != '#' {
			result = append(result, line...)
			result = append(result, '\n')
		}
	}
	return result
}
