package agentic

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Exec-guard seal envelope for hosted launch plans.
//
// The hosted launch contract (option D, r2 §3 and §5) carries one guard per
// process: {schema, schema_version, data} under the exec-guard schema below.
// Phase 1 admits Claude only, and Claude is unsealed, so the only closed
// guard data is {"kind":"unsealed"}. Everything else on this page exists so
// that unsealed stays exactly that small while sealed plans still export
// their seal for local verification, marked not hosted-admissible until
// closed sealed schemas land.
//
// The module still owns no exec: a seal binds the binary, argv, env-sensitive
// selectors and artifact digests a consumer must re-check, and the consumer
// starts its own process. There is no os/exec anywhere under pkg/agentic;
// seal_boundary_test.go holds that line.
const (
	// ExecGuardVersion is the only admitted guard schema version. Changed
	// closed shapes take a new version; 1.0.0 stays frozen.
	ExecGuardVersion = "1.0.0"

	// SealKindUnsealed is the Phase 1 closed guard data kind.
	SealKindUnsealed = "unsealed"
	// SealKindUnsealedBound carries a local-only finalized unsealed binding.
	// It cannot extend or be admitted as the frozen hosted unsealed marker.
	SealKindUnsealedBound = "unsealed-bound"
	// SealKindSealed is the module-internal sealed guard data kind. It is
	// not a closed hosted schema: sealed exports verify locally and are
	// marked not hosted-admissible.
	SealKindSealed = "sealed"
)

var (
	// ErrSealUnknownSchema refuses a guard naming a schema this module does
	// not implement. Dispatch precedes structural validation.
	ErrSealUnknownSchema = errors.New("agentic: unknown exec-guard schema")
	// ErrSealUnknownVersion refuses a guard naming an unsupported schema
	// version of a known schema.
	ErrSealUnknownVersion = errors.New("agentic: unsupported exec-guard schema version")
	// ErrSealMalformed refuses structurally incomplete guard data: a
	// sealed kind without its payload, an unsealed kind carrying one, an
	// empty sealed binary, or an unknown kind.
	ErrSealMalformed = errors.New("agentic: exec-guard data is malformed")
	// ErrSealTampered refuses sealed data whose integrity digest does not
	// match its contents. The digest is corruption evidence over a private
	// channel, never authentication: anyone holding the bytes can recompute
	// it, and the hosted wire relies on the payload content digest instead.
	ErrSealTampered = errors.New("agentic: exec-guard data failed its integrity check")
	// ErrSealNotExportable refuses an export for a plan with no exportable
	// seal basis: no verifier at all, or a verifier type that exports no
	// guard. A sealed-system plan never downgrades to unsealed here.
	ErrSealNotExportable = errors.New("agentic: plan carries no exportable exec-guard basis")
	// ErrSealUnsealedRefused refuses an unsealed guard for a plugin that
	// must not verify under it: one declaring a sealer (no downgrade), or
	// one declaring no sealer without opting into unsealed (Muse).
	ErrSealUnsealedRefused = errors.New("agentic: plugin refuses the unsealed exec guard")
	// ErrSealImportRefused refuses sealed guard data the plugin cannot
	// import: data naming another system, or a plugin with no sealed
	// importer.
	ErrSealImportRefused = errors.New("agentic: plugin imports no sealed exec guard")
)

// Seal is the versioned exec-guard envelope: the wire {schema,
// schema_version, data} triple plus the module-side hosted-admission verdict,
// which never serializes.
type Seal struct {
	Schema        string   `json:"schema"`
	SchemaVersion string   `json:"schema_version"`
	Data          SealData `json:"data"`
}

// HostedAdmissible reports whether a host may accept this guard in the
// current phase. Only the closed unsealed kind is admissible; sealed exports
// verify locally and stay refused by hosts until closed sealed schemas land.
func (s Seal) HostedAdmissible() bool {
	return s.Schema == ExecGuardSchema && s.SchemaVersion == ExecGuardVersion && s.Data.Kind == SealKindUnsealed && s.Data.Binding == nil && s.Data.Sealed == nil
}

// SealData is the guard payload. Unsealed marshals to exactly
// {"kind":"unsealed"} for a base plan, the closed Phase 1 shape. Finalized
// unsealed bindings and sealed payloads are local-only projections.
type SealData struct {
	Kind    string          `json:"kind"`
	Binding *ProcessBinding `json:"binding,omitempty"`
	Sealed  *SealedData     `json:"sealed,omitempty"`
}

// SealedData binds the exact sealed process and its launch-owned artifacts.
// System and Digest are stamped by ExportSeal, never by the plugin: the
// plugin reports what it sealed, and the envelope reports who sealed it and
// seals the bytes themselves.
type SealedData struct {
	System    string            `json:"system"`
	Binary    string            `json:"binary"`
	Argv      []string          `json:"argv"`
	Artifacts []SealedArtifact  `json:"artifacts"`
	Selectors map[string]string `json:"selectors,omitempty"`
	Digest    string            `json:"digest"`
	Binding   *ProcessBinding   `json:"binding,omitempty"`
}

// SealedArtifact is one launch-owned file the verifier re-reads: the
// absolute path it must still occupy and the digest its bytes must still
// carry. Name is the plugin's stable label for the artifact kind.
type SealedArtifact struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// ExecSealExporter is implemented by sealed verifiers to report the process
// and artifacts they bound. It reads verifier memory only and performs no
// I/O: exporting never re-reads the artifacts it reports.
type ExecSealExporter interface {
	ExecPlanVerifier
	ExportSealedData() SealedData
}

// ExecSealImporter is implemented by system plugins that can rebuild a
// sealed verifier from exported guard data.
type ExecSealImporter interface {
	ImportSealedData(SealedData) (ExecPlanVerifier, error)
}

// ExecArtifactVerifier is implemented by sealed verifiers to re-verify
// their artifacts without rebinding argv. Finalization carries the original
// digests forward through this interface instead of minting new ones.
type ExecArtifactVerifier interface {
	ExecPlanVerifier
	VerifySealedArtifacts() error
}

// ExecFinalizedPlanVerifier is implemented by sealed verifiers whose
// final-process checks go beyond re-hashing artifacts: the finalized
// wrapper calls VerifyFinalizedPlan with the final plan after the process
// binding verifies, instead of VerifySealedArtifacts. A verifier that
// needs no plan-aware check leaves this unimplemented and the wrapper
// falls back to VerifySealedArtifacts.
type ExecFinalizedPlanVerifier interface {
	ExecArtifactVerifier
	VerifyFinalizedPlan(Plan) error
}

// ExecSealKeyedExporter is implemented by sealed verifiers whose exported
// selectors commit under an explicit commitment key. FinalizePlan calls it
// with the finalization key so plugin selectors travel under the same key
// as the process binding: a consumer holding that key verifies in its own
// process. A verifier that leaves this unimplemented keeps the
// process-local export and stays same-process-only.
type ExecSealKeyedExporter interface {
	ExecSealExporter
	ExportSealedDataWithKey(SealCommitmentKey) SealedData
}

// ExecSealKeyedImporter is implemented by system plugins that rebuild a
// sealed verifier under an explicit commitment key. ImportSeal calls it
// with the caller-supplied key (or the process-local key when the caller
// supplies none) so cross-process guards verify if and only if the caller
// holds the committing key. A different or missing key refuses typed.
type ExecSealKeyedImporter interface {
	ExecSealImporter
	ImportSealedDataWithKey(SealedData, SealCommitmentKey) (ExecPlanVerifier, error)
}

// UnsealedExecGuard is implemented by system plugins whose unsealed plans
// may be exported and verified under the unsealed guard. Phase 1 admits
// Claude only: a plugin declaring no sealer and no opt-in (notably Muse)
// refuses the unsealed guard at both export and import.
type UnsealedExecGuard interface {
	AcceptUnsealedExecGuard()
}

// unsealedVerifier is the explicit verifier for unsealed plans. Unsealed is
// a positive fact about the plan, not the absence of a seal: a nil verifier
// means no plugin bound anything, while this one means the plugin bound
// nothing on purpose and said so.
type unsealedVerifier struct{}

func (unsealedVerifier) VerifyBeforeExec(Plan) error { return nil }

// ExportSeal returns the versioned exec-guard envelope for this plan. A
// sealed verifier exports its bound process and artifacts; an explicit
// unsealed verifier exports the closed unsealed kind. Anything else refuses:
// a nil verifier (Muse, an unsealed sealed-system plan, a zero Plan) and an
// unknown verifier type carry no guard this envelope may claim.
func (p Plan) ExportSeal() (Seal, error) {
	var seal Seal
	switch verifier := p.execVerifier.(type) {
	case nil:
		return Seal{}, fmt.Errorf("%w: system %q bound no guard", ErrSealNotExportable, string(p.System))
	case unsealedVerifier:
		seal = Seal{Schema: ExecGuardSchema, SchemaVersion: ExecGuardVersion, Data: SealData{Kind: SealKindUnsealed}}
	case finalizedUnsealedVerifier:
		seal = Seal{Schema: ExecGuardSchema, SchemaVersion: ExecGuardVersion, Data: SealData{Kind: SealKindUnsealedBound, Binding: verifier.bindings.export()}}
	case ExecSealExporter:
		data := cloneSealedData(verifier.ExportSealedData())
		data.System = string(p.System)
		// Check before the digest's marshal as well as before returning
		// the envelope. The process binding exporter only hashes validated
		// producer inputs; a plugin exporter may carry arbitrary strings.
		if !sealedDataRepresentable(data) {
			return Seal{}, fmt.Errorf("%w: sealed payload carries a string that cannot survive guard JSON", ErrSealMalformed)
		}
		data.Digest = sealIntegrityDigest(data)
		seal = Seal{Schema: ExecGuardSchema, SchemaVersion: ExecGuardVersion, Data: SealData{Kind: SealKindSealed, Sealed: &data}}
	default:
		return Seal{}, fmt.Errorf("%w: verifier %T exports no guard", ErrSealNotExportable, verifier)
	}
	if !sealRepresentable(seal) {
		return Seal{}, fmt.Errorf("%w: guard carries a string that cannot survive guard JSON", ErrSealMalformed)
	}
	return seal, nil
}

// ImportSeal rebuilds a verifier from an exported guard for the given
// plugin. Unknown schema or version refuse before structural validation; a
// truncated payload refuses as malformed, and sealed data whose integrity
// digest mismatches refuses as tampered. An unsealed guard yields a verifier
// only for a plugin that declares no sealer and opts into unsealed; sealed
// data imports only through the named system's own importer.
func ImportSeal(sys System, seal Seal, keys ...SealCommitmentKey) (ExecPlanVerifier, error) {
	// Validate original caller bytes before dispatch or marshal. A repaired
	// projection must never validate a different binding than we retain.
	if !sealRepresentable(seal) {
		return nil, fmt.Errorf("%w: guard carries a string that cannot survive guard JSON", ErrSealMalformed)
	}
	if seal.Schema != ExecGuardSchema {
		return nil, fmt.Errorf("%w: %q", ErrSealUnknownSchema, seal.Schema)
	}
	if seal.SchemaVersion != ExecGuardVersion {
		return nil, fmt.Errorf("%w: %q", ErrSealUnknownVersion, seal.SchemaVersion)
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		return nil, fmt.Errorf("%w: guard encoding", ErrSealMalformed)
	}
	if err := validateSealWire(wire); err != nil {
		return nil, err
	}
	switch seal.Data.Kind {
	case SealKindUnsealed, SealKindUnsealedBound:
		if seal.Data.Sealed != nil {
			return nil, fmt.Errorf("%w: unsealed kind carries a sealed payload", ErrSealMalformed)
		}
		var verifier ExecPlanVerifier
		if imported, err := importUnsealedGuard(sys); err != nil {
			return nil, err
		} else {
			verifier = imported
		}
		if seal.Data.Binding == nil {
			return verifier, nil
		}
		var binding finalizedBindings
		if imported, err := importProcessBinding(seal.Data.Binding, keys); err != nil {
			return nil, err
		} else {
			binding = imported
		}
		return finalizedUnsealedVerifier{bindings: binding}, nil
	case SealKindSealed:
		if seal.Data.Binding != nil {
			return nil, fmt.Errorf("%w: sealed kind carries a separate binding", ErrSealMalformed)
		}
		return importSealedGuard(sys, seal.Data.Sealed, keys)
	default:
		return nil, fmt.Errorf("%w: unknown kind %q", ErrSealMalformed, seal.Data.Kind)
	}
}

func importUnsealedGuard(sys System) (ExecPlanVerifier, error) {
	if _, sealed := sys.(ExecPlanSealer); sealed {
		return nil, fmt.Errorf("%w: system %q declares a sealer", ErrSealUnsealedRefused, string(sys.ID()))
	}
	if _, ok := sys.(UnsealedExecGuard); !ok {
		return nil, fmt.Errorf("%w: system %q opts out of unsealed", ErrSealUnsealedRefused, string(sys.ID()))
	}
	return unsealedVerifier{}, nil
}

func importSealedGuard(sys System, data *SealedData, keys []SealCommitmentKey) (ExecPlanVerifier, error) {
	if data == nil {
		return nil, fmt.Errorf("%w: sealed kind carries no payload", ErrSealMalformed)
	}
	if strings.TrimSpace(data.Binary) == "" {
		return nil, fmt.Errorf("%w: sealed payload carries no binary", ErrSealMalformed)
	}
	if sealIntegrityDigest(*data) != data.Digest {
		return nil, fmt.Errorf("%w: sealed payload digest mismatch", ErrSealTampered)
	}
	if data.System != string(sys.ID()) {
		return nil, fmt.Errorf("%w: sealed payload names system %q", ErrSealImportRefused, data.System)
	}
	importer, ok := sys.(ExecSealImporter)
	if !ok {
		return nil, fmt.Errorf("%w: system %q implements no sealed importer", ErrSealImportRefused, string(sys.ID()))
	}
	pluginData := cloneSealedData(*data)
	if pluginData.Binding != nil {
		// The finalized projection merges the final-process commitments
		// into the top-level selectors beside the plugin's own; the
		// plugin importer sees only its own keys. The envelope digest
		// already authenticates both maps, so the strip set is trusted,
		// and a colliding plugin key strips to a missing selector the
		// importer refuses malformed.
		for key := range pluginData.Binding.Selectors {
			delete(pluginData.Selectors, key)
		}
	}
	var verifier ExecPlanVerifier
	if keyed, ok := sys.(ExecSealKeyedImporter); ok {
		// A keyed plugin verifies its selectors under the caller-supplied
		// key (or the process-local key when the caller supplies none),
		// so a finalized guard committed under an explicit key verifies
		// in another process holding that same key, while a different or
		// missing key refuses typed at the key-id comparison.
		var selected SealCommitmentKey
		if key, err := selectCommitmentKey(keys); err != nil {
			return nil, err
		} else {
			selected = key
		}
		if imported, err := keyed.ImportSealedDataWithKey(pluginData, selected); err != nil {
			return nil, err
		} else {
			verifier = imported
		}
	} else if imported, err := importer.ImportSealedData(pluginData); err != nil {
		return nil, err
	} else {
		verifier = imported
	}
	if data.Binding == nil && len(data.Selectors) == 0 {
		return verifier, nil
	}
	artifacts, ok := verifier.(ExecArtifactVerifier)
	if !ok {
		return nil, fmt.Errorf("%w: imported verifier binds no artifacts for %d selectors", ErrSealImportRefused, len(data.Selectors))
	}
	if data.Binding == nil {
		// Plugin-opaque selectors with no finalized binding: the importer
		// consumed them and no finalized process exists to wrap, so the
		// importer's own artifact-backed verifier stands alone. The
		// artifact-basis gate above still refuses selectors that no
		// verifier re-verifies.
		return verifier, nil
	}
	var binding finalizedBindings
	if imported, err := importProcessBinding(data.Binding, keys); err != nil {
		return nil, err
	} else {
		binding = imported
	}
	return finalizedSealedVerifier{sealed: cloneSealedData(*data), bindings: binding, artifacts: artifacts}, nil
}

// sealIntegrityCanonical is the fixed field order the seal integrity digest
// covers. JSON fixes the order by struct layout and sorts map keys, so the
// same payload always digests identically.
type sealIntegrityCanonical struct {
	Binding   *ProcessBinding   `json:"binding,omitempty"`
	System    string            `json:"system"`
	Binary    string            `json:"binary"`
	Argv      []string          `json:"argv"`
	Artifacts []SealedArtifact  `json:"artifacts"`
	Selectors map[string]string `json:"selectors,omitempty"`
}

func sealIntegrityDigest(data SealedData) string {
	canonical, err := json.Marshal(sealIntegrityCanonical{
		Binding:   data.Binding,
		System:    data.System,
		Binary:    data.Binary,
		Argv:      data.Argv,
		Artifacts: data.Artifacts,
		Selectors: data.Selectors,
	})
	if err != nil {
		return "sha256:invalid"
	}
	sum := sha256.Sum256(canonical)
	return fmt.Sprintf("sha256:%x", sum)
}

func cloneSealSelectors(selectors map[string]string) map[string]string {
	// A required empty selectors map stays a non-nil empty map so it
	// serializes as {} rather than null: the closed wire rightly refuses
	// null for required members, and an untouched empty environment must
	// still round-trip. Only a genuinely absent map stays nil.
	if selectors == nil {
		return nil
	}
	cloned := make(map[string]string, len(selectors))
	for name, value := range selectors {
		cloned[name] = value
	}
	return cloned
}
