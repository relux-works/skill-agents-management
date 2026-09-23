package claude

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/toolprobe"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// ProbeToolRelease implements agentic.ToolReleaseProber: it resolves the
// binary through the same path every launch uses, runs `<binary>
// --version` against the launch environment, and parses this tool's
// release out of the answer.
//
// Every failure is ErrToolReleaseUndetected (joined with its cause), and
// the caller answers it by passing ToolRelease "" and planning anyway —
// never by synthesizing a release. Yolo then fails closed in the
// capability lookup and native forwards verbatim.
func (*System) ProbeToolRelease(ctx context.Context, env []string) (string, error) {
	binary, err := resolveBinary(env)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("claude: resolving the binary: %v", err))
	}
	out, err := toolprobe.VersionOutput(ctx, binary, env)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("claude: probing the binary: %v", err))
	}
	release, err := parseToolRelease(out)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("claude: parsing the answer: %v", err))
	}
	return release, nil
}

// parseToolRelease reads the release out of `claude --version`: the
// first field of the first line, `2.1.274 (Claude Code)` at installed
// 2.1.274. Anything else — an empty answer, a first field that is not
// an exact triple — is undetected, never guessed at.
func parseToolRelease(out []byte) (string, error) {
	line, _, _ := strings.Cut(string(out), "\n")
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", fmt.Errorf("the answer is empty")
	}
	if !toolprobe.IsReleaseTriple(fields[0]) {
		return "", fmt.Errorf("the first field %q is not a release triple", fields[0])
	}
	return fields[0], nil
}
