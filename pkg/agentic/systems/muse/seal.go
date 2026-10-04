// Interactive exec-plan sealer for the Muse plugin.
//
// Every Muse LaunchModeInteractive plan carries this seal. It binds the
// absolute binary path plus the SHA-256 of the probed binary, the exact
// final argv, the probed full build identity (1.4.1-R4503.1 or
// 1.4.2-R4684.1 from the module-verified list), and the XDG_* override
// identity plus MUSE_NO_AUTO_UPDATE=1. VerifyBeforeExec refuses typed on
// a binary swap, an argv change, a release mismatch, or a changed or
// duplicated XDG_*/MUSE_NO_AUTO_UPDATE entry. A binary that attests no
// verified build — another release, an unpinned revision of a verified
// triple, silence, or a misattributed answer — refuses typed at plan
// time: no interactive plan ships unsealed.
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
// path probe.go owns, parsed for the full build identity) and hashes its
// bytes. Verification re-probes and re-hashes: the release re-probe is
// what catches a same-bytes dispatch shim resolving a different effective
// release behind the sealed path, which the byte hash alone cannot see.
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
	// ErrMuseSealReleaseUnverified refuses an interactive plan whose
	// binary attests no module-verified build: another release, an
	// unpinned revision of a verified triple, or any answer outside the
	// closed verified-build list. Undetected answers (silence,
	// misattribution, probe failure) refuse as undetected instead.
	ErrMuseSealReleaseUnverified = errors.New("muse: sealed tool build is not module-verified")
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
// file the verifier re-hashes; the release selector carries the verified
// full build. "tool-release" and "commitment-key-id" cannot collide with
// a sealed environment name: the seal only ever reads XDG_* names and the
// updater pin.
const (
	sealedMuseBinaryName   = "muse-binary"
	sealedReleaseKey       = "tool-release"
	sealedKeyIDKey         = "commitment-key-id"
	sealedDigestPrefix     = "sha256:"
	sealedCommitmentPrefix = "hmac-sha256:"
)

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
// path, its byte digest, the exact argv, the verified full build, and the
// single-entry XDG_*/MUSE_NO_AUTO_UPDATE identity — values in memory,
// keyed commitments once imported.
type interactiveExecSeal struct {
	binary  string
	digest  string
	argv    []string
	release string
	env     map[string]string
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
	// A seal binds a verified attestation or nothing at all: an
	// undetected or unverified build refuses typed here, at plan time.
	// No interactive plan ships unsealed.
	var probed string
	if build, err := probeInteractiveSealBuild(plan.Binary, plan.Env); err != nil {
		return nil, err
	} else {
		probed = build
	}
	if err := verifySealedBuild(probed); err != nil {
		return nil, err
	}
	var digest string
	if hashed, err := hashInteractiveSealBinary(plan.Binary); err != nil {
		return nil, fmt.Errorf("%w: sealed binary cannot be read: %v", ErrMuseSealMalformed, err)
	} else {
		digest = hashed
	}
	seal := &interactiveExecSeal{
		binary:  plan.Binary,
		digest:  digest,
		argv:    append([]string(nil), plan.Argv...),
		release: sealBuildIdentity(probed),
		env:     maps.Clone(identity),
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
// dispatch shims.
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
	var release string
	if probed, err := probeInteractiveSealBuild(plan.Binary, plan.Env); err != nil {
		return err
	} else {
		release = sealBuildIdentity(probed)
	}
	if release != seal.release {
		return fmt.Errorf("%w: probed %q, sealed %q", ErrMuseSealReleaseChanged, release, seal.release)
	}
	if !slices.Equal(plan.Argv, seal.argv) {
		return fmt.Errorf("%w: argv has %d elements, sealed %d", ErrMuseSealArgvChanged, len(plan.Argv), len(seal.argv))
	}
	if !seal.envIdentityMatches(identity) {
		return fmt.Errorf("%w: XDG_*/%s identity differs from the sealed identity", ErrMuseSealEnvChanged, museNoAutoUpdateEnv)
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
	selectors := make(map[string]string, len(seal.env)+2)
	selectors[sealedReleaseKey] = seal.release
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
	selectors := make(map[string]string, len(seal.env)+2)
	selectors[sealedReleaseKey] = seal.release
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
// before the byte hash and the release re-probe. The probe never runs
// unpinned or on a drifted process.
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
	var current string
	if probed, err := probeInteractiveSealBuild(plan.Binary, plan.Env); err != nil {
		return err
	} else {
		current = sealBuildIdentity(probed)
	}
	if current != seal.release {
		return fmt.Errorf("%w: probed %q, sealed %q", ErrMuseSealReleaseChanged, current, seal.release)
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
// verified full build, and the same XDG_*/MUSE_NO_AUTO_UPDATE identity in
// commitment form. The build string is required to be module-verified;
// the envelope integrity digest already authenticates it against
// post-export tampering, and no probe runs at import.
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
	if err := verifySealedBuild(release); err != nil {
		return nil, fmt.Errorf("muse: refusing to import: %w", err)
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
		if name == sealedReleaseKey || name == sealedKeyIDKey {
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
		binary:       data.Binary,
		digest:       digest,
		argv:         append([]string(nil), data.Argv...),
		release:      release,
		env:          env,
		committed:    true,
		commitKeyID:  keyID,
		commitKey:    key,
		hasCommitKey: keyed,
	}
	if err := seal.VerifySealedArtifacts(); err != nil {
		return nil, err
	}
	return seal, nil
}

// verifySealedBuild admits exactly the module-verified builds: the full
// identities from policy.go, never a bare triple and never an unpinned
// revision of a verified triple.
func verifySealedBuild(build string) error {
	for _, verified := range verifiedBuilds {
		if build == verified {
			return nil
		}
	}
	return fmt.Errorf("%w: probed build %q is not module-verified (verified: %s)", ErrMuseSealReleaseUnverified, build, strings.Join(verifiedBuilds, ", "))
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
// `--version`, the shared bounded wait, and the pinned release grammar
// parsed for the build. The seal API carries no context, so the probe
// runs on a background context under toolprobe's own timeout. Every
// failure is undetected, never a synthesized build.
func probeInteractiveSealBuild(binary string, env []string) (string, error) {
	out, err := toolprobe.VersionOutput(context.Background(), binary, env)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("muse: probing the sealed binary: %v", err))
	}
	build, err := parseToolBuild(out)
	if err != nil {
		return "", errors.Join(agentic.ErrToolReleaseUndetected, fmt.Errorf("muse: parsing the sealed answer: %v", err))
	}
	return build, nil
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
