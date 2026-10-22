// Package catalogobservation composes engine observation for one selected
// local-model binding. It reads the caller's parsed Config, never another
// engines.toml, and owns no engine lifecycle or runtime registry.
package catalogobservation

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/relux-works/skill-agents-management/pkg/engineobservation"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

var ErrBindingInvalid = errors.New("catalogobservation: selected binding is invalid")
var ErrEngineUnsupported = errors.New("catalogobservation: selected engine has no concrete observation kind")

// Factory owns an immutable snapshot of concrete declarations in the existing
// generic plugin graph. It has no runtime-ID dispatch table.
type Factory struct{ engines *plugin.Registry }

type observationPlugin struct {
	declaration plugin.Declaration
	kind        inferenceengine.EngineKind
}

func (p observationPlugin) PluginDeclaration() plugin.Declaration {
	d := p.declaration
	d.Dependencies = append([]plugin.Ref(nil), d.Dependencies...)
	return d
}
func (p observationPlugin) ObservationKind() inferenceengine.EngineKind { return p.kind }

// NewFactory validates concrete plugins and freezes their observation kinds.
// Neither registration nor binding construction observes or executes an engine.
func NewFactory(engines ...inferenceengine.ObservationPlugin) (*Factory, error) {
	registry := plugin.NewRegistry()
	plugins := make([]plugin.Plugin, len(engines))
	for i, engine := range engines {
		plugins[i] = engine
	}
	if err := registry.RegisterAll(plugins...); err != nil {
		return nil, err
	}
	frozen := make([]plugin.Plugin, 0, len(engines))
	for _, engine := range engines {
		d := engine.PluginDeclaration()
		d.Dependencies = append([]plugin.Ref(nil), d.Dependencies...)
		registered, _ := registry.Declaration(d.ID)
		kind := engine.ObservationKind()
		if !reflect.DeepEqual(d, registered) || kind != engine.ObservationKind() || d.Kind != inferenceengine.Kind || (kind != inferenceengine.EngineKindNativeTransformer && kind != inferenceengine.EngineKindGGUFServer) {
			return nil, fmt.Errorf("%w: invalid concrete declaration", ErrEngineUnsupported)
		}
		frozen = append(frozen, observationPlugin{declaration: d, kind: kind})
	}
	snapshot := plugin.NewRegistry()
	if err := snapshot.RegisterAll(frozen...); err != nil {
		return nil, err
	}
	return &Factory{engines: snapshot}, nil
}

// Binding supplies the selected concrete graph node and its scoped adapters.
// Replace that identity's configured node when composing the consumer registry;
// registering both nodes would be a duplicate. A pointer-only row is empty.
type Binding struct {
	Engine   plugin.Plugin
	Adapters []vendorplugin.EngineObservationAdapter
}

// ForBinding constructs from the exact selected row, including model-level
// overrides. Unrelated unsupported engine rows do not affect the selection.
func (f *Factory) ForBinding(cfg localmodels.Config, runtime vendorplugin.RuntimeID, model vendorplugin.ModelID, cacheRoot string, opts ...engineobservation.Option) (Binding, error) {
	var selected *localmodels.RuntimeEntry
	for i := range cfg.Runtimes {
		if cfg.Runtimes[i].ID == runtime {
			if selected != nil {
				return Binding{}, fmt.Errorf("%w: duplicate selected runtime", ErrBindingInvalid)
			}
			selected = &cfg.Runtimes[i]
		}
	}
	if selected == nil {
		return Binding{}, fmt.Errorf("%w: runtime is not declared", ErrBindingInvalid)
	}
	row, ok := selected.Models[model]
	if !ok {
		return Binding{}, fmt.Errorf("%w: model is not declared", ErrBindingInvalid)
	}
	engine := row.Engine
	if engine == (plugin.Ref{}) {
		engine = selected.Engine
	}
	if engine == (plugin.Ref{}) {
		return Binding{}, nil
	}
	if f == nil || f.engines == nil {
		return Binding{}, ErrEngineUnsupported
	}
	concrete, ok := f.engines.Lookup(engine.ID)
	if !ok || concrete.PluginDeclaration().Ref() != engine {
		return Binding{}, ErrEngineUnsupported
	}
	implementation := concrete.(inferenceengine.ObservationPlugin)
	a, err := engineobservation.NewCatalogAdapter(implementation.ObservationKind(), engineobservation.CatalogScope{
		Engine: engine, Runtime: runtime, Model: model, Profile: row.Pointer.CuratorEnginesProfile,
		Project: row.Pointer.CuratorEnginesProject, EnginesConfig: cfg.EnginesConfig, CacheRoot: cacheRoot,
	}, opts...)
	if err != nil {
		return Binding{}, err
	}
	return Binding{Engine: concrete, Adapters: []vendorplugin.EngineObservationAdapter{a}}, nil
}
