// Package toolprobe runs one tool's `--version` probe: the `<binary>
// --version` subprocess a ToolReleaseProber resolves and parses.
//
// It performs NO caching and NO parsing: every call is a fresh subprocess
// read, and the release grammar is each tool's own (a plugin's probe.go).
// What is shared here is the part every probe must get identically right:
// the argv (exactly `--version`, never a flag that could start a session),
// the bounded wait (the caller's context, plus this package's own
// timeout — whichever fires first), the byte cap (a version answer is
// one line; anything past the cap is a refusal, not a truncation), and
// the environment discipline (the child sees the handed launch
// environment, or an empty one — never the ambient process environment,
// which the System contract forbids a plugin from reading).
package toolprobe

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// versionArg is the probe's whole argv after the binary. It is a const
// rather than a parameter so no caller can probe with anything else: a
// probe that could spell an arbitrary argv is a launcher wearing a
// probe's name.
const versionArg = "--version"

// versionTimeout is this package's own bound on a `--version`
// subprocess call. It composes with whatever deadline the caller's ctx
// already carries: context.WithTimeout always fires at the EARLIER of
// the two, so a caller with a tighter budget keeps it. The exact value
// is not a behavior contract — any bound in seconds serves — which is
// why no test pins it; the honoring of a fired context is what the
// tests prove, with a short caller ctx and a sleeping stub.
const versionTimeout = 10 * time.Second

// maxVersionBytes caps a `--version` answer. A version is one line;
// 64 KiB is orders of magnitude past that and still cheap to hold while
// the answer is checked. Past the cap the probe refuses rather than
// truncating: a truncated answer could parse as a release it is not.
const maxVersionBytes = 64 << 10

// VersionOutput runs binary with argv `--version` in env and returns its
// stdout. Stderr is discarded: all three probed tools print their
// release on stdout (claude `2.1.274 (Claude Code)`, codex `codex-cli
// 0.153.4`, pi `0.84.2`, each exit 0), and a diagnostic there must not
// become part of the parsed answer.
//
// A nil env runs the child with an EMPTY environment, not the process's
// own: exec would otherwise inherit ambient state the System contract
// forbids.
func VersionOutput(ctx context.Context, binary string, env []string) ([]byte, error) {
	if strings.TrimSpace(binary) == "" {
		return nil, fmt.Errorf("toolprobe: no binary to probe")
	}
	bounded, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(bounded, binary, versionArg)
	cmd.Env = env
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("toolprobe: %s --version: %w", binary, err)
	}
	if stdout.Len() > maxVersionBytes {
		return nil, fmt.Errorf("toolprobe: %s --version answered %d bytes, past the %d byte cap", binary, stdout.Len(), maxVersionBytes)
	}
	return stdout.Bytes(), nil
}

// IsReleaseTriple reports whether s is a dotted release triple:
// three dot-separated runs of ASCII digits, "2.1.261". There is no
// leading-zero rule and no prerelease suffix: the pinned releases are
// exact triples, and anything else fails closed at the probe (never
// established) rather than matching a row it merely resembles. A future
// verified release with a suffix re-verifies this rule with its grammar
// version.
func IsReleaseTriple(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false
			}
		}
	}
	return true
}
