package runtimecatalog

import (
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

func ParseRuntimeCatalog(data []byte) (Catalog, error) {
	document, err := parseDocument(RuntimesFile, data)
	if err != nil {
		return Catalog{}, err
	}
	if err := requireSchemaVersion(document, RuntimesFile); err != nil {
		return Catalog{}, err
	}
	rawRuntimes, ok := table(document["runtimes"])
	if !ok {
		return Catalog{}, refuse(RefusalMalformed, RuntimesFile, "runtimes")
	}

	catalog := Catalog{}
	for rawID, rawValue := range rawRuntimes {
		entry, ok := table(rawValue)
		if !ok {
			return Catalog{}, refuse(RefusalMalformed, RuntimesFile, "runtime")
		}
		id, err := vendorplugin.NormalizeRuntimeID(rawID)
		if err != nil {
			return Catalog{}, refuse(RefusalMalformed, RuntimesFile, "runtime")
		}
		if runtimeExists(catalog.Runtimes, id) {
			return Catalog{}, refuse(RefusalConflicting, RuntimesFile, string(id))
		}
		rawSystem, ok := entry["system"].(string)
		if !ok || strings.TrimSpace(rawSystem) == "" {
			return Catalog{}, refuse(RefusalMalformed, RuntimesFile, string(id))
		}
		system, err := agentic.NormalizeSystemID(rawSystem)
		if err != nil {
			return Catalog{}, refuse(RefusalMalformed, RuntimesFile, string(id))
		}
		runtime := Runtime{ID: id, System: system, Fields: without(entry, "models", "engine")}
		if rawEngine, exists := entry["engine"]; exists {
			runtime.Engine, err = parseEngineReference(rawEngine, RuntimesFile, string(id))
			if err != nil {
				return Catalog{}, err
			}
		}
		if rawModels, exists := entry["models"]; exists {
			runtime.Models, err = parseModels(rawModels, string(id))
			if err != nil {
				return Catalog{}, err
			}
		}
		catalog.Runtimes = append(catalog.Runtimes, runtime)
	}
	return catalog, nil
}

func ParseEngineCatalog(data []byte) (EngineCatalog, error) {
	document, err := parseDocument(EnginesFile, data)
	if err != nil {
		return EngineCatalog{}, err
	}
	rawEngines, ok := table(document["engines"])
	if !ok {
		return EngineCatalog{}, refuse(RefusalMalformed, EnginesFile, "engines")
	}
	catalog := EngineCatalog{}
	for name, rawValue := range rawEngines {
		entry, ok := table(rawValue)
		if !ok || strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) {
			return EngineCatalog{}, refuse(RefusalMalformed, EnginesFile, "engine")
		}
		kind, kindOK := entry["engine"].(string)
		if !kindOK || strings.TrimSpace(kind) == "" || kind != strings.TrimSpace(kind) {
			return EngineCatalog{}, refuse(RefusalMalformed, EnginesFile, name)
		}
		catalog.Entries = append(catalog.Entries, EngineEntry{Name: name, Kind: strings.TrimSpace(kind)})
	}
	return catalog, nil
}

func ParseBindingCatalog(data []byte) (BindingCatalog, error) {
	document, err := parseDocument(ModelsFile, data)
	if err != nil {
		return BindingCatalog{}, err
	}
	if err := requireSchemaVersion(document, ModelsFile); err != nil {
		return BindingCatalog{}, err
	}
	catalog := BindingCatalog{}
	rawBindings, exists := document["bindings"]
	if !exists {
		return catalog, nil
	}
	bindings, ok := table(rawBindings)
	if !ok {
		return BindingCatalog{}, refuse(RefusalMalformed, ModelsFile, "bindings")
	}
	for role, rawValue := range bindings {
		if strings.TrimSpace(role) == "" {
			return BindingCatalog{}, refuse(RefusalMalformed, ModelsFile, "role")
		}
		entry, ok := table(rawValue)
		if !ok {
			return BindingCatalog{}, refuse(RefusalMalformed, ModelsFile, role)
		}
		binding := Binding{Role: role}
		for field, target := range map[string]*string{"run": &binding.Run, "profile": &binding.Profile} {
			if rawField, found := entry[field]; found {
				value, ok := rawField.(string)
				if !ok {
					return BindingCatalog{}, refuse(RefusalMalformed, ModelsFile, role)
				}
				if field == "run" {
					run := strings.TrimSpace(value)
					if run != "" {
						if _, err := vendorplugin.NormalizeRuntimeID(run); err != nil {
							return BindingCatalog{}, refuse(RefusalMalformed, ModelsFile, role)
						}
					}
					*target = run
				} else {
					*target = value
				}
			}
		}
		catalog.Bindings = append(catalog.Bindings, binding)
	}
	return catalog, nil
}

func parseModels(raw any, runtimeID string) ([]Model, error) {
	models, ok := table(raw)
	if !ok {
		return nil, refuse(RefusalMalformed, RuntimesFile, runtimeID)
	}
	result := make([]Model, 0, len(models))
	for rawID, rawValue := range models {
		entry, ok := table(rawValue)
		if !ok {
			return nil, refuse(RefusalMalformed, RuntimesFile, runtimeID)
		}
		id := vendorplugin.ModelID(rawID)
		if err := vendorplugin.ValidateModelID(id); err != nil {
			return nil, refuse(RefusalMalformed, RuntimesFile, runtimeID)
		}
		model := Model{ID: id, Fields: without(entry, "engine")}
		if rawEngine, exists := entry["engine"]; exists {
			model.Engine, _ = parseEngineReference(rawEngine, RuntimesFile, runtimeID)
			if model.Engine == nil {
				return nil, refuse(RefusalMalformed, RuntimesFile, runtimeID)
			}
		}
		if rawWindow, exists := entry["context_window_tokens"]; exists {
			window, ok := integer(rawWindow)
			if !ok || window < 0 {
				return nil, refuse(RefusalMalformed, RuntimesFile, runtimeID)
			}
		}
		if rawBudget, exists := entry["cache_budget_bytes"]; exists {
			budget, ok := integer(rawBudget)
			if !ok || budget <= 0 {
				return nil, refuse(RefusalMalformed, RuntimesFile, runtimeID)
			}
		}
		if rawEffort, exists := entry["effort_support"]; exists {
			effort, ok := rawEffort.(string)
			if !ok || (effort != "none" && effort != "required") {
				return nil, refuse(RefusalMalformed, RuntimesFile, runtimeID)
			}
		}
		result = append(result, model)
	}
	return result, nil
}

func parseEngineReference(raw any, file, subject string) (*EngineReference, error) {
	entry, ok := table(raw)
	if !ok {
		return nil, refuse(RefusalMalformed, file, subject)
	}
	pluginID, pluginOK := entry["plugin"].(string)
	name, nameOK := entry["name"].(string)
	if !pluginOK || !nameOK || strings.TrimSpace(pluginID) == "" || strings.TrimSpace(name) == "" || pluginID != strings.TrimSpace(pluginID) || name != strings.TrimSpace(name) {
		return nil, refuse(RefusalMalformed, file, subject)
	}
	return &EngineReference{Plugin: pluginID, Name: name}, nil
}

func parseDocument(file string, data []byte) (map[string]any, error) {
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil || document == nil {
		return nil, refuse(RefusalMalformed, file, "")
	}
	if err := rejectCredentialKeys(document, file); err != nil {
		return nil, err
	}
	return document, nil
}

func requireSchemaVersion(document map[string]any, file string) error {
	version, ok := integer(document["schema_version"])
	if !ok {
		return refuse(RefusalMalformed, file, "schema_version")
	}
	if version > SchemaVersion {
		return refuse(RefusalUnsupported, file, "schema_version")
	}
	if version != SchemaVersion {
		return refuse(RefusalMalformed, file, "schema_version")
	}
	return nil
}

func runtimeExists(runtimes []Runtime, id vendorplugin.RuntimeID) bool {
	for _, runtime := range runtimes {
		if runtime.ID == id {
			return true
		}
	}
	return false
}

func table(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	if ok {
		return result, true
	}
	result, ok = value.(map[string]interface{})
	return result, ok
}

func integer(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	default:
		return 0, false
	}
}

func without(values map[string]any, excluded ...string) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		if contains(excluded, key) {
			continue
		}
		result[key] = value
	}
	return result
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
