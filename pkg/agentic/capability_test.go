package agentic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// This file is the core half of the versioned provider-capability table
// (curator-spec Decision 0018 choices 3 and 6): the NativeArgs scope the
// core owns, the release lookup every plugin reads, and the probe
// dispatcher the launcher calls. The per-release rows, the closed
// grammars, and the yolo/native argv are each plugin's to hold and each
// plugin's policy_test.go to prove.

// Native arguments outside LaunchModeInteractive are refused — in every
// non-interactive mode, before any plugin surface runs. Only the
// interactive grammar has a verbatim suffix to forward them through.
func TestBuildPlanRefusesNativeArgsOutsideInteractiveLaunches(t *testing.T) {
	modes := map[string]LaunchMode{
		"exec":            LaunchModeExec,
		"dry-run":         LaunchModeDryRun,
		"managed-session": LaunchModeManagedSession,
	}
	for name, mode := range modes {
		t.Run(name, func(t *testing.T) {
			sys := interactivePangolin()
			registry := registerPangolin(t, sys)
			req := pangolinRequest()
			req.NativeArgs = []string{"--native", "x"}

			_, err := BuildPlan(registry, req, mode)
			if !errors.Is(err, ErrNativeArgsNotInteractive) {
				t.Fatalf("err = %v, want ErrNativeArgsNotInteractive", err)
			}
			if sys.calls["Argv"] != 0 {
				t.Error("Argv was dispatched for a non-interactive request carrying native arguments")
			}
		})
	}

	t.Run("and no native arguments in those modes is admitted", func(t *testing.T) {
		// The narrowing: the refusal is the MEMBER's, not a new rule
		// against the modes. Every request written before the member
		// existed carries none and must keep building.
		for name, mode := range modes {
			sys := interactivePangolin()
			registry := registerPangolin(t, sys)
			if _, err := BuildPlan(registry, pangolinRequest(), mode); err != nil {
				t.Errorf("%s: the request without native arguments was refused: %v", name, err)
			}
		}
	})
}

// The tool release is an observation, not launch content: outside the
// interactive yolo path it is ignored, even when it names a release no
// table verifies. Ignoring it cannot misdirect a launch — no argv
// outside that path reads it — so there is no scope refusal for it,
// and this test pins the leniency rather than leaving it implied.
func TestBuildPlanIgnoresToolReleaseOutsideTheInteractiveYoloPath(t *testing.T) {
	for name, mode := range map[string]LaunchMode{
		"exec":            LaunchModeExec,
		"dry-run":         LaunchModeDryRun,
		"managed-session": LaunchModeManagedSession,
	} {
		t.Run(name, func(t *testing.T) {
			registry := registerPangolin(t, interactivePangolin())
			req := pangolinRequest()
			req.ToolRelease = "9.9.9-unverified"
			if _, err := BuildPlan(registry, req, mode); err != nil {
				t.Errorf("%s: a request carrying an unverified tool release was refused: %v", name, err)
			}
		})
	}
	t.Run("and an interactive native launch ignores it too", func(t *testing.T) {
		registry := registerPangolin(t, interactivePangolin())
		req := interactiveRequest()
		req.ToolRelease = "9.9.9-unverified"
		if _, err := BuildPlan(registry, req, LaunchModeInteractive); err != nil {
			t.Errorf("interactive native with an unverified tool release was refused: %v", err)
		}
	})
}

// The lookup is the single reader over the per-plugin rows: an exact
// hit returns its row, and an empty release, an unpinned or newer one,
// or a row naming an unimplemented grammar all refuse with the drift
// sentinel. The refusal names the grammar in force, so the launcher
// citing the token can see what would have to be re-verified.
func TestLookupReleaseCapabilityReadsOneRowPerRelease(t *testing.T) {
	rows := []ReleaseCapability{
		{Release: "2.1.261", Grammar: PermissionGrammarV1, YoloSupported: true},
		{Release: "0.84.2", Grammar: PermissionGrammarV1, YoloSupported: false},
	}
	t.Run("an exact hit returns its row", func(t *testing.T) {
		row, err := LookupReleaseCapability(rows, "2.1.261")
		if err != nil {
			t.Fatalf("LookupReleaseCapability: %v", err)
		}
		if row.Release != "2.1.261" || row.Grammar != PermissionGrammarV1 || !row.YoloSupported {
			t.Errorf("row = %+v, want the 2.1.261 row with grammar v1 and yolo supported", row)
		}
	})
	t.Run("surrounding whitespace trims, near-misses refuse", func(t *testing.T) {
		if _, err := LookupReleaseCapability(rows, "  2.1.261\t"); err != nil {
			t.Errorf("a trimmable release was refused: %v", err)
		}
		for _, release := range []string{"v2.1.261", "2.1.2610", "2.1.26", "2.1.274", "9.9.9"} {
			if _, err := LookupReleaseCapability(rows, release); !errors.Is(err, ErrPermissionModeUnverifiedRelease) {
				t.Errorf("release %q: err = %v, want ErrPermissionModeUnverifiedRelease", release, err)
			}
		}
	})
	t.Run("an empty release refuses as undetected, not as unknown", func(t *testing.T) {
		_, err := LookupReleaseCapability(rows, "  ")
		if !errors.Is(err, ErrPermissionModeUnverifiedRelease) {
			t.Fatalf("err = %v, want ErrPermissionModeUnverifiedRelease", err)
		}
		if !strings.Contains(err.Error(), "no tool release was established") {
			t.Errorf("refusal %q does not say the release was never established", err)
		}
	})
	t.Run("the refusal names the grammar and the verified releases", func(t *testing.T) {
		_, err := LookupReleaseCapability(rows, "2.1.274")
		if !errors.Is(err, ErrPermissionModeUnverifiedRelease) {
			t.Fatalf("err = %v, want ErrPermissionModeUnverifiedRelease", err)
		}
		for _, want := range []string{string(PermissionGrammarV1), "2.1.261", "0.84.2"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal %q does not name %q", err, want)
			}
		}
	})
	t.Run("a row naming an unimplemented grammar selects no mapping", func(t *testing.T) {
		// Helper-direct and disclosed as a bound: every production row
		// names PermissionGrammarV1, so no BuildPlan input reaches this
		// branch today. It is the fail-closed half of "rows naming a new
		// grammar ship with the code that implements it" — a v2 row
		// meeting v1 code refuses rather than classifying under a
		// grammar the scan does not know.
		future := []ReleaseCapability{{Release: "3.0.0", Grammar: "permission-grammar-v2", YoloSupported: true}}
		_, err := LookupReleaseCapability(future, "3.0.0")
		if !errors.Is(err, ErrPermissionModeUnverifiedRelease) {
			t.Fatalf("err = %v, want ErrPermissionModeUnverifiedRelease", err)
		}
		if !strings.Contains(err.Error(), "does not implement") {
			t.Errorf("refusal %q does not say the grammar is unimplemented", err)
		}
	})
}

// probingPangolin is the double with a release probe: the pangolin
// contract plus one scriptable answer.
type probingPangolin struct {
	*pangolinSystem
	release    string
	probeErr   error
	probeCalls int
	probeEnv   []string
}

func (p *probingPangolin) ProbeToolRelease(_ context.Context, env []string) (string, error) {
	p.probeCalls++
	p.probeEnv = append([]string(nil), env...)
	return p.release, p.probeErr
}

// The dispatcher calls the system's own probe with the handed
// environment, and answers a system with no probe — or no system —
// with the undetected sentinel. The probe's own error passes through
// unwrapped: the plugin already joined the sentinel to its cause.
func TestProbeToolReleaseDispatchesToTheSystem(t *testing.T) {
	t.Run("a system with a probe is called once with the handed env", func(t *testing.T) {
		sys := &probingPangolin{pangolinSystem: newPangolin(), release: "2.1.261"}
		env := []string{"PATH=/bin"}
		release, err := ProbeToolRelease(context.Background(), sys, env)
		if err != nil {
			t.Fatalf("ProbeToolRelease: %v", err)
		}
		if release != "2.1.261" {
			t.Errorf("release = %q, want %q", release, "2.1.261")
		}
		if sys.probeCalls != 1 {
			t.Errorf("the probe ran %d time(s), want exactly once", sys.probeCalls)
		}
		if len(sys.probeEnv) != 1 || sys.probeEnv[0] != "PATH=/bin" {
			t.Errorf("the probe saw env %q, want the handed environment", sys.probeEnv)
		}
	})
	t.Run("a probe failure passes through", func(t *testing.T) {
		boom := errors.New("boom")
		sys := &probingPangolin{pangolinSystem: newPangolin(), probeErr: boom}
		if _, err := ProbeToolRelease(context.Background(), sys, nil); !errors.Is(err, boom) {
			t.Errorf("err = %v, want the probe's own error", err)
		}
	})
	t.Run("a system with no probe is undetected, never synthesized", func(t *testing.T) {
		if _, err := ProbeToolRelease(context.Background(), newPangolin(), nil); !errors.Is(err, ErrToolReleaseUndetected) {
			t.Errorf("err = %v, want ErrToolReleaseUndetected", err)
		}
	})
	t.Run("a nil system is undetected, not a panic", func(t *testing.T) {
		if _, err := ProbeToolRelease(context.Background(), nil, nil); !errors.Is(err, ErrToolReleaseUndetected) {
			t.Errorf("err = %v, want ErrToolReleaseUndetected", err)
		}
	})
}
