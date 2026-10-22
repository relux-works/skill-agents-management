package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// settingsRequest builds a local-provider launch carrying the given settings,
// through the production BuildPlan entry point.
func settingsRequest(t *testing.T, home, workDir string, settings []agentic.LocalProviderSetting) agentic.LaunchRequest {
	t.Helper()
	req := catalogRequest(t, home, workDir, "low")
	req.LocalProvider.Settings = settings
	return req
}

func requireLocalSettingsRefusal(t *testing.T, err error, kind agentic.LocalProviderRefusalKind, want error, key, value string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a typed %s refusal for key %q, got a plan", kind, key)
	}
	if !errors.Is(err, want) {
		t.Fatalf("error %v does not match %v", err, want)
	}
	var refusal *agentic.LocalProviderRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("error %v is not a LocalProviderRefusal", err)
	}
	if refusal.Kind != kind {
		t.Fatalf("refusal kind = %q, want %q (%v)", refusal.Kind, kind, err)
	}
	// The key and value never enter the error: a value may carry a secret,
	// and the typed kind already classifies the refusal.
	if key != "" && strings.Contains(err.Error(), key) {
		t.Fatalf("refusal %q leaks the setting key", err)
	}
	if value != "" && strings.Contains(err.Error(), value) {
		t.Fatalf("refusal %q leaks the setting value", err)
	}
}

// catalogArtifactSnapshot captures the process-wide launch-catalog root and
// the bytes of every entry in it, as SHA-256 hex digests. The refusal tests
// are sequential, so no other test materialises while they run and a
// before/after diff is deterministic. Names alone would miss an overwrite of
// an existing entry, so the bytes are compared too.
func catalogArtifactSnapshot(t *testing.T) (string, map[string]string) {
	t.Helper()
	launchCatalogs.Lock()
	defer launchCatalogs.Unlock()
	if launchCatalogs.root == "" {
		return "", nil
	}
	entries, err := os.ReadDir(launchCatalogs.root)
	if err != nil {
		t.Fatalf("read launch catalog root: %v", err)
	}
	snapshot := make(map[string]string, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(launchCatalogs.root, entry.Name()))
		if err != nil {
			t.Fatalf("read launch catalog entry: %v", err)
		}
		sum := sha256.Sum256(data)
		snapshot[entry.Name()] = hex.EncodeToString(sum[:])
	}
	return launchCatalogs.root, snapshot
}

// TestCarriedSettingsReachArgvAndVerify is AC1's positive: an admitted key
// reaches the local-provider argv before the seal, on the ID-only and the
// snapshot paths, and the sealed plan verifies.
func TestCarriedSettingsReachArgvAndVerify(t *testing.T) {
	settings := []agentic.LocalProviderSetting{
		{Key: "show_raw_agent_reasoning", Value: "true"},
		{Key: "hide_agent_reasoning", Value: "false"},
	}
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			home, workDir := t.TempDir(), t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			req := settingsRequest(t, home, workDir, settings)
			snapshot := mustSnapshot(t, req)
			for _, bound := range []struct {
				name string
				req  agentic.LaunchRequest
			}{
				{"id", req},
				{"snapshot", withCarriedSettings(withSnapshot(req, snapshot), settings)},
			} {
				t.Run(bound.name, func(t *testing.T) {
					plan, err := buildCodexPlanForMode(t, bound.req, mode)
					if err != nil {
						t.Fatalf("BuildPlan(local with carried settings, %s): %v", mode, err)
					}
					if !containsPair(plan.Argv, "-c", "hide_agent_reasoning=false") {
						t.Fatalf("argv %q does not carry hide_agent_reasoning", plan.Argv)
					}
					if !containsPair(plan.Argv, "-c", "show_raw_agent_reasoning=true") {
						t.Fatalf("argv %q does not carry show_raw_agent_reasoning", plan.Argv)
					}
					// Emission is sorted by key, so operator order never
					// affects the sealed bytes.
					if pairIndex(plan.Argv, "hide_agent_reasoning=false") > pairIndex(plan.Argv, "show_raw_agent_reasoning=true") {
						t.Fatalf("argv %q does not emit carried settings sorted by key", plan.Argv)
					}
					if err := plan.VerifyBeforeExec(); err != nil {
						t.Fatalf("VerifyBeforeExec with carried settings: %v", err)
					}
				})
			}
		})
	}
}

// TestCarriedSettingsEmptyCarriesNothing pins the len==0 fast path: an
// explicitly empty slice plans exactly like a nil one.
func TestCarriedSettingsEmptyCarriesNothing(t *testing.T) {
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	bare := catalogRequest(t, home, workDir, "low")
	empty := settingsRequest(t, home, workDir, []agentic.LocalProviderSetting{})
	barePlan, err := buildCodexPlan(t, bare)
	if err != nil {
		t.Fatalf("BuildPlan(bare): %v", err)
	}
	emptyPlan, err := buildCodexPlan(t, empty)
	if err != nil {
		t.Fatalf("BuildPlan(empty settings): %v", err)
	}
	if !reflect.DeepEqual(emptyPlan.Argv, barePlan.Argv) {
		t.Fatalf("empty-settings argv = %q, want the bare argv %q", emptyPlan.Argv, barePlan.Argv)
	}
}

// carriedSettingsRefusalRow is one refused settings shape: the policy class
// or malformed shape, the settings that carry it, and the typed refusal the
// pre-plan boundary must return before any file is read or materialised.
type carriedSettingsRefusalRow struct {
	name     string
	settings []agentic.LocalProviderSetting
	kind     agentic.LocalProviderRefusalKind
	want     error
}

// carriedSettingsRefusalRows is the single refusal table both ordering
// witnesses drive: one row per refused class plus the unknown key and the
// malformed shapes.
func carriedSettingsRefusalRows() []carriedSettingsRefusalRow {
	return []carriedSettingsRefusalRow{
		{name: "credential", settings: []agentic.LocalProviderSetting{{Key: "api_key", Value: "test-only-not-a-credential"}}, kind: agentic.LocalProviderUnsupported, want: agentic.ErrLocalProviderUnsupported},
		{name: "provider", settings: []agentic.LocalProviderSetting{{Key: "model_provider", Value: "local-other"}}, kind: agentic.LocalProviderUnsupported, want: agentic.ErrLocalProviderUnsupported},
		// The unowned model row carries a boolean value so the closed allow-list
		// is the only gate it can meet: under the admit-model narrowing
		// mutant this exact row is admitted instead of refused, and under
		// the defer-model-to-argv ordering mutant it falls through to
		// binary resolution instead of refusing at the pre-plan boundary.
		{name: "unowned-model", settings: []agentic.LocalProviderSetting{{Key: "model_reasoning_summary", Value: "true"}}, kind: agentic.LocalProviderUnsupported, want: agentic.ErrLocalProviderUnsupported},
		{name: "sandbox", settings: []agentic.LocalProviderSetting{{Key: "sandbox_mode", Value: "workspace-write"}}, kind: agentic.LocalProviderUnsupported, want: agentic.ErrLocalProviderUnsupported},
		{name: "approval", settings: []agentic.LocalProviderSetting{{Key: "approval_policy", Value: "never"}}, kind: agentic.LocalProviderUnsupported, want: agentic.ErrLocalProviderUnsupported},
		{name: "network", settings: []agentic.LocalProviderSetting{{Key: "sandbox_workspace_write.network_access", Value: "true"}}, kind: agentic.LocalProviderUnsupported, want: agentic.ErrLocalProviderUnsupported},
		{name: "tool", settings: []agentic.LocalProviderSetting{{Key: "mcp_servers.extra-tools.command", Value: "extra-tools-command"}}, kind: agentic.LocalProviderUnsupported, want: agentic.ErrLocalProviderUnsupported},
		{name: "unknown", settings: []agentic.LocalProviderSetting{{Key: "not_a_codex_key", Value: "true"}}, kind: agentic.LocalProviderUnsupported, want: agentic.ErrLocalProviderUnsupported},
		{name: "empty-key", settings: []agentic.LocalProviderSetting{{Key: "", Value: "true"}}, kind: agentic.LocalProviderMalformed, want: agentic.ErrLocalProviderMalformed},
		{name: "spaced-key", settings: []agentic.LocalProviderSetting{{Key: " hide_agent_reasoning", Value: "true"}}, kind: agentic.LocalProviderMalformed, want: agentic.ErrLocalProviderMalformed},
		{name: "interior-space-key", settings: []agentic.LocalProviderSetting{{Key: "hide_agent reasoning", Value: "true"}}, kind: agentic.LocalProviderMalformed, want: agentic.ErrLocalProviderMalformed},
		{name: "control-char-key", settings: []agentic.LocalProviderSetting{{Key: "hide_agent_reasoning\x07", Value: "true"}}, kind: agentic.LocalProviderMalformed, want: agentic.ErrLocalProviderMalformed},
		{name: "non-bool-value", settings: []agentic.LocalProviderSetting{{Key: "hide_agent_reasoning", Value: "yes"}}, kind: agentic.LocalProviderMalformed, want: agentic.ErrLocalProviderMalformed},
		{name: "capitalised-bool-value", settings: []agentic.LocalProviderSetting{{Key: "hide_agent_reasoning", Value: "True"}}, kind: agentic.LocalProviderMalformed, want: agentic.ErrLocalProviderMalformed},
		{
			name:     "duplicate-key",
			settings: []agentic.LocalProviderSetting{{Key: "hide_agent_reasoning", Value: "true"}, {Key: "hide_agent_reasoning", Value: "true"}},
			kind:     agentic.LocalProviderConflicting,
			want:     agentic.ErrLocalProviderConflicting,
		},
	}
}

// TestCarriedSettingsRefuseBeforeAnyFileIsMaterialised is AC2's negative
// table: one row per refused class plus the unknown key and the malformed
// shapes. Missing home errors must not mask the typed refusal, and the
// before/after catalog byte snapshot checks materialisation. This does not
// observe reads; TestCarriedSettingsRefuseBeforeReads records them.
func TestCarriedSettingsRefuseBeforeAnyFileIsMaterialised(t *testing.T) {
	for _, tt := range carriedSettingsRefusalRows() {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			home := filepath.Join(t.TempDir(), "no-such-home")
			beforeRoot, beforeEntries := catalogArtifactSnapshot(t)
			_, err := buildCodexPlan(t, settingsRequest(t, home, workDir, tt.settings))
			requireLocalSettingsRefusal(t, err, tt.kind, tt.want, tt.settings[0].Key, tt.settings[0].Value)
			if afterRoot, afterEntries := catalogArtifactSnapshot(t); afterRoot != beforeRoot || !reflect.DeepEqual(afterEntries, beforeEntries) {
				t.Fatalf("refused settings changed a catalog artifact: root %q -> %q, entries %q -> %q", beforeRoot, afterRoot, beforeEntries, afterEntries)
			}
		})
	}
}

// withoutBinaryOnPath returns the request with its PATH pointed at an empty
// directory holding no codex binary, so binary resolution observably fails.
// Any managed package root is dropped too, so resolution cannot escape the
// empty PATH through another layout.
func withoutBinaryOnPath(t *testing.T, req agentic.LaunchRequest) agentic.LaunchRequest {
	t.Helper()
	empty := t.TempDir()
	env := make([]string, 0, len(req.Env))
	replaced := false
	for _, item := range req.Env {
		key, _, found := strings.Cut(item, "=")
		if !found {
			env = append(env, item)
			continue
		}
		switch key {
		case "PATH":
			env = append(env, "PATH="+empty)
			replaced = true
		case managedPackageRootEnv:
			continue
		default:
			env = append(env, item)
		}
	}
	if !replaced {
		t.Fatalf("request carries no PATH to empty")
	}
	req.Env = env
	return req
}

// TestCarriedSettingsRefuseBeforeBinaryResolution is the regression for
// review finding settings-refusal-after-binary-resolution: with no codex
// binary on PATH and a home that does not exist, every refused class still
// refuses typed. The pre-plan boundary runs before binary resolution and
// before every home/catalog read, so an unavailable binary or a missing home
// never masks the policy refusal. This observes error precedence only; an
// ignored-result read would be invisible here. TestCarriedSettingsRefuseBeforeReads
// records attempted reads independently of their results. This ports the reviewer's
// TestReviewRefusalBeforeBinaryResolution intent into the real suite,
// extended to every refusal row, in exec and dry-run.
func TestCarriedSettingsRefuseBeforeBinaryResolution(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			for _, tt := range carriedSettingsRefusalRows() {
				t.Run(tt.name, func(t *testing.T) {
					workDir := t.TempDir()
					home := filepath.Join(t.TempDir(), "no-such-home")
					req := withoutBinaryOnPath(t, settingsRequest(t, home, workDir, tt.settings))
					beforeRoot, beforeEntries := catalogArtifactSnapshot(t)
					_, err := buildCodexPlanForMode(t, req, mode)
					if err != nil && strings.Contains(err.Error(), "could not resolve its binary") {
						t.Fatalf("settings refusal lost to binary resolution (ordering): %v", err)
					}
					requireLocalSettingsRefusal(t, err, tt.kind, tt.want, tt.settings[0].Key, tt.settings[0].Value)
					if afterRoot, afterEntries := catalogArtifactSnapshot(t); afterRoot != beforeRoot || !reflect.DeepEqual(afterEntries, beforeEntries) {
						t.Fatalf("refused settings changed a catalog artifact: root %q -> %q, entries %q -> %q", beforeRoot, afterRoot, beforeEntries, afterEntries)
					}
				})
			}
		})
	}
}

// TestCarriedSettingsRefuseOnAnOtherwiseValidHome proves the refusal is the
// settings policy, not a missing home: the config is valid and the plan
// would succeed without the refused key. It is the narrowing-mutant witness:
// under the legacy admit-model selector this unowned key is admitted and the test fails.
func TestCarriedSettingsRefuseOnAnOtherwiseValidHome(t *testing.T) {
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	bare, err := buildCodexPlan(t, catalogRequest(t, home, workDir, "low"))
	if err != nil {
		t.Fatalf("BuildPlan(bare): %v", err)
	}
	if err := bare.VerifyBeforeExec(); err != nil {
		t.Fatalf("VerifyBeforeExec(bare): %v", err)
	}
	_, err = buildCodexPlan(t, settingsRequest(t, home, workDir, []agentic.LocalProviderSetting{{Key: "model_reasoning_summary", Value: "true"}}))
	requireLocalSettingsRefusal(t, err, agentic.LocalProviderUnsupported, agentic.ErrLocalProviderUnsupported, "model_reasoning_summary", "true")
}

// TestCarriedSettingsSealStillRefusesChangedArgv is AC3: the carried settings
// ride the sealed argv, so the untouched plan verifies and any post-seal argv
// change — a flipped value or an appended override — is refused as
// conflicting.
func TestCarriedSettingsSealStillRefusesChangedArgv(t *testing.T) {
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	plan, err := buildCodexPlan(t, settingsRequest(t, home, workDir, []agentic.LocalProviderSetting{{Key: "hide_agent_reasoning", Value: "true"}}))
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if err := plan.VerifyBeforeExec(); err != nil {
		t.Fatalf("VerifyBeforeExec with carried settings: %v", err)
	}
	flipped := plan
	flipped.Argv = append([]string(nil), plan.Argv...)
	at := pairIndex(flipped.Argv, "hide_agent_reasoning=true")
	if at < 0 {
		t.Fatalf("argv %q does not carry the setting", plan.Argv)
	}
	flipped.Argv[at] = "hide_agent_reasoning=false"
	var refusal *agentic.LocalProviderRefusal
	if err := flipped.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("flipped setting verified: %v", err)
	} else if !errors.As(err, &refusal) || refusal.Kind != agentic.LocalProviderConflicting {
		t.Fatalf("flipped setting refused without the conflicting kind: %v", err)
	}
	extended := plan
	extended.Argv = append(append([]string(nil), plan.Argv...), "-c", "show_raw_agent_reasoning=true")
	if err := extended.VerifyBeforeExec(); !errors.Is(err, agentic.ErrLocalProviderConflicting) {
		t.Fatalf("appended setting verified: %v", err)
	} else if !errors.As(err, &refusal) || refusal.Kind != agentic.LocalProviderConflicting {
		t.Fatalf("appended setting refused without the conflicting kind: %v", err)
	}
}

// TestAllowedLocalProviderSettingsAreDetachedAndExact pins the closed
// allow-list: exactly the two cosmetic keys, and a copy the caller cannot
// use to admit more.
func TestAllowedLocalProviderSettingsAreDetachedAndExact(t *testing.T) {
	t.Parallel()
	want := []string{"hide_agent_reasoning", "show_raw_agent_reasoning"}
	if got := AllowedLocalProviderSettings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedLocalProviderSettings() = %q, want %q", got, want)
	}
	mutated := AllowedLocalProviderSettings()
	mutated[0] = "model"
	if got := AllowedLocalProviderSettings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("caller mutated the allow-list: %q", got)
	}
	for _, key := range []string{"", "model", "model_provider", "model_catalog_json", "api_key", "env_key", "sandbox_mode", "approval_policy", "sandbox_workspace_write.network_access", "mcp_servers", "not_a_codex_key", " hide_agent_reasoning", "HIDE_AGENT_REASONING"} {
		if AllowedLocalProviderSettings()[0] == key || AllowedLocalProviderSettings()[1] == key {
			t.Errorf("unadmitted key %q is allow-listed", key)
		}
	}
}

// TestCarriedSettingsAdmitMutantKilledByName runs every narrowing mutant:
// each weakens one gate for one member only, and its named witness must
// fail. The control (no mutant) must pass for every witness.
func TestCarriedSettingsAdmitMutantKilledByName(t *testing.T) {
	mutants := []struct {
		mutant  string
		witness string
	}{
		// The allow-list gate: with an unowned model key admitted, the valid-home
		// witness plans instead of refusing.
		{mutant: "admit-model", witness: "TestCarriedSettingsRefuseOnAnOtherwiseValidHome"},
		// The shape gate: with the spaced key past the shape check, the
		// allow-list refuses it instead, so the materialisation row fails
		// on the kind change.
		{mutant: "admit-spaced-key", witness: "TestCarriedSettingsRefuseBeforeAnyFileIsMaterialised"},
		// The value grammar: with True admitted, the capitalised row is
		// carried instead of refused.
		{mutant: "admit-capitalized-bool", witness: "TestCarriedSettingsRefuseBeforeAnyFileIsMaterialised"},
		// The duplicate gate: with the second entry carried, the
		// duplicate row plans instead of conflicting.
		{mutant: "admit-duplicate", witness: "TestCarriedSettingsRefuseBeforeAnyFileIsMaterialised"},
		// The pre-plan ordering: with an unowned model key deferred to Argv, the
		// unavailable-binary witness meets the binary error instead of
		// the typed refusal — while every other refused class still
		// refuses at the pre-plan boundary.
		{mutant: "defer-model-to-argv", witness: "TestCarriedSettingsRefuseBeforeBinaryResolution"},
	}
	for _, m := range mutants {
		t.Run(m.mutant, func(t *testing.T) {
			runSettingsMutantChild(t, "", m.witness, true)
			runSettingsMutantChild(t, m.mutant, m.witness, false)
		})
	}
}

// runSettingsMutantChild runs one witness in a child go test process under
// the providersettingsmutant tag. A control run must pass; a mutant run
// must fail its named witness with exit 1 and no panic or build failure.
func runSettingsMutantChild(t *testing.T, mutant, witness string, control bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "-tags", "providersettingsmutant", "-count=1", "-run", "^"+witness+"$", ".")
	cmd.Env = append(os.Environ(), "CODEX_PROVIDER_SETTINGS_MUTANT="+mutant)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("mutant timed out: %s", out)
	}
	if control {
		if err != nil {
			t.Fatalf("control: %v\n%s", err, out)
		}
		return
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "--- FAIL: "+witness) || strings.Contains(string(out), "panic:") || strings.Contains(string(out), "[build failed]") {
		t.Fatalf("mutant did not fail its named witness: %v\n%s", err, out)
	}
	t.Logf("mutant %s killed by %s; child exit 1\n%s", mutant, witness, out)
}

// withCarriedSettings re-attaches settings after withSnapshot rebuilds the
// binding without them.
func withCarriedSettings(req agentic.LaunchRequest, settings []agentic.LocalProviderSetting) agentic.LaunchRequest {
	req.LocalProvider.Settings = settings
	return req
}

// pairIndex returns the index of the value token following a "-c" flag, or -1.
func pairIndex(argv []string, value string) int {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "-c" && argv[i+1] == value {
			return i + 1
		}
	}
	return -1
}
