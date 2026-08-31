package vendorplugin_test

import (
	"context"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

type externalObservationAdapter struct{}

func (externalObservationAdapter) EngineObservationAdapterDeclaration() vendorplugin.EngineObservationAdapterDeclaration {
	return vendorplugin.EngineObservationAdapterDeclaration{
		Contract:       vendorplugin.EngineObservationAdapterContract,
		SchemaVersion:  vendorplugin.EngineObservationAdapterSchemaVersion,
		Engine:         plugin.Ref{ID: "external-engine", Kind: inferenceengine.Kind},
		EngineKind:     inferenceengine.EngineKindNativeTransformer,
		EngineContract: inferenceengine.ContractVersion,
	}
}

func (externalObservationAdapter) ObserveEngine(_ context.Context, query vendorplugin.EngineObservationQuery) (vendorplugin.EngineObservation, error) {
	now := time.Now()
	return vendorplugin.EngineObservation{
		Contract: vendorplugin.EngineObservationAdapterContract, SchemaVersion: vendorplugin.EngineObservationAdapterSchemaVersion,
		Engine: query.Engine, Runtime: query.Runtime, Model: query.Model, Profile: query.Profile,
		ObservedAt: now, ValidUntil: now.Add(time.Minute),
	}, nil
}

func TestPublicObservationAdapterAPICompilesForExternalConsumer(t *testing.T) {
	registry, err := vendorplugin.NewRegistryWithEngineObservationAdapters(agentic.NewRegistry(), externalObservationAdapter{})
	if err != nil || registry == nil {
		t.Fatalf("NewRegistryWithEngineObservationAdapters=%p, %v", registry, err)
	}
}
