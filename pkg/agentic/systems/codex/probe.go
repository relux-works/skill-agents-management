package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/toolprobe"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// ProbeToolRelease implements agentic.ToolReleaseProber: it resolves the
// binary through the same path every launch uses (the managed package,
// the unwrapped shim, or PATH — binary.go), runs `<binary> --version`
// against the launch environment, and parses this tool's release out of
// the answer.
//
// Every failure is ErrToolReleaseUndetected (joined with its cause), and
// the caller answers it by passing ToolRelease "" and planning anyway —
// never by synthesizing a release. Yolo then fails closed in the
// capability lookup and native forwards verbatim.
func (*System) ProbeToolRelease(ctx context.Context, env []string) (string, error) {
	binary, err := resolveBinary(env)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("codex: resolving the binary: %v", err))
	}
	out, err := toolprobe.VersionOutput(ctx, binary, env)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("codex: probing the binary: %v", err))
	}
	release, err := parseToolRelease(out)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("codex: parsing the answer: %v", err))
	}
	return release, nil
}

// parseToolRelease reads the release out of `codex --version`: exactly
// `codex-cli <triple>` on the first line (`codex-cli 0.153.4` at
// installed 0.153.4). Anything else — a missing tag, extra fields, a
// second field that is not an exact triple — is undetected, never
// guessed at.
func parseToolRelease(out []byte) (string, error) {
	line, _, _ := strings.Cut(string(out), "\n")
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "codex-cli" || !toolprobe.IsReleaseTriple(fields[1]) {
		return "", fmt.Errorf("the answer %q is not `codex-cli <release>`", line)
	}
	return fields[1], nil
}
