package muse

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/nativeargs"
	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const verifiedMuseRelease = "1.4.1"

// The Curator bridge remains the construction gate even for newly supported
// interactive plugins. Muse has no Curator channel mapping and must not silently
// discard a fragment for another environment.
func TestMuseInteractiveBuildPlanKeepsCuratorBridgeRefusal(t *testing.T) {
	for _, mode := range []agentic.PermissionMode{agentic.PermissionModeNative, agentic.PermissionModeYolo} {
		t.Run(string(mode), func(t *testing.T) {
			req := museInteractiveRequest(t)
			req.PermissionMode = mode
			req.ToolRelease = verifiedMuseRelease
			req.Home = "/managed/home"
			req.Context = &agentic.CuratorContext{
				Revision:    agentic.CuratorLaunchFragmentV1,
				Environment: "codex_cli",
				Profile:     agentic.CuratorProfilePin{Name: "managed-profile", LockSHA256: strings.Repeat("a", 64)},
				Precedence:  agentic.CuratorPrecedence{Winner: "higher-weight", Placement: "winner-last"},
				Env:         map[string]string{"CODEX_HOME": req.Home},
			}
			plan, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrCuratorContextUnsupported) {
				t.Fatalf("BuildPlan Curator context error = %v, want ErrCuratorContextUnsupported", err)
			}
			if !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("refused Curator context returned a launch plan: %#v", plan)
			}
		})
	}
}

func TestMuseInteractiveNativePassesNoPostureFlag(t *testing.T) {
	req := museInteractiveRequest(t)
	req.PermissionMode = agentic.PermissionModeNative
	// Native does not need a release row; the default posture forwards the
	// caller's settings without consulting the yolo policy.
	req.ToolRelease = ""

	plan := buildMuseInteractivePlan(t, New(), req)
	want := []string{
		"--model", req.Model.ID,
		"--reasoning-effort", req.Effort,
		"--workspace", req.WorkDir,
	}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Fatalf("BuildPlan interactive native argv = %#v, want %#v", plan.Argv, want)
	}
	if err := assertMuseNativePosture(plan); err != nil {
		t.Fatal(err)
	}
}

func TestMuseInteractiveYoloEmitsOneFlagAndForwardsNativeArgs(t *testing.T) {
	req := museInteractiveRequest(t)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = verifiedMuseRelease
	req.NativeArgs = []string{"--trust-workspace", "open the current project"}

	plan := buildMuseInteractivePlan(t, New(), req)
	want := []string{
		"--model", req.Model.ID,
		"--reasoning-effort", req.Effort,
		"--workspace", req.WorkDir,
		museYoloFlag,
		"--trust-workspace", "open the current project",
	}
	if !reflect.DeepEqual(plan.Argv, want) {
		t.Fatalf("BuildPlan interactive yolo argv = %#v, want %#v", plan.Argv, want)
	}
	if err := assertMuseYoloExactlyOnce(plan); err != nil {
		t.Fatal(err)
	}
}

func TestMuseInteractiveYoloRefusesAnUnlistedRelease(t *testing.T) {
	req := museInteractiveRequest(t)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "1.5.0"

	_, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
	if !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
		t.Fatalf("BuildPlan with unlisted Muse release error = %v, want ErrPermissionModeUnsupported", err)
	}
	if !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
		t.Fatalf("BuildPlan with unlisted Muse release error = %v, want the specific unverified-release classification too", err)
	}
}

func TestMuseInteractiveYoloRefusesDuplicateNativePostures(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "explicit yolo", args: []string{"--yolo"}},
		{name: "yolo equals form", args: []string{"--yolo=true"}},
		{name: "yolo false equals form", args: []string{"--yolo=false"}},
		{name: "equivalent pair", args: []string{"--disable-approval", "--disable-sandbox"}},
		{name: "equivalent pair reversed", args: []string{"--disable-sandbox", "--disable-approval"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := museInteractiveRequest(t)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = verifiedMuseRelease
			req.NativeArgs = c.args
			_, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive)
			if !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
				t.Fatalf("BuildPlan with duplicate native posture error = %v, want ErrPermissionModeDuplicate", err)
			}
		})
	}
}

func TestMuseInteractiveYoloTreatsPostSeparatorTextAsPrompt(t *testing.T) {
	req := museInteractiveRequest(t)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = verifiedMuseRelease
	req.NativeArgs = []string{"--", "--yolo", "--disable-approval", "--disable-sandbox"}

	plan := buildMuseInteractivePlan(t, New(), req)
	if got := activeMuseFlagCount(plan.Argv, museYoloFlag); got != 1 {
		t.Fatalf("active --yolo count in BuildPlan argv = %d, want 1: %#v", got, plan.Argv)
	}
	if !reflect.DeepEqual(plan.Argv[len(plan.Argv)-len(req.NativeArgs):], req.NativeArgs) {
		t.Fatalf("BuildPlan did not preserve post-separator prompt text: %#v", plan.Argv)
	}
}

func TestMuseInteractiveYoloAllowsOneHalfOfTheEquivalentPair(t *testing.T) {
	req := museInteractiveRequest(t)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = verifiedMuseRelease
	req.NativeArgs = []string{"--disable-sandbox"}

	plan := buildMuseInteractivePlan(t, New(), req)
	if err := assertMuseYoloExactlyOnce(plan); err != nil {
		t.Fatal(err)
	}
}

func TestMuseInteractiveKeepsExecBinaryEnvironmentAndStdinContracts(t *testing.T) {
	req := museInteractiveRequest(t)
	plan := buildMuseInteractivePlan(t, New(), req)
	if !strings.HasSuffix(plan.Binary, "/muse") {
		t.Errorf("interactive binary = %q, want Muse resolved from the request PATH", plan.Binary)
	}
	if plan.Stdin.Attached || len(plan.Stdin.Bytes) != 0 {
		t.Errorf("interactive stdin = %#v, want no attached stdin payload", plan.Stdin)
	}
	if got, ok := paritycase.Lookup(plan.Env, museNoAutoUpdateEnv); !ok || got != "1" {
		t.Errorf("interactive environment %s = %q (present=%v), want the exec-mode pin 1", museNoAutoUpdateEnv, got, ok)
	}
}

func TestMuseInteractiveNativeYoloNarrowingMutantIsDetected(t *testing.T) {
	// Keep the searched-for source token while adding an independent native
	// posture builder. The source gate must see the new site; the behavioral
	// half below then runs the corresponding mutant through BuildPlan.
	sourceMutant := map[string]string{
		"pkg/agentic/systems/muse/native-yolo-mutant.go": `package muse
const nativeMutantYoloFlag = "--yolo"
func appendNativeYolo(args []string) []string {
	return append(args, nativeMutantYoloFlag)
}`,
	}
	violations := scanMuseArgv(t, sourceMutant, map[string]string{})
	foundSourceMutant := false
	for _, violation := range violations {
		if violation.Name == "appendNativeYolo" {
			foundSourceMutant = true
		}
	}
	if !foundSourceMutant {
		t.Fatalf("source-text gate missed a token-preserving native-yolo builder: %v", violations)
	}

	req := museInteractiveRequest(t)
	req.PermissionMode = agentic.PermissionModeNative
	req.ToolRelease = ""

	clean := buildMuseInteractivePlan(t, New(), req)
	if err := assertMuseNativePosture(clean); err != nil {
		t.Fatalf("unmutated BuildPlan does not satisfy the native posture assertion: %v", err)
	}

	mutant := buildMuseInteractivePlan(t, nativeYoloMutant{System: New()}, req)
	if err := assertMuseNativePosture(mutant); err == nil {
		t.Fatal("narrowing mutant survived: BuildPlan's native posture check accepted a plugin that emits --yolo")
	}
}

func TestMuseInteractiveDuplicateYoloNarrowingMutantIsDetected(t *testing.T) {
	req := museInteractiveRequest(t)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = verifiedMuseRelease
	req.NativeArgs = []string{museYoloFlag}
	if _, err := tryBuildMusePlan(t, New(), req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeDuplicate) {
		t.Fatalf("unmutated duplicate gate error = %v, want ErrPermissionModeDuplicate", err)
	}

	mutant := buildMuseInteractivePlan(t, duplicateYoloMutant{System: New()}, req)
	if err := assertMuseYoloExactlyOnce(mutant); err == nil {
		t.Fatalf("narrowing mutant survived: BuildPlan accepted duplicate active --yolo flags in %#v", mutant.Argv)
	}
}

func TestMuseInteractiveUnlistedReleaseWideningMutantIsDetected(t *testing.T) {
	req := museInteractiveRequest(t)
	req.PermissionMode = agentic.PermissionModeYolo
	req.ToolRelease = "1.5.0"
	if err := requireMuseUnlistedReleaseRefusal(New(), req); err != nil {
		t.Fatalf("unmutated policy did not refuse the unlisted release: %v", err)
	}
	if err := requireMuseUnlistedReleaseRefusal(unlistedReleaseMutant{System: New()}, req); err == nil {
		t.Fatal("narrowing mutant survived: BuildPlan admitted yolo after the unlisted release was mapped onto the verified row")
	}
}

func TestMuseInteractivePolicyPinsOnlyTheVerifiedRelease(t *testing.T) {
	if len(verifiedReleases) != 2 {
		t.Fatalf("Muse permission policy has %d rows, want exactly the two pinned TUI releases", len(verifiedReleases))
	}
	for i, release := range []string{verifiedMuseRelease, "1.4.2"} {
		row := verifiedReleases[i]
		if row.Release != release || row.Grammar != agentic.PermissionGrammarV1 || !row.YoloSupported {
			t.Fatalf("Muse permission policy row = %#v, want release %q with grammar v1 and yolo supported", row, release)
		}
	}
}

func museInteractiveRequest(t *testing.T) agentic.LaunchRequest {
	t.Helper()
	req := launchRequest(t, "")
	req.Model.Effort = agentic.EffortSupportRequired
	req.Effort = "high"
	return req
}

func buildMuseInteractivePlan(t *testing.T, system agentic.System, req agentic.LaunchRequest) agentic.Plan {
	t.Helper()
	return buildMusePlan(t, system, req, agentic.LaunchModeInteractive)
}

func buildMusePlan(t *testing.T, system agentic.System, req agentic.LaunchRequest, mode agentic.LaunchMode) agentic.Plan {
	t.Helper()
	plan, err := tryBuildMusePlan(t, system, req, mode)
	if err != nil {
		t.Fatalf("agentic.BuildPlan(%s): %v", mode, err)
	}
	return plan
}

func tryBuildMusePlan(t *testing.T, system agentic.System, req agentic.LaunchRequest, mode agentic.LaunchMode) (agentic.Plan, error) {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(system); err != nil {
		t.Fatalf("Register Muse system: %v", err)
	}
	return agentic.BuildPlan(registry, req, mode)
}

func assertMuseNativePosture(plan agentic.Plan) error {
	for _, index := range nativeargs.FlagIndexes(plan.Argv) {
		name, _, _ := nativeargs.SplitFlagValue(plan.Argv[index])
		if name == museYoloFlag || name == museDisableApprovalFlag || name == museDisableSandboxFlag {
			return fmt.Errorf("native interactive argv carries posture flag %q: %#v", name, plan.Argv)
		}
	}
	return nil
}

func assertMuseYoloExactlyOnce(plan agentic.Plan) error {
	if got := activeMuseFlagCount(plan.Argv, museYoloFlag); got != 1 {
		return fmt.Errorf("interactive yolo argv carries %d active --yolo flags, want exactly 1: %#v", got, plan.Argv)
	}
	return nil
}

func activeMuseFlagCount(args []string, flag string) int {
	count := 0
	for _, index := range nativeargs.FlagIndexes(args) {
		name, _, _ := nativeargs.SplitFlagValue(args[index])
		if name == flag {
			count++
		}
	}
	return count
}

func requireMuseUnlistedReleaseRefusal(system agentic.System, req agentic.LaunchRequest) error {
	registry := agentic.NewRegistry()
	if err := registry.Register(system); err != nil {
		return fmt.Errorf("Register Muse system: %w", err)
	}
	_, err := agentic.BuildPlan(registry, req, agentic.LaunchModeInteractive)
	if err == nil {
		return errors.New("BuildPlan admitted yolo for an unlisted Muse release")
	}
	if !errors.Is(err, agentic.ErrPermissionModeUnsupported) || !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
		return fmt.Errorf("BuildPlan refused the unlisted release with the wrong classification: %w", err)
	}
	return nil
}

type nativeYoloMutant struct{ agentic.System }

func (m nativeYoloMutant) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	if mode == agentic.LaunchModeInteractive && req.PermissionMode != agentic.PermissionModeYolo {
		return append(args, museYoloFlag), nil
	}
	return args, nil
}

type duplicateYoloMutant struct{ agentic.System }

func (m duplicateYoloMutant) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if mode != agentic.LaunchModeInteractive || req.PermissionMode != agentic.PermissionModeYolo {
		return m.System.Argv(req, mode)
	}
	callerArgs := append([]string(nil), req.NativeArgs...)
	req.NativeArgs = nil
	args, err := m.System.Argv(req, mode)
	if err != nil {
		return nil, err
	}
	return append(args, callerArgs...), nil
}

type unlistedReleaseMutant struct{ agentic.System }

func (m unlistedReleaseMutant) Argv(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	if mode == agentic.LaunchModeInteractive && req.PermissionMode == agentic.PermissionModeYolo && req.ToolRelease == "1.5.0" {
		req.ToolRelease = verifiedMuseRelease
	}
	return m.System.Argv(req, mode)
}
