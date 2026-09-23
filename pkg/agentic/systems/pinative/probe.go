package pinative

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/toolprobe"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// ProbeToolRelease implements agentic.ToolReleaseProber: it resolves the
// raw `pi` binary through the same path every launch uses, runs `<binary>
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
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("pinative: resolving the binary: %v", err))
	}
	out, err := toolprobe.VersionOutput(ctx, binary, env)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("pinative: probing the binary: %v", err))
	}
	release, err := parseToolRelease(out)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("pinative: parsing the answer: %v", err))
	}
	return release, nil
}

// parseToolRelease reads the release out of `pi --version`: the whole
// trimmed answer, `0.84.2` at installed 0.84.2. Anything else — an
// empty answer, anything but an exact triple — is undetected, never
// guessed at.
func parseToolRelease(out []byte) (string, error) {
	if release := strings.TrimSpace(string(out)); toolprobe.IsReleaseTriple(release) {
		return release, nil
	}
	return "", fmt.Errorf("the answer %q is not a release triple", strings.TrimSpace(string(out)))
}
