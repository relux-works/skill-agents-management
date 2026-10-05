package codex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Witness suffixes for the narrowing mutants in
// tools/launchcontext-mutants/codex_tempdir_mutants.go. Each mutant admits
// only values carrying its suffix; the named subtests below build absolute
// witness paths with these suffixes (platform-neutral: no literal is
// absolute on every OS, so the mutant matches the suffix, not the path).
const (
	tempDirMappingWitnessSuffix = "codex-tempdir-witness-map"
	tempDirDriftWitnessSuffix   = "codex-tempdir-witness-drift"
)

// tempDirPtr states a present per-launch temp dir. Absence is nil, never
// a pointer to an empty string: the two are different facts and the gate
// refuses the second.
func tempDirPtr(value string) *string { return &value }

// tempDirPlan builds an exec plan for a TempDir-carrying request through the
// production entry point: a real registry and agentic.BuildPlan. parentEnv
// is seeded before the stub PATH entry. A nil tempDir states absence.
func tempDirPlan(t *testing.T, tempDir *string, parentEnv []string) agentic.Plan {
	t.Helper()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)

	req := parityRequest(workDir)
	req.TempDir = tempDir
	req.PromptPath = writePromptFile(t, workDir, "per-launch temp dir prompt")
	req.Env = append(append([]string(nil), parentEnv...), "PATH="+binDir)

	return buildParityPlan(t, New(), req, agentic.LaunchModeExec)
}

// requireSingleTMPDIR asserts the plan carries exactly one TMPDIR entry
// with the wanted value. The failure text names the mapping, because this
// is the assertion the mapping mutant must break.
func requireSingleTMPDIR(t *testing.T, plan agentic.Plan, want string) {
	t.Helper()
	var found []string
	for _, entry := range plan.Env {
		if name, value, ok := strings.Cut(entry, "="); ok && name == TempDirEnv {
			found = append(found, value)
		}
	}
	if len(found) != 1 {
		t.Fatalf("TMPDIR missing for a set TempDir: want exactly one %s entry, found %d in %v", TempDirEnv, len(found), plan.Env)
	}
	if found[0] != want {
		t.Fatalf("TMPDIR = %q, want the mapped TempDir %q", found[0], want)
	}
}

// TestCodexTempDirReachesChildEnvLast is AC1 through BuildPlan: a set
// TempDir reaches the child as exactly one TMPDIR entry, written as the
// last env layer for that variable.
func TestCodexTempDirReachesChildEnvLast(t *testing.T) {
	t.Parallel()
	t.Run("maps-absolute-path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		plan := tempDirPlan(t, tempDirPtr(dir), nil)
		requireSingleTMPDIR(t, plan, dir)
		if got := plan.Env[len(plan.Env)-1]; got != TempDirEnv+"="+dir {
			t.Fatalf("last env entry = %q, want the TMPDIR write last; the mapping is not the last layer for this variable", got)
		}
	})
	t.Run("replaces-inherited-value", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		plan := tempDirPlan(t, tempDirPtr(dir), []string{TempDirEnv + "=/parent/tmp"})
		requireSingleTMPDIR(t, plan, dir)
	})
	t.Run("replaces-duplicate-inherited-values", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		plan := tempDirPlan(t, tempDirPtr(dir), []string{TempDirEnv + "=/parent/a", TempDirEnv + "=/parent/b"})
		requireSingleTMPDIR(t, plan, dir)
	})
	t.Run("trims-surrounding-whitespace", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		plan := tempDirPlan(t, tempDirPtr("  "+dir+"  "), nil)
		requireSingleTMPDIR(t, plan, dir)
	})
	t.Run("witness", func(t *testing.T) {
		t.Parallel()
		witness := filepath.Join(t.TempDir(), tempDirMappingWitnessSuffix)
		plan := tempDirPlan(t, tempDirPtr(witness), nil)
		requireSingleTMPDIR(t, plan, witness)
	})
}

// TestCodexTempDirJoinsOwnedEnv proves the last-layer claim against the one
// layer that applies after ChildEnv: the network patch. TMPDIR mapped from
// TempDir joins OwnedEnv, so a patch touching it refuses typed as an owned
// key instead of overriding the mapping.
func TestCodexTempDirJoinsOwnedEnv(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	dir := t.TempDir()

	req := parityRequest(workDir)
	req.TempDir = tempDirPtr(dir)
	req.PromptPath = writePromptFile(t, workDir, "owned temp dir prompt")
	req.Env = []string{"PATH=" + binDir}

	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	result, err := agentic.BuildPlanWithEnvironment(registry, req, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildPlanWithEnvironment: %v", err)
	}
	var found []string
	for _, entry := range result.OwnedEnv {
		if name, value, ok := strings.Cut(entry, "="); ok && name == TempDirEnv {
			found = append(found, value)
		}
	}
	if len(found) != 1 || found[0] != dir {
		t.Fatalf("OwnedEnv TMPDIR entries = %v, want exactly [%s]; the mapping is not owned and a later layer could override it", found, dir)
	}
}

// TestAbsentTempDirKeepsEnvByteForByte is AC3: without TempDir the module
// writes nothing to TMPDIR and whatever the parent carried passes through
// untouched — including duplicates and position.
func TestAbsentTempDirKeepsEnvByteForByte(t *testing.T) {
	t.Parallel()
	t.Run("no-tmpdir-stays-absent", func(t *testing.T) {
		t.Parallel()
		plan := tempDirPlan(t, nil, []string{"KEEP=yes"})
		for _, entry := range plan.Env {
			if name, _, ok := strings.Cut(entry, "="); ok && name == TempDirEnv {
				t.Fatalf("absent TempDir injected %q; the module must not write TMPDIR it was not given", entry)
			}
		}
	})
	t.Run("inherited-value-passes-through-in-place", func(t *testing.T) {
		t.Parallel()
		parent := []string{"KEEP=yes", TempDirEnv + "=/parent/tmp", "OTHER=no"}
		plan := tempDirPlan(t, nil, parent)
		var positions []int
		for i, entry := range plan.Env {
			if name, value, ok := strings.Cut(entry, "="); ok && name == TempDirEnv {
				positions = append(positions, i)
				if value != "/parent/tmp" {
					t.Fatalf("inherited TMPDIR changed to %q", value)
				}
			}
		}
		if len(positions) != 1 {
			t.Fatalf("inherited TMPDIR entries = %d, want exactly the parent's one, byte for byte", len(positions))
		}
	})
	t.Run("inherited-duplicates-pass-through-verbatim", func(t *testing.T) {
		t.Parallel()
		plan := tempDirPlan(t, nil, []string{TempDirEnv + "=/parent/a", TempDirEnv + "=/parent/b"})
		var found []string
		for _, entry := range plan.Env {
			if name, value, ok := strings.Cut(entry, "="); ok && name == TempDirEnv {
				found = append(found, value)
			}
		}
		if len(found) != 2 || found[0] != "/parent/a" || found[1] != "/parent/b" {
			t.Fatalf("inherited TMPDIR entries = %v, want the parent's two duplicates verbatim; an absent TempDir must not normalize what it did not write", found)
		}
	})
}

// TestCodexTempDirRefusesRelativeOrEmpty is AC4 through BuildPlan: a
// relative or blank TempDir refuses typed at plan time, for every caller,
// before any plugin surface runs.
func TestCodexTempDirRefusesRelativeOrEmpty(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		tempDir *string
	}{
		{name: "relative", tempDir: tempDirPtr("relative/path")},
		{name: "dot-relative", tempDir: tempDirPtr("./relative")},
		{name: "parent-relative", tempDir: tempDirPtr("../up")},
		{name: "blank-spaces", tempDir: tempDirPtr("   ")},
		{name: "blank-tab", tempDir: tempDirPtr("\t")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workDir := tempSlot(t)
			binDir := tempSlot(t)
			writeStubExecutable(t, binDir, executableName)
			req := parityRequest(workDir)
			req.TempDir = tc.tempDir
			req.PromptPath = writePromptFile(t, workDir, "refused temp dir prompt")
			req.Env = []string{"PATH=" + binDir}
			registry := agentic.NewRegistry()
			if err := registry.Register(New()); err != nil {
				t.Fatalf("Register: %v", err)
			}
			if _, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrTempDirInvalid) {
				t.Fatalf("BuildPlan with TempDir %q err = %v, want ErrTempDirInvalid", *tc.tempDir, err)
			}
		})
	}
	t.Run("absent-builds", func(t *testing.T) {
		t.Parallel()
		plan := tempDirPlan(t, nil, nil)
		if plan.Binary == "" {
			t.Fatal("absent TempDir refused a plan it must leave alone")
		}
	})
	t.Run("absolute-builds", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		plan := tempDirPlan(t, tempDirPtr(dir), nil)
		requireSingleTMPDIR(t, plan, dir)
	})
}

// tempDirDispatchSpy wraps the Codex plugin with the three pre-plan plugin
// surfaces BuildPlan can reach before planning — the Curator validator, the
// descriptor validator and request preparation — recording each invocation.
// Codex implements no preparer of its own; the spy adds the surface so the
// ordering test pins the gate ahead of preparation dispatch too.
type tempDirDispatchSpy struct {
	*System
	calls map[string]int

	prepareErr error
}

var (
	_ agentic.CuratorContextValidator    = (*tempDirDispatchSpy)(nil)
	_ agentic.ContextDescriptorValidator = (*tempDirDispatchSpy)(nil)
	_ agentic.LaunchRequestPreparer      = (*tempDirDispatchSpy)(nil)
)

func (s *tempDirDispatchSpy) ValidateCuratorContext(req agentic.LaunchRequest, mode agentic.LaunchMode) error {
	s.calls["ValidateCuratorContext"]++
	return s.System.ValidateCuratorContext(req, mode)
}

func (s *tempDirDispatchSpy) ValidateContextDescriptors(req agentic.LaunchRequest, mode agentic.LaunchMode) error {
	s.calls["ValidateContextDescriptors"]++
	return s.System.ValidateContextDescriptors(req, mode)
}

func (s *tempDirDispatchSpy) PrepareLaunchRequest(req agentic.LaunchRequest, _ agentic.LaunchMode) (agentic.LaunchRequestPreparation, error) {
	s.calls["PrepareLaunchRequest"]++
	if s.prepareErr != nil {
		return agentic.LaunchRequestPreparation{}, s.prepareErr
	}
	return agentic.LaunchRequestPreparation{PromptPath: req.PromptPath, Prompt: append([]byte(nil), req.Prompt...)}, nil
}

// tempDirDispatchWitnesses are the six invalid spellings the round-2 dispatch
// probe drove: four non-clean absolute paths, present-empty, and relative.
var tempDirDispatchWitnesses = []struct {
	name string
	raw  string
}{
	{name: "dot-dot-traversal", raw: "/x/../escape"},
	{name: "duplicate-separator", raw: "/x//run"},
	{name: "dot-element", raw: "/x/./run"},
	{name: "trailing-slash", raw: "/x/run/"},
	{name: "present-empty", raw: ""},
	{name: "relative", raw: "relative/path"},
}

// tempDirPrePlanSurfaces are the BuildPlan plugin dispatches the shape gate
// precedes.
var tempDirPrePlanSurfaces = []string{"ValidateCuratorContext", "ValidateContextDescriptors", "PrepareLaunchRequest"}

// validTempDirDispatchContext is a Curator fragment that passes the pure
// module checks for a codex_cli launch, so the plugin Curator validator is
// the surface that would run for it.
func validTempDirDispatchContext(home string) *agentic.CuratorContext {
	return &agentic.CuratorContext{
		Revision:    agentic.CuratorLaunchFragmentV1,
		Environment: "codex_cli",
		Profile:     agentic.CuratorProfilePin{Name: "managed-profile", LockSHA256: strings.Repeat("a", 64)},
		Precedence:  agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
		Env:         map[string]string{"CODEX_HOME": home},
	}
}

// tempDirDispatchRequest builds a fully launchable request — stub binary,
// prompt file, valid descriptor and valid Curator fragment — differing only
// by the TempDir under test.
func tempDirDispatchRequest(t *testing.T, tempDir *string) agentic.LaunchRequest {
	t.Helper()
	workDir := tempSlot(t)
	home := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	req := parityRequest(workDir)
	req.TempDir = tempDir
	req.Home = home
	req.Context = validTempDirDispatchContext(home)
	req.ContextDescriptors = []agentic.ContextDescriptor{{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: "valid context"}}}
	req.PromptPath = writePromptFile(t, workDir, "dispatch-ordered temp dir prompt")
	req.Env = []string{"PATH=" + binDir}
	return req
}

// TestCodexTempDirRefusesBeforePluginDispatch is the round-3
// tempdir-after-plugin-dispatch regression through BuildPlan: for each of the
// six invalid spellings, the typed refusal fires with zero calls on every
// pre-plan plugin surface. The absent-field control proves the same request
// shape dispatches all three surfaces when the gate has nothing to refuse,
// so the zero counts are the gate's doing, not an undispatched fixture.
func TestCodexTempDirRefusesBeforePluginDispatch(t *testing.T) {
	t.Parallel()
	for _, witness := range tempDirDispatchWitnesses {
		t.Run(witness.name, func(t *testing.T) {
			t.Parallel()
			raw := witness.raw
			spy := &tempDirDispatchSpy{System: New(), calls: map[string]int{}}
			registry := agentic.NewRegistry()
			if err := registry.Register(spy); err != nil {
				t.Fatalf("Register: %v", err)
			}
			if _, err := agentic.BuildPlan(registry, tempDirDispatchRequest(t, &raw), agentic.LaunchModeExec); !errors.Is(err, agentic.ErrTempDirInvalid) {
				t.Fatalf("BuildPlan with TempDir %q err = %v, want ErrTempDirInvalid", raw, err)
			}
			for _, surface := range tempDirPrePlanSurfaces {
				if calls := spy.calls[surface]; calls != 0 {
					t.Fatalf("invalid TempDir reached plugin dispatch: %s ran %d time(s), want 0", surface, calls)
				}
			}
		})
	}
	t.Run("absent-dispatches", func(t *testing.T) {
		t.Parallel()
		spy := &tempDirDispatchSpy{System: New(), calls: map[string]int{}}
		registry := agentic.NewRegistry()
		if err := registry.Register(spy); err != nil {
			t.Fatalf("Register: %v", err)
		}
		plan, err := agentic.BuildPlan(registry, tempDirDispatchRequest(t, nil), agentic.LaunchModeExec)
		if err != nil {
			t.Fatalf("absent TempDir refused a dispatchable request: %v", err)
		}
		if plan.Binary == "" {
			t.Fatal("absent TempDir built a plan with no binary")
		}
		for _, surface := range tempDirPrePlanSurfaces {
			if calls := spy.calls[surface]; calls != 1 {
				t.Fatalf("absent TempDir dispatched %s %d time(s), want exactly 1; the zero-call legs above would be vacuous", surface, calls)
			}
		}
	})
}

// TestCodexTempDirRefusalPrecedesPluginError pins the typed refusal ahead of
// every plugin failure it could be masked by: a descriptor the plugin would
// refuse, a Curator fragment the plugin would refuse, and a failing preparer
// all lose to ErrTempDirInvalid when the TempDir is invalid. Each control
// proves its fixture genuinely triggers the plugin error it names.
func TestCodexTempDirRefusalPrecedesPluginError(t *testing.T) {
	t.Parallel()
	invalid := "/x/../escape"
	emptyDescriptor := []agentic.ContextDescriptor{{Kind: agentic.ContextSystemPrompt, SystemPrompt: &agentic.SystemPromptContext{Text: ""}}}
	t.Run("descriptor-error", func(t *testing.T) {
		t.Parallel()
		req := tempDirDispatchRequest(t, tempDirPtr(invalid))
		req.ContextDescriptors = emptyDescriptor
		registry := agentic.NewRegistry()
		if err := registry.Register(New()); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if _, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrTempDirInvalid) {
			t.Fatalf("TempDir refusal masked by the descriptor error: %v", err)
		}
	})
	t.Run("descriptor-error-control", func(t *testing.T) {
		t.Parallel()
		req := tempDirDispatchRequest(t, nil)
		req.ContextDescriptors = emptyDescriptor
		registry := agentic.NewRegistry()
		if err := registry.Register(New()); err != nil {
			t.Fatalf("Register: %v", err)
		}
		err := func() error {
			_, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
			return err
		}()
		if err == nil || errors.Is(err, agentic.ErrTempDirInvalid) {
			t.Fatalf("empty descriptor control err = %v, want the plugin descriptor error; the masking leg above would be vacuous", err)
		}
	})
	t.Run("curator-error", func(t *testing.T) {
		t.Parallel()
		home := tempSlot(t)
		req := tempDirDispatchRequest(t, tempDirPtr(invalid))
		req.Home = home
		// A pi fragment passes the pure module checks — pi is a supported
		// typed environment — and the Codex plugin refuses it for the
		// non-codex_cli environment. The TempDir gate precedes both.
		req.Context = &agentic.CuratorContext{
			Revision:    agentic.CuratorLaunchFragmentV1,
			Environment: "pi",
			Profile:     agentic.CuratorProfilePin{Name: "managed-profile", LockSHA256: strings.Repeat("a", 64)},
			Precedence:  agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
			Env:         map[string]string{"PI_CODING_AGENT_DIR": home},
		}
		registry := agentic.NewRegistry()
		if err := registry.Register(New()); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if _, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrTempDirInvalid) {
			t.Fatalf("TempDir refusal masked by the Curator error: %v", err)
		}
	})
	t.Run("curator-error-control", func(t *testing.T) {
		t.Parallel()
		home := tempSlot(t)
		req := tempDirDispatchRequest(t, nil)
		req.Home = home
		req.Context = &agentic.CuratorContext{
			Revision:    agentic.CuratorLaunchFragmentV1,
			Environment: "pi",
			Profile:     agentic.CuratorProfilePin{Name: "managed-profile", LockSHA256: strings.Repeat("a", 64)},
			Precedence:  agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
			Env:         map[string]string{"PI_CODING_AGENT_DIR": home},
		}
		registry := agentic.NewRegistry()
		if err := registry.Register(New()); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if _, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
			t.Fatalf("pi fragment control err = %v, want the plugin Curator error; the masking leg above would be vacuous", err)
		}
	})
	t.Run("preparer-error", func(t *testing.T) {
		t.Parallel()
		spy := &tempDirDispatchSpy{System: New(), calls: map[string]int{}, prepareErr: errors.New("spy preparation refused")}
		registry := agentic.NewRegistry()
		if err := registry.Register(spy); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if _, err := agentic.BuildPlan(registry, tempDirDispatchRequest(t, tempDirPtr(invalid)), agentic.LaunchModeExec); !errors.Is(err, agentic.ErrTempDirInvalid) {
			t.Fatalf("TempDir refusal masked by the preparer error: %v", err)
		}
		if calls := spy.calls["PrepareLaunchRequest"]; calls != 0 {
			t.Fatalf("invalid TempDir reached plugin dispatch: PrepareLaunchRequest ran %d time(s), want 0", calls)
		}
	})
}

// TestCodexTempDirRefusesNonCleanPaths is the round-1 F1 regression through
// BuildPlan: a non-clean absolute spelling — a ".." traversal, a duplicate
// separator, a "." element, a trailing slash — refuses typed with
// ErrTempDirInvalid instead of being admitted or silently cleaned. The
// witnesses are built by string concatenation, never filepath.Join, because
// Join cleans the spelling the gate must see.
func TestCodexTempDirRefusesNonCleanPaths(t *testing.T) {
	t.Parallel()
	sep := string(os.PathSeparator)
	base := tempSlot(t)
	cases := []struct {
		name    string
		tempDir string
	}{
		{name: "dot-dot-traversal", tempDir: base + sep + "run" + sep + ".." + sep + "escape"},
		{name: "duplicate-separator", tempDir: base + sep + sep + "run"},
		{name: "dot-element", tempDir: base + sep + "." + sep + "run"},
		{name: "trailing-slash", tempDir: base + sep + "run" + sep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workDir := tempSlot(t)
			binDir := tempSlot(t)
			writeStubExecutable(t, binDir, executableName)
			req := parityRequest(workDir)
			req.TempDir = tempDirPtr(tc.tempDir)
			req.PromptPath = writePromptFile(t, workDir, "refused temp dir prompt")
			req.Env = []string{"PATH=" + binDir}
			registry := agentic.NewRegistry()
			if err := registry.Register(New()); err != nil {
				t.Fatalf("Register: %v", err)
			}
			if _, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrTempDirInvalid) {
				t.Fatalf("non-clean TempDir admitted: %q err = %v, want ErrTempDirInvalid", tc.tempDir, err)
			}
		})
	}
}

// TestCodexTempDirPresentEmptyRefusesTyped pins the presence contract
// through BuildPlan: nil states absence and builds, while a present but
// empty value refuses typed instead of collapsing onto absence.
func TestCodexTempDirPresentEmptyRefusesTyped(t *testing.T) {
	t.Parallel()
	workDir := tempSlot(t)
	binDir := tempSlot(t)
	writeStubExecutable(t, binDir, executableName)
	build := func(tempDir *string) error {
		req := parityRequest(workDir)
		req.TempDir = tempDir
		req.PromptPath = writePromptFile(t, workDir, "presence temp dir prompt")
		req.Env = []string{"PATH=" + binDir}
		registry := agentic.NewRegistry()
		if err := registry.Register(New()); err != nil {
			t.Fatalf("Register: %v", err)
		}
		_, err := agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
		return err
	}
	if err := build(nil); err != nil {
		t.Fatalf("absent TempDir refused a plan it must leave alone: %v", err)
	}
	if err := build(tempDirPtr("")); !errors.Is(err, agentic.ErrTempDirInvalid) {
		t.Fatalf("present-empty TempDir admitted: err = %v, want ErrTempDirInvalid", err)
	}
}

// TestCodexChildEnvRefusesRelativeTempDirDirectly pins the second gate: a
// caller holding the plugin directly meets the same typed refusal
// BuildPlan reports, rather than a TMPDIR the module would never seal.
func TestCodexChildEnvRefusesRelativeTempDirDirectly(t *testing.T) {
	t.Parallel()
	req := parityRequest(t.TempDir())
	req.TempDir = tempDirPtr("relative/path")
	if _, err := New().ChildEnv(nil, req); !errors.Is(err, agentic.ErrTempDirInvalid) {
		t.Fatalf("ChildEnv with relative TempDir err = %v, want ErrTempDirInvalid", err)
	}
	req.TempDir = tempDirPtr("   ")
	if _, err := New().ChildEnv(nil, req); !errors.Is(err, agentic.ErrTempDirInvalid) {
		t.Fatalf("ChildEnv with blank TempDir err = %v, want ErrTempDirInvalid", err)
	}
	req.TempDir = tempDirPtr("")
	if _, err := New().ChildEnv(nil, req); !errors.Is(err, agentic.ErrTempDirInvalid) {
		t.Fatalf("ChildEnv with present-empty TempDir err = %v, want ErrTempDirInvalid", err)
	}
	nonClean := tempSlot(t) + string(os.PathSeparator) + "run" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "escape"
	req.TempDir = tempDirPtr(nonClean)
	if _, err := New().ChildEnv(nil, req); !errors.Is(err, agentic.ErrTempDirInvalid) {
		t.Fatalf("ChildEnv with non-clean TempDir err = %v, want ErrTempDirInvalid", err)
	}
}

// mutateTMPDIR rewrites the plan's TMPDIR entries through one mutation:
// change the value, remove every entry, or append a duplicate.
func mutateTMPDIR(plan agentic.Plan, mutate func(entries []string) []string) agentic.Plan {
	changed := plan
	changed.Env = append([]string(nil), plan.Env...)
	var kept []string
	var tmpdirs []string
	for _, entry := range changed.Env {
		if name, _, ok := strings.Cut(entry, "="); ok && name == TempDirEnv {
			tmpdirs = append(tmpdirs, entry)
			continue
		}
		kept = append(kept, entry)
	}
	changed.Env = append(kept, mutate(tmpdirs)...)
	return changed
}

// TestCodexTempDirSealRefusesChangedOrDuplicated is AC2 through the
// production seal: BuildPlan seals the TMPDIR selection, and a changed,
// removed or duplicated TMPDIR after sealing refuses typed.
func TestCodexTempDirSealRefusesChangedOrDuplicated(t *testing.T) {
	t.Parallel()
	sealed := filepath.Join(t.TempDir(), "codex-tempdir-sealed")
	drift := filepath.Join(t.TempDir(), tempDirDriftWitnessSuffix)
	plan := tempDirPlan(t, tempDirPtr(sealed), nil)
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("sealed plan refused itself: %v", err)
	}
	t.Run("changed-value", func(t *testing.T) {
		t.Parallel()
		changed := mutateTMPDIR(plan, func([]string) []string { return []string{TempDirEnv + "=" + drift} })
		if err := changed.VerifyBeforeExec(); !errors.Is(err, ErrCodexTempDirChanged) {
			t.Fatalf("changed TMPDIR admitted: %v", err)
		}
	})
	t.Run("removed", func(t *testing.T) {
		t.Parallel()
		changed := mutateTMPDIR(plan, func([]string) []string { return nil })
		if err := changed.VerifyBeforeExec(); !errors.Is(err, ErrCodexTempDirChanged) {
			t.Fatalf("removed TMPDIR admitted: %v", err)
		}
	})
	t.Run("duplicate-same", func(t *testing.T) {
		t.Parallel()
		changed := mutateTMPDIR(plan, func(entries []string) []string {
			return append(append([]string(nil), entries...), TempDirEnv+"="+sealed)
		})
		if err := changed.VerifyBeforeExec(); !errors.Is(err, ErrCodexTempDirChanged) {
			t.Fatalf("duplicate TMPDIR admitted: %v", err)
		}
	})
	t.Run("duplicate-differing", func(t *testing.T) {
		t.Parallel()
		changed := mutateTMPDIR(plan, func(entries []string) []string {
			return append(append([]string(nil), entries...), TempDirEnv+"="+drift)
		})
		if err := changed.VerifyBeforeExec(); !errors.Is(err, ErrCodexTempDirChanged) {
			t.Fatalf("duplicate TMPDIR admitted: %v", err)
		}
	})
}

// TestCodexTempDirSealBindsNothingWhenAbsent pins the unbound half of the
// seal contract: a plan sealed with no TMPDIR entry verifies exactly as
// before, and a later TMPDIR appearance trips no temp-dir refusal.
func TestCodexTempDirSealBindsNothingWhenAbsent(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, catalogRequest(t, home, workDir, "low"))
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("sealed catalog plan refused itself: %v", err)
	}
	changed := plan
	changed.Env = append(append([]string(nil), plan.Env...), TempDirEnv+"=/appeared-after-seal")
	if err := changed.VerifyBeforeExec(); err != nil {
		t.Fatalf("TMPDIR appearance on an unbound seal refused: %v", err)
	}
}

// TestCodexTempDirSealBindsInheritedSelection pins the boundary the seal
// documents: exactly one TMPDIR entry at seal time is a selection whether
// the module mapped it or the parent carried it, so mutating an inherited
// selection refuses the same way.
func TestCodexTempDirSealBindsInheritedSelection(t *testing.T) {
	t.Parallel()
	plan := tempDirPlan(t, nil, []string{TempDirEnv + "=/inherited/tmp"})
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("sealed plan refused itself: %v", err)
	}
	changed := mutateTMPDIR(plan, func([]string) []string { return []string{TempDirEnv + "=/mutated/tmp"} })
	if err := changed.VerifyBeforeExec(); !errors.Is(err, ErrCodexTempDirChanged) {
		t.Fatalf("changed inherited TMPDIR admitted: %v", err)
	}
}

// TestCodexCatalogAndTempDirSealBindBothHalves pins the composed seal: a
// local-provider plan with TempDir binds the catalog digest and the TMPDIR
// selection together, and each half refuses with its own typed refusal.
func TestCodexCatalogAndTempDirSealBindBothHalves(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	dir := t.TempDir()
	req := catalogRequest(t, home, workDir, "low")
	req.TempDir = tempDirPtr(dir)
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("sealed plan refused itself: %v", err)
	}
	changed := mutateTMPDIR(plan, func([]string) []string {
		return []string{TempDirEnv + "=" + filepath.Join(t.TempDir(), "elsewhere")}
	})
	if err := changed.VerifyBeforeExec(); !errors.Is(err, ErrCodexTempDirChanged) {
		t.Fatalf("changed TMPDIR on a catalog plan admitted: %v", err)
	}
	moved := plan
	moved.Binary += "-changed"
	if err := moved.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("changed binary on a catalog plan admitted: %v", err)
	}
}

// TestCodexTempDirSealExportImportRoundTrip proves the TempDir-only seal
// crosses the export/import boundary: the guard carries a commitment, never
// the literal directory, and the imported verifier binds the same
// selection.
func TestCodexTempDirSealExportImportRoundTrip(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "codex-tempdir-canary")
	plan := tempDirPlan(t, tempDirPtr(dir), nil)
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	if seal.Data.Kind != agentic.SealKindSealed || seal.Data.Sealed == nil {
		t.Fatalf("data kind = %q, want sealed with a payload", seal.Data.Kind)
	}
	if len(seal.Data.Sealed.Artifacts) != 0 {
		t.Fatalf("artifacts = %+v, want none for a TempDir-only seal", seal.Data.Sealed.Artifacts)
	}
	commitment, ok := seal.Data.Sealed.Selectors[TempDirEnv]
	if !ok || !strings.HasPrefix(commitment, "hmac-sha256:") {
		t.Fatalf("TMPDIR selector = %q, want a keyed commitment", commitment)
	}
	wire, err := json.Marshal(seal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), dir) {
		t.Fatal("exported guard contains the literal temp dir")
	}
	verifier, err := agentic.ImportSeal(New(), seal)
	if err != nil {
		t.Fatalf("ImportSeal: %v", err)
	}
	if err := verifier.VerifyBeforeExec(plan); err != nil {
		t.Fatalf("imported verifier refused the untouched process: %v", err)
	}
	changed := mutateTMPDIR(plan, func([]string) []string {
		return []string{TempDirEnv + "=" + filepath.Join(t.TempDir(), "elsewhere")}
	})
	if err := verifier.VerifyBeforeExec(changed); !errors.Is(err, ErrCodexTempDirChanged) {
		t.Fatalf("imported verifier admitted a changed TMPDIR: %v", err)
	}
	duplicated := mutateTMPDIR(plan, func(entries []string) []string {
		return append(append([]string(nil), entries...), TempDirEnv+"="+dir)
	})
	if err := verifier.VerifyBeforeExec(duplicated); !errors.Is(err, ErrCodexTempDirChanged) {
		t.Fatalf("imported verifier admitted a duplicated TMPDIR: %v", err)
	}
}

// TestCodexTempDirImportRefusesMalformedSelector drives the plugin importer
// directly: a TempDir-only shape without a well-formed commitment, and a
// catalog shape carrying a literal instead of a commitment, both refuse
// malformed with the existing sanitized shape.
func TestCodexTempDirImportRefusesMalformedSelector(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := tempDirPlan(t, tempDirPtr(dir), nil)
	seal, err := plan.ExportSeal()
	if err != nil {
		t.Fatalf("ExportSeal: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*agentic.SealedData)
	}{
		{name: "no-selector", mutate: func(data *agentic.SealedData) { data.Selectors = nil }},
		{name: "literal-instead-of-commitment", mutate: func(data *agentic.SealedData) {
			data.Selectors[TempDirEnv] = dir
		}},
		{name: "empty-commitment", mutate: func(data *agentic.SealedData) {
			data.Selectors[TempDirEnv] = ""
		}},
		{name: "wrong-prefix", mutate: func(data *agentic.SealedData) {
			data.Selectors[TempDirEnv] = "sha256:abc"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := *seal.Data.Sealed
			data.Selectors = map[string]string{}
			for name, value := range seal.Data.Sealed.Selectors {
				data.Selectors[name] = value
			}
			tc.mutate(&data)
			if _, err := New().ImportSealedData(data); !errors.Is(err, agentic.ErrLocalProviderMalformed) {
				t.Fatalf("ImportSealedData err = %v, want ErrLocalProviderMalformed", err)
			}
		})
	}
	t.Run("catalog-with-literal-selector", func(t *testing.T) {
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
		catalogPlan, err := buildCodexPlan(t, catalogRequest(t, home, workDir, "low"))
		if err != nil {
			t.Fatal(err)
		}
		exported, err := catalogPlan.ExportSeal()
		if err != nil {
			t.Fatalf("ExportSeal: %v", err)
		}
		data := *exported.Data.Sealed
		data.Selectors = map[string]string{TempDirEnv: dir}
		if _, err := New().ImportSealedData(data); !errors.Is(err, agentic.ErrLocalProviderMalformed) {
			t.Fatalf("ImportSealedData err = %v, want ErrLocalProviderMalformed", err)
		}
	})
}

// TestCodexTempDirFinalizedPlanBindsTmpdir proves finalization carries the
// binding: the final plan verifies, and a TMPDIR change after finalization
// refuses as a changed final process.
func TestCodexTempDirFinalizedPlanBindsTmpdir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := tempDirPlan(t, tempDirPtr(dir), nil)
	final, err := agentic.FinalizePlan(plan, agentic.FinalizeOverlays{
		FragmentEnv: []string{"SEAL_CODEX_TMPDIR_A=alpha"},
		NativeTail:  []string{"--final-tail"},
	}, nil)
	if err != nil {
		t.Fatalf("FinalizePlan: %v", err)
	}
	if err := final.VerifyBeforeExec(); err != nil {
		t.Fatalf("finalized plan refused itself: %v", err)
	}
	changed := mutateTMPDIR(final, func([]string) []string {
		return []string{TempDirEnv + "=" + filepath.Join(t.TempDir(), "elsewhere")}
	})
	if err := changed.VerifyBeforeExec(); !errors.Is(err, agentic.ErrFinalizedProcessChanged) {
		t.Fatalf("changed TMPDIR after finalization admitted: %v", err)
	}
}

// TestCodexTempDirCreatesNoDirectory is AC5: the module carries the host's
// path to the child and creates nothing. A plan over a nonexistent
// absolute directory builds fine and the directory stays absent.
func TestCodexTempDirCreatesNoDirectory(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist-xyz")
	plan := tempDirPlan(t, tempDirPtr(missing), nil)
	requireSingleTMPDIR(t, plan, missing)
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("TempDir %q exists after planning (stat err = %v); the host creates the directory, never the module", missing, err)
	}
}
