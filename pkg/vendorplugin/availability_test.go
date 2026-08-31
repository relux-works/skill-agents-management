package vendorplugin

import (
	"errors"
	"testing"
	"time"
)

// TestTheVerdictExpressesEveryStateWithoutAnInterfaceChange is AC4.
//
// All four answers — healthy, limited-until, unreachable, unknown — are built
// from the same struct with no type assertion, no second interface and no
// caller-side switch on a concrete type. That is what "without an interface
// break" has to mean: the local-model resource plane described in
// docs/architecture.md reports through these same fields when it arrives.
func TestTheVerdictExpressesEveryStateWithoutAnInterfaceChange(t *testing.T) {
	clears := time.Date(2026, 8, 21, 18, 30, 0, 0, time.UTC)

	verdicts := map[string]Availability{
		"healthy": Healthy("narwhal /v1/quota response headers"),
		"limited-until": LimitedUntil(clears, Observation{
			Source: "narwhal 429 response",
			Detail: "retry-after: 900",
			At:     clears.Add(-15 * time.Minute),
		}),
		"unreachable": Unreachable(
			[]Observation{{Source: "dial narwhal.example:443", Detail: "connection refused"}},
			nil,
		),
		"unknown after a check that found nothing": UnknownAfterCheck("narwhal /v1/quota response headers"),
		"unknown because nobody looked":            Unchecked(),
		"unknown because the read failed": {
			State:    AvailabilityUnknown,
			Checked:  []string{"~/.narwhal/limit-state.json"},
			Failures: []ReadFailure{{Source: "~/.narwhal/limit-state.json", Reason: "permission denied"}},
		},
	}
	for name, verdict := range verdicts {
		t.Run(name, func(t *testing.T) {
			if err := verdict.Validate(); err != nil {
				t.Fatalf("a legitimate %s verdict was refused: %v", name, err)
			}
		})
	}

	if !verdicts["healthy"].Serviceable() {
		t.Error("a healthy verdict is not serviceable")
	}
	for name, verdict := range verdicts {
		if name == "healthy" && verdict.Serviceable() {
			continue
		}
		if verdict.Serviceable() {
			t.Errorf("%s reported itself serviceable; only an observed healthy verdict may", name)
		}
	}
}

// The two unknowns are different facts and a caller acts on them differently:
// one says the sources were read and established nothing, the other says
// nobody read anything. A verdict type that could not tell them apart would
// make "we never checked" indistinguishable from "we checked and it was
// quiet" — the exact confusion the muse broker record exists to preserve.
func TestCheckedAndEmptyIsDistinguishableFromNeverChecked(t *testing.T) {
	checked := UnknownAfterCheck("narwhal /v1/quota response headers")
	never := Unchecked()

	if len(checked.Checked) == 0 {
		t.Error("a checked-and-empty verdict records no source, so it reads as never checked")
	}
	if len(never.Checked) != 0 {
		t.Error("a never-checked verdict claims to have read something")
	}
	if checked.State != never.State {
		t.Fatal("the two unknowns have different states; the distinction belongs in the evidence, not in a fifth state")
	}
}

// The zero value must be unknown, not healthy. A verdict nobody filled in has
// proved nothing, and a caller that read the zero value as "go ahead" would be
// launching on a struct literal.
func TestTheZeroVerdictIsUnknownAndNotServiceable(t *testing.T) {
	var zero Availability
	if zero.State != AvailabilityUnknown {
		t.Errorf("the zero verdict is %s; a struct nobody filled in must not read as an answer", zero.State)
	}
	if zero.Serviceable() {
		t.Error("the zero verdict reported itself serviceable")
	}
}

// Validate is a gate, so it needs the negatives. Each case below is a verdict
// whose state contradicts its own evidence, and each is a shape a plugin
// produces by accident: a health claim with nothing behind it, a limit with no
// clear time, an unknown carrying one anyway.
func TestValidateRefusesVerdictsThatContradictTheirEvidence(t *testing.T) {
	clears := time.Date(2026, 8, 21, 18, 30, 0, 0, time.UTC)

	cases := map[string]Availability{
		"healthy with nothing checked": {State: AvailabilityHealthy},
		"healthy while a source could not be read": {
			State:    AvailabilityHealthy,
			Checked:  []string{"~/.narwhal/limit-state.json"},
			Failures: []ReadFailure{{Source: "~/.narwhal/limit-state.json", Reason: "permission denied"}},
		},
		"limited with no clear time": {
			State:    AvailabilityLimited,
			Checked:  []string{"narwhal 429 response"},
			Observed: []Observation{{Source: "narwhal 429 response", Detail: "rate limited"}},
		},
		"limited with no observation": {
			State:   AvailabilityLimited,
			Until:   clears,
			Checked: []string{"narwhal 429 response"},
		},
		"unreachable with nothing behind it": {State: AvailabilityUnreachable},
		"unknown carrying a clear time": {
			State:   AvailabilityUnknown,
			Until:   clears,
			Checked: []string{"narwhal 429 response"},
		},
		"healthy carrying a clear time": {
			State:   AvailabilityHealthy,
			Until:   clears,
			Checked: []string{"narwhal /v1/quota response headers"},
		},
		"a state nobody declared": {State: AvailabilityState(9)},
		"a blank checked source":  {State: AvailabilityUnknown, Checked: []string{"  "}},
		"an observation naming no source": {
			State:    AvailabilityUnreachable,
			Observed: []Observation{{Detail: "connection refused"}},
		},
		"an observation recording nothing": {
			State:    AvailabilityUnreachable,
			Observed: []Observation{{Source: "dial narwhal.example:443"}},
		},
		"a read failure naming no source": {
			State:    AvailabilityUnknown,
			Failures: []ReadFailure{{Reason: "permission denied"}},
		},
		"a read failure giving no reason": {
			State:    AvailabilityUnknown,
			Failures: []ReadFailure{{Source: "~/.narwhal/limit-state.json"}},
		},
	}
	for name, verdict := range cases {
		t.Run(name, func(t *testing.T) {
			requireErrorIs(t, verdict.Validate(), ErrAvailabilityInvalid, "Validate("+name+")")
		})
	}
}

// CheckAvailability is the production call site for the verdict gate. A vendor
// that reports health it never observed must be refused THERE, not only by a
// helper somebody remembered to call.
func TestCheckAvailabilityRefusesAVendorsDishonestVerdict(t *testing.T) {
	vendor := newNarwhal()
	vendor.availability = Availability{State: AvailabilityHealthy}
	registry := registerNarwhal(t, vendor)

	_, err := CheckAvailability(registry, narwhalID, AvailabilityQuery{})
	requireErrorIs(t, err, ErrVendorContract, "CheckAvailability(vendor claiming health with nothing checked)")
	requireMentions(t, err, string(narwhalID))
}

func TestCheckAvailabilityReturnsTheVendorsVerdict(t *testing.T) {
	clears := time.Now().Add(15 * time.Minute)
	vendor := newNarwhal()
	vendor.availability = LimitedUntil(clears, Observation{Source: "narwhal 429 response", Detail: "retry-after: 900"})
	registry := registerNarwhal(t, vendor)

	verdict, err := CheckAvailability(registry, narwhalID, AvailabilityQuery{Model: "narwhal-deep"})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if verdict.State != AvailabilityLimited || !verdict.Until.Equal(clears) {
		t.Fatalf("verdict = %#v, want the vendor's limited-until answer", verdict)
	}
	if verdict.Serviceable() {
		t.Error("a limited verdict reported itself serviceable")
	}
}

// A failure to produce a verdict is not a verdict of unknown. Converting the
// error would hand the caller an answer nobody gave — and unknown is an answer,
// with a record of what was checked.
func TestCheckAvailabilityDoesNotConvertAPluginErrorIntoUnknown(t *testing.T) {
	vendor := newNarwhal()
	vendor.availabilityErr = errors.New("the quota endpoint returned malformed JSON")
	registry := registerNarwhal(t, vendor)

	verdict, err := CheckAvailability(registry, narwhalID, AvailabilityQuery{})
	if err == nil {
		t.Fatal("a plugin that could not answer produced no error; a failed read became a verdict")
	}
	if verdict.State != AvailabilityUnknown || len(verdict.Checked) != 0 {
		t.Fatalf("verdict = %#v, want the zero value alongside the error rather than a manufactured answer", verdict)
	}
	requireMentions(t, err, "malformed JSON")
}

func TestCheckAvailabilityRefusesUnknownVendorsAndModels(t *testing.T) {
	registry := registerNarwhal(t, newNarwhal())

	_, err := CheckAvailability(registry, "walrus", AvailabilityQuery{})
	requireErrorIs(t, err, ErrVendorNotRegistered, "CheckAvailability(unregistered vendor)")

	_, err = CheckAvailability(registry, narwhalID, AvailabilityQuery{Model: "narwhal-imaginary"})
	requireErrorIs(t, err, ErrUnknownModel, "CheckAvailability(model the vendor does not declare)")
}

// The seam the local-model resource plane fits behind, demonstrated rather
// than asserted in prose: the four answers that plane needs are expressible in
// the verdict as it stands today, with the plane as an evidence source. If a
// future change breaks one of these, it breaks here rather than in the story
// that tries to build the plane.
func TestTheLocalResourcePlaneFitsBehindTheVerdict(t *testing.T) {
	const plane = "local-models resource plane"
	evictionDone := time.Now().Add(90 * time.Second)

	planeVerdicts := map[string]Availability{
		"loaded and idle": Healthy(plane),
		"busy until the running turn finishes": LimitedUntil(evictionDone,
			Observation{Source: plane, Detail: "inference running on narwhal-deep since 18:02"}),
		"waiting on an eviction before it can load": LimitedUntil(evictionDone,
			Observation{Source: plane, Detail: "no memory free; narwhal-flat must unload first"}),
		"the plane is not running": Unreachable(
			[]Observation{{Source: plane, Detail: "no socket at /run/local-models.sock"}}, nil),
		"the plane was asked and could not tell": {
			State:    AvailabilityUnknown,
			Checked:  []string{plane},
			Failures: []ReadFailure{{Source: plane, Reason: "the status endpoint timed out"}},
		},
	}
	for name, verdict := range planeVerdicts {
		t.Run(name, func(t *testing.T) {
			if err := verdict.Validate(); err != nil {
				t.Fatalf("the resource plane cannot express %q through this verdict: %v", name, err)
			}
		})
	}
	if planeVerdicts["the plane was asked and could not tell"].Serviceable() {
		t.Error("a plane that could not answer was treated as ready to serve")
	}
}
