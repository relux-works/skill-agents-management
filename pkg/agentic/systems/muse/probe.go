package muse

import (
	"context"
	"errors"
	"fmt"

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
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("muse: resolving the binary: %w", err))
	}
	out, err := toolprobe.VersionOutput(ctx, binary, childEnv(env, agentic.LaunchRequest{}))
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("muse: probing the binary: %w", err))
	}
	var release string
	if parsed, err := parseToolRelease(out); err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("muse: parsing the answer: %w", err))
	} else {
		release = parsed
	}
	return release, nil
}

// parseToolVersion reads the first line of Muse's version answer, such as
// Muse Code 1.4.2 (1.4.2-R4684.1), and returns both the release triple and
// the full build identity through the shared build-identity grammar. The
// release triple must agree with the build id's triple; anything else is
// unparsable, never a guessed release.
func parseToolVersion(out []byte) (release, build string, err error) {
	if identity, err := ParseBuildIdentity(out); err != nil {
		return "", "", err
	} else {
		return identity.Release, identity.Build, nil
	}
}

// parseToolRelease reads the release triple out of Muse's version answer.
// Permission capabilities are keyed by the release, not the build
// revision; an unverified triple is detected here and refused by policy.go.
func parseToolRelease(out []byte) (string, error) {
	if release, _, err := parseToolVersion(out); err != nil {
		return "", err
	} else {
		return release, nil
	}
}
