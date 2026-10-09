// Interactive exec-plan sealer for the Muse plugin.
//
// Every Muse LaunchModeInteractive plan carries this seal. It binds the
// absolute binary path plus the SHA-256 of the probed binary, the exact
// final argv, the actual attested full build identity (any well-formed
// build the binary attests — there is no verified-build list), the
// effective permission posture, and the XDG_* override identity plus
// MUSE_NO_AUTO_UPDATE=1. Yolo seals additionally bind the help evidence
// the mapping resolved from: the full help stdout digest and the matched
// option-declaration digest. VerifyBeforeExec refuses typed on a binary
// swap, an argv change, a release mismatch, a help-evidence drift, or a
// changed or duplicated XDG_*/MUSE_NO_AUTO_UPDATE entry. A binary that
// attests no well-formed build — silence or a misattributed answer —
// refuses typed at plan time: no interactive plan ships unsealed.
//
// The seal is exportable through the R-SEAL API: ExportSeal carries the
// bound process over the closed wire and ImportSeal rebuilds an equivalent
// verifier, so the hosted R2 import constructor can refuse unsealed Muse
// plans instead of recomposing them. The exported guard is sealed and
// therefore NOT hosted-admissible: no closed sealed Muse schema is pinned
// in PinnedHostedSchema until that shape is final. Exported environment
// selectors carry names plus keyed commitments, never literal values.
//
// The managed-network exec sealer in network_launch.go is unchanged and
// still exports nothing: hosted launches refuse managed network scope
// before sealing, so a network plan has no hosted import path. Plain
// exec and dry-run plans carry no seal, exactly as before.
//
// Sealing probes the binary (`--version` through the same ToolReleaseProber
// path probe.go owns, parsed for the full build identity, plus `--help`
// for yolo plans) and hashes its bytes. Verification re-probes and
// re-hashes: the release re-probe is what catches a same-bytes dispatch
// shim resolving a different effective release behind the sealed path,
// which the byte hash alone cannot see.
package muse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/relux-works/skill-agents-management/internal/toolprobe"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

var (
	// ErrMuseSealBinaryChanged refuses a plan whose binary no longer
	// matches the seal: another path, unreadable or non-regular bytes, or
	// bytes whose SHA-256 differs from the sealed digest.
	ErrMuseSealBinaryChanged = errors.New("muse: sealed binary changed after planning")
	// ErrMuseSealReleaseChanged refuses a plan whose binary now probes
	// to a different tool release than the sealed one. Same bytes can
	// still resolve a different release behind a dispatch shim, which is
	// why this check runs even when the byte hash matches.
	ErrMuseSealReleaseChanged = errors.New("muse: sealed tool release changed after planning")
	// ErrMuseSealHelpChanged refuses a yolo plan whose binary help no
	// longer matches the sealed help evidence: a changed help digest or
	// a lost option declaration. Sealed evidence drift is an integrity
	// refusal, never a mapping question.
	ErrMuseSealHelpChanged = errors.New("muse: sealed help evidence changed after planning")
	// ErrMuseToolReleaseMismatch refuses a plan whose caller-supplied
	// tool release disagrees with what the sealed binary attests. The
	// host's earlier probe is not permission to trust caller-supplied
	// version text.
	ErrMuseToolReleaseMismatch = errors.New("muse: caller tool release does not match the attested release")
	// ErrMuseSealArgvChanged refuses a plan whose argv no longer equals
	// the exact sealed argv.
	ErrMuseSealArgvChanged = errors.New("muse: sealed argv changed after planning")
	// ErrMuseSealEnvChanged refuses a plan whose sealed environment
	// selectors no longer match: a changed XDG_* or MUSE_NO_AUTO_UPDATE
	// value, a selector that appeared or disappeared, or a duplicated
	// selector entry. Duplicates refuse even when every copy carries the
	// sealed value, because the child resolves them last-wins.
	ErrMuseSealEnvChanged = errors.New("muse: sealed environment selectors changed after planning")
	// ErrMuseSealMalformed refuses a seal that cannot be formed or
	// imported: a non-absolute binary, an unreadable binary at seal
	// time, or sealed guard data whose artifact or selector shape is not
	// this sealer's own.
	ErrMuseSealMalformed = errors.New("muse: exec-guard seal data is malformed")
)

// Seal artifact and selector labels. The artifact names the probed binary
// file the verifier re-hashes; the release selector carries the attested
// full build; the mode selector carries the sealed posture; the help
// selectors carry the yolo help evidence digests. None of them can
// collide with a sealed environment name: the seal only ever reads XDG_*
// names and the updater pin, and none can collide with a finalized
// binding key either: those are dense "0".."N" indices.
const (
	sealedMuseBinaryName   = "muse-binary"
	sealedReleaseKey       = "tool-release"
	sealedModeKey          = "permission-mode"
	sealedHelpStdoutKey    = "help-stdout-sha256"
	sealedHelpLinesKey     = "help-flags-sha256"
	sealedKeyIDKey         = "commitment-key-id"
	sealedDigestPrefix     = "sha256:"
	sealedCommitmentPrefix = "hmac-sha256:"
)

// sealProbeAggregateTimeout is the aggregate wall deadline one sealer
// call imposes on its own probes: seal creation, pre-exec verification
// and finalized verification each run their version and help probes
// under it. Every nested probe keeps its own execution bound inside the
// aggregate; callers of a context-free verifier can incur at most this,
// never an unbounded wait.
const sealProbeAggregateTimeout = 30 * time.Second

var (
	_ agentic.ExecSealExporter             = (*interactiveExecSeal)(nil)
	_ agentic.ExecSealKeyedExporter        = (*interactiveExecSeal)(nil)
	_ agentic.ExecArtifactVerifier         = (*interactiveExecSeal)(nil)
	_ agentic.ExecFinalizedPlanVerifier    = (*interactiveExecSeal)(nil)
	_ agentic.ExecFinalizeOverlayValidator = (*interactiveExecSeal)(nil)
	_ agentic.ExecSealImporter             = (*System)(nil)
	_ agentic.ExecSealKeyedImporter        = (*System)(nil)
)

// interactiveExecSeal is the bound interactive plan: the absolute binary
// path, its byte digest, the exact argv, the attested full build, the
// sealed permission posture, the yolo help evidence digests, and the
// single-entry XDG_*/MUSE_NO_AUTO_UPDATE identity — values in memory,
// keyed commitments once imported.
type interactiveExecSeal struct {
	binary          string
	digest          string
	argv            []string
	release         string
	mode            agentic.PermissionMode
	helpFullDigest  string
	helpLinesDigest string
	env             map[string]string
	// committed reports the imported form: env holds keyed commitments
	// rather than sealed values.
	committed bool
	// commitKeyID is the ID of the key the imported commitments verify
	// under: the process-local ID for a same-process import, the explicit
	// finalization key's ID for a cross-process finalized import. It is
	// empty for an in-process (uncommitted) seal, which holds values.
	commitKeyID string
	// commitKey is the explicit verification key for a keyed import. It is
	// set only when the importer supplied one; a legacy process-local
	// import verifies through CommitSealLiteral instead, without holding
	// the process key bytes. The key never serializes.
	commitKey    agentic.SealCommitmentKey
	hasCommitKey bool
}

// sealInteractiveExecPlan binds an interactive plan and nothing else. A
// non-interactive plan has no seal basis here and keeps its existing
// behavior: the network sealer owns managed plans before this line is
// reached, and plain exec and dry-run plans stay unsealed.
//
// Establishment order is pin/hash -> version -> help -> mapping -> seal:
// the updater pin and binary digest come first, then the version probe
// attests the build, then the help probe acquires the yolo evidence the
// internal permission mapper gates, and only then is the seal built and
// immediately re-verified. Argv mapping probes nothing, so the sealed
// evidence is the only help answer the plan ever observes.
func sealInteractiveExecPlan(plan agentic.Plan) (agentic.ExecPlanVerifier, error) {
	if plan.Mode != agentic.LaunchModeInteractive {
		return nil, nil
	}
	if !filepath.IsAbs(plan.Binary) {
		return nil, fmt.Errorf("%w: sealed binary %q is not absolute", ErrMuseSealMalformed, plan.Binary)
	}
	var identity map[string]string
	if computed, err := interactiveSealEnvIdentity(plan.Env); err != nil {
		return nil, err
	} else {
		identity = computed
	}
	if envValue(plan.Env, museNoAutoUpdateEnv) != "1" {
		return nil, fmt.Errorf("%w: %s is not pinned to 1", ErrMuseSealEnvChanged, museNoAutoUpdateEnv)
	}
	var mode agentic.PermissionMode
	if resolved, err := plan.PermissionMode.Resolve(); err != nil {
		return nil, fmt.Errorf("%w: sealed permission mode: %w", ErrMuseSealMalformed, err)
	} else {
		mode = resolved
	}
	// Establishment runs pin/hash -> version -> help -> mapping -> seal:
	// the binary hash precedes the version probe, so an unhashable
	// binary refuses as malformed before any probe runs, and the yolo
	// mapping refusal precedes seal construction, so absent help
	// evidence refuses as unsupported before any seal exists to verify.
	probeCtx, cancel := context.WithTimeout(context.Background(), sealProbeAggregateTimeout)
	defer cancel()
	var digest string
	if hashed, err := hashInteractiveSealBinary(plan.Binary); err != nil {
		return nil, fmt.Errorf("%w: sealed binary cannot be read: %v", ErrMuseSealMalformed, err)
	} else {
		digest = hashed
	}
	// A seal binds an attested well-formed build or nothing at all: an
	// undetected build refuses typed here, at plan time. No interactive
	// plan ships unsealed, and no release list gates the admission.
	var probed BuildIdentity
	if identity, err := probeInteractiveSealBuild(probeCtx, plan.Binary, plan.Env); err != nil {
		return nil, err
	} else {
		probed = identity
	}
	if claimed := strings.TrimSpace(plan.ToolRelease); claimed != "" && claimed != probed.Release {
		return nil, fmt.Errorf("%w: caller claims release %q, binary attests %q", ErrMuseToolReleaseMismatch, claimed, probed.Release)
	}
	var helpFullDigest, helpLinesDigest string
	if mode == agentic.PermissionModeYolo {
		if evidence, err := probeMuseHelpEvidence(probeCtx, plan.Binary, plan.Env, probed.Build, digest); err != nil {
			return nil, err
		} else {
			helpFullDigest, helpLinesDigest = evidence.fullStdoutSHA256, evidence.matchedLinesSHA256
		}
	}
	seal := &interactiveExecSeal{
		binary:          plan.Binary,
		digest:          digest,
		argv:            append([]string(nil), plan.Argv...),
		release:         sealBuildIdentity(probed.Build),
		mode:            mode,
		helpFullDigest:  helpFullDigest,
		helpLinesDigest: helpLinesDigest,
		env:             maps.Clone(identity),
	}
	if err := seal.VerifyBeforeExec(plan); err != nil {
		return nil, err
	}
	return seal, nil
}

// verifyMuseUpdaterPin is the single updater-pin invariant every Muse
// verifier composes: the pin must read exactly "1". Both VerifyBeforeExec
// and VerifyFinalizedPlan call it before any hash or probe, so the
// finalized wrapper cannot drop the pin the way it once dropped the
// release check. A missing, zeroed or otherwise changed pin refuses
// without executing anything.
func verifyMuseUpdaterPin(env []string) error {
	if envValue(env, museNoAutoUpdateEnv) != "1" {
		return fmt.Errorf("%w: %s is not pinned to 1", ErrMuseSealEnvChanged, museNoAutoUpdateEnv)
	}
	return nil
}

// VerifyBeforeExec re-checks the sealed plan immediately before the
// consumer starts its own process. The updater pin is verified before the
// release re-probe: probing with auto-update enabled could reach the
// network through a launcher shim, so an unpinned plan refuses without
// executing anything. The byte hash precedes the release re-probe so a
// deleted binary reads as a binary swap rather than a probe failure; the
// release re-probe then guards the residual same-bytes class behind
// dispatch shims. The help re-probe guards the sealed yolo evidence the
// same way.
func (seal *interactiveExecSeal) VerifyBeforeExec(plan agentic.Plan) error {
	var identity map[string]string
	if computed, err := interactiveSealEnvIdentity(plan.Env); err != nil {
		return err // base verifier: duplicates before anything else
	} else {
		identity = computed
	}
	if plan.Binary != seal.binary {
		return fmt.Errorf("%w: binary is %q, sealed %q", ErrMuseSealBinaryChanged, plan.Binary, seal.binary)
	}
	if err := verifyMuseUpdaterPin(plan.Env); err != nil {
		return err // base verifier: pin before hash and probe
	}
	var digest string
	if hashed, err := hashInteractiveSealBinary(plan.Binary); err != nil {
		return fmt.Errorf("%w: sealed binary cannot be re-read: %v", ErrMuseSealBinaryChanged, err)
	} else {
		digest = hashed
	}
	if digest != seal.digest {
		return fmt.Errorf("%w: binary bytes no longer match the sealed digest", ErrMuseSealBinaryChanged)
	}
	probeCtx, cancel := context.WithTimeout(context.Background(), sealProbeAggregateTimeout)
	defer cancel()
	var release string
	if probed, err := probeInteractiveSealBuild(probeCtx, plan.Binary, plan.Env); err != nil {
		return err
	} else {
		release = sealBuildIdentity(probed.Build)
	}
	if release != seal.release {
		return fmt.Errorf("%w: probed %q, sealed %q", ErrMuseSealReleaseChanged, release, seal.release)
	}
	if seal.mode == agentic.PermissionModeYolo {
		if err := seal.verifyHelpEvidence(probeCtx, plan.Binary, plan.Env); err != nil {
			return err
		}
	}
	if !slices.Equal(plan.Argv, seal.argv) {
		return fmt.Errorf("%w: argv has %d elements, sealed %d", ErrMuseSealArgvChanged, len(plan.Argv), len(seal.argv))
	}
	if !seal.envIdentityMatches(identity) {
		return fmt.Errorf("%w: XDG_*/%s identity differs from the sealed identity", ErrMuseSealEnvChanged, museNoAutoUpdateEnv)
	}
	return nil
}

// verifyHelpEvidence re-probes the sealed yolo help evidence and refuses
// typed on any drift: a changed help digest or a lost option
// declaration. A probe failure is attempt evidence and propagates for
// host routing; only the evidence comparison itself is an integrity
// refusal.
func (seal *interactiveExecSeal) verifyHelpEvidence(ctx context.Context, binary string, env []string) error {
	if current, err := probeMuseHelpEvidence(ctx, binary, env, seal.release, seal.digest); err != nil {
		if errors.Is(err, agentic.ErrPermissionModeUnsupported) {
			return fmt.Errorf("%w: sealed help declaration is gone from the binary help", ErrMuseSealHelpChanged)
		}
		return err
	} else if current.fullStdoutSHA256 != seal.helpFullDigest || current.matchedLinesSHA256 != seal.helpLinesDigest {
		return fmt.Errorf("%w: binary help no longer matches the sealed evidence", ErrMuseSealHelpChanged)
	}
	return nil
}

// envIdentityMatches compares the presented identity against the sealed
// one. An in-process seal holds values; an imported seal holds keyed
// commitments, so the presented identity is committed before comparing:
// under the supplied explicit key for a keyed import, under the
// process-local key for a legacy same-process import. Either way a
// changed, appeared or disappeared selector mismatches. (Duplicates never
// reach the comparison: the identity reader refuses them first.)
func (seal *interactiveExecSeal) envIdentityMatches(identity map[string]string) bool {
	if !seal.committed {
		return maps.Equal(identity, seal.env)
	}
	if len(identity) != len(seal.env) {
		return false
	}
	commit := agentic.CommitSealLiteral
	if seal.hasCommitKey {
		key := seal.commitKey
		commit = func(name, value string) string {
			return agentic.CommitSealLiteralWithKey(key, name, value)
		}
	}
	for name, value := range identity {
		if seal.env[name] != commit(name, value) {
			return false
		}
	}
	return true
}

// ExportSealedData implements agentic.ExecSealExporter. It reports the
// bound binary, exact argv, binary artifact and selectors from verifier
// memory and performs no I/O: a swapped binary still exports the ORIGINAL
// digest and refuses at import or verification time instead of silently
// resealing. System and integrity digest are stamped by agentic.ExportSeal.
// Environment selectors carry keyed commitments, never literal values;
// the key id lets an importer refuse a guard committed under a key it
// does not hold. An imported seal re-exports its stored commitments and
// stored key id verbatim, never re-committing under a different key.
func (seal *interactiveExecSeal) ExportSealedData() agentic.SealedData {
	selectors := make(map[string]string, len(seal.env)+5)
	selectors[sealedReleaseKey] = seal.release
	selectors[sealedModeKey] = string(seal.mode)
	if seal.mode == agentic.PermissionModeYolo {
		selectors[sealedHelpStdoutKey] = seal.helpFullDigest
		selectors[sealedHelpLinesKey] = seal.helpLinesDigest
	}
	if seal.committed {
		selectors[sealedKeyIDKey] = seal.commitKeyID
		for name, value := range seal.env {
			selectors[name] = value
		}
	} else {
		selectors[sealedKeyIDKey] = agentic.LocalCommitmentKeyID()
		for name, value := range seal.env {
			selectors[name] = agentic.CommitSealLiteral(name, value)
		}
	}
	return agentic.SealedData{
		Binary: seal.binary,
		Argv:   append([]string(nil), seal.argv...),
		Artifacts: []agentic.SealedArtifact{{
			Name:   sealedMuseBinaryName,
			Path:   seal.binary,
			Digest: sealedDigestPrefix + seal.digest,
		}},
		Selectors: selectors,
	}
}

// ExportSealedDataWithKey implements agentic.ExecSealKeyedExporter. It
// commits an in-process seal's values under the caller's explicit key so a
// finalized guard verifies in another process holding that same key. An
// already-imported seal holds commitments, not values, so it re-exports
// verbatim under its stored key id: the same key re-imports, a different
// key refuses. The key itself never serializes; only its ID travels.
func (seal *interactiveExecSeal) ExportSealedDataWithKey(key agentic.SealCommitmentKey) agentic.SealedData {
	if seal.committed {
		return seal.ExportSealedData()
	}
	selectors := make(map[string]string, len(seal.env)+5)
	selectors[sealedReleaseKey] = seal.release
	selectors[sealedModeKey] = string(seal.mode)
	if seal.mode == agentic.PermissionModeYolo {
		selectors[sealedHelpStdoutKey] = seal.helpFullDigest
		selectors[sealedHelpLinesKey] = seal.helpLinesDigest
	}
	selectors[sealedKeyIDKey] = agentic.CommitmentKeyID(key)
	for name, value := range seal.env {
		selectors[name] = agentic.CommitSealLiteralWithKey(key, name, value)
	}
	return agentic.SealedData{
		Binary: seal.binary,
		Argv:   append([]string(nil), seal.argv...),
		Artifacts: []agentic.SealedArtifact{{
			Name:   sealedMuseBinaryName,
			Path:   seal.binary,
			Digest: sealedDigestPrefix + seal.digest,
		}},
		Selectors: selectors,
	}
}

// VerifySealedArtifacts implements agentic.ExecArtifactVerifier. It
// re-hashes the sealed binary file against the ORIGINAL digest with the
// same checks the in-process verifier applies.
func (seal *interactiveExecSeal) VerifySealedArtifacts() error {
	var digest string
	if hashed, err := hashInteractiveSealBinary(seal.binary); err != nil {
		return fmt.Errorf("%w: sealed binary cannot be re-read: %v", ErrMuseSealBinaryChanged, err)
	} else {
		digest = hashed
	}
	if digest != seal.digest {
		return fmt.Errorf("%w: binary bytes no longer match the sealed digest", ErrMuseSealBinaryChanged)
	}
	return nil
}

// VerifyFinalizedPlan implements agentic.ExecFinalizedPlanVerifier: the
// checks a finalized plan still owes the plugin after the process binding
// takes over binary path, exact argv and full ordered environment. The
// binding proves the final process equals the bound one, but it cannot
// prove the bound values are admissible — an overlaid pin of "0" or a
// repointed XDG directory binds exactly — so this verifier composes the
// same pre-probe invariants the base verifier enforces (duplicates, then
// the updater pin from the single verifyMuseUpdaterPin list, then the
// full sealed XDG/pin identity from the same envIdentityMatches list)
// before the byte hash and the release and help re-probes. The probes
// never run unpinned or on a drifted process.
func (seal *interactiveExecSeal) VerifyFinalizedPlan(plan agentic.Plan) error {
	var identity map[string]string
	if computed, err := interactiveSealEnvIdentity(plan.Env); err != nil {
		return err // finalized verifier: duplicates before hash and probe
	} else {
		identity = computed
	}
	if err := verifyMuseUpdaterPin(plan.Env); err != nil {
		return err // finalized verifier: pin before hash and probe
	}
	if !seal.envIdentityMatches(identity) {
		return fmt.Errorf("%w: XDG_*/%s identity differs from the sealed identity", ErrMuseSealEnvChanged, museNoAutoUpdateEnv) // finalized verifier: sealed identity before hash and probe
	}
	if err := seal.VerifySealedArtifacts(); err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(context.Background(), sealProbeAggregateTimeout)
	defer cancel()
	var current string
	if probed, err := probeInteractiveSealBuild(probeCtx, plan.Binary, plan.Env); err != nil {
		return err
	} else {
		current = sealBuildIdentity(probed.Build)
	}
	if current != seal.release {
		return fmt.Errorf("%w: probed %q, sealed %q", ErrMuseSealReleaseChanged, current, seal.release)
	}
	if seal.mode == agentic.PermissionModeYolo {
		if err := seal.verifyHelpEvidence(probeCtx, plan.Binary, plan.Env); err != nil {
			return err
		}
	}
	return nil
}

// ValidateFinalizeOverlays implements
// agentic.ExecFinalizeOverlayValidator. The sealed XDG override identity
// is immutable after sealing, exactly like the updater pin the base
// already pins to 1: any overlay naming a sealed selector — the pin or
// any XDG_* name, either layer, any value — is a change, addition or
// duplicate intent and refuses before the binding can attest it. The
// predicate is the same isInteractiveSealSelector list the identity
// reader and the finalized verifier compare against, so the overlay gate
// and the verifier check cannot drift apart again. A cross-overlay
// duplicate refuses at the first layer's touch.
func (seal *interactiveExecSeal) ValidateFinalizeOverlays(overlays agentic.FinalizeOverlays) error {
	for _, entry := range overlays.FragmentEnv {
		if name, _, ok := strings.Cut(entry, "="); ok && isInteractiveSealSelector(name) {
			return fmt.Errorf("%w: finalize overlay must not touch %s", ErrMuseSealEnvChanged, name)
		}
	}
	for _, entry := range overlays.PromptEnv {
		if name, _, ok := strings.Cut(entry, "="); ok && isInteractiveSealSelector(name) {
			return fmt.Errorf("%w: finalize overlay must not touch %s", ErrMuseSealEnvChanged, name)
		}
	}
	return nil
}

// ImportSealedData implements agentic.ExecSealImporter. It rebuilds the
// verifier the in-process sealer would have built: the same binary, the
// same byte digest — verified against the file on disk right now, so an
// already-swapped binary refuses at import — the same exact argv, the same
// attested full build, the same sealed posture with its help evidence,
// and the same XDG_*/MUSE_NO_AUTO_UPDATE identity in commitment form.
// The build string must be well-formed and the yolo help selectors must
// be present digests; the envelope integrity digest already authenticates
// them against post-export tampering, and no probe runs at import.
func (*System) ImportSealedData(data agentic.SealedData) (agentic.ExecPlanVerifier, error) {
	return importMuseSealedData(data, agentic.LocalCommitmentKeyID(), agentic.SealCommitmentKey{}, false)
}

// ImportSealedDataWithKey implements agentic.ExecSealKeyedImporter. It
// rebuilds the same verifier, but the commitments verify under the
// caller-supplied key rather than the process-local one: the same explicit
// key that finalized the guard re-imports in another process, while a
// different or missing key refuses typed at the key-id comparison. The
// supplied key never serializes; the verifier holds it only to commit
// presented values at verification time.
func (*System) ImportSealedDataWithKey(data agentic.SealedData, key agentic.SealCommitmentKey) (agentic.ExecPlanVerifier, error) {
	return importMuseSealedData(data, agentic.CommitmentKeyID(key), key, true)
}

func importMuseSealedData(data agentic.SealedData, expectedKeyID string, key agentic.SealCommitmentKey, keyed bool) (agentic.ExecPlanVerifier, error) {
	if len(data.Artifacts) != 1 || data.Artifacts[0].Name != sealedMuseBinaryName {
		return nil, fmt.Errorf("%w: want exactly the %q artifact", ErrMuseSealMalformed, sealedMuseBinaryName)
	}
	artifact := data.Artifacts[0]
	if !filepath.IsAbs(data.Binary) || artifact.Path != data.Binary {
		return nil, fmt.Errorf("%w: sealed binary is not the absolute artifact path", ErrMuseSealMalformed)
	}
	digest, ok := parseInteractiveSealDigest(artifact.Digest)
	if !ok {
		return nil, fmt.Errorf("%w: sealed artifact digest is not sha256 hex", ErrMuseSealMalformed)
	}
	release, ok := data.Selectors[sealedReleaseKey]
	if !ok {
		return nil, fmt.Errorf("%w: sealed selectors carry no %q", ErrMuseSealMalformed, sealedReleaseKey)
	}
	if _, err := ParseBuildID(release); err != nil {
		return nil, fmt.Errorf("%w: sealed release is not a well-formed build: %w", ErrMuseSealMalformed, err)
	}
	modeValue, ok := data.Selectors[sealedModeKey]
	if !ok {
		return nil, fmt.Errorf("%w: sealed selectors carry no %q", ErrMuseSealMalformed, sealedModeKey)
	}
	var mode agentic.PermissionMode
	switch modeValue {
	case string(agentic.PermissionModeNative):
		mode = agentic.PermissionModeNative
	case string(agentic.PermissionModeYolo):
		mode = agentic.PermissionModeYolo
	default:
		return nil, fmt.Errorf("%w: sealed permission mode %q is neither native nor yolo", ErrMuseSealMalformed, modeValue)
	}
	var helpFullDigest, helpLinesDigest string
	if mode == agentic.PermissionModeYolo {
		helpFullDigest, ok = data.Selectors[sealedHelpStdoutKey]
		if !ok || !isLowerHex64(helpFullDigest) {
			return nil, fmt.Errorf("%w: sealed yolo selectors carry no %q digest", ErrMuseSealMalformed, sealedHelpStdoutKey)
		}
		helpLinesDigest, ok = data.Selectors[sealedHelpLinesKey]
		if !ok || !isLowerHex64(helpLinesDigest) {
			return nil, fmt.Errorf("%w: sealed yolo selectors carry no %q digest", ErrMuseSealMalformed, sealedHelpLinesKey)
		}
	} else {
		if _, present := data.Selectors[sealedHelpStdoutKey]; present {
			return nil, fmt.Errorf("%w: sealed native selectors carry unexpected %q", ErrMuseSealMalformed, sealedHelpStdoutKey)
		}
		if _, present := data.Selectors[sealedHelpLinesKey]; present {
			return nil, fmt.Errorf("%w: sealed native selectors carry unexpected %q", ErrMuseSealMalformed, sealedHelpLinesKey)
		}
	}
	keyID, ok := data.Selectors[sealedKeyIDKey]
	if !ok {
		return nil, fmt.Errorf("%w: sealed selectors carry no %q", ErrMuseSealMalformed, sealedKeyIDKey)
	}
	if keyID != expectedKeyID {
		return nil, fmt.Errorf("%w: sealed commitment key is unavailable", agentic.ErrSealImportRefused)
	}
	env := make(map[string]string, len(data.Selectors))
	for name, value := range data.Selectors {
		if name == sealedReleaseKey || name == sealedKeyIDKey || name == sealedModeKey || name == sealedHelpStdoutKey || name == sealedHelpLinesKey {
			continue
		}
		if !isInteractiveSealSelector(name) {
			return nil, fmt.Errorf("%w: sealed selector %q is not an XDG_* or %s entry", ErrMuseSealMalformed, name, museNoAutoUpdateEnv)
		}
		if !isSealedCommitment(value) {
			return nil, fmt.Errorf("%w: sealed selector %q is not a keyed commitment", ErrMuseSealMalformed, name)
		}
		env[name] = value
	}
	seal := &interactiveExecSeal{
		binary:          data.Binary,
		digest:          digest,
		argv:            append([]string(nil), data.Argv...),
		release:         release,
		mode:            mode,
		helpFullDigest:  helpFullDigest,
		helpLinesDigest: helpLinesDigest,
		env:             env,
		committed:       true,
		commitKeyID:     keyID,
		commitKey:       key,
		hasCommitKey:    keyed,
	}
	if err := seal.VerifySealedArtifacts(); err != nil {
		return nil, err
	}
	return seal, nil
}

// sealBuildIdentity is the build identity the seal binds: the FULL
// attested build (1.4.1-R4503.1), never the short triple. Same-triple
// revisions are different builds; binding the triple would admit
// revision drift at seal, verify and export alike.
func sealBuildIdentity(probed string) string { return probed }

// isSealedCommitment reports whether value has the keyed-commitment shape
// the exporter writes: the hmac-sha256 label plus 64 lowercase hex.
func isSealedCommitment(value string) bool {
	hexDigest := strings.TrimPrefix(value, sealedCommitmentPrefix)
	if hexDigest == value || len(hexDigest) != 64 {
		return false
	}
	for _, r := range hexDigest {
		digit := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !digit {
			return false
		}
	}
	return true
}

// parseInteractiveSealDigest splits a sha256:<64 hex> artifact digest back
// into the bare hex the binary verifier compares. Anything else is
// malformed data, never a different hash.
func parseInteractiveSealDigest(digest string) (string, bool) {
	hexDigest := strings.TrimPrefix(digest, sealedDigestPrefix)
	if hexDigest == digest || len(hexDigest) != 64 {
		return "", false
	}
	for _, r := range hexDigest {
		digit := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !digit {
			return "", false
		}
	}
	return hexDigest, true
}

// isInteractiveSealSelector reports whether name selects state this sealer
// binds: the updater pin or any XDG_ directory. A late duplicate of one of
// these reaches the child last-wins, so the seal refuses a final env
// carrying more than one entry for them, whoever appended it, even when
// the duplicate repeats the sealed value.
func isInteractiveSealSelector(name string) bool {
	if name == museNoAutoUpdateEnv {
		return true
	}
	return strings.HasPrefix(name, "XDG_")
}

// interactiveSealEnvIdentity reads the single-entry XDG_*/MUSE_NO_AUTO_UPDATE
// identity: the effective last-wins value of every present selector, failing
// closed on duplicates. With no duplicates the forward read equals the
// last-wins child view; a bare entry without "=" selects nothing, matching
// envValue and the network duplicate scanner.
func interactiveSealEnvIdentity(env []string) (map[string]string, error) {
	identity := make(map[string]string)
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || !isInteractiveSealSelector(name) {
			continue
		}
		if _, duplicate := identity[name]; duplicate {
			return nil, fmt.Errorf("%w: duplicate %s entry", ErrMuseSealEnvChanged, name)
		}
		identity[name] = value
	}
	return identity, nil
}

// probeInteractiveSealBuild establishes the full build identity of the
// sealed binary through the same version probe probe.go owns: exactly
// `--version`, the bounded runner, and the shared build-identity grammar
// parsed for release and build. Every failure is undetected, never a
// synthesized build; attempt failures keep their typed probe error for
// host routing.
func probeInteractiveSealBuild(ctx context.Context, binary string, env []string) (BuildIdentity, error) {
	out, err := toolprobe.VersionOutput(ctx, binary, env)
	if err != nil {
		return BuildIdentity{}, errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("muse: probing the sealed binary: %w", err))
	}
	var identity BuildIdentity
	if parsed, err := ParseBuildIdentity(out); err != nil {
		return BuildIdentity{}, errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("muse: parsing the sealed answer: %w", err))
	} else {
		identity = parsed
	}
	return identity, nil
}

// hashInteractiveSealBinary streams the SHA-256 of the sealed binary file
// without holding its bytes: a pinned Muse binary is hundreds of
// megabytes. The file must be regular: resolution verified that once, but
// a post-resolution swap to a FIFO would otherwise block the verifier's
// read instead of refusing.
func hashInteractiveSealBinary(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("sealed binary is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
