package vendorplugin

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// tempDirVendorWitnesses are the six invalid spellings the round-2 vendor
// probe drove: four non-clean absolute paths, present-empty, and relative.
var tempDirVendorWitnesses = []struct {
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

func assertTempDirSpySilent(t *testing.T, sys *networkOrderSpySystem, vendor *narwhalVendor) {
	t.Helper()
	for surface, count := range sys.calls {
		if count != 0 {
			t.Errorf("%s ran %d time(s), want 0; the temp-dir refusal must come before every wrapper dispatch", surface, count)
		}
	}
	for surface, count := range vendor.calls {
		if count != 0 {
			t.Errorf("vendor %s ran %d time(s), want 0; the temp-dir refusal must come before every vendor dispatch", surface, count)
		}
	}
}

// TestBuildLaunchTempDirRefusesBeforeVendorDispatch is the round-3
// tempdir-after-plugin-dispatch regression through the wrapper entry point:
// for each of the six invalid spellings, the typed refusal fires with zero
// calls on every surface — vendor Spawn and Models, effort admission,
// request preparation and preflight. The witness legs use passing doubles
// on purpose: the gate-moved-back mutant must still refuse typed after the
// late dispatch, so the kill lands on the zero-call assertion rather than
// on a masked error. The refusal-precedes-plugin-errors leg then pins the
// typed refusal ahead of every spy failure at once with failing doubles.
// The absent-field control proves the same request shape dispatches the
// vendor when the gate has nothing to refuse, so the zero counts are the
// gate's doing, not an undispatched fixture. Only BuildLaunch is driven:
// BuildLaunchWithEnvironment shares the same buildLaunch path and gate.
func TestBuildLaunchTempDirRefusesBeforeVendorDispatch(t *testing.T) {
	for _, witness := range tempDirVendorWitnesses {
		t.Run(witness.name, func(t *testing.T) {
			sys := &networkOrderSpySystem{pangolinSystem: newPangolinSystem(), calls: map[string]int{}}
			vendor := newNarwhal()
			registry := registerNetworkOrderSpy(t, vendor, sys)
			raw := witness.raw
			req := narwhalRequest()
			req.TempDir = &raw
			plan, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeDryRun)
			if !errors.Is(err, agentic.ErrTempDirInvalid) {
				t.Fatalf("BuildLaunch with TempDir %q err = %v, want ErrTempDirInvalid", raw, err)
			}
			if !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("BuildLaunch plan = %#v, want zero; a refusal carries no plan", plan)
			}
			if calls := vendor.calls["Spawn"]; calls != 0 {
				t.Fatalf("invalid TempDir reached vendor Spawn: ran %d time(s), want 0", calls)
			}
			assertTempDirSpySilent(t, sys, vendor)
		})
	}
	t.Run("refusal-precedes-plugin-errors", func(t *testing.T) {
		sys, vendor := newFailingNetworkOrderSpy()
		registry := registerNetworkOrderSpy(t, vendor, sys)
		raw := "/x/../escape"
		req := narwhalRequest()
		req.TempDir = &raw
		plan, err := BuildLaunch(context.Background(), registry, req, agentic.LaunchModeDryRun)
		if !errors.Is(err, agentic.ErrTempDirInvalid) {
			t.Fatalf("TempDir refusal masked by a plugin error: %v", err)
		}
		if !reflect.DeepEqual(plan, agentic.Plan{}) {
			t.Fatalf("BuildLaunch plan = %#v, want zero; a refusal carries no plan", plan)
		}
		assertTempDirSpySilent(t, sys, vendor)
	})
	t.Run("absent-dispatches", func(t *testing.T) {
		sys := &networkOrderSpySystem{pangolinSystem: newPangolinSystem(), calls: map[string]int{}}
		vendor := newNarwhal()
		registry := registerNetworkOrderSpy(t, vendor, sys)
		plan, err := BuildLaunch(context.Background(), registry, narwhalRequest(), agentic.LaunchModeDryRun)
		if err != nil {
			t.Fatalf("absent TempDir refused a dispatchable request: %v", err)
		}
		if plan.Binary == "" {
			t.Fatal("absent TempDir built a plan with no binary")
		}
		if calls := vendor.calls["Spawn"]; calls != 1 {
			t.Fatalf("absent TempDir dispatched vendor Spawn %d time(s), want exactly 1; the zero-call legs above would be vacuous", calls)
		}
		if calls := sys.calls["AdmitEffort"]; calls != 1 {
			t.Fatalf("absent TempDir dispatched AdmitEffort %d time(s), want exactly 1; the zero-call legs above would be vacuous", calls)
		}
	})
}
