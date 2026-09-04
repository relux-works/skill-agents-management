package vendorplugin

import (
	"context"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
)

// THE AUDIT HALF OF THE ALIAS CONTRACT.
//
// An alias launch splits one model id into two facts and each has exactly one
// correct home: argv gets the IDENTITY, because the provider's backend is the
// thing that refused "muse-spark", and the provenance record gets the
// REQUESTED SPELLING, because an audit exists to reproduce what a human asked
// for. muse_alias_test.go and pkg/agentic/alias_test.go pin the argv half end
// to end. Nothing pinned the audit half, and it was measured: with the whole
// module suite green, rewriting spawn.go's
//
//	Model: string(model.ID)  ->  Model: plan.ModelIdentity.Launched
//
// still exits 0. That mutant is not hypothetical drift — it is the exact
// simplification "the plan already knows the model" invites, and it would make
// an alias launch indistinguishable in the persisted record from a launch of
// the identity: the one distinction the record is kept for.
//
// The reason no existing test catches it is structural rather than an
// oversight, which is why a test rather than a comment is the fix. Provenance
// is only populated on the inference-engine path (BuildLaunch sets it under
// `requestedEngine != plugin.Ref{}`), every fixture on that path launches a
// row that declares no alias, and for such a row the requested spelling and
// the launched identity are the same string. The two candidate expressions are
// therefore equal in every case the suite runs, and an alias on the engine
// path is the only observation that separates them.

// aliasEngineRegistry is engineRegistry's shape with an ALIASED lineup: the
// same real construction path (a registered engine plugin, a scripted
// observation adapter, Registry.Register, DeclareRuntime) so what is measured
// below is the production BuildLaunch, not a hand-built Plan.
//
// The engine is set on BOTH model rows, and on the runtime declaration, on
// purpose. checkAliases' stated bound does not hold Engine equal between an
// alias and its target, so a fixture that set it on one row would be resolving
// the engine from whichever row BuildLaunch happens to read — turning this
// test into a second, accidental assertion about engine resolution and making
// a failure ambiguous between the two. resolveInferenceEngine additionally
// refuses a runtime and a model that name different engines, so the
// declaration has to agree or every case below dies on that refusal instead of
// reaching the record it is about.
func aliasEngineRegistry(t *testing.T) *Registry {
	t.Helper()
	registry, err := NewRegistryWithEngineObservationAdapters(
		systemsWithPangolin(t),
		&scriptedEngineObservationAdapter{engine: testEngineRef},
	)
	if err != nil {
		t.Fatalf("NewRegistryWithEngineObservationAdapters: %v", err)
	}
	if err := registry.RegisterPlugin(inferenceengine.NewConfigured(testEngineRef.ID)); err != nil {
		t.Fatalf("RegisterPlugin(engine): %v", err)
	}
	double := newNarwhal()
	double.models = aliasModels()
	for index := range double.models {
		double.models[index].Engine = testEngineRef
		double.models[index].Publisher = "narwhal-labs"
		double.models[index].Family = "narwhal"
	}
	// identityVendor rather than the bare double because narwhalVendor.Spawn
	// returns no profile, and an engine-bound provenance record without one
	// fails ConsumerProvenance' own validation before the model field this
	// test is about is ever compared.
	if err := registry.Register(&identityVendor{narwhalVendor: double}); err != nil {
		t.Fatalf("Register(vendor): %v", err)
	}
	declaration := tuskDeclaration()
	declaration.Engine = testEngineRef
	if err := registry.DeclareRuntime(declaration); err != nil {
		t.Fatalf("DeclareRuntime: %v", err)
	}
	return registry
}

// aliasLaunch drives the production entry point, BuildLaunch, in exec mode —
// the mode that actually starts a process and therefore the one whose record
// an audit reads.
func aliasLaunch(t *testing.T, model ModelID) agentic.Plan {
	t.Helper()
	request := narwhalRequest()
	request.Model = model
	request.Engine = testEngineRef
	// An engine-bound provenance record with no profile fails
	// ConsumerProvenance' own validation before the model field is ever read,
	// so the profile is part of reaching the assertion, not decoration.
	request.Profile = "narwhal-deep-8bit"
	plan, err := BuildLaunch(context.Background(), aliasEngineRegistry(t), request, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch(%q): %v", model, err)
	}
	return plan
}

// TestTheProvenanceRecordKeepsTheRequestedSpelling is the negative test for the
// mutant above: it fails when the record names the identity the launch
// resolved to instead of the spelling the caller wrote.
func TestTheProvenanceRecordKeepsTheRequestedSpelling(t *testing.T) {
	plan := aliasLaunch(t, "narwhal")

	// The launch really is an alias launch. Without this the assertion below
	// would pass vacuously the day the alias is dropped from the fixture:
	// "requested == recorded" is trivially true when nothing was substituted.
	want := agentic.ModelIdentity{Requested: "narwhal", Launched: "narwhal-deep"}
	if plan.ModelIdentity != want {
		t.Fatalf("ModelIdentity = %#v, want %#v; the fixture is not exercising an alias", plan.ModelIdentity, want)
	}
	if plan.Provenance.Model != "narwhal" {
		t.Errorf("Provenance.Model = %q, want the requested spelling %q; the record cannot distinguish this launch from a launch of the identity", plan.Provenance.Model, "narwhal")
	}

	// The persisted projection is what a consumer actually stores, so the
	// spelling has to survive the boundary and not only the in-memory struct.
	projection, err := plan.ConsumerProvenance()
	if err != nil {
		t.Fatalf("ConsumerProvenance: %v", err)
	}
	if projection.Model != "narwhal" {
		t.Errorf("persisted provenance model = %q, want %q", projection.Model, "narwhal")
	}
	// And the requested spelling must still satisfy the consumer gate. An
	// audit record an operator cannot re-validate is not an audit record, and
	// the alias is a registered row precisely so this keeps working.
	if err := aliasEngineRegistry(t).ValidateLaunchProvenance(projection); err != nil {
		t.Errorf("ValidateLaunchProvenance(alias spelling) = %v, want the registered alias row to validate", err)
	}
}

// TestTheProvenanceRecordIsUnchangedForANonAliasRow narrows the rule to the
// alias. A record that started reporting something other than the requested id
// for an ordinary row would be a regression this file must also catch, and it
// is the control that shows the assertion above is about substitution rather
// than about provenance being populated at all.
func TestTheProvenanceRecordIsUnchangedForANonAliasRow(t *testing.T) {
	plan := aliasLaunch(t, "narwhal-deep")

	if plan.ModelIdentity.IsAlias() {
		t.Fatalf("%q was substituted to %q and declares no alias", plan.ModelIdentity.Requested, plan.ModelIdentity.Launched)
	}
	if plan.Provenance.Model != "narwhal-deep" {
		t.Errorf("Provenance.Model = %q, want %q", plan.Provenance.Model, "narwhal-deep")
	}
}
