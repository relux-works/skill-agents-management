package runtimecatalog

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	localmodels "github.com/relux-works/skill-agents-management/pkg/vendorplugin/vendors/local-models"
)

type ImportPlan struct {
	Runtimes []byte
	Models   []byte
	Result   ImportResult
}

// PlanImport performs a read-only, deterministic migration plan. The legacy
// local-models source supplies runtime/model declarations; the legacy operator
// TOML supplies bindings and other model policy. Equal values are retained and
// disagreement refuses instead of overwriting either source.
func PlanImport(currentRuntimes, currentModels, engines, localModels, legacyConfig []byte) (ImportPlan, error) {
	result := ImportResult{}
	engineCatalog := EngineCatalog{}
	if engines != nil {
		parsed, err := ParseEngineCatalog(engines)
		if err != nil {
			return ImportPlan{}, err
		}
		engineCatalog = parsed
	}

	runtimeDocument, runtimePresent, err := currentDocument(currentRuntimes, RuntimesFile, true)
	if err != nil {
		return ImportPlan{}, err
	}
	if !runtimePresent {
		runtimeDocument = map[string]any{"schema_version": int64(SchemaVersion), "runtimes": map[string]any{}}
	}
	if _, err := parseRuntimeCatalogDocument(runtimeDocument); err != nil && runtimePresent {
		return ImportPlan{}, err
	}

	modelsDocument, modelsPresent, err := currentDocument(currentModels, ModelsFile, false)
	if err != nil {
		return ImportPlan{}, err
	}
	if modelsPresent {
		if _, exists := modelsDocument["schema_version"]; !exists {
			modelsDocument["schema_version"] = int64(SchemaVersion)
		} else if err := requireSchemaVersion(modelsDocument, ModelsFile); err != nil {
			return ImportPlan{}, err
		}
	} else {
		modelsDocument = map[string]any{"schema_version": int64(SchemaVersion)}
	}

	sourceCount := 0
	if localModels != nil {
		sourceCount++
		localSource, err := parseDocument("local-models.toml", localModels)
		if err != nil {
			return ImportPlan{}, err
		}
		converted, importedIDs, unbound, err := convertLocalModels(localModels, engineCatalog)
		if err != nil {
			return ImportPlan{}, err
		}
		if err := mergeRuntimeDocument(runtimeDocument, converted); err != nil {
			return ImportPlan{}, err
		}
		localPolicy := without(localSource, "schema_version", "inference_engines", "runtimes")
		if err := mergeDocument(modelsDocument, localPolicy, ModelsFile, ""); err != nil {
			return ImportPlan{}, err
		}
		result.RuntimeIDs = append(result.RuntimeIDs, importedIDs...)
		result.Unbound = append(result.Unbound, unbound...)
		result.SourceNames = append(result.SourceNames, "local-models.toml")
	}

	if legacyConfig != nil {
		sourceCount++
		legacy, err := parseDocument("legacy-config.toml", legacyConfig)
		if err != nil {
			return ImportPlan{}, err
		}
		if err := validateOptionalLegacyVersion(legacy, "legacy-config.toml"); err != nil {
			return ImportPlan{}, err
		}
		if _, exists := legacy["runtimes"]; exists {
			if _, versioned := legacy["schema_version"]; versioned {
				legacyRuntimeDoc := map[string]any{
					"schema_version": legacy["schema_version"],
					"runtimes":       legacy["runtimes"],
				}
				if err := requireSchemaVersion(legacyRuntimeDoc, "legacy-config.toml"); err != nil {
					return ImportPlan{}, err
				}
				if _, err := parseRuntimeCatalogDocument(legacyRuntimeDoc); err != nil {
					return ImportPlan{}, err
				}
				if err := mergeRuntimeDocument(runtimeDocument, legacyRuntimeDoc); err != nil {
					return ImportPlan{}, err
				}
			} else {
				legacyRuntimeDoc, importedIDs, unbound, err := convertLocalModelsDocument(legacy, engineCatalog)
				if err != nil {
					return ImportPlan{}, err
				}
				if err := mergeRuntimeDocument(runtimeDocument, legacyRuntimeDoc); err != nil {
					return ImportPlan{}, err
				}
				result.RuntimeIDs = append(result.RuntimeIDs, importedIDs...)
				result.Unbound = append(result.Unbound, unbound...)
			}
		}
		modelsSource := without(legacy, "schema_version", "runtimes", "inference_engines")
		if err := mergeDocument(modelsDocument, modelsSource, ModelsFile, ""); err != nil {
			return ImportPlan{}, err
		}
		result.SourceNames = append(result.SourceNames, "legacy-config.toml")
	}

	if sourceCount == 0 {
		if !runtimePresent && !modelsPresent {
			return ImportPlan{}, refuse(RefusalAbsent, RuntimesFile, "import sources")
		}
		result.Noop = true
		return ImportPlan{Result: result}, nil
	}

	if _, err := parseRuntimeCatalogDocument(runtimeDocument); err != nil {
		return ImportPlan{}, err
	}
	if _, err := parseBindingCatalogDocument(modelsDocument); err != nil {
		return ImportPlan{}, err
	}
	result.Unbound = uniqueRoles(result.Unbound)
	result.RuntimeIDs = uniqueRoles(result.RuntimeIDs)
	result.SourceNames = uniqueRoles(result.SourceNames)

	runtimeBytes, err := toml.Marshal(runtimeDocument)
	if err != nil {
		return ImportPlan{}, refuse(RefusalMalformed, RuntimesFile, "")
	}
	modelsBytes, err := toml.Marshal(modelsDocument)
	if err != nil {
		return ImportPlan{}, refuse(RefusalMalformed, ModelsFile, "")
	}
	if !runtimePresent || !reflect.DeepEqual(runtimeDocument, mustParseDocument(currentRuntimes, RuntimesFile)) {
		result.RuntimesChanged = true
	}
	if !modelsPresent || !reflect.DeepEqual(modelsDocument, mustParseDocument(currentModels, ModelsFile)) {
		result.ModelsChanged = true
	}
	result.Noop = !result.RuntimesChanged && !result.ModelsChanged
	plan := ImportPlan{Result: result}
	if result.RuntimesChanged {
		plan.Runtimes = runtimeBytes
	} else {
		plan.Runtimes = append([]byte(nil), currentRuntimes...)
	}
	if result.ModelsChanged {
		plan.Models = modelsBytes
	} else if modelsPresent {
		plan.Models = append([]byte(nil), currentModels...)
	}
	return plan, nil
}

// ImportAt reads the explicit legacy sources, plans the migration, then stages
// and replaces changed catalogs with mode 0600. Source files are never modified.
func ImportAt(directory, localModelsPath, legacyConfigPath string) (ImportResult, error) {
	currentRuntimes, runtimesPresent, err := readOptional(directory, RuntimesFile)
	if err != nil {
		return ImportResult{}, err
	}
	currentModels, modelsPresent, err := readOptional(directory, ModelsFile)
	if err != nil {
		return ImportResult{}, err
	}
	engines, enginesPresent, err := readOptional(directory, EnginesFile)
	if err != nil {
		return ImportResult{}, err
	}
	localModels, localModelsPresent, err := readOptionalPath(localModelsPath, "local-models.toml")
	if err != nil {
		return ImportResult{}, err
	}
	legacyConfig, legacyPresent, err := readOptionalPath(legacyConfigPath, "legacy-config.toml")
	if err != nil {
		return ImportResult{}, err
	}
	if !runtimesPresent {
		currentRuntimes = nil
	}
	if !modelsPresent {
		currentModels = nil
	}
	if !enginesPresent {
		engines = nil
	}
	if !localModelsPresent {
		localModels = nil
	}
	if !legacyPresent {
		legacyConfig = nil
	}
	if err := ensureSourcesDistinct(directory, localModelsPath, legacyConfigPath); err != nil {
		return ImportResult{}, err
	}
	plan, err := PlanImport(currentRuntimes, currentModels, engines, localModels, legacyConfig)
	if err != nil {
		return ImportResult{}, err
	}
	if plan.Result.Noop {
		return plan.Result, nil
	}
	files := make(map[string][]byte, 2)
	if plan.Result.RuntimesChanged {
		files[RuntimesFile] = plan.Runtimes
	}
	if plan.Result.ModelsChanged {
		files[ModelsFile] = plan.Models
	}
	if err := writeAtomic(directory, files); err != nil {
		return ImportResult{}, err
	}
	return plan.Result, nil
}

func currentDocument(data []byte, file string, strict bool) (map[string]any, bool, error) {
	if data == nil {
		return nil, false, nil
	}
	document, err := parseDocument(file, data)
	if err != nil {
		return nil, false, err
	}
	if strict {
		if err := requireSchemaVersion(document, file); err != nil {
			return nil, false, err
		}
	}
	return document, true, nil
}

func parseRuntimeCatalogDocument(document map[string]any) (Catalog, error) {
	encoded, err := toml.Marshal(document)
	if err != nil {
		return Catalog{}, refuse(RefusalMalformed, RuntimesFile, "")
	}
	return ParseRuntimeCatalog(encoded)
}

func parseBindingCatalogDocument(document map[string]any) (BindingCatalog, error) {
	encoded, err := toml.Marshal(document)
	if err != nil {
		return BindingCatalog{}, refuse(RefusalMalformed, ModelsFile, "")
	}
	return ParseBindingCatalog(encoded)
}

func convertLocalModels(data []byte, engines EngineCatalog) (map[string]any, []string, []string, error) {
	document, err := parseDocument("local-models.toml", data)
	if err != nil {
		return nil, nil, nil, err
	}
	return convertLocalModelsDocument(document, engines)
}

func convertLocalModelsDocument(document map[string]any, engines EngineCatalog) (map[string]any, []string, []string, error) {
	rawRuntimes, ok := table(document["runtimes"])
	if !ok {
		return nil, nil, nil, refuse(RefusalMalformed, "local-models.toml", "runtimes")
	}
	seenIDs := make([]vendorplugin.RuntimeID, 0, len(rawRuntimes))
	for rawID := range rawRuntimes {
		id, err := vendorplugin.NormalizeRuntimeID(rawID)
		if err != nil {
			return nil, nil, nil, refuse(RefusalMalformed, "local-models.toml", "runtime")
		}
		if runtimeIDIn(seenIDs, id) {
			return nil, nil, nil, refuse(RefusalConflicting, "local-models.toml", string(id))
		}
		seenIDs = append(seenIDs, id)
	}
	encoded, err := toml.Marshal(document)
	if err != nil {
		return nil, nil, nil, refuse(RefusalMalformed, "local-models.toml", "")
	}
	parsed, err := localmodels.ParseConfig(encoded)
	if err != nil {
		return nil, nil, nil, refuse(RefusalMalformed, "local-models.toml", "")
	}
	convertedRuntimes := map[string]any{}
	var runtimeIDs []string
	var unbound []string
	for _, runtime := range parsed.Runtimes {
		rawRuntime, found := matchingRuntimeTable(rawRuntimes, string(runtime.ID))
		if !found {
			return nil, nil, nil, refuse(RefusalMalformed, "local-models.toml", string(runtime.ID))
		}
		runtimeDoc, ok := table(rawRuntime)
		if !ok {
			return nil, nil, nil, refuse(RefusalMalformed, "local-models.toml", string(runtime.ID))
		}
		newRuntime := without(runtimeDoc, "engine", "models")
		newRuntime["system"] = string(runtime.System)
		newModels := map[string]any{}
		rawModels, ok := table(runtimeDoc["models"])
		if !ok {
			return nil, nil, nil, refuse(RefusalMalformed, "local-models.toml", string(runtime.ID))
		}
		for modelID, model := range runtime.Models {
			rawModel, exists := rawModels[string(modelID)]
			modelDoc, valid := table(rawModel)
			if !exists || !valid {
				return nil, nil, nil, refuse(RefusalMalformed, "local-models.toml", string(runtime.ID))
			}
			newModel := without(modelDoc, "engine", "pointer")
			effectiveEngine := model.Engine
			if effectiveEngine.ID == "" {
				effectiveEngine = runtime.Engine
			}
			if effectiveEngine.ID == "" {
				if strings.TrimSpace(model.Pointer.AgentsInfraProfile) != "" {
					unbound = append(unbound, string(runtime.ID)+"/"+string(modelID))
				}
			} else {
				engineName := model.Pointer.AgentsInfraProfile
				newModel["engine"] = map[string]any{"plugin": string(effectiveEngine.ID), "name": engineName}
				entry, found := engines.Entry(engineName)
				if !found {
					unbound = append(unbound, string(runtime.ID)+"/"+string(modelID))
				} else if entry.Kind != string(effectiveEngine.ID) {
					return nil, nil, nil, refuse(RefusalUnsupported, EnginesFile, engineName)
				}
			}
			newModels[string(modelID)] = newModel
		}
		newRuntime["models"] = newModels
		convertedRuntimes[string(runtime.ID)] = newRuntime
		runtimeIDs = append(runtimeIDs, string(runtime.ID))
	}
	converted := map[string]any{"schema_version": int64(SchemaVersion), "runtimes": convertedRuntimes}
	return converted, runtimeIDs, unbound, nil
}

func runtimeIDIn(ids []vendorplugin.RuntimeID, expected vendorplugin.RuntimeID) bool {
	for _, id := range ids {
		if id == expected {
			return true
		}
	}
	return false
}

func mergeRuntimeDocument(destination, source map[string]any) error {
	destinationRuntimes, ok := table(destination["runtimes"])
	if !ok {
		return refuse(RefusalMalformed, RuntimesFile, "runtimes")
	}
	sourceRuntimes, ok := table(source["runtimes"])
	if !ok {
		return refuse(RefusalMalformed, RuntimesFile, "runtimes")
	}
	for rawID, sourceValue := range sourceRuntimes {
		id, err := vendorplugin.NormalizeRuntimeID(rawID)
		if err != nil {
			return refuse(RefusalMalformed, RuntimesFile, "runtime")
		}
		destinationKey := ""
		for currentID := range destinationRuntimes {
			current, normalizeErr := vendorplugin.NormalizeRuntimeID(currentID)
			if normalizeErr == nil && current == id {
				destinationKey = currentID
				break
			}
		}
		if destinationKey == "" {
			destinationRuntimes[string(id)] = deepClone(sourceValue)
			continue
		}
		currentTable, currentOK := table(destinationRuntimes[destinationKey])
		sourceTable, sourceOK := table(sourceValue)
		if !currentOK || !sourceOK {
			return refuse(RefusalMalformed, RuntimesFile, string(id))
		}
		if err := mergeDocument(currentTable, sourceTable, RuntimesFile, string(id)); err != nil {
			return err
		}
	}
	for key, value := range source {
		if key == "schema_version" || key == "runtimes" {
			continue
		}
		if err := mergeValue(destination, key, value, RuntimesFile, key); err != nil {
			return err
		}
	}
	destination["schema_version"] = int64(SchemaVersion)
	return nil
}

func mergeDocument(destination, source map[string]any, file, subject string) error {
	for key, value := range source {
		if key == "schema_version" {
			continue
		}
		if err := mergeValue(destination, key, value, file, subject+key); err != nil {
			return err
		}
	}
	return nil
}

func mergeValue(destination map[string]any, key string, source any, file, subject string) error {
	current, exists := destination[key]
	if !exists {
		destination[key] = deepClone(source)
		return nil
	}
	currentTable, currentIsTable := table(current)
	sourceTable, sourceIsTable := table(source)
	if currentIsTable && sourceIsTable {
		return mergeDocument(currentTable, sourceTable, file, subject)
	}
	if reflect.DeepEqual(current, source) {
		return nil
	}
	return refuse(RefusalConflicting, file, subject)
}

func validateOptionalLegacyVersion(document map[string]any, file string) error {
	raw, exists := document["schema_version"]
	if !exists {
		return nil
	}
	version, ok := integer(raw)
	if !ok {
		return refuse(RefusalMalformed, file, "schema_version")
	}
	if version > SchemaVersion {
		return refuse(RefusalUnsupported, file, "schema_version")
	}
	if version < 1 {
		return refuse(RefusalMalformed, file, "schema_version")
	}
	return nil
}

func matchingRuntimeTable(runtimes map[string]any, expected string) (any, bool) {
	normalized, err := vendorplugin.NormalizeRuntimeID(expected)
	if err != nil {
		return nil, false
	}
	for rawID, value := range runtimes {
		current, normalizeErr := vendorplugin.NormalizeRuntimeID(rawID)
		if normalizeErr == nil && current == normalized {
			return value, true
		}
	}
	return nil, false
}

func mustParseDocument(data []byte, file string) map[string]any {
	if data == nil {
		return nil
	}
	document, _ := parseDocument(file, data)
	return document
}

func deepClone(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = deepClone(child)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = deepClone(child)
		}
		return result
	default:
		return typed
	}
}

func readOptionalPath(path, file string) ([]byte, bool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, false, nil
	}
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, &Refusal{Kind: RefusalReadFailed, File: file}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, &Refusal{Kind: RefusalReadFailed, File: file}
	}
	return data, true, nil
}

func ensureSourcesDistinct(directory string, sources ...string) error {
	for _, source := range sources {
		if strings.TrimSpace(source) == "" {
			continue
		}
		sourceInfo, err := os.Stat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return &Refusal{Kind: RefusalReadFailed, File: filepath.Base(source)}
		}
		for _, destination := range []string{RuntimesFile, ModelsFile} {
			destinationInfo, err := os.Stat(filepath.Join(directory, destination))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return &Refusal{Kind: RefusalReadFailed, File: destination}
			}
			if os.SameFile(sourceInfo, destinationInfo) {
				return refuse(RefusalConflicting, filepath.Base(source), destination)
			}
		}
	}
	return nil
}

func writeAtomic(directory string, files map[string][]byte) error {
	if len(files) == 0 {
		return nil
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return refuse(RefusalReadFailed, "operator-files", "directory")
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	type stagedFile struct{ name, temp string }
	staged := make([]stagedFile, 0, len(names))
	cleanup := func() {
		for _, file := range staged {
			_ = os.Remove(file.temp)
		}
	}
	for _, name := range names {
		file, err := os.CreateTemp(directory, ".runtimecatalog-*")
		if err != nil {
			cleanup()
			return refuse(RefusalReadFailed, name, "staging")
		}
		if _, err = file.Write(files[name]); err == nil {
			err = file.Chmod(0o600)
		}
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(file.Name())
			cleanup()
			return refuse(RefusalReadFailed, name, "staging")
		}
		staged = append(staged, stagedFile{name: name, temp: file.Name()})
	}
	for index, file := range staged {
		if err := os.Rename(file.temp, filepath.Join(directory, file.name)); err != nil {
			for _, pending := range staged[index:] {
				_ = os.Remove(pending.temp)
			}
			return refuse(RefusalReadFailed, file.name, "replace")
		}
	}
	return nil
}
