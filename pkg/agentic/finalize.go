package agentic

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Post-seal finalization for hosted launch plans.
//
// BuildPlan seals the module-owned base process. The launcher then applies
// its permitted overlays in native overlay order — fragment env, prompt env,
// native tail — and FinalizePlan validates those overlays and yields a
// verifier for the EXACT final process. The finalized verifier binds the
// final binary, exact final argv and full ordered environment via keyed
// commitments. It carries the base seal's original artifact digests forward
// verbatim: finalization never
// re-reads the artifacts to mint new digests, so bytes that changed on disk
// after sealing still refuse at verification time. Any other mutation — an
// unknown binding kind, a malformed overlay entry, a second finalization —
// refuses typed.
var (
	// ErrFinalizeNoSealBasis refuses a base plan finalization cannot build
	// on: no verifier at all, an already-finalized plan, or a verifier
	// type that exports neither sealed data nor artifact re-verification.
	ErrFinalizeNoSealBasis = errors.New("agentic: plan has no seal basis to finalize")
	// ErrFinalizeOverlayMalformed refuses an overlay entry finalization
	// cannot apply: a fragment or prompt entry without NAME=value shape, a
	// name that is not an environment name, a NUL-carrying value, a name
	// set twice in one overlay, or a native tail token carrying NUL.
	ErrFinalizeOverlayMalformed = errors.New("agentic: finalize overlay entry is malformed")
	// ErrFinalizeBindingUnknown refuses a typed binding: no binding kinds
	// are admitted yet, so any binding is unknown.
	ErrFinalizeBindingUnknown = errors.New("agentic: finalize binding kind is unknown")
	// ErrFinalizedProcessChanged refuses a process that differs from the
	// finalized one: another binary, other argv, or changed env-sensitive
	// selectors. Artifact drift keeps the plugin's own refusal.
	ErrFinalizedProcessChanged = errors.New("agentic: process differs from the finalized process")
)

// FinalizeOverlays are the permitted post-seal overlays in native overlay
// order: the module-owned base snapshot first, then fragment env, then
// prompt env, then the native tail. A later overlay overrides an earlier one
// by environment name; the native tail appends to argv in order.
type FinalizeOverlays struct {
	// FragmentEnv holds NAME=value entries from the launch fragment.
	FragmentEnv []string
	// PromptEnv holds NAME=value entries from the prompt environment.
	PromptEnv []string
	// NativeTail holds argv tokens appended after the sealed argv, in order.
	NativeTail []string
}

// TypedBinding is a future typed launch binding. The kinds land later; until
// they do, FinalizePlan refuses every binding as unknown rather than
// carrying a value it cannot validate.
type TypedBinding struct {
	Kind  string
	Name  string
	Value string
}

// ExecFinalizeOverlayValidator is implemented by sealed verifiers whose
// invariants forbid some otherwise well-formed overlays: a Muse seal
// refuses any overlay touching the updater pin, for example, because the
// base already pins it and any mention is a change or duplicate intent.
// FinalizePlan calls it before applying overlays; a verifier that leaves
// this unimplemented admits every well-formed overlay.
type ExecFinalizeOverlayValidator interface {
	ValidateFinalizeOverlays(FinalizeOverlays) error
}

// FinalizePlan validates the permitted overlays and typed bindings against
// the sealed base plan, applies them in native overlay order, and returns
// the final plan with a verifier bound to the EXACT final process. The
// base's original artifact digests travel into the finalized verifier
// untouched; current disk contents are never resealed. Validation runs in
// overlay order — fragment, then prompt, then native tail, then bindings —
// so the first invalid overlay names the refusal.
func FinalizePlan(base Plan, overlays FinalizeOverlays, bindings []TypedBinding, keys ...SealCommitmentKey) (Plan, error) {
	// Unrepresentable input refuses before any other processing: a base
	// that cannot survive guard JSON is malformed, whether or not it also
	// changed after sealing. Valid pre-mutations still reach the snapshot
	// check below.
	if !guardStringsRepresentable(base.Binary) {
		return Plan{}, fmt.Errorf("%w: base binary carries a string that cannot survive guard JSON", ErrFinalizeOverlayMalformed)
	}
	for _, arg := range base.Argv {
		if !guardStringsRepresentable(arg) {
			return Plan{}, fmt.Errorf("%w: base argv carries a string that cannot survive guard JSON", ErrFinalizeOverlayMalformed)
		}
	}
	if base.baseProcess != nil && !isFinalizedVerifier(base.execVerifier) {
		if err := base.baseProcess.verify(base); err != nil {
			return Plan{}, err
		}
	}
	if _, err := validateFinalizeEnvOverlay(base.Env, "base"); err != nil {
		return Plan{}, err
	}
	var key SealCommitmentKey
	if selected, err := selectCommitmentKey(keys); err != nil {
		return Plan{}, err
	} else {
		key = selected
	}
	switch held := base.execVerifier.(type) {
	case unsealedVerifier:
		return finalizeUnsealedPlan(base, overlays, bindings, key)
	case nil, finalizedSealedVerifier, finalizedUnsealedVerifier:
		return Plan{}, fmt.Errorf("%w: system %q", ErrFinalizeNoSealBasis, string(base.System))
	default:
		exporter, exports := held.(ExecSealExporter)
		artifacts, verifies := held.(ExecArtifactVerifier)
		if !exports || !verifies {
			return Plan{}, fmt.Errorf("%w: system %q", ErrFinalizeNoSealBasis, string(base.System))
		}
		return finalizeSealedPlan(base, artifacts, exporter, overlays, bindings, key)
	}
}

func finalizeUnsealedPlan(base Plan, overlays FinalizeOverlays, bindings []TypedBinding, key SealCommitmentKey) (Plan, error) {
	var env []string
	var selectors map[string]string
	if applied, appliedSelectors, err := applyFinalizeOverlays(base.Env, overlays); err != nil {
		return Plan{}, err
	} else {
		env, selectors = applied, appliedSelectors
	}
	if err := refuseFinalizeBindings(bindings); err != nil {
		return Plan{}, err
	}
	final := base
	final.Env = env
	final.Argv = append(append([]string(nil), base.Argv...), overlays.NativeTail...)
	if err := rederiveFinalSession(&final, overlays.NativeTail); err != nil {
		return Plan{}, err
	}
	_ = selectors
	final.execVerifier = finalizedUnsealedVerifier{bindings: bindFinalProcess(final, key)}
	return final, nil
}

func finalizeSealedPlan(base Plan, artifacts ExecArtifactVerifier, exporter ExecSealExporter, overlays FinalizeOverlays, bindings []TypedBinding, key SealCommitmentKey) (Plan, error) {
	// A plugin with overlay invariants (a Muse updater pin) refuses
	// forbidden overlays before anything is applied or bound: the binding
	// would otherwise attest the forbidden value as the final process.
	if validator, ok := artifacts.(ExecFinalizeOverlayValidator); ok {
		if err := validator.ValidateFinalizeOverlays(overlays); err != nil {
			return Plan{}, err
		}
	}
	// A keyed plugin commits its selectors under the finalization key so
	// the finalized guard verifies cross-process with that same key; any
	// other plugin keeps its process-local export.
	var data SealedData
	if keyed, ok := exporter.(ExecSealKeyedExporter); ok {
		data = keyed.ExportSealedDataWithKey(key)
	} else {
		data = exporter.ExportSealedData()
	}
	var env []string
	var selectors map[string]string
	if applied, appliedSelectors, err := applyFinalizeOverlays(base.Env, overlays); err != nil {
		return Plan{}, err
	} else {
		env, selectors = applied, appliedSelectors
	}
	if err := refuseFinalizeBindings(bindings); err != nil {
		return Plan{}, err
	}
	final := base
	final.Env = env
	final.Argv = append(append([]string(nil), base.Argv...), overlays.NativeTail...)
	if err := rederiveFinalSession(&final, overlays.NativeTail); err != nil {
		return Plan{}, err
	}
	data.Argv = append([]string(nil), final.Argv...)
	_ = selectors
	data = cloneSealedData(data)
	b := bindFinalProcess(final, key)
	data.Binding = b.export()
	// The finalized projection carries the plugin's own selectors beside
	// the final-process commitments: the plugin importer needs its
	// selectors back to rebuild an equivalent verifier at import, and the
	// binding commitments stay where the finalized shape has always
	// carried them. Import strips the binding keys before plugin dispatch,
	// so plugins never see them. (Binding keys are dense "0".."N" indices;
	// no plugin emits numeric selector keys, and a colliding plugin key
	// would strip to a missing selector and refuse malformed at import.)
	if data.Selectors == nil {
		data.Selectors = make(map[string]string, len(b.selectors))
	}
	for key, value := range b.selectors {
		data.Selectors[key] = value
	}
	final.execVerifier = finalizedSealedVerifier{sealed: data, bindings: b, artifacts: artifacts}
	return final, nil
}

// rederiveFinalSession keeps Session describing the argv the finalized plan
// carries. A native tail appends tokens after the base argv, and a tail token
// can be a session selector, so the record is derived again over the final
// argv through the plugin that filled it. Without a tail the final argv is
// the base argv and the sealed base record stands. Any other system (no
// planner) carries no record, and none is invented.
func rederiveFinalSession(final *Plan, tail []string) error {
	if final.sessionPlanner == nil || len(tail) == 0 {
		return nil
	}
	if session, err := deriveSession(final.sessionPlanner, final.System, final.Argv, final.WorkDir, final.Mode); err != nil {
		return err
	} else {
		final.Session = session
	}
	return nil
}

// applyFinalizeOverlays validates the fragment, prompt and native overlays
// in native overlay order and applies the env halves. It returns the final
// env and the final values of every overlaid name: the env-sensitive
// selectors the finalized verifier binds.
func applyFinalizeOverlays(base []string, overlays FinalizeOverlays) ([]string, map[string]string, error) {
	var fragment sealEnvOverlay
	if validated, err := validateFinalizeEnvOverlay(overlays.FragmentEnv, "fragment"); err != nil {
		return nil, nil, err
	} else {
		fragment = validated
	}
	var prompt sealEnvOverlay
	if validated, err := validateFinalizeEnvOverlay(overlays.PromptEnv, "prompt"); err != nil {
		return nil, nil, err
	} else {
		prompt = validated
	}
	if err := validateFinalizeNativeTail(overlays.NativeTail); err != nil {
		return nil, nil, err
	}
	final := applySealEnvOverlay(append([]string(nil), base...), fragment)
	final = applySealEnvOverlay(final, prompt)
	var selectors map[string]string
	if len(fragment.order)+len(prompt.order) > 0 {
		values := sealEnvValues(final)
		selectors = make(map[string]string, len(fragment.order)+len(prompt.order))
		for _, name := range fragment.order {
			selectors[name] = values[name]
		}
		for _, name := range prompt.order {
			selectors[name] = values[name]
		}
	}
	return final, selectors, nil
}

// sealEnvOverlay is one validated env overlay: names in overlay order plus
// their values.
type sealEnvOverlay struct {
	order  []string
	values map[string]string
}

// validateFinalizeEnvOverlay refuses a malformed overlay entry and returns
// the overlay's names in order with their values. A name set twice in ONE
// overlay refuses: across overlays the later one wins by order, but within
// one overlay a duplicate is an ambiguous intent, not a precedence.
func validateFinalizeEnvOverlay(entries []string, overlay string) (sealEnvOverlay, error) {
	validated := sealEnvOverlay{values: make(map[string]string, len(entries))}
	for index, entry := range entries {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			return sealEnvOverlay{}, fmt.Errorf("%w: %s entry %d carries no value", ErrFinalizeOverlayMalformed, overlay, index)
		}
		if !validEnvironmentName(name) {
			return sealEnvOverlay{}, fmt.Errorf("%w: %s entry %d names %q", ErrFinalizeOverlayMalformed, overlay, index, name)
		}
		if strings.IndexByte(value, 0) >= 0 {
			return sealEnvOverlay{}, fmt.Errorf("%w: %s entry %q carries a value that cannot live in an environment", ErrFinalizeOverlayMalformed, overlay, name)
		}
		if !guardStringsRepresentable(value) {
			return sealEnvOverlay{}, fmt.Errorf("%w: %s entry %q carries a value that cannot survive guard JSON", ErrFinalizeOverlayMalformed, overlay, name)
		}
		if _, duplicate := validated.values[name]; duplicate {
			return sealEnvOverlay{}, fmt.Errorf("%w: %s entry %q sets the same variable twice", ErrFinalizeOverlayMalformed, overlay, name)
		}
		validated.order = append(validated.order, name)
		validated.values[name] = value
	}
	return validated, nil
}

func validateFinalizeNativeTail(tail []string) error {
	for index, token := range tail {
		if strings.IndexByte(token, 0) >= 0 {
			return fmt.Errorf("%w: native tail token %d carries a byte that cannot live in argv", ErrFinalizeOverlayMalformed, index)
		}
		if !guardStringsRepresentable(token) {
			return fmt.Errorf("%w: native tail token %d carries a string that cannot survive guard JSON", ErrFinalizeOverlayMalformed, index)
		}
	}
	return nil
}

func refuseFinalizeBindings(bindings []TypedBinding) error {
	if len(bindings) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrFinalizeBindingUnknown, bindings[0].Kind)
}

// applySealEnvOverlay sets each overlaid name to its overlay value. An
// overridden entry keeps its first position and takes the new value; stale
// duplicates of an overlaid name collapse so the name appears exactly once
// afterwards, matching the last-wins read the verifier performs. A new name
// appends in overlay order.
func applySealEnvOverlay(env []string, overlay sealEnvOverlay) []string {
	if len(overlay.order) == 0 {
		return env
	}
	first := make(map[string]int, len(overlay.order))
	for index, entry := range env {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, overlaid := overlay.values[name]; !overlaid {
			continue
		}
		if _, seen := first[name]; !seen {
			first[name] = index
		}
	}
	applied := make([]string, 0, len(env)+len(overlay.order))
	replaced := make(map[string]bool, len(overlay.order))
	for index, entry := range env {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			applied = append(applied, entry)
			continue
		}
		value, overlaid := overlay.values[name]
		if !overlaid {
			applied = append(applied, entry)
			continue
		}
		if keep := first[name]; keep != index {
			continue
		}
		applied = append(applied, name+"="+value)
		replaced[name] = true
	}
	for _, name := range overlay.order {
		if !replaced[name] {
			applied = append(applied, name+"="+overlay.values[name])
		}
	}
	return applied
}

func sealEnvValues(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		values[name] = value
	}
	return values
}

// finalizedBindings are the exact final process facts every finalized
// verifier checks: the binary, the ordered argv, and the final values of
// all environment entries in their original order.
type finalizedBindings struct {
	binary    string
	argv      []string
	selectors map[string]string
	envNames  []string
	key       SealCommitmentKey
	// session is the Session the plan was finalized with, bound in-process
	// only: presence (nil versus present), name, RCEnabled and RCIndices with
	// nil-versus-empty. sessionBound separates "finalized with no record"
	// from a binding rebuilt from the frozen guard wire (ImportSeal), which
	// carries no Session and never binds one: across a process boundary
	// Session is UNVERIFIED, see session.go.
	session      *PlanSession
	sessionBound bool
}

func (b finalizedBindings) verify(plan Plan) error {
	if plan.Binary != b.binary {
		return fmt.Errorf("%w: binary differs", ErrFinalizedProcessChanged)
	}
	if !slices.Equal(plan.Argv, b.argv) {
		return fmt.Errorf("%w: argv differs (got %d elements, sealed %d)", ErrFinalizedProcessChanged, len(plan.Argv), len(b.argv))
	}
	if b.sessionBound && !sessionsEqual(plan.Session, b.session) {
		return fmt.Errorf("%w: session record differs", ErrFinalizedProcessChanged)
	}
	names := environmentNames(plan.Env)
	if !slices.Equal(names, b.envNames) {
		return fmt.Errorf("%w: environment names or order differ", ErrFinalizedProcessChanged)
	}
	for i, name := range names {
		_, value, found := strings.Cut(plan.Env[i], "=")
		if !found {
			return fmt.Errorf("%w: env entry is malformed", ErrFinalizedProcessChanged)
		}
		if literalCommitment(b.key, name, value) != b.selectors[fmt.Sprint(i)] {
			return fmt.Errorf("%w: env selector %q changed", ErrFinalizedProcessChanged, name)
		}
	}
	return nil
}

// finalizedSealedVerifier binds the exact final process of a sealed base:
// the final binary, argv and selectors, plus the base seal's original
// artifacts, re-verified — never re-minted — at exec time.
type finalizedSealedVerifier struct {
	sealed    SealedData
	bindings  finalizedBindings
	artifacts ExecArtifactVerifier
}

func (v finalizedSealedVerifier) VerifyBeforeExec(plan Plan) error {
	if err := v.bindings.verify(plan); err != nil {
		return err
	}
	// A plugin with plan-aware final checks (a release re-probe) runs
	// them against the final plan; anything else re-verifies artifacts.
	if final, ok := v.artifacts.(ExecFinalizedPlanVerifier); ok {
		return final.VerifyFinalizedPlan(plan)
	}
	return v.artifacts.VerifySealedArtifacts()
}

func (v finalizedSealedVerifier) ExportSealedData() SealedData {
	data := cloneSealedData(v.sealed)
	data.Argv = append([]string(nil), v.bindings.argv...)
	data.Binding = v.bindings.export()
	return data
}

func (v finalizedSealedVerifier) VerifySealedArtifacts() error {
	return v.artifacts.VerifySealedArtifacts()
}

// finalizedUnsealedVerifier binds the exact final process of an unsealed
// base: the final binary, argv and selectors, with no artifacts to carry.
type finalizedUnsealedVerifier struct {
	bindings finalizedBindings
}

func (v finalizedUnsealedVerifier) VerifyBeforeExec(plan Plan) error {
	return v.bindings.verify(plan)
}

func isFinalizedVerifier(v ExecPlanVerifier) bool {
	switch v.(type) {
	case finalizedSealedVerifier, finalizedUnsealedVerifier:
		return true
	}
	return false
}
