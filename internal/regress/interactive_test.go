package regress

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/codex"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/pinative"
)

// CLASS 6 — INTERACTIVE. An interactive plan (curator-spec Decision 0013 §5)
// is what a launcher hands to a human's terminal, and the failure it must
// never ship is a headless or unrestricted launch wearing the interactive
// mode's name: `-p`, `exec`, a permission bypass, an output format, a budget,
// a goal directive or a composition prefix reaching a terminal session. Each
// plugin's own interactive_test.go pins its exact argv; what this file covers
// is the cross-cutting shape — every system that declares the mode, one
// sweep, one list of markers — so a core change that lets a marker through
// for all of them is diagnosed once.
//
// The systems that do NOT declare the mode are not silently skipped: the
// registry refuses them with ErrUnsupportedLaunchMode, and that refusal is
// asserted below so a declaration added without a construction is noticed.
//
// The sweep covers module-spelled argv only: no row here sets NativeArgs,
// and the caller's verbatim suffix may carry caller-chosen markers —
// that is the caller's spelling under Decision 0018 item 4 (raw bypass
// stays available untracked), not a marker this module emitted.
//
// pi is the third mapped system and is NOT here, for a reason this package
// already enforces: importing its plugin registers it into the default
// registry, and TestEveryLayerOneSystemHasASmokeCase then demands a golden pi
// cannot have (its Process-A surface was never captured by the source). Its
// interactive negative lives in pkg/agentic/systems/pi/interactive_test.go
// with the same marker discipline; this file covers the two systems whose
// registration this net already carries.

// execModeMarkers is the union of the decision's list and each mapped
// system's own headless grammar.
var execModeMarkers = []string{
	"-p", "--print", "--output-format", "--dangerously-skip-permissions",
	"--append-system-prompt-file", "--max-budget-usd", "--mcp-config",
	"exec", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check",
	"--sandbox", "--ask-for-approval", "-C", "--add-dir", "-",
	"spawn", "--prompt", "--deadline", "--result-schema",
}

func markersOn(argv []string) []string {
	var found []string
	for _, arg := range argv {
		if strings.HasPrefix(arg, "/goal ") {
			found = append(found, arg)
			continue
		}
		for _, marker := range execModeMarkers {
			if arg == marker {
				found = append(found, arg)
			}
		}
	}
	return found
}

// interactiveCase is one mapped system and the two requests it needs: the
// interactive one, and the exec one whose argv the sweep must fire on.
type interactiveCase struct {
	system agentic.System
	stub   string
	// exec adapts the interactive request into an admissible exec one. It is
	// NIL for a system that declares no exec mode at all (pi-native): there
	// the narrowing is the registry's refusal of exec, and the marker sweep is
	// proven live by the other cases in the same run.
	exec func(t *testing.T, req agentic.LaunchRequest, workDir string) agentic.LaunchRequest
	// vendor is the LaunchRequest.Vendor a system that qualifies its model
	// identity needs (pi-native); BuildLaunch sets it in production.
	vendor string
	// yoloFlag is the ONE bypass flag the system maps PermissionMode "yolo"
	// to (curator-spec Decision 0018), empty when the system refuses yolo
	// outright (pi-native: pi 0.84.2 documents no such flag).
	yoloFlag string
	// toolRelease is the pinned release the yolo mapping was verified
	// against (Decision 0018 choice 6). The yolo sweep below carries it;
	// the drift test carries anything but.
	toolRelease string
}

var interactiveCases = map[string]interactiveCase{
	"claude-code": {
		system: claude.New(), stub: "claude",
		yoloFlag:    "--dangerously-skip-permissions",
		toolRelease: "2.1.261",
		exec: func(t *testing.T, req agentic.LaunchRequest, workDir string) agentic.LaunchRequest {
			req.PromptPath = paritycase.WritePromptFile(t, workDir, "body")
			return req
		},
	},
	"codex": {
		system: codex.New(), stub: "codex",
		yoloFlag:    "--dangerously-bypass-approvals-and-sandbox",
		toolRelease: "0.153.2",
		exec: func(_ *testing.T, req agentic.LaunchRequest, workDir string) agentic.LaunchRequest {
			req.Prompt = []byte("body")
			return req
		},
	},
	"pi-native": {
		system: pinative.New(), stub: "pi", vendor: "anthropic",
		toolRelease: "0.84.2",
	},
}

func interactiveRequest(t *testing.T, c interactiveCase, workDir, binDir string) agentic.LaunchRequest {
	t.Helper()
	paritycase.WriteStubExecutable(t, binDir, c.stub)
	req := agentic.LaunchRequest{
		System:  c.system.ID(),
		Model:   agentic.Model{ID: "model-under-test", Effort: agentic.EffortSupportRequired},
		Effort:  "high",
		WorkDir: workDir,
		Env:     []string{"PATH=" + binDir},
	}
	if c.system.Capabilities().EffortTransport == agentic.EffortTransportNone {
		req.Model.Effort = agentic.EffortSupportNone
		req.Effort = ""
	}
	req.Vendor = c.vendor
	return req
}

func TestAnInteractivePlanCarriesNoExecMarkerForAnyMappedSystem(t *testing.T) {
	for name, c := range interactiveCases {
		t.Run(name, func(t *testing.T) {
			workDir, binDir := paritycase.TempSlot(t), paritycase.TempSlot(t)
			req := interactiveRequest(t, c, workDir, binDir)
			plan := paritycase.BuildPlan(t, c.system, req, agentic.LaunchModeInteractive)
			if found := markersOn(plan.Argv); len(found) != 0 {
				t.Errorf("%s: the interactive argv %v carries exec-mode marker(s) %v", name, plan.Argv, found)
			}
			if plan.Stdin.Attached {
				t.Errorf("%s: the interactive plan attached a stdin under effort transport %s", name, c.system.Capabilities().EffortTransport)
			}
			if len(plan.Argv) < 2 {
				t.Errorf("%s: the interactive argv %v is too short to select a model", name, plan.Argv)
			}

			// The narrowing: the same sweep on the same system's exec plan
			// must fire, or its silence above measured nothing. A system with
			// no exec mode has no such plan; its narrowing is that the
			// registry refuses exec outright, and the sweep's liveness is
			// carried by the cases that do have one.
			if c.exec == nil {
				if _, err := paritycase.TryBuildPlan(c.system, req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrUnsupportedLaunchMode) {
					t.Fatalf("%s: declares no exec mode and BuildPlan(exec) = %v, want ErrUnsupportedLaunchMode", name, err)
				}
				return
			}
			execReq := c.exec(t, req, workDir)
			execPlan := paritycase.BuildPlan(t, c.system, execReq, agentic.LaunchModeExec)
			if found := markersOn(execPlan.Argv); len(found) < 2 {
				t.Fatalf("%s: the sweep saw %v on the exec argv %v; it has to see that system's headless grammar there", name, found, execPlan.Argv)
			}
		})
	}
}

// TestAYoloPlanCarriesItsOneBypassFlagAndNoOtherMarker is the Decision 0018
// half of the sweep above: native interactive plans carry no bypass flag (that
// test), and yolo interactive plans carry exactly one — the system's own
// mapping, once — and no other exec-mode marker. A system with no mapping
// (pi-native) refuses yolo outright instead of launching native under a yolo
// name.
func TestAYoloPlanCarriesItsOneBypassFlagAndNoOtherMarker(t *testing.T) {
	for name, c := range interactiveCases {
		t.Run(name, func(t *testing.T) {
			workDir, binDir := paritycase.TempSlot(t), paritycase.TempSlot(t)
			req := interactiveRequest(t, c, workDir, binDir)
			req.PermissionMode = agentic.PermissionModeYolo
			req.ToolRelease = c.toolRelease

			if c.yoloFlag == "" {
				if _, err := paritycase.TryBuildPlan(c.system, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnsupported) {
					t.Fatalf("%s: err = %v, want ErrPermissionModeUnsupported", name, err)
				}
				return
			}
			plan := paritycase.BuildPlan(t, c.system, req, agentic.LaunchModeInteractive)
			count := 0
			var other []string
			for _, arg := range plan.Argv {
				if arg == c.yoloFlag {
					count++
					continue
				}
				other = append(other, markersOn([]string{arg})...)
			}
			if count != 1 {
				t.Errorf("%s: the yolo argv %v carries the bypass flag %d time(s), want exactly once", name, plan.Argv, count)
			}
			if len(other) != 0 {
				t.Errorf("%s: the yolo argv %v carries other exec-mode marker(s) %v", name, plan.Argv, other)
			}
			if plan.Stdin.Attached {
				t.Errorf("%s: the yolo plan attached a stdin under effort transport %s", name, c.system.Capabilities().EffortTransport)
			}
		})
	}
}

// TestAYoloPlanRefusesDriftForEveryMappedSystem is the Decision 0018
// choice-6 half of the sweep above: yolo at an unverified release — a
// newer one, or none established at all — fails closed with the drift
// sentinel for every mapped system, while the same request at native
// builds. Pi-native's verified release stays unsupported (the sweep
// above); drift there refuses as unverified like everywhere else.
func TestAYoloPlanRefusesDriftForEveryMappedSystem(t *testing.T) {
	for name, c := range interactiveCases {
		for _, release := range []string{"9.9.9", ""} {
			t.Run(name+"/release-"+driftName(release), func(t *testing.T) {
				workDir, binDir := paritycase.TempSlot(t), paritycase.TempSlot(t)
				req := interactiveRequest(t, c, workDir, binDir)
				req.PermissionMode = agentic.PermissionModeYolo
				req.ToolRelease = release
				if _, err := paritycase.TryBuildPlan(c.system, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrPermissionModeUnverifiedRelease) {
					t.Fatalf("%s: err = %v, want ErrPermissionModeUnverifiedRelease", name, err)
				}
			})
		}
		t.Run(name+"/native-builds-unverified", func(t *testing.T) {
			workDir, binDir := paritycase.TempSlot(t), paritycase.TempSlot(t)
			req := interactiveRequest(t, c, workDir, binDir)
			req.PermissionMode = agentic.PermissionModeNative
			req.ToolRelease = "9.9.9"
			if _, err := paritycase.TryBuildPlan(c.system, req, agentic.LaunchModeInteractive); err != nil {
				t.Fatalf("%s: native at an unverified release was refused: %v", name, err)
			}
		})
	}
}

func driftName(release string) string {
	if release == "" {
		return "unestablished"
	}
	return release
}

// TestAnInteractivePlanRefusesACompositionForEveryMappedSystem is the
// decision's sentinel, once per system, through the real registry: whatever
// grammar the system declares, the composer's prefix does not reach a terminal.
//
// Each system is driven with both halves, the prefix alone and the server
// list alone, so a gate narrowed to `Prefix && Servers` fails here per plugin.
func TestAnInteractivePlanRefusesACompositionForEveryMappedSystem(t *testing.T) {
	prefix := []string{"--x", "y"}
	servers := []agentic.CompositionServer{{Name: "x", Transport: "http"}}
	compositions := map[string]agentic.Composition{
		"prefix and servers": {Prefix: prefix, Servers: servers},
		"prefix only":        {Prefix: prefix},
		"servers only":       {Servers: servers},
	}
	for name, c := range interactiveCases {
		for shape, composition := range compositions {
			t.Run(name+"/"+shape, func(t *testing.T) {
				workDir, binDir := paritycase.TempSlot(t), paritycase.TempSlot(t)
				req := interactiveRequest(t, c, workDir, binDir)
				req.Composition = composition
				_, err := paritycase.TryBuildPlan(c.system, req, agentic.LaunchModeInteractive)
				if !errors.Is(err, agentic.ErrCompositionNotInteractive) {
					t.Fatalf("%s/%s: err = %v, want ErrCompositionNotInteractive", name, shape, err)
				}
			})
		}
	}
}

// TestAPlanRefusesAnEmptyModelForEveryMappedSystemInEveryMode is the core
// ErrModelMissing sentinel through the real registry: whatever the mode, a
// request naming no model never reaches a plugin's argv, where the exec
// grammar used to admit `--model ""`.
func TestAPlanRefusesAnEmptyModelForEveryMappedSystemInEveryMode(t *testing.T) {
	for name, c := range interactiveCases {
		for _, mode := range []agentic.LaunchMode{agentic.LaunchModeInteractive, agentic.LaunchModeExec} {
			if mode == agentic.LaunchModeExec && c.exec == nil {
				mode = agentic.LaunchModeDryRun
			}
			t.Run(name+"/"+mode.String(), func(t *testing.T) {
				workDir, binDir := paritycase.TempSlot(t), paritycase.TempSlot(t)
				req := interactiveRequest(t, c, workDir, binDir)
				if mode == agentic.LaunchModeExec {
					req = c.exec(t, req, workDir)
				}
				req.Model.ID = ""
				_, err := paritycase.TryBuildPlan(c.system, req, mode)
				if !errors.Is(err, agentic.ErrModelMissing) {
					t.Fatalf("%s/%s: err = %v, want ErrModelMissing", name, mode, err)
				}
			})
		}
	}
}

// TestSystemsWithoutTheModeRefuseIt keeps the undeclared set explicit: agy,
// gemini, muse and qwen were left out of the interactive change deliberately,
// and the registry's refusal is what stands in for a construction there. It
// ranges over the default registry, so a plugin that declares the mode without
// a case in this file — or a case here for a plugin that stopped declaring it
// — is reported. pi is not in this binary's default registry at all (the
// package deliberately never imports it, for the reason stated at the top), so
// it is neither ranged over nor exempted here.
func TestSystemsWithoutTheModeRefuseIt(t *testing.T) {
	declared := map[agentic.SystemID]bool{}
	for _, c := range interactiveCases {
		declared[c.system.ID()] = true
	}
	for _, id := range agentic.Default.IDs() {
		sys, ok := agentic.Default.Lookup(id)
		if !ok {
			t.Fatalf("Lookup(%q) failed for an id the registry listed", id)
		}
		if declared[id] {
			if !sys.Capabilities().SupportsMode(agentic.LaunchModeInteractive) {
				t.Errorf("%s has an interactive case here but does not declare the mode", id)
			}
			continue
		}
		if sys.Capabilities().SupportsMode(agentic.LaunchModeInteractive) {
			t.Errorf("%s declares LaunchModeInteractive but has no case in this file; a declared mode with no cross-cutting negative is a capability claim this net does not see", id)
		}
		req := agentic.LaunchRequest{System: id, Model: agentic.Model{ID: "m"}, WorkDir: filepath.Join(paritycase.TempSlot(t), "w")}
		if _, err := agentic.BuildPlan(agentic.Default, req, agentic.LaunchModeInteractive); !errors.Is(err, agentic.ErrUnsupportedLaunchMode) {
			t.Errorf("%s: err = %v, want ErrUnsupportedLaunchMode", id, err)
		}
	}
}
