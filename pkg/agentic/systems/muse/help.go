package muse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/toolprobe"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// museHelpEvidenceSchema versions the bound yolo help evidence. A future
// evidence shape takes a new token; this one stays frozen.
const museHelpEvidenceSchema = "muse-help-v1"

// helpDeclaration is the parsed answer to the only question the yolo
// mapper asks: does this help output declare the bypass flag as its own
// option? It carries no release, no version and no arity: arity is never
// inferred from an unknown option, and the release never gates the map.
type helpDeclaration struct {
	declared bool
	// lines holds the matched declaration lines joined with "\n",
	// empty when nothing declared the flag.
	lines string
}

// parseHelpDeclaration matches an option-declaration line for exactly
// the standalone flag: a line whose first whitespace-trimmed token
// starts with the flag followed by a boundary (end of line, blank or
// comma). A substring in prose or an example never matches, because
// prose does not start a line with the bare option. The flag arrives as
// a parameter so this matcher names no CLI spelling of its own.
func parseHelpDeclaration(help []byte, flag string) helpDeclaration {
	if flag == "" {
		return helpDeclaration{}
	}
	var matched []string
	for _, line := range strings.Split(string(help), "\n") {
		trimmed := strings.TrimLeft(strings.TrimSuffix(line, "\r"), " \t")
		if trimmed == flag {
			matched = append(matched, trimmed)
			continue
		}
		if !strings.HasPrefix(trimmed, flag) {
			continue
		}
		if rest := trimmed[len(flag):]; rest != "" {
			switch rest[0] {
			case ' ', '\t', ',':
				matched = append(matched, trimmed)
			}
		}
	}
	if len(matched) == 0 {
		return helpDeclaration{}
	}
	return helpDeclaration{declared: true, lines: strings.Join(matched, "\n")}
}

// helpEvidence is the bound yolo evidence the seal carries and
// re-verifies: the schema, the full help stdout digest, the matched
// declaration lines and their digest, and the attested build and binary
// digest the help came from. Help documents syntax; it proves no future
// behavior and no harmlessness.
type helpEvidence struct {
	schema             string
	fullStdoutSHA256   string
	matchedLinesSHA256 string
	matchedLines       string
	build              string
	sha256             string
}

// probeMuseHelpEvidence runs the bounded help probe against the selected
// binary and binds the yolo evidence for the attested build and digest.
// A probe failure returns the typed attempt error for host routing; a
// help text without the bypass declaration refuses as unsupported on
// this binary, never as a release-list miss.
func probeMuseHelpEvidence(ctx context.Context, binary string, env []string, build, digest string) (helpEvidence, error) {
	out, err := toolprobe.HelpOutput(ctx, binary, env)
	if err != nil {
		return helpEvidence{}, fmt.Errorf("muse: probing help evidence: %w", err)
	}
	declaration := parseHelpDeclaration(out, museYoloFlag)
	// The internal permission mapper gates the evidence: the argv already
	// carries the mapped flag, so only its refusal is consumed here.
	if _, err := permissionMappingWithEvidence(declaration, agentic.PermissionModeYolo); err != nil {
		return helpEvidence{}, err
	}
	fullSum := sha256.Sum256(out)
	linesSum := sha256.Sum256([]byte(declaration.lines))
	return helpEvidence{
		schema:             museHelpEvidenceSchema,
		fullStdoutSHA256:   hex.EncodeToString(fullSum[:]),
		matchedLinesSHA256: hex.EncodeToString(linesSum[:]),
		matchedLines:       declaration.lines,
		build:              build,
		sha256:             digest,
	}, nil
}
