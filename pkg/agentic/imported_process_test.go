package agentic_test

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/muse"
)

// importedStubID names a sealed test-only system: a plugin declaring a
// sealer, an exporter and an importer. The double proves the generic sealed
// path — export, import, mismatch, delegation — through the public registry
// API, and its importer is wrappable so a verifier-only importer (one that
// hides the optional exporter) can be built without touching a real plugin.
const importedStubID = "imported-stub"

var importedStubArtifact = errors.New("imported-stub: sealed artifact changed")

var importedStubArtifacts = struct {
	sync.Mutex
	ok      map[string]bool
	counter uint64
}{ok: map[string]bool{}}

type importedStubSeal struct {
	binary string
	argv   []string
	path   string
}

func (s *importedStubSeal) VerifyBeforeExec(plan agentic.Plan) error {
	if plan.Binary != s.binary || !slices.Equal(plan.Argv, s.argv) {
		return errors.New("imported-stub: sealed process differs")
	}
	return s.VerifySealedArtifacts()
}

func (s *importedStubSeal) ExportSealedData() agentic.SealedData {
	return agentic.SealedData{
		Binary: s.binary,
		Argv:   slices.Clone(s.argv),
		Artifacts: []agentic.SealedArtifact{{
			Name:   "stub-artifact",
			Path:   s.path,
			Digest: "sha256:" + strings.Repeat("a", 64),
		}},
	}
}

func (s *importedStubSeal) VerifySealedArtifacts() error {
	importedStubArtifacts.Lock()
	defer importedStubArtifacts.Unlock()
	if !importedStubArtifacts.ok[s.path] {
		return importedStubArtifact
	}
	return nil
}

type importedStubSystem struct{}

func (importedStubSystem) ID() agentic.SystemID { return importedStubID }
func (importedStubSystem) Capabilities() agentic.Capabilities {
	return agentic.Capabilities{LaunchModes: []agentic.LaunchMode{agentic.LaunchModeExec}, EffortTransport: agentic.EffortTransportNone}
}
func (importedStubSystem) ResolveBinary(agentic.LaunchRequest) (string, error) {
	return "/synthetic/bin/imported-stub", nil
}
func (importedStubSystem) Argv(agentic.LaunchRequest, agentic.LaunchMode) ([]string, error) {
	return []string{"--stub"}, nil
}
func (importedStubSystem) ChildEnv(parent []string, _ agentic.LaunchRequest) ([]string, error) {
	return append([]string(nil), parent...), nil
}
func (importedStubSystem) Stdin(agentic.LaunchRequest) (agentic.StdinPayload, error) {
	return agentic.StdinPayload{}, nil
}
func (importedStubSystem) ValidateComposition(c agentic.Composition) error {
	if !c.IsZero() {
		return errors.New("imported-stub: no composition grammar")
	}
	return nil
}
func (importedStubSystem) SealExecPlan(plan agentic.Plan) (agentic.ExecPlanVerifier, error) {
	serial := strconv.FormatUint(atomic.AddUint64(&importedStubArtifacts.counter, 1), 10)
	path := "/synthetic/imported-stub/artifact-" + serial
	importedStubArtifacts.Lock()
	importedStubArtifacts.ok[path] = true
	importedStubArtifacts.Unlock()
	seal := &importedStubSeal{binary: plan.Binary, argv: slices.Clone(plan.Argv), path: path}
	if err := seal.VerifyBeforeExec(plan); err != nil {
		return nil, err
	}
	return seal, nil
}
func (importedStubSystem) ImportSealedData(data agentic.SealedData) (agentic.ExecPlanVerifier, error) {
	if len(data.Artifacts) != 1 || data.Artifacts[0].Name != "stub-artifact" {
		return nil, errors.New("imported-stub: sealed artifact is malformed")
	}
	seal := &importedStubSeal{binary: data.Binary, argv: slices.Clone(data.Argv), path: data.Artifacts[0].Path}
	if err := seal.VerifySealedArtifacts(); err != nil {
		return nil, err
	}
	return seal, nil
}

func importedStubPlan(t *testing.T) (agentic.Plan, agentic.Seal) {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(importedStubSystem{}); err != nil {
		t.Fatalf("Register stub: %v", err)
	}
	plan, err := agentic.BuildPlan(registry, agentic.LaunchRequest{
		System:  importedStubID,
		Model:   agentic.Model{ID: "stub-test"},
		WorkDir: t.TempDir(),
		Home:    t.TempDir(),
		Env:     []string{"PATH=/synthetic/bin"},
	}, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlan stub: %v", err)
	}
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal stub: %v", err)
	}
	return plan, seal
}

func importedClaudePlan(t *testing.T) (agentic.Plan, agentic.Seal) {
	t.Helper()
	plan := sealTestClaudePlan(t)
	// The shared helper leaves the documentation home ("~/.claude"); the
	// contract's final process carries an absolute managed home, so bind
	// that shape before exporting. The unsealed export binds no process
	// facts either way.
	plan.Home = t.TempDir()
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal Claude: %v", err)
	}
	return plan, seal
}

func TestNewImportedProcessBindsExactFinalProcess(t *testing.T) {
	plan, seal := importedClaudePlan(t)
	imported, err := agentic.NewImportedProcess(claude.New(), seal, plan)
	if err != nil {
		t.Fatalf("NewImportedProcess: %v", err)
	}
	if err := imported.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("VerifyBeforeExec untouched: %v", err)
	}
	round := imported.Process()
	if round.System != plan.System || round.Mode != plan.Mode || round.Binary != plan.Binary || !slices.Equal(round.Argv, plan.Argv) || !slices.Equal(round.Env, plan.Env) || round.WorkDir != plan.WorkDir || round.Home != plan.Home || round.Stdin.Attached != plan.Stdin.Attached || string(round.Stdin.Bytes) != string(plan.Stdin.Bytes) {
		t.Fatalf("Process() = %+v, want the bound exact process", round)
	}
	if err := imported.VerifyBeforeExec(round); err != nil {
		t.Fatalf("VerifyBeforeExec Process(): %v", err)
	}
}

func TestNewImportedProcessRefusesNilSystem(t *testing.T) {
	_, seal := importedClaudePlan(t)
	if _, err := agentic.NewImportedProcess(nil, seal, agentic.Plan{}); err == nil {
		t.Fatal("nil system admitted")
	}
}

func TestNewImportedProcessRefusesUnsealedForSealerSystem(t *testing.T) {
	plan, seal := importedClaudePlan(t)
	if seal.Data.Kind != agentic.SealKindUnsealed {
		t.Fatalf("seal kind = %q, want the closed unsealed marker", seal.Data.Kind)
	}
	for name, sys := range map[string]agentic.System{"muse": muse.New(), "codex": codex.New()} {
		claudePlan := plan
		claudePlan.System = sys.ID()
		if _, err := agentic.NewImportedProcess(sys, seal, claudePlan); !errors.Is(err, agentic.ErrSealUnsealedRefused) {
			t.Fatalf("%s unsealed import err = %v, want ErrSealUnsealedRefused", name, err)
		}
	}
}

func TestNewImportedProcessRefusesMismatchedSystem(t *testing.T) {
	plan, seal := importedClaudePlan(t)
	plan.System = muse.New().ID()
	if _, err := agentic.NewImportedProcess(claude.New(), seal, plan); !errors.Is(err, agentic.ErrImportedVerifierMismatch) {
		t.Fatalf("cross-system process err = %v, want ErrImportedVerifierMismatch", err)
	}
}

func TestNewImportedProcessRefusesNonAbsolutePaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*agentic.Plan)
		fatal  string
	}{
		{name: "relative-binary", mutate: func(p *agentic.Plan) { p.Binary = "claude" }, fatal: "relative binary admitted"},
		{name: "relative-binary-witness", mutate: func(p *agentic.Plan) { p.Binary = "witness-relative-binary" }, fatal: "witness relative binary admitted"},
		{name: "empty-binary", mutate: func(p *agentic.Plan) { p.Binary = "" }, fatal: "empty binary admitted"},
		{name: "relative-cwd", mutate: func(p *agentic.Plan) { p.WorkDir = "relative" }, fatal: "relative cwd admitted"},
		{name: "relative-cwd-witness", mutate: func(p *agentic.Plan) { p.WorkDir = "witness-relative-cwd" }, fatal: "witness relative cwd admitted"},
		{name: "empty-cwd", mutate: func(p *agentic.Plan) { p.WorkDir = "" }, fatal: "empty cwd admitted"},
		{name: "relative-home", mutate: func(p *agentic.Plan) { p.Home = "relative" }, fatal: "relative home admitted"},
		{name: "relative-home-witness", mutate: func(p *agentic.Plan) { p.Home = "witness-relative-home" }, fatal: "witness relative home admitted"},
		{name: "empty-home", mutate: func(p *agentic.Plan) { p.Home = "" }, fatal: "empty home admitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, seal := importedClaudePlan(t)
			tc.mutate(&plan)
			if _, err := agentic.NewImportedProcess(claude.New(), seal, plan); !errors.Is(err, agentic.ErrImportedProcessMalformed) {
				t.Fatalf("%s: %v", tc.fatal, err)
			}
		})
	}
}

func TestNewImportedProcessRefusesArgvZero(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*agentic.Plan)
		fatal  string
	}{
		{name: "argv0", mutate: func(p *agentic.Plan) { p.Argv = []string{p.Binary} }, fatal: "argv carrying argv[0] admitted"},
		{name: "argv0-witness", mutate: func(p *agentic.Plan) {
			p.Binary = "/synthetic/witness-argv0"
			p.Argv = []string{"/synthetic/witness-argv0"}
		}, fatal: "witness argv[0] admitted"},
		{name: "argv0-with-tail", mutate: func(p *agentic.Plan) { p.Argv = append([]string{p.Binary}, "--tail") }, fatal: "argv[0] with tail admitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, seal := importedClaudePlan(t)
			tc.mutate(&plan)
			if _, err := agentic.NewImportedProcess(claude.New(), seal, plan); !errors.Is(err, agentic.ErrImportedProcessMalformed) {
				t.Fatalf("%s: %v", tc.fatal, err)
			}
		})
	}
}

func TestNewImportedProcessRefusesMalformedEnv(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   []string
		fatal string
	}{
		{name: "no-equals", env: []string{"BROKEN"}, fatal: "env entry without value admitted"},
		{name: "no-equals-witness", env: []string{"WITNESS_NO_EQUALS"}, fatal: "witness env entry without value admitted"},
		{name: "empty-name", env: []string{"=value"}, fatal: "env entry with empty name admitted"},
		{name: "empty-name-witness", env: []string{"=witness-empty-name"}, fatal: "witness env entry with empty name admitted"},
		{name: "bad-name-digit", env: []string{"9lives=x"}, fatal: "env entry with invalid name admitted"},
		{name: "bad-name-dash", env: []string{"has-dash=x"}, fatal: "env entry with dashed name admitted"},
		{name: "nul-value", env: []string{"NAME=bad\x00value"}, fatal: "env entry with NUL admitted"},
		{name: "nul-value-witness", env: []string{"WITNESS_NUL=bad\x00value"}, fatal: "witness env entry with NUL admitted"},
		{name: "nul-name", env: []string{"NA\x00ME=x"}, fatal: "env entry with NUL name admitted"},
		{name: "duplicate", env: []string{"DUP_SHAPE=1", "DUP_SHAPE=2"}, fatal: "duplicate env name admitted"},
		{name: "duplicate-witness", env: []string{"WITNESS_DUP=1", "WITNESS_DUP=2"}, fatal: "witness duplicate env name admitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, seal := importedClaudePlan(t)
			plan.Env = tc.env
			if _, err := agentic.NewImportedProcess(claude.New(), seal, plan); !errors.Is(err, agentic.ErrImportedProcessMalformed) {
				t.Fatalf("%s: %v", tc.fatal, err)
			}
		})
	}
}

func TestNewImportedProcessRefusesDetachedStdinWithBytes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bytes []byte
		fatal string
	}{
		{name: "detached-bytes", bytes: []byte("stray"), fatal: "detached stdin with bytes admitted"},
		{name: "detached-bytes-witness", bytes: []byte("witness-detached"), fatal: "witness detached stdin admitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, seal := importedClaudePlan(t)
			plan.Stdin = agentic.StdinPayload{Attached: false, Bytes: tc.bytes}
			if _, err := agentic.NewImportedProcess(claude.New(), seal, plan); !errors.Is(err, agentic.ErrImportedProcessMalformed) {
				t.Fatalf("%s: %v", tc.fatal, err)
			}
		})
	}
}

func TestNewImportedProcessAcceptsContractStdinShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stdin agentic.StdinPayload
	}{
		{name: "null", stdin: agentic.StdinPayload{}},
		{name: "empty-attached", stdin: agentic.StdinPayload{Attached: true}},
		{name: "empty-bytes", stdin: agentic.StdinPayload{Attached: true, Bytes: []byte{}}},
		{name: "bytes", stdin: agentic.StdinPayload{Attached: true, Bytes: []byte("input")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, seal := importedClaudePlan(t)
			plan.Stdin = tc.stdin
			imported, err := agentic.NewImportedProcess(claude.New(), seal, plan)
			if err != nil {
				t.Fatalf("contract stdin shape %s refused: %v", tc.name, err)
			}
			if err := imported.VerifyBeforeExec(plan); err != nil {
				t.Fatalf("VerifyBeforeExec %s: %v", tc.name, err)
			}
		})
	}
}

func TestNewImportedProcessRefusesMismatchedBinding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*agentic.Plan)
		fatal  string
	}{
		{name: "argv", mutate: func(p *agentic.Plan) { p.Argv = append(slices.Clone(p.Argv), "--other") }, fatal: "argv mismatch admitted"},
		{name: "binary", mutate: func(p *agentic.Plan) { p.Binary = "/synthetic/bin/other" }, fatal: "binary mismatch admitted"},
		{name: "binary-witness", mutate: func(p *agentic.Plan) { p.Binary = "/synthetic/witness-seal-binary" }, fatal: "witness sealed binary mismatch admitted"},
		{name: "argv-witness", mutate: func(p *agentic.Plan) { p.Argv = append(slices.Clone(p.Argv), "--witness-seal-argv") }, fatal: "witness sealed argv mismatch admitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, seal := importedStubPlan(t)
			tc.mutate(&plan)
			if _, err := agentic.NewImportedProcess(importedStubSystem{}, seal, plan); !errors.Is(err, agentic.ErrImportedVerifierMismatch) {
				t.Fatalf("%s: %v", tc.fatal, err)
			}
		})
	}
	plan, seal := importedStubPlan(t)
	imported, err := agentic.NewImportedProcess(importedStubSystem{}, seal, plan)
	if err != nil {
		t.Fatalf("NewImportedProcess stub: %v", err)
	}
	if err := imported.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("VerifyBeforeExec stub untouched: %v", err)
	}
}

// importedVerifierOnlySystem hides the stub's optional exporter behind the
// bare importer contract: a legal verifier-only importer. The construction
// mismatch must come from the seal's own binding data, never from the
// verifier's capabilities.
type importedVerifierOnlySystem struct{ importedStubSystem }

func (s importedVerifierOnlySystem) ImportSealedData(data agentic.SealedData) (agentic.ExecPlanVerifier, error) {
	verifier, err := s.importedStubSystem.ImportSealedData(data)
	if err != nil {
		return nil, err
	}
	return struct{ agentic.ExecPlanVerifier }{verifier}, nil
}

func TestNewImportedProcessRefusesMismatchWithoutExporter(t *testing.T) {
	plan, seal := importedStubPlan(t)
	if _, ok := mustImportStubVerifier(t, seal).(agentic.ExecSealExporter); ok {
		t.Fatal("verifier-only double exposes the exporter it must hide")
	}
	mismatched := plan
	mismatched.Argv = append(slices.Clone(plan.Argv), "--different")
	if _, err := agentic.NewImportedProcess(importedVerifierOnlySystem{}, seal, mismatched); !errors.Is(err, agentic.ErrImportedVerifierMismatch) {
		t.Fatalf("verifier-only argv mismatch err = %v, want ErrImportedVerifierMismatch", err)
	}
	mismatched = plan
	mismatched.Binary = "/synthetic/bin/different"
	if _, err := agentic.NewImportedProcess(importedVerifierOnlySystem{}, seal, mismatched); !errors.Is(err, agentic.ErrImportedVerifierMismatch) {
		t.Fatalf("verifier-only binary mismatch err = %v, want ErrImportedVerifierMismatch", err)
	}
	imported, err := agentic.NewImportedProcess(importedVerifierOnlySystem{}, seal, plan)
	if err != nil {
		t.Fatalf("verifier-only matching import refused: %v", err)
	}
	if err := imported.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("VerifyBeforeExec verifier-only untouched: %v", err)
	}
}

func mustImportStubVerifier(t *testing.T, seal agentic.Seal) agentic.ExecPlanVerifier {
	t.Helper()
	verifier, err := agentic.ImportSeal(importedVerifierOnlySystem{}, seal)
	if err != nil {
		t.Fatalf("ImportSeal verifier-only: %v", err)
	}
	return verifier
}

func TestNewImportedProcessRefusesFinalizedBindingMismatch(t *testing.T) {
	base, _ := importedClaudePlan(t)
	final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{NativeTail: []string{"--tail"}}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	seal, err := final.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal finalized: %v", err)
	}
	if seal.Data.Kind != agentic.SealKindUnsealedBound || seal.Data.Binding == nil {
		t.Fatalf("seal kind = %q, want a finalized unsealed binding", seal.Data.Kind)
	}
	mismatched := final
	mismatched.Argv = append(slices.Clone(final.Argv), "--different")
	if _, err := agentic.NewImportedProcess(claude.New(), seal, mismatched); !errors.Is(err, agentic.ErrImportedVerifierMismatch) {
		t.Fatalf("finalized argv mismatch err = %v, want ErrImportedVerifierMismatch", err)
	}
	imported, err := agentic.NewImportedProcess(claude.New(), seal, final)
	if err != nil {
		t.Fatalf("finalized matching import refused: %v", err)
	}
	if err := imported.VerifyBeforeExec(final); err != nil {
		t.Fatalf("VerifyBeforeExec finalized untouched: %v", err)
	}
}

func TestImportedProcessStubDelegationRefusal(t *testing.T) {
	plan, seal := importedStubPlan(t)
	imported, err := agentic.NewImportedProcess(importedStubSystem{}, seal, plan)
	if err != nil {
		t.Fatalf("NewImportedProcess stub: %v", err)
	}
	if err := imported.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("VerifyBeforeExec stub untouched: %v", err)
	}
	// Flip the sealed artifact behind the imported verifier: the exact
	// process still matches, so only the delegate can refuse.
	artifact := seal.Data.Sealed.Artifacts[0].Path
	importedStubArtifacts.Lock()
	importedStubArtifacts.ok[artifact] = false
	importedStubArtifacts.Unlock()
	if err := imported.VerifyBeforeExec(plan); !errors.Is(err, importedStubArtifact) {
		t.Fatalf("swapped stub artifact err = %v, want the delegate refusal", err)
	}
}

func TestZeroImportedProcessRefuses(t *testing.T) {
	plan := sealTestClaudePlan(t)
	if err := (agentic.ImportedProcess{}).VerifyBeforeExec(plan); !errors.Is(err, agentic.ErrImportedVerifierMissing) {
		t.Fatalf("zero ImportedProcess err = %v, want ErrImportedVerifierMissing", err)
	}
}

func TestImportedProcessRefusesDrift(t *testing.T) {
	plan, seal := importedClaudePlan(t)
	mutate := func(change func(*agentic.Plan)) agentic.Plan {
		changed := plan
		changed.Argv = slices.Clone(plan.Argv)
		changed.Env = slices.Clone(plan.Env)
		changed.Stdin.Bytes = slices.Clone(plan.Stdin.Bytes)
		change(&changed)
		return changed
	}
	bind := func(bound agentic.Plan) agentic.ImportedProcess {
		imported, err := agentic.NewImportedProcess(claude.New(), seal, bound)
		if err != nil {
			t.Fatalf("NewImportedProcess: %v", err)
		}
		return imported
	}
	reorderedArgv := mutate(func(p *agentic.Plan) { p.Argv = append(p.Argv, "--reorder-a", "--reorder-b") })
	reorderedArgv.Argv[len(reorderedArgv.Argv)-2], reorderedArgv.Argv[len(reorderedArgv.Argv)-1] = reorderedArgv.Argv[len(reorderedArgv.Argv)-1], reorderedArgv.Argv[len(reorderedArgv.Argv)-2]
	reorderedEnv := mutate(func(p *agentic.Plan) { p.Env = append(p.Env, "IMPORTED_REORDER_A=1", "IMPORTED_REORDER_B=2") })
	reorderedEnv.Env[len(reorderedEnv.Env)-2], reorderedEnv.Env[len(reorderedEnv.Env)-1] = reorderedEnv.Env[len(reorderedEnv.Env)-1], reorderedEnv.Env[len(reorderedEnv.Env)-2]
	cases := []struct {
		name    string
		bound   agentic.Plan
		current agentic.Plan
		fatal   string
	}{
		{name: "binary", bound: plan, current: mutate(func(p *agentic.Plan) { p.Binary = "/synthetic/bin/other" }), fatal: "binary change admitted"},
		{name: "binary-witness", bound: plan, current: mutate(func(p *agentic.Plan) { p.Binary = "/synthetic/witness-binary" }), fatal: "witness binary change admitted"},
		{name: "argv-append", bound: plan, current: mutate(func(p *agentic.Plan) { p.Argv = append(p.Argv, "--other") }), fatal: "argv append admitted"},
		{name: "argv-witness", bound: plan, current: mutate(func(p *agentic.Plan) { p.Argv = append(p.Argv, "--imported-witness") }), fatal: "witness argv append admitted"},
		{name: "argv-remove", bound: mutate(func(p *agentic.Plan) { p.Argv = append(p.Argv, "--removable") }), current: plan, fatal: "argv removal admitted"},
		{name: "argv-reorder", bound: mutate(func(p *agentic.Plan) { p.Argv = append(p.Argv, "--reorder-a", "--reorder-b") }), current: reorderedArgv, fatal: "argv reorder admitted"},
		{name: "env-value", bound: plan, current: mutate(func(p *agentic.Plan) { p.Env = append(p.Env, "IMPORTED_DRIFT=1") }), fatal: "env append admitted"},
		{name: "env-witness", bound: plan, current: mutate(func(p *agentic.Plan) { p.Env = append(p.Env, "IMPORTED_WITNESS=env-appended") }), fatal: "witness env append admitted"},
		{name: "env-change", bound: mutate(func(p *agentic.Plan) { p.Env = append(p.Env, "IMPORTED_CHANGE=base") }), current: mutate(func(p *agentic.Plan) { p.Env = append(p.Env, "IMPORTED_CHANGE=changed") }), fatal: "env value change admitted"},
		{name: "env-reorder", bound: mutate(func(p *agentic.Plan) { p.Env = append(p.Env, "IMPORTED_REORDER_A=1", "IMPORTED_REORDER_B=2") }), current: reorderedEnv, fatal: "env reorder admitted"},
		// No duplicate-collapse leg: construction refuses duplicate env
		// names, so a bound process can never carry them. Duplicates on
		// the candidate side still trip the ordered comparison below, and
		// TestNewImportedProcessRefusesMalformedEnv pins the construction
		// refusal.
		{name: "env-duplicate-add", bound: plan, current: mutate(func(p *agentic.Plan) { p.Env = append(p.Env, "IMPORTED_DUP_ADD=1", "IMPORTED_DUP_ADD=1") }), fatal: "duplicate add admitted"},
		{name: "cwd", bound: plan, current: mutate(func(p *agentic.Plan) { p.WorkDir = "/synthetic/other" }), fatal: "cwd change admitted"},
		{name: "cwd-witness", bound: plan, current: mutate(func(p *agentic.Plan) { p.WorkDir = "/synthetic/witness-cwd" }), fatal: "witness cwd change admitted"},
		{name: "home", bound: plan, current: mutate(func(p *agentic.Plan) { p.Home = "/synthetic/other-home" }), fatal: "home change admitted"},
		{name: "home-witness", bound: plan, current: mutate(func(p *agentic.Plan) { p.Home = "/synthetic/witness-home" }), fatal: "witness home change admitted"},
		{name: "stdin-attach-witness", bound: plan, current: mutate(func(p *agentic.Plan) {
			p.Stdin.Attached = true
			p.Stdin.Bytes = []byte("imported-witness")
		}), fatal: "witness stdin attach admitted"},
		{name: "stdin-attach", bound: plan, current: mutate(func(p *agentic.Plan) {
			p.Stdin.Attached = true
			p.Stdin.Bytes = []byte("other")
		}), fatal: "stdin attach admitted"},
		{name: "stdin-bytes", bound: mutate(func(p *agentic.Plan) {
			p.Stdin.Attached = true
			p.Stdin.Bytes = []byte("base")
		}), current: mutate(func(p *agentic.Plan) {
			p.Stdin.Attached = true
			p.Stdin.Bytes = []byte("changed")
		}), fatal: "stdin bytes change admitted"},
		{name: "stdin-detach", bound: mutate(func(p *agentic.Plan) {
			p.Stdin.Attached = true
			p.Stdin.Bytes = []byte("base")
		}), current: plan, fatal: "stdin detach admitted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			imported := bind(tc.bound)
			if err := imported.VerifyBeforeExec(tc.bound); err != nil {
				t.Fatalf("VerifyBeforeExec bound: %v", err)
			}
			if err := imported.VerifyBeforeExec(tc.current); !errors.Is(err, agentic.ErrImportedProcessChanged) {
				t.Fatalf("%s: %v", tc.fatal, err)
			}
		})
	}
}

func TestImportedProcessDeepCopies(t *testing.T) {
	plan, seal := importedClaudePlan(t)
	input := plan
	input.Argv = []string{"--copy-probe"}
	input.Env = append(slices.Clone(plan.Env), "IMPORTED_COPY=probe")
	input.Stdin = agentic.StdinPayload{Attached: true, Bytes: []byte("input")}
	want := input
	want.Argv = slices.Clone(input.Argv)
	want.Env = slices.Clone(input.Env)
	want.Stdin.Bytes = slices.Clone(input.Stdin.Bytes)
	imported, err := agentic.NewImportedProcess(claude.New(), seal, input)
	if err != nil {
		t.Fatalf("NewImportedProcess: %v", err)
	}
	input.Argv[0] = "--mutated-after-import"
	input.Env[len(input.Env)-1] = "IMPORTED_COPY=mutated"
	input.Stdin.Bytes[0] = 'X'
	if err := imported.VerifyBeforeExec(want); err != nil {
		t.Fatalf("mutated inputs moved the binding: %v", err)
	}
	if err := imported.VerifyBeforeExec(input); !errors.Is(err, agentic.ErrImportedProcessChanged) {
		t.Fatalf("mutated inputs admitted: %v", err)
	}
	pristine := imported.Process()
	if err := imported.VerifyBeforeExec(pristine); err != nil {
		t.Fatalf("VerifyBeforeExec pristine: %v", err)
	}
	pristine.Argv[0] = "--mutated-accessor"
	pristine.Env[len(pristine.Env)-1] = "IMPORTED_COPY=mutated"
	pristine.Stdin.Bytes[0] = 'Y'
	again := imported.Process()
	if err := imported.VerifyBeforeExec(again); err != nil {
		t.Fatalf("VerifyBeforeExec after accessor mutation: %v", err)
	}
	if again.Argv[0] == "--mutated-accessor" || again.Env[len(again.Env)-1] == "IMPORTED_COPY=mutated" || again.Stdin.Bytes[0] == 'Y' {
		t.Fatal("accessor mutation moved the binding")
	}
}

func TestImportedProcessAccessorNeverAliases(t *testing.T) {
	plan, seal := importedClaudePlan(t)
	plan.Argv = []string{"--alias-probe"}
	imported, err := agentic.NewImportedProcess(claude.New(), seal, plan)
	if err != nil {
		t.Fatalf("NewImportedProcess: %v", err)
	}
	first := imported.Process()
	first.Argv[0] = "--aliased"
	second := imported.Process()
	if second.Argv[0] == "--aliased" {
		t.Fatal("accessor argv aliases bound process")
	}
	if err := imported.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("VerifyBeforeExec after accessor mutation: %v", err)
	}
}
