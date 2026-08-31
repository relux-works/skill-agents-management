package vendorplugin

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

type declaredObservationAdapter struct {
	declaration EngineObservationAdapterDeclaration
}

func (a *declaredObservationAdapter) EngineObservationAdapterDeclaration() EngineObservationAdapterDeclaration {
	return a.declaration
}

func (*declaredObservationAdapter) ObserveEngine(context.Context, EngineObservationQuery) (EngineObservation, error) {
	return EngineObservation{}, errors.New("not called")
}

func validAdapterDeclaration(engine plugin.Ref) EngineObservationAdapterDeclaration {
	return EngineObservationAdapterDeclaration{
		Contract: EngineObservationAdapterContract, SchemaVersion: EngineObservationAdapterSchemaVersion,
		Engine: engine, EngineKind: inferenceengine.EngineKindNativeTransformer, EngineContract: inferenceengine.ContractVersion,
	}
}

func TestNewRegistryWithEngineObservationAdaptersRefusesInvalidBatchAtomically(t *testing.T) {
	valid := validAdapterDeclaration(testEngineRef)
	var typedNil *declaredObservationAdapter
	tests := []struct {
		name     string
		adapters []EngineObservationAdapter
		want     error
	}{
		{"nil", []EngineObservationAdapter{nil}, ErrEngineObservationAdapterInvalid},
		{"typed nil", []EngineObservationAdapter{typedNil}, ErrEngineObservationAdapterInvalid},
		{"empty engine", []EngineObservationAdapter{&declaredObservationAdapter{declaration: func() EngineObservationAdapterDeclaration { d := valid; d.Engine = plugin.Ref{}; return d }()}}, ErrEngineObservationAdapterInvalid},
		{"wrong engine kind", []EngineObservationAdapter{&declaredObservationAdapter{declaration: func() EngineObservationAdapterDeclaration { d := valid; d.Engine.Kind = "vendor"; return d }()}}, ErrEngineObservationAdapterInvalid},
		{"unknown implementation kind", []EngineObservationAdapter{&declaredObservationAdapter{declaration: func() EngineObservationAdapterDeclaration { d := valid; d.EngineKind = "future"; return d }()}}, ErrEngineObservationAdapterInvalid},
		{"unknown adapter contract", []EngineObservationAdapter{&declaredObservationAdapter{declaration: func() EngineObservationAdapterDeclaration { d := valid; d.Contract = "future"; return d }()}}, ErrEngineObservationVersion},
		{"unknown adapter schema", []EngineObservationAdapter{&declaredObservationAdapter{declaration: func() EngineObservationAdapterDeclaration { d := valid; d.SchemaVersion = 2; return d }()}}, ErrEngineObservationVersion},
		{"unknown engine contract", []EngineObservationAdapter{&declaredObservationAdapter{declaration: func() EngineObservationAdapterDeclaration {
			d := valid
			d.EngineContract = "observed-process/v3"
			return d
		}()}}, ErrEngineObservationVersion},
		{"duplicate", []EngineObservationAdapter{&declaredObservationAdapter{declaration: valid}, &declaredObservationAdapter{declaration: valid}}, ErrEngineObservationAdapterInvalid},
		{"valid then invalid remains atomic", []EngineObservationAdapter{&declaredObservationAdapter{declaration: valid}, nil}, ErrEngineObservationAdapterInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, err := NewRegistryWithEngineObservationAdapters(agentic.NewRegistry(), test.adapters...)
			if registry != nil || !errors.Is(err, test.want) {
				t.Fatalf("registry=%p err=%v, want nil and %v", registry, err, test.want)
			}
		})
	}
}

func TestAdapterDeclarationIsReadOnceAndSnapshotted(t *testing.T) {
	ids := launchIdentityCorpus()[1]
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	fixture := newIdentityLaunchFixture(t, ids, engine, engine)
	fixture.adapter.engine = plugin.Ref{ID: "redirected-engine", Kind: inferenceengine.Kind}
	fixture.adapter.kind = "future-kind"

	if _, err := BuildLaunch(context.Background(), fixture.registry, fixture.request, agentic.LaunchModeExec); err != nil {
		t.Fatalf("BuildLaunch after adapter mutation: %v", err)
	}
	if fixture.adapter.declarations != 1 || fixture.adapter.calls != 1 {
		t.Fatalf("declaration/observe calls=%d/%d, want 1/1", fixture.adapter.declarations, fixture.adapter.calls)
	}
}

func TestBuildLaunchValidatesObservationVersionIdentityAndFreshnessBeforePreflight(t *testing.T) {
	ids := launchIdentityCorpus()[1]
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	tests := []struct {
		name   string
		mutate func(*EngineObservation)
		want   error
	}{
		{"missing contract", func(o *EngineObservation) { o.Contract = "" }, ErrEngineObservationVersion},
		{"unknown schema", func(o *EngineObservation) { o.SchemaVersion = 2 }, ErrEngineObservationVersion},
		{"engine redirect", func(o *EngineObservation) { o.Engine.ID = "other" }, ErrEngineObservationIdentity},
		{"runtime redirect", func(o *EngineObservation) { o.Runtime = "other" }, ErrEngineObservationIdentity},
		{"model redirect", func(o *EngineObservation) { o.Model = "other" }, ErrEngineObservationIdentity},
		{"profile redirect", func(o *EngineObservation) { o.Profile = "other" }, ErrEngineObservationIdentity},
		{"zero observed", func(o *EngineObservation) { o.ObservedAt = time.Time{} }, ErrEngineObservationStale},
		{"future observed", func(o *EngineObservation) {
			o.ObservedAt = time.Now().Add(time.Minute)
			o.ValidUntil = time.Now().Add(2 * time.Minute)
		}, ErrEngineObservationStale},
		{"expired", func(o *EngineObservation) { o.ValidUntil = time.Now().Add(-time.Second) }, ErrEngineObservationStale},
		{"inverted", func(o *EngineObservation) { o.ValidUntil = o.ObservedAt.Add(-time.Second) }, ErrEngineObservationStale},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newIdentityLaunchFixture(t, ids, engine, engine)
			fixture.adapter.mutate = test.mutate
			_, err := BuildLaunch(context.Background(), fixture.registry, fixture.request, agentic.LaunchModeExec)
			if !errors.Is(err, test.want) {
				t.Fatalf("BuildLaunch=%v, want %v", err, test.want)
			}
			if fixture.vendor.calls["Spawn"] != 1 || fixture.adapter.calls != 1 || fixture.system.preflightCalls != 0 {
				t.Fatalf("Spawn/Observe/Preflight=%d/%d/%d, want 1/1/0", fixture.vendor.calls["Spawn"], fixture.adapter.calls, fixture.system.preflightCalls)
			}
		})
	}
}

func TestBuildLaunchObservationReadAndContextFailuresDoNotReachPreflight(t *testing.T) {
	ids := launchIdentityCorpus()[1]
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}

	t.Run("read failure preserves cause", func(t *testing.T) {
		fixture := newIdentityLaunchFixture(t, ids, engine, engine)
		cause := errors.New("bounded status read failed")
		fixture.adapter.err = cause
		_, err := BuildLaunch(context.Background(), fixture.registry, fixture.request, agentic.LaunchModeExec)
		if !errors.Is(err, inferenceengine.ErrObservationRead) || !errors.Is(err, cause) || fixture.system.preflightCalls != 0 {
			t.Fatalf("err=%v preflight=%d", err, fixture.system.preflightCalls)
		}
	})

	t.Run("pre-cancelled refuses before pure vendor and adapter", func(t *testing.T) {
		fixture := newIdentityLaunchFixture(t, ids, engine, engine)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := BuildLaunch(ctx, fixture.registry, fixture.request, agentic.LaunchModeExec)
		if !errors.Is(err, context.Canceled) || fixture.vendor.calls["Spawn"] != 0 || fixture.adapter.calls != 0 || fixture.system.preflightCalls != 0 {
			t.Fatalf("err=%v Spawn/Observe/Preflight=%d/%d/%d", err, fixture.vendor.calls["Spawn"], fixture.adapter.calls, fixture.system.preflightCalls)
		}
	})

	t.Run("earlier caller deadline bounds cooperative adapter", func(t *testing.T) {
		fixture := newIdentityLaunchFixture(t, ids, engine, engine)
		fixture.adapter.hang = true
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := BuildLaunch(ctx, fixture.registry, fixture.request, agentic.LaunchModeExec)
		if !errors.Is(err, context.DeadlineExceeded) || fixture.adapter.calls != 1 || fixture.system.preflightCalls != 0 {
			t.Fatalf("err=%v Observe/Preflight=%d/%d", err, fixture.adapter.calls, fixture.system.preflightCalls)
		}
	})

	t.Run("late response after cancellation is never admitted", func(t *testing.T) {
		fixture := newIdentityLaunchFixture(t, ids, engine, engine)
		ctx, cancel := context.WithCancel(context.Background())
		fixture.adapter.before = cancel
		_, err := BuildLaunch(ctx, fixture.registry, fixture.request, agentic.LaunchModeExec)
		if !errors.Is(err, context.Canceled) || fixture.system.preflightCalls != 0 {
			t.Fatalf("err=%v preflight=%d", err, fixture.system.preflightCalls)
		}
	})
}

func TestBuildLaunchAdapterReceivesOnlyAuthoritativeVendorProfile(t *testing.T) {
	ids := launchIdentityCorpus()[1]
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	fixture := newIdentityLaunchFixture(t, ids, engine, engine)
	fixture.request.Profile = ""
	fixture.vendor.resolvedProfile = ids.profile

	plan, err := BuildLaunch(context.Background(), fixture.registry, fixture.request, agentic.LaunchModeExec)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if len(fixture.adapter.queries) != 1 || fixture.adapter.queries[0].Profile != ids.profile || plan.Provenance.Profile != ids.profile {
		t.Fatalf("query=%+v provenance=%+v, want profile %q", fixture.adapter.queries, plan.Provenance, ids.profile)
	}
}

// BuildLaunch is the production gate. A cooperative adapter would echo a
// vendor-redirected profile and make the response look self-consistent, so the
// caller/vendor conflict must refuse before adapter lookup or Preflight.
func TestBuildLaunchRefusesVendorProfileRedirectBeforeObservation(t *testing.T) {
	ids := launchIdentityCorpus()[1]
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	fixture := newIdentityLaunchFixture(t, ids, engine, engine)
	fixture.vendor.profileOverride = "redirected-profile"

	plan, err := BuildLaunch(context.Background(), fixture.registry, fixture.request, agentic.LaunchModeExec)
	if !errors.Is(err, ErrVendorContract) {
		t.Fatalf("BuildLaunch profile redirect error=%v, want ErrVendorContract", err)
	}
	if !reflect.DeepEqual(plan, agentic.Plan{}) || fixture.adapter.calls != 0 || fixture.system.preflightCalls != 0 {
		t.Fatalf("plan=%+v Observe/Preflight=%d/%d, want zero effects", plan, fixture.adapter.calls, fixture.system.preflightCalls)
	}
}

func TestBuildLaunchDryRunSkipsObservationEvenWithoutAdapter(t *testing.T) {
	ids := launchIdentityCorpus()[1]
	engine := plugin.Ref{ID: plugin.ID(ids.engine), Kind: inferenceengine.Kind}
	fixture := newIdentityLaunchFixture(t, ids, engine, engine)
	fixture.registry.engineAdapters = nil
	if _, err := BuildLaunch(context.Background(), fixture.registry, fixture.request, agentic.LaunchModeDryRun); err != nil {
		t.Fatalf("BuildLaunch(dry-run): %v", err)
	}
	if fixture.adapter.calls != 0 || fixture.system.preflightCalls != 0 {
		t.Fatalf("Observe/Preflight=%d/%d, want 0/0", fixture.adapter.calls, fixture.system.preflightCalls)
	}
}
