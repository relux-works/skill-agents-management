package vendorplugin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/relux-works/skill-agents-management/internal/ident"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

const EngineObservationAdapterContract = "agents-management.engine-observation-adapter"
const EngineObservationAdapterSchemaVersion = 1

// EngineObservationAdapterDeclaration is the immutable identity and schema
// snapshot taken once when a Registry is constructed. EngineContract is the
// inferenceengine observation contract the adapter implements.
type EngineObservationAdapterDeclaration struct {
	Contract       string
	SchemaVersion  int
	Engine         plugin.Ref
	EngineKind     inferenceengine.EngineKind
	EngineContract string
}

// EngineObservationQuery contains only authoritative values resolved by
// BuildLaunch. A launch caller cannot supply an adapter or an observation.
type EngineObservationQuery struct {
	Engine  plugin.Ref
	Runtime RuntimeID
	Model   ModelID
	Profile string
}

// EngineObservation is the sanitized, versioned response accepted from an
// agents-infra-owned, bounded, read-only adapter.
type EngineObservation struct {
	Contract      string
	SchemaVersion int
	Engine        plugin.Ref
	Runtime       RuntimeID
	Model         ModelID
	Profile       string
	ObservedAt    time.Time
	ValidUntil    time.Time
	Readings      []inferenceengine.Reading
}

// EngineObservationAdapter performs one cooperative bounded observation. It
// does not own engine lifecycle; Process B remains owned by agents-infra.
type EngineObservationAdapter interface {
	EngineObservationAdapterDeclaration() EngineObservationAdapterDeclaration
	ObserveEngine(context.Context, EngineObservationQuery) (EngineObservation, error)
}

type registeredEngineObservationAdapter struct {
	declaration EngineObservationAdapterDeclaration
	adapter     EngineObservationAdapter
}

var (
	ErrEngineObservationAdapterMissing = errors.New("vendorplugin: inference-engine observation adapter is missing")
	ErrEngineObservationAdapterInvalid = errors.New("vendorplugin: inference-engine observation adapter is invalid")
	ErrEngineObservationVersion        = errors.New("vendorplugin: inference-engine observation version is unsupported")
	ErrEngineObservationIdentity       = errors.New("vendorplugin: inference-engine observation identity does not match the query")
	ErrEngineObservationStale          = errors.New("vendorplugin: inference-engine observation is stale")
)

func validateEngineObservationAdapter(adapter EngineObservationAdapter) (registeredEngineObservationAdapter, error) {
	if nilEngineObservationAdapter(adapter) {
		return registeredEngineObservationAdapter{}, fmt.Errorf("%w: nil adapter", ErrEngineObservationAdapterInvalid)
	}
	declaration := adapter.EngineObservationAdapterDeclaration()
	if declaration.Contract != EngineObservationAdapterContract || declaration.SchemaVersion != EngineObservationAdapterSchemaVersion || declaration.EngineContract != inferenceengine.ContractVersion {
		return registeredEngineObservationAdapter{}, fmt.Errorf("%w: contract=%q schema=%d engine_contract=%q", ErrEngineObservationVersion, declaration.Contract, declaration.SchemaVersion, declaration.EngineContract)
	}
	if err := validateInferenceEngineRef("observation adapter", declaration.Engine); err != nil || declaration.Engine == (plugin.Ref{}) {
		return registeredEngineObservationAdapter{}, fmt.Errorf("%w: engine declaration: %v", ErrEngineObservationAdapterInvalid, err)
	}
	switch declaration.EngineKind {
	case inferenceengine.EngineKindNativeTransformer, inferenceengine.EngineKindGGUFServer:
	default:
		return registeredEngineObservationAdapter{}, fmt.Errorf("%w: engine kind %q", ErrEngineObservationAdapterInvalid, declaration.EngineKind)
	}
	return registeredEngineObservationAdapter{declaration: declaration, adapter: adapter}, nil
}

func nilEngineObservationAdapter(adapter EngineObservationAdapter) bool {
	if adapter == nil {
		return true
	}
	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func resolveEngineObservations(ctx context.Context, registered registeredEngineObservationAdapter, query EngineObservationQuery) (inferenceengine.Resolution, error) {
	observation, err := registered.adapter.ObserveEngine(ctx, query)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return inferenceengine.Resolution{}, err
		}
		return inferenceengine.Resolution{}, fmt.Errorf("%w: %w", inferenceengine.ErrObservationRead, err)
	}
	if err := ctx.Err(); err != nil {
		return inferenceengine.Resolution{}, err
	}
	if observation.Contract != EngineObservationAdapterContract || observation.SchemaVersion != EngineObservationAdapterSchemaVersion {
		return inferenceengine.Resolution{}, fmt.Errorf("%w: contract=%q schema=%d", ErrEngineObservationVersion, observation.Contract, observation.SchemaVersion)
	}
	if observation.Engine != query.Engine || observation.Runtime != query.Runtime || observation.Model != query.Model || observation.Profile != query.Profile {
		return inferenceengine.Resolution{}, fmt.Errorf("%w: response identity differs from authoritative query", ErrEngineObservationIdentity)
	}
	now := time.Now()
	if observation.ObservedAt.IsZero() || observation.ValidUntil.IsZero() || observation.ObservedAt.After(now) || !observation.ValidUntil.After(observation.ObservedAt) || !now.Before(observation.ValidUntil) {
		return inferenceengine.Resolution{}, ErrEngineObservationStale
	}
	readings := append([]inferenceengine.Reading(nil), observation.Readings...)
	return inferenceengine.ValidateReadings(query.Engine.ID, registered.declaration.EngineKind, readings)
}

var (
	ErrInferenceEngineInvalid  = errors.New("vendorplugin: inference-engine reference is malformed")
	ErrInferenceEngineMismatch = errors.New("vendorplugin: inference-engine identities do not match")
)

func validateInferenceEngineRef(owner string, ref plugin.Ref) error {
	if ref == (plugin.Ref{}) {
		return nil
	}
	normalized, reason, ok := ident.Normalize(string(ref.ID))
	if !ok || normalized != string(ref.ID) {
		return fmt.Errorf("%w: %s names id %q: %s", ErrInferenceEngineInvalid, owner, ref.ID, reason)
	}
	if ref.Kind != inferenceengine.Kind {
		return fmt.Errorf("%w: %s names kind %q, want %q", ErrInferenceEngineInvalid, owner, ref.Kind, inferenceengine.Kind)
	}
	return nil
}

func resolveInferenceEngine(r *Registry, runtimeRef, modelRef, callerRef plugin.Ref) (plugin.Ref, plugin.Ref, error) {
	for _, candidate := range []struct {
		owner string
		ref   plugin.Ref
	}{{"runtime", runtimeRef}, {"model", modelRef}, {"caller", callerRef}} {
		if err := validateInferenceEngineRef(candidate.owner, candidate.ref); err != nil {
			return plugin.Ref{}, plugin.Ref{}, err
		}
	}
	if runtimeRef == (plugin.Ref{}) && modelRef == (plugin.Ref{}) {
		if callerRef != (plugin.Ref{}) {
			return plugin.Ref{}, plugin.Ref{}, fmt.Errorf("%w: caller requested %#v but runtime and model require no engine", ErrInferenceEngineMismatch, callerRef)
		}
		return plugin.Ref{}, plugin.Ref{}, nil
	}
	if runtimeRef == (plugin.Ref{}) || modelRef == (plugin.Ref{}) || runtimeRef != modelRef {
		return plugin.Ref{}, plugin.Ref{}, fmt.Errorf("%w: runtime requests %#v and model requests %#v", ErrInferenceEngineMismatch, runtimeRef, modelRef)
	}
	requested := runtimeRef
	if callerRef != (plugin.Ref{}) {
		if callerRef != runtimeRef {
			return plugin.Ref{}, plugin.Ref{}, fmt.Errorf("%w: caller requests %#v but declaration requires %#v", ErrInferenceEngineMismatch, callerRef, runtimeRef)
		}
		requested = callerRef
	}
	resolution, err := r.Graph().Resolve(requested.ID)
	if err != nil {
		return plugin.Ref{}, plugin.Ref{}, fmt.Errorf("vendorplugin: resolving inference engine %#v: %w", requested, err)
	}
	resolved := resolution.Declaration.Ref()
	if resolved != requested {
		return plugin.Ref{}, plugin.Ref{}, fmt.Errorf("%w: requested %#v resolved as %#v", ErrInferenceEngineMismatch, requested, resolved)
	}
	return requested, resolved, nil
}
