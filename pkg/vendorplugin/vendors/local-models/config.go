// Package localmodels is the local-models vendor plugin: the generic
// resource plane for locally-running models, and the Alibaba Qwen/MLX
// profile (local-qwen) declared through it.
//
// Unlike every other vendor plugin in this module, it does NOT self-register
// in an init function. Registry.Register refuses ANY vendor whose Models()
// returns zero rows, and this vendor's catalog depends on a machine-local
// file that may not exist — registering it unconditionally would make a
// missing ~/.agents/.configs/local-models.toml poison registry construction
// for every OTHER runtime, including ones with nothing to do with local
// models. So registration here is CONDITIONAL, and the decision belongs to
// the caller building the shared registry (see Peek and New below), never to
// this package's own init.
package localmodels

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/inferenceengine"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// ErrConfigMalformed names a local-models.toml that was FOUND but failed to
// parse or validate — distinct from absence, which is not an error at all.
var ErrConfigMalformed = errors.New("localmodels: local-models.toml is malformed")

// Pointer is what local-models.toml stores for one (RuntimeID, ModelID)
// row: a pointer from that pair to a locally-running agents-infra project
// and profile.
//
// Neither field ever travels through remote board policy — they exist only
// in this operator-populated, global, machine-local file. Remote/local board
// policy carries RuntimeID/ModelID only.
type Pointer struct {
	// AgentsInfraProject is the absolute local filesystem path to the
	// agents-infra checkout on THIS machine that owns the weights.
	AgentsInfraProject string
	// AgentsInfraProfile is the agents.pi.profiles.<name> key inside that
	// checkout's project-config.toml.
	AgentsInfraProfile string
}

// ModelEntry is one declared model row under one runtime.
type ModelEntry struct {
	Description         string
	Publisher           string
	Family              string
	Lifecycle           vendorplugin.Lifecycle
	ContextWindowTokens int
	EffortSupport       agentic.EffortSupport
	Pointer             Pointer
	Engine              plugin.Ref
}

// RuntimeEntry is one declared runtime: its id, which agentic system drives
// it, and the model rows it owns.
//
// Config.Runtimes is a SLICE, not a map[vendorplugin.RuntimeID]RuntimeEntry.
// pkg/vendorplugin/registry.go is the one file in this module allowed to
// bind a RuntimeID, and pkg/agentic/singlesource_guard_test.go fails the
// build over a second binding table anywhere else — a Config-shaped map
// keyed by RuntimeID is exactly the shadow table that guard exists to catch,
// however local-only its scope. A handful of declared runtimes costs
// nothing to scan linearly (see runtimeEntry/pointerFor in models.go).
type RuntimeEntry struct {
	ID     vendorplugin.RuntimeID
	System agentic.SystemID
	Engine plugin.Ref
	Models map[vendorplugin.ModelID]ModelEntry
}

// Config is the fully parsed, fully validated content of one
// local-models.toml.
type Config struct {
	InferenceEngines []plugin.ID
	Runtimes         []RuntimeEntry
}

// EnginePlugins materializes configured inference-engine identities without
// interpreting their names. The shared graph owns validation and edges.
func (c Config) EnginePlugins() []plugin.Plugin {
	out := make([]plugin.Plugin, 0, len(c.InferenceEngines))
	for _, id := range c.InferenceEngines {
		out = append(out, inferenceengine.NewConfigured(id))
	}
	return out
}

// ConfigResult is loadConfig's three-way answer: the machine genuinely has
// no local-models.toml (Absent), the file was found but could not be
// honestly read (Err, wrapping ErrConfigMalformed), or a valid Config.
//
// Absent and Err are mutually exclusive and both distinct from a valid,
// zero-model Config — a file that PARSES but declares no models under a
// runtime is a real configuration error the vendor registry itself refuses
// (Registry.Register's unconditional ErrNoModels), not something this type
// collapses into either of the other two.
type ConfigResult struct {
	Config Config
	Absent bool
	Err    error
}

// localModelsFilePath is the one path this package ever reads: global only,
// no project-local tier, because what it stores is a MACHINE fact (which
// local server, if any, is available on this workstation), not a
// per-calling-Story one.
func localModelsFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("localmodels: resolving the home directory: %w", err)
	}
	return filepath.Join(home, ".agents", ".configs", "local-models.toml"), nil
}

// readHomeConfigFile is the production file reader: os.ReadFile against the
// resolved home path, reporting absence via os.IsNotExist rather than
// conflating it with every other read failure.
func readHomeConfigFile() ([]byte, bool, error) {
	path, err := localModelsFilePath()
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return data, false, nil
}

// configLoader is the lazy, memoized-forever loader §2.2.1 specifies,
// wrapped in a type (rather than bare package-level vars) so a test can
// construct an isolated instance with its own fake reader instead of
// contending with the real, process-wide singleton every production caller
// shares.
type configLoader struct {
	once   sync.Once
	result ConfigResult
	read   func() (data []byte, absent bool, err error)
}

func newConfigLoader(read func() ([]byte, bool, error)) *configLoader {
	return &configLoader{read: read}
}

// load returns the memoized result, performing the actual read only on the
// very first call — a sync.Once single-flights concurrent callers and
// guarantees every later call, however the underlying source changes
// afterward, returns the SAME result.
func (l *configLoader) load() ConfigResult {
	l.once.Do(func() {
		data, absent, err := l.read()
		switch {
		case err != nil:
			l.result = ConfigResult{Err: fmt.Errorf("%w: %v", ErrConfigMalformed, err)}
		case absent:
			l.result = ConfigResult{Absent: true}
		default:
			cfg, err := parseConfig(data)
			if err != nil {
				l.result = ConfigResult{Err: err}
				return
			}
			l.result = ConfigResult{Config: cfg}
		}
	})
	return l.result
}

// homeLoader is the one process-wide, memoized-forever singleton every
// production caller shares. Peek and every Vendor constructed from a real
// machine read through it.
var homeLoader = newConfigLoader(readHomeConfigFile)

// Peek exposes the memoized-forever load result to a caller that must decide
// whether to register this vendor at all, WITHOUT constructing a Vendor
// value first. It calls the SAME loader production's Vendor values are
// eventually built from — the memoization guarantees this costs one file
// read total, however many callers ask.
func Peek() ConfigResult { return homeLoader.load() }

// --- TOML schema and parsing/validation ---

type wireFile struct {
	InferenceEngines []string               `toml:"inference_engines"`
	Runtimes         map[string]wireRuntime `toml:"runtimes"`
}

type wireRuntime struct {
	System string               `toml:"system"`
	Engine *string              `toml:"engine"`
	Models map[string]wireModel `toml:"models"`
}

type wireModel struct {
	Description         string      `toml:"description"`
	Publisher           string      `toml:"publisher"`
	Family              string      `toml:"family"`
	Lifecycle           string      `toml:"lifecycle"`
	ContextWindowTokens int         `toml:"context_window_tokens"`
	EffortSupport       string      `toml:"effort_support"`
	Engine              *string     `toml:"engine"`
	Pointer             wirePointer `toml:"pointer"`
}

type wirePointer struct {
	AgentsInfraProject string `toml:"agents_infra_project"`
	AgentsInfraProfile string `toml:"agents_infra_profile"`
}

// parseConfig decodes and validates one local-models.toml body, returning a
// single named, wrapped error for every way it can be dishonest: it does not
// parse as TOML, a runtime names no models, a model's pointer is missing a
// required field, or a model's declared lifecycle/effort word is not one
// this module recognizes.
func parseConfig(data []byte) (Config, error) {
	var file wireFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrConfigMalformed, err)
	}

	var cfg Config
	for _, rawEngineID := range file.InferenceEngines {
		probe := plugin.NewRegistry()
		if err := probe.Register(inferenceengine.NewConfigured(plugin.ID(rawEngineID))); err != nil {
			return Config{}, fmt.Errorf("%w: inference engine %q: %v", ErrConfigMalformed, rawEngineID, err)
		}
		for _, existing := range cfg.InferenceEngines {
			if existing == plugin.ID(rawEngineID) {
				return Config{}, fmt.Errorf("%w: inference engine %q is declared twice", ErrConfigMalformed, rawEngineID)
			}
		}
		cfg.InferenceEngines = append(cfg.InferenceEngines, plugin.ID(rawEngineID))
	}
	for rawRuntimeID, wireRt := range file.Runtimes {
		runtimeID, err := vendorplugin.NormalizeRuntimeID(rawRuntimeID)
		if err != nil {
			return Config{}, fmt.Errorf("%w: runtime %q: %v", ErrConfigMalformed, rawRuntimeID, err)
		}
		system, err := agentic.NormalizeSystemID(wireRt.System)
		if err != nil {
			return Config{}, fmt.Errorf("%w: runtime %q names system %q: %v", ErrConfigMalformed, rawRuntimeID, wireRt.System, err)
		}
		runtimeEngine, err := configuredEngineRef(cfg, wireRt.Engine)
		if err != nil {
			return Config{}, fmt.Errorf("%w: runtime %q: %v", ErrConfigMalformed, rawRuntimeID, err)
		}
		entry := RuntimeEntry{ID: runtimeID, System: system, Engine: runtimeEngine, Models: map[vendorplugin.ModelID]ModelEntry{}}
		for rawModelID, wireM := range wireRt.Models {
			modelID := vendorplugin.ModelID(rawModelID)
			if err := vendorplugin.ValidateModelID(modelID); err != nil {
				return Config{}, fmt.Errorf("%w: runtime %q: %v", ErrConfigMalformed, rawRuntimeID, err)
			}
			model, err := parseModel(cfg, rawRuntimeID, rawModelID, wireM)
			if err != nil {
				return Config{}, err
			}
			entry.Models[modelID] = model
		}
		cfg.Runtimes = append(cfg.Runtimes, entry)
	}
	return cfg, nil
}

func parseModel(cfg Config, runtimeID, modelID string, wireM wireModel) (ModelEntry, error) {
	if strings.TrimSpace(wireM.Pointer.AgentsInfraProject) == "" {
		return ModelEntry{}, fmt.Errorf("%w: runtime %q model %q: pointer.agents_infra_project is required", ErrConfigMalformed, runtimeID, modelID)
	}
	if strings.TrimSpace(wireM.Pointer.AgentsInfraProfile) == "" {
		return ModelEntry{}, fmt.Errorf("%w: runtime %q model %q: pointer.agents_infra_profile is required", ErrConfigMalformed, runtimeID, modelID)
	}
	lifecycle := vendorplugin.Lifecycle(wireM.Lifecycle)
	if err := lifecycle.Validate(); err != nil {
		return ModelEntry{}, fmt.Errorf("%w: runtime %q model %q: %v", ErrConfigMalformed, runtimeID, modelID, err)
	}
	effort, err := parseEffortSupport(wireM.EffortSupport)
	if err != nil {
		return ModelEntry{}, fmt.Errorf("%w: runtime %q model %q: %v", ErrConfigMalformed, runtimeID, modelID, err)
	}
	if strings.TrimSpace(wireM.Description) == "" {
		return ModelEntry{}, fmt.Errorf("%w: runtime %q model %q: description is required", ErrConfigMalformed, runtimeID, modelID)
	}
	if wireM.ContextWindowTokens < 0 {
		return ModelEntry{}, fmt.Errorf("%w: runtime %q model %q: context_window_tokens is negative", ErrConfigMalformed, runtimeID, modelID)
	}
	engine, err := configuredEngineRef(cfg, wireM.Engine)
	if err != nil {
		return ModelEntry{}, fmt.Errorf("%w: runtime %q model %q: %v", ErrConfigMalformed, runtimeID, modelID, err)
	}
	return ModelEntry{
		Description:         wireM.Description,
		Publisher:           wireM.Publisher,
		Family:              wireM.Family,
		Lifecycle:           lifecycle,
		ContextWindowTokens: wireM.ContextWindowTokens,
		EffortSupport:       effort,
		Engine:              engine,
		Pointer: Pointer{
			AgentsInfraProject: wireM.Pointer.AgentsInfraProject,
			AgentsInfraProfile: wireM.Pointer.AgentsInfraProfile,
		},
	}, nil
}

func configuredEngineRef(cfg Config, raw *string) (plugin.Ref, error) {
	if raw == nil {
		return plugin.Ref{}, nil
	}
	if strings.TrimSpace(*raw) == "" {
		return plugin.Ref{}, errors.New("engine is present but blank")
	}
	for _, id := range cfg.InferenceEngines {
		if string(id) == *raw {
			return plugin.Ref{ID: id, Kind: inferenceengine.Kind}, nil
		}
	}
	return plugin.Ref{}, fmt.Errorf("engine %q is not declared in inference_engines", *raw)
}

// parseEffortSupport reads the TOML's plain-word effort vocabulary into
// agentic.EffortSupport. Only "none" and "required" are declared spellings;
// anything else, including a blank value, is refused rather than defaulted.
func parseEffortSupport(word string) (agentic.EffortSupport, error) {
	switch strings.TrimSpace(word) {
	case "none":
		return agentic.EffortSupportNone, nil
	case "required":
		return agentic.EffortSupportRequired, nil
	default:
		return 0, fmt.Errorf("effort_support %q is not \"none\" or \"required\"", word)
	}
}
