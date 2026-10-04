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

var museVersionPattern = regexp.MustCompile(`^Muse Code ([0-9]+\.[0-9]+\.[0-9]+) \(([0-9]+\.[0-9]+\.[0-9]+-R[0-9]+\.[0-9]+)\)$`)

// parseToolVersion reads the first line of Muse's version answer, such as
// Muse Code 1.4.2 (1.4.2-R4684.1), and returns both the release triple and
// the full build identity. The release triple must agree with the build
// id's triple; anything else is unparsable, never a guessed release.
func parseToolVersion(out []byte) (release, build string, err error) {
	line, _, _ := strings.Cut(string(out), "\n")
	fields := museVersionPattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(fields) != 3 || !strings.HasPrefix(fields[2], fields[1]+"-R") {
		return "", "", fmt.Errorf("the answer %q is not `Muse Code <release> (<release>-R<revision>)`", line)
	}
	return fields[1], fields[2], nil
}

// parseToolRelease reads the release triple out of Muse's version answer.
// Permission capabilities are keyed by the release, not the build
// revision; an unverified triple is detected here and refused by policy.go.
func parseToolRelease(out []byte) (string, error) {
	release, _, err := parseToolVersion(out)
	return release, err
}

// parseToolBuild reads the full build identity (release plus revision,
// such as 1.4.2-R4684.1) out of Muse's version answer. The interactive
// exec-plan sealer binds this identity: same-triple revisions are
// different builds, and the seal refuses revision drift.
func parseToolBuild(out []byte) (string, error) {
	_, build, err := parseToolVersion(out)
	return build, err
}
