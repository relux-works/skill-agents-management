package muse

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/toolprobe"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

var _ agentic.ToolReleaseProber = (*System)(nil)

// ProbeToolRelease resolves the launch binary and runs --version with the same
// curated child environment, including the forced MUSE_NO_AUTO_UPDATE=1 pin.
// The shared probe bounds the context; every failure is undetected, never a
// synthesized release or a permission capability claim.
func (*System) ProbeToolRelease(ctx context.Context, env []string) (string, error) {
	binary, err := resolveBinary(env)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("muse: resolving the binary: %v", err))
	}
	out, err := toolprobe.VersionOutput(ctx, binary, childEnv(env, agentic.LaunchRequest{}))
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("muse: probing the binary: %v", err))
	}
	release, err := parseToolRelease(out)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("muse: parsing the answer: %v", err))
	}
	return release, nil
}

var museVersionPattern = regexp.MustCompile(`^Muse Code ([0-9]+\.[0-9]+\.[0-9]+) \(([0-9]+\.[0-9]+\.[0-9]+)-R[0-9]+\.[0-9]+\)$`)

// parseToolRelease reads the first line of Muse's version answer, such as
// Muse Code 1.4.2 (1.4.2-R4684.1). The release triple must agree with the build
// id's triple. Permission capabilities are keyed by the release, not the build
// revision; an unverified triple is detected here and refused by policy.go.
func parseToolRelease(out []byte) (string, error) {
	line, _, _ := strings.Cut(string(out), "\n")
	fields := museVersionPattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(fields) != 3 || fields[1] != fields[2] {
		return "", fmt.Errorf("the answer %q is not `Muse Code <release> (<release>-R<revision>)`", line)
	}
	return fields[1], nil
}
