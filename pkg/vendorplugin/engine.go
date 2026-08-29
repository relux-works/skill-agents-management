package vendorplugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/relux-works/skill-agents-management/internal/ident"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine/engines/mlx"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

type engineObservationQuery struct {
	Runtime RuntimeID
	Profile string
	Model   ModelID
}

// engineFactSource is deliberately package-private. NewRegistry installs the
// concrete production implementation and no public constructor accepts a
// replacement, so an ordinary BuildLaunch caller cannot register or retain an
// evidence producer. Tests in this package use newRegistryWithEngineFactSource
// to fake the agents-infra boundary without live process, SSH, or network I/O.
type engineFactSource interface {
	ReadEngineFacts(context.Context, plugin.Ref, engineObservationQuery) (inferenceengine.EngineKind, []inferenceengine.Reading, error)
}

type productionEngineFactSource struct{}

func (productionEngineFactSource) ReadEngineFacts(_ context.Context, ref plugin.Ref, _ engineObservationQuery) (inferenceengine.EngineKind, []inferenceengine.Reading, error) {
	if ref.Kind != inferenceengine.Kind {
		return "", nil, fmt.Errorf("%w: observation reference names kind %q, want %q", ErrInferenceEngineInvalid, ref.Kind, inferenceengine.Kind)
	}
	if ref.ID != mlx.ID {
		return "", nil, fmt.Errorf("%w: configured engine %q has no trusted concrete implementation", inferenceengine.ErrEngineContractMissing, ref.ID)
	}
	readings := make([]inferenceengine.Reading, 0, len(inferenceengine.MeasuredFacts()))
	for _, definition := range inferenceengine.MeasuredFacts() {
		readings = append(readings, inferenceengine.NewReading(definition.Fact,
			inferenceengine.ReadFailure(inferenceengine.NotObservedUnsupported, "agents-infra has not supplied the concrete MLX observation adapter")))
	}
	return inferenceengine.EngineKindNativeTransformer, readings, nil
}

func resolveEngineObservations(ctx context.Context, source engineFactSource, ref plugin.Ref, query engineObservationQuery) (inferenceengine.Resolution, error) {
	if source == nil {
		return inferenceengine.Resolution{}, fmt.Errorf("%w: engine fact source is absent", inferenceengine.ErrEngineContractMissing)
	}
	kind, readings, err := source.ReadEngineFacts(ctx, ref, query)
	if err != nil {
		return inferenceengine.Resolution{}, err
	}
	return inferenceengine.ValidateReadings(ref.ID, kind, readings)
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
