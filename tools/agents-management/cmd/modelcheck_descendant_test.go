package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Rev5 F2 proof: readiness-gated descendant-kill evidence.
//
// Round 4 showed the F2 witness could attest a group kill it never observed:
// the 300ms deadline started before process startup, so when the descendant
// had not yet inherited the stdout pipe at the kill instant, the mutant (group
// kill disabled) still returned before the threshold and survived. The fix is
// structural, not a longer sleep: the measured cancellation window starts only
// after positive evidence that the child and its descendant both started and
// hold stdout. Absent readiness is a test failure, never a pass.
//
// The descendant writes its marker after fork (stdout already inherited) and
// then execs into sleep, which preserves file descriptors; the foreground
// execs into sleep the same way. At the window start both markers exist, so
// both sleeps hold the pipe, and the window can only close through the group
// kill (fast) or WaitDelay (slow). The window close cancels the process
// context; exec's Cancel path — group kill plus WaitDelay — is identical for
// cancellation and for a true context deadline, only the surfaced error
// differs (context.Canceled here).
//
// BUG-261002-13ll6s: the same race flaked the G1 adapter and status-reader
// witnesses, which opened a caller deadline through runRoot before the
// descendant could inherit the pipe. Every deadline/descendant witness now
// shares this one proof: the caller prepares a fixture (script plus ready
// dir), adapts its production entry point to a launch closure, and the proof
// waits for readiness, opens the measured window, and times the close under
// the hard outer bound. Pi, the observation adapter and the status reader
// differ only in their launch closure and their production error shape.

const (
	// modelCheckDescendantReadyBound is the hard bound for the readiness
	// wait. Normal startup signals in milliseconds; expiry fails the test.
	modelCheckDescendantReadyBound = 10 * time.Second
	// modelCheckDescendantWindow is the measured cancellation window opened
	// after readiness is established.
	modelCheckDescendantWindow = 300 * time.Millisecond
	// modelCheckDescendantKillThreshold separates the group-kill close
	// (~window + epilogue) from the WaitDelay close (~window + 2s).
	modelCheckDescendantKillThreshold = 1500 * time.Millisecond
	// modelCheckDescendantOuterBound is the hard outer bound for one proof.
	modelCheckDescendantOuterBound = 30 * time.Second
	// modelCheckDescendantHoldSeconds bounds the worst case: every hold
	// shape sleeps this long, far past window + WaitDelay.
	modelCheckDescendantHoldSeconds = "10"
)

// modelCheckDescendantFamily is one descendant-hold topology over which the
// F2 invariant is proved: a script plus the readiness markers it must emit
// before the measured window may start.
type modelCheckDescendantFamily struct {
	name    string
	script  string
	markers []string
}

// modelCheckDescendantCatalog enumerates every hold shape the property test
// proves. The F2 witness runs the canonical family; the property test runs
// all of them, so a shape the witness never exercises cannot silently regress
// the invariant.
var modelCheckDescendantCatalog = []modelCheckDescendantFamily{
	{
		name: "background-plus-foreground",
		script: "#!/bin/sh\n" +
			"echo child-ready >> \"$MODELCHECK_READY_DIR/child\"\n" +
			"( echo descendant-ready >> \"$MODELCHECK_READY_DIR/descendant\"; exec /bin/sleep " + modelCheckDescendantHoldSeconds + " ) &\n" +
			"exec /bin/sleep " + modelCheckDescendantHoldSeconds + "\n",
		markers: []string{"child", "descendant"},
	},
	{
		name: "nested-grandchild",
		script: "#!/bin/sh\n" +
			"echo child-ready >> \"$MODELCHECK_READY_DIR/child\"\n" +
			"( ( echo descendant-ready >> \"$MODELCHECK_READY_DIR/descendant\"; exec /bin/sleep " + modelCheckDescendantHoldSeconds + " ) & exec /bin/sleep " + modelCheckDescendantHoldSeconds + " ) &\n" +
			"exec /bin/sleep " + modelCheckDescendantHoldSeconds + "\n",
		markers: []string{"child", "descendant"},
	},
	{
		name: "slow-start-descendant",
		script: "#!/bin/sh\n" +
			"echo child-ready >> \"$MODELCHECK_READY_DIR/child\"\n" +
			"( /bin/sleep 1; echo descendant-ready >> \"$MODELCHECK_READY_DIR/descendant\"; exec /bin/sleep " + modelCheckDescendantHoldSeconds + " ) &\n" +
			"exec /bin/sleep " + modelCheckDescendantHoldSeconds + "\n",
		markers: []string{"child", "descendant"},
	},
	{
		name: "fan-out",
		script: "#!/bin/sh\n" +
			"echo child-ready >> \"$MODELCHECK_READY_DIR/child\"\n" +
			"( echo descendant-ready >> \"$MODELCHECK_READY_DIR/descendant\"; exec /bin/sleep " + modelCheckDescendantHoldSeconds + " ) &\n" +
			"( exec /bin/sleep " + modelCheckDescendantHoldSeconds + " ) &\n" +
			"exec /bin/sleep " + modelCheckDescendantHoldSeconds + "\n",
		markers: []string{"child", "descendant"},
	},
}

// modelCheckDescendantFamily resolves one catalog family by name, failing the
// test on an unknown name so a renamed witness shape cannot silently leave
// the catalog.
func modelCheckDescendantFamilyByName(t *testing.T, name string) modelCheckDescendantFamily {
	t.Helper()
	for _, family := range modelCheckDescendantCatalog {
		if family.name == name {
			return family
		}
	}
	t.Fatalf("descendant family %q is not in the catalog (%d families)", name, len(modelCheckDescendantCatalog))
	return modelCheckDescendantFamily{}
}

// modelCheckDescendantFixture is the on-disk fixture one proof runs: an
// executable script plus the directory its readiness markers land in.
type modelCheckDescendantFixture struct {
	// Dir holds the script. Name-resolved entry points (the observation
	// adapter, the status reader) find the script by prepending Dir to
	// PATH; the script name is then the resolved binary name.
	Dir string
	// Script is the absolute script path for direct-exec entry points (Pi).
	Script string
	// ReadyDir receives the readiness markers. Entry points that inherit
	// their environment read it as MODELCHECK_READY_DIR; direct-exec
	// entry points pass it through their explicit environment.
	ReadyDir string
}

// prepareModelCheckDescendantFixture writes script as an executable name in a
// fresh directory with an empty ready dir. Publishing the ready dir to the
// subprocess environment stays with the caller, next to the launch closure,
// so each entry point's environment contract reads in one place.
func prepareModelCheckDescendantFixture(t *testing.T, name, script string) modelCheckDescendantFixture {
	t.Helper()
	directory := t.TempDir()
	scriptPath := filepath.Join(directory, name)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write proof script %s: %v", name, err)
	}
	readyDir := filepath.Join(directory, "ready")
	if err := os.Mkdir(readyDir, 0o755); err != nil {
		t.Fatalf("create ready dir: %v", err)
	}
	return modelCheckDescendantFixture{Dir: directory, Script: scriptPath, ReadyDir: readyDir}
}

// modelCheckDescendantProof is one readiness-gated cancellation measurement
// over a production subprocess entry point. ReadyErr is nil only when every
// marker was observed before the readiness bound; Elapsed measures window
// start to launch return and is valid only then; Err is the production error
// the entry point reported. A launch that never started its subprocess
// writes no markers, so absent readiness refuses without needing a
// start flag from entry points that report none.
type modelCheckDescendantProof struct {
	ReadyErr error
	Elapsed  time.Duration
	Err      error
}

// Success reports whether the proof observed a group-kill close through an
// entry point that attributes the window close to the caller context:
// readiness established, the launch stopped by the window close, well before
// WaitDelay would release Wait. The Pi runner and the observation adapter
// share this shape; the status reader wraps its killed read as
// ErrStatusReadFailed and asserts that production shape itself.
func (p modelCheckDescendantProof) Success() bool {
	return p.ReadyErr == nil &&
		errors.Is(p.Err, context.Canceled) &&
		p.Elapsed < modelCheckDescendantKillThreshold
}

// modelCheckPiDescendantLaunch adapts the production Pi runner to the shared
// proof: the fixture script becomes the Pi plan, with the ready dir passed
// through the plan's explicit environment.
func modelCheckPiDescendantLaunch(fixture modelCheckDescendantFixture) func(context.Context) error {
	return func(ctx context.Context) error {
		plan := agentic.Plan{
			Binary: fixture.Script,
			Env:    append(os.Environ(), "MODELCHECK_READY_DIR="+fixture.ReadyDir),
		}
		return runModelCheckProcess(ctx, plan).Err
	}
}

// proveModelCheckDescendantKill runs one readiness-gated proof: start launch
// under a cancellable context, wait for every marker with readyBound, then
// open the measured window (closed by timer) and wait for launch under the
// hard outer bound. It returns values instead of failing so negative shapes
// can assert non-success.
func proveModelCheckDescendantKill(t *testing.T, fixture modelCheckDescendantFixture, markers []string, readyBound, window time.Duration, launch func(context.Context) error) modelCheckDescendantProof {
	t.Helper()
	outer, outerCancel := context.WithTimeout(context.Background(), modelCheckDescendantOuterBound)
	defer outerCancel()
	procCtx, procCancel := context.WithCancel(outer)
	defer procCancel()

	results := make(chan error, 1)
	go func() {
		results <- launch(procCtx)
	}()

	if err := awaitModelCheckDescendantMarkers(fixture.ReadyDir, markers, readyBound); err != nil {
		procCancel()
		proof := modelCheckDescendantProof{ReadyErr: err}
		select {
		case proof.Err = <-results:
		case <-outer.Done():
			proof.Err = outer.Err()
		}
		return proof
	}

	windowStart := time.Now()
	timer := time.AfterFunc(window, procCancel)
	defer timer.Stop()
	select {
	case err := <-results:
		return modelCheckDescendantProof{Elapsed: time.Since(windowStart), Err: err}
	case <-outer.Done():
		return modelCheckDescendantProof{
			Elapsed: time.Since(windowStart),
			Err:     outer.Err(),
		}
	}
}

// awaitModelCheckDescendantMarkers waits for every named marker file to appear
// with non-blank content, returning an error when the bound expires first.
// Absent readiness refuses: the caller must fail, never report success.
func awaitModelCheckDescendantMarkers(readyDir string, markers []string, bound time.Duration) error {
	deadline := time.Now().Add(bound)
	for _, marker := range markers {
		for {
			content, err := os.ReadFile(filepath.Join(readyDir, marker))
			if err == nil && len(bytes.TrimSpace(content)) > 0 {
				break
			}
			if time.Now().After(deadline) {
				return errors.New("descendant readiness marker " + marker + " not observed within bound")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	return nil
}

// TestModelCheckDescendantKillProofHoldsAcrossHoldCatalog is the generated
// property test closing class modelcheck-f2-witness-false-negative: for every
// catalog family it proves (a) readiness is observed before the window, (b)
// the window closes through the group kill well before WaitDelay, and (c) the
// same family with a withheld marker refuses instead of reporting success.
// A new hold shape added to the catalog is proved automatically; a shape that
// cannot prove readiness fails here instead of attesting a kill it never saw.
func TestModelCheckDescendantKillProofHoldsAcrossHoldCatalog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill is unix-only; windows bounds the direct child plus WaitDelay")
	}
	if len(modelCheckDescendantCatalog) == 0 {
		t.Fatal("descendant catalog is empty: the F2 invariant has no proved shape")
	}
	seenCanonical := false
	for _, family := range modelCheckDescendantCatalog {
		if family.name == "" || family.script == "" || len(family.markers) == 0 {
			t.Fatalf("catalog entry %+v is incomplete: every family needs a name, a script and markers", family)
		}
		if family.name == "background-plus-foreground" {
			seenCanonical = true
		}
		t.Run(family.name, func(t *testing.T) {
			fixture := prepareModelCheckDescendantFixture(t, "pi", family.script)
			proof := proveModelCheckDescendantKill(t, fixture, family.markers, modelCheckDescendantReadyBound, modelCheckDescendantWindow, modelCheckPiDescendantLaunch(fixture))
			if proof.ReadyErr != nil {
				t.Fatalf("readiness not observed for %q: cannot attest the group kill (err=%v)", family.name, proof.Err)
			}
			if !proof.Success() {
				t.Fatalf("family %q did not close through the group kill: elapsed=%v err=%v", family.name, proof.Elapsed, proof.Err)
			}
			t.Logf("family %q: readiness observed, window closed in %v", family.name, proof.Elapsed)
		})
		// The paired negative is generated from the same family: withhold one
		// marker the script never writes, and the proof must refuse.
		withheld := append(append([]string(nil), family.markers...), "never-written")
		t.Run(family.name+"/withheld-marker-refuses", func(t *testing.T) {
			fixture := prepareModelCheckDescendantFixture(t, "pi", family.script)
			proof := proveModelCheckDescendantKill(t, fixture, withheld, 200*time.Millisecond, modelCheckDescendantWindow, modelCheckPiDescendantLaunch(fixture))
			if proof.ReadyErr == nil {
				t.Fatalf("family %q admitted a withheld marker as ready", family.name)
			}
			if proof.Success() {
				t.Fatalf("family %q with a withheld marker counted as a successful group-kill proof", family.name)
			}
		})
	}
	if !seenCanonical {
		t.Fatal("descendant catalog lost the canonical background-plus-foreground family the F2 witness runs")
	}
}

// TestModelCheckDescendantProofRefusesAbsentReadiness pins the negative
// direction of the F2 invariant through the same proof: a script that exits
// without signalling readiness, and a script that signals readiness without
// holding the pipe, must both fail Success. Either shape reporting success
// would be the round-4 false negative.
func TestModelCheckDescendantProofRefusesAbsentReadiness(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill is unix-only; windows bounds the direct child plus WaitDelay")
	}
	t.Run("no-markers-no-hold", func(t *testing.T) {
		fixture := prepareModelCheckDescendantFixture(t, "pi", "#!/bin/sh\nexit 0\n")
		proof := proveModelCheckDescendantKill(t, fixture,
			[]string{"child", "descendant"}, 200*time.Millisecond, modelCheckDescendantWindow, modelCheckPiDescendantLaunch(fixture))
		if proof.ReadyErr == nil {
			t.Fatal("a script that wrote no markers was admitted as ready")
		}
		if proof.Success() {
			t.Fatal("absent descendant readiness counted as a successful group-kill proof")
		}
	})
	t.Run("markers-without-hold", func(t *testing.T) {
		script := "#!/bin/sh\n" +
			"echo child-ready >> \"$MODELCHECK_READY_DIR/child\"\n" +
			"echo descendant-ready >> \"$MODELCHECK_READY_DIR/descendant\"\n" +
			"exit 0\n"
		fixture := prepareModelCheckDescendantFixture(t, "pi", script)
		proof := proveModelCheckDescendantKill(t, fixture,
			[]string{"child", "descendant"}, modelCheckDescendantReadyBound, modelCheckDescendantWindow, modelCheckPiDescendantLaunch(fixture))
		if proof.ReadyErr != nil {
			t.Fatalf("markers were written but readiness refused: %v", proof.ReadyErr)
		}
		if proof.Success() {
			t.Fatalf("an unheld pipe counted as a successful group-kill proof: elapsed=%v err=%v", proof.Elapsed, proof.Err)
		}
	})
}
