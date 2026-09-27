package runtimecatalog

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PreflightAt reads the three operator catalogs and returns a row for each
// requested role. Missing operator files become visible unbound rows; unreadable
// and malformed files return typed refusals and never fall back to legacy data.
func PreflightAt(directory string, roles []string, supportsSystem SystemSupport) (PreflightResult, error) {
	runtimeBytes, runtimesPresent, err := readOptional(directory, RuntimesFile)
	if err != nil {
		return PreflightResult{}, err
	}
	var runtimes Catalog
	if runtimesPresent {
		runtimes, err = ParseRuntimeCatalog(runtimeBytes)
		if err != nil {
			return PreflightResult{}, err
		}
	}

	engineBytes, enginesPresent, err := readOptional(directory, EnginesFile)
	if err != nil {
		return PreflightResult{}, err
	}
	var engines EngineCatalog
	if enginesPresent {
		engines, err = ParseEngineCatalog(engineBytes)
		if err != nil {
			return PreflightResult{}, err
		}
	}

	modelBytes, modelsPresent, err := readOptional(directory, ModelsFile)
	if err != nil {
		return PreflightResult{}, err
	}
	var bindings BindingCatalog
	if modelsPresent {
		bindings, err = ParseBindingCatalog(modelBytes)
		if err != nil {
			return PreflightResult{}, err
		}
	}

	requested := uniqueRoles(roles)
	if len(requested) == 0 && modelsPresent {
		for _, binding := range bindings.Bindings {
			requested = append(requested, binding.Role)
		}
		sort.Strings(requested)
	}
	result := PreflightResult{Rows: make([]PreflightRow, 0, len(requested))}
	for _, role := range requested {
		row := PreflightRow{Role: role, State: RowUnbound}
		switch {
		case !modelsPresent:
			row.Reason = "models.toml is absent"
			row.Refusal = &Refusal{Kind: RefusalAbsent, File: ModelsFile, Subject: role}
		case !runtimesPresent:
			row.Reason = "runtimes.toml is absent"
			row.Refusal = &Refusal{Kind: RefusalAbsent, File: RuntimesFile, Subject: role}
		default:
			binding, found := bindings.Binding(role)
			if !found {
				row.Reason = "role has no binding table"
				row.Refusal = &Refusal{Kind: RefusalUnbound, File: ModelsFile, Subject: role}
				break
			}
			row.Run = binding.Run
			row.Profile = binding.Profile
			row.Provenance = Provenance{
				BindingSource:        ModelsFile,
				BindingSchemaVersion: SchemaVersion,
				RuntimeSource:        RuntimesFile,
				RuntimeSchemaVersion: SchemaVersion,
			}
			if strings.TrimSpace(binding.Run) == "" || strings.TrimSpace(binding.Profile) == "" {
				row.Reason = "role binding is incomplete"
				row.Refusal = &Refusal{Kind: RefusalUnbound, File: ModelsFile, Subject: role}
				break
			}
			runtime, found := runtimes.Runtime(binding.Run)
			if !found {
				row.Reason = "bound runtime is not present on this machine"
				row.Refusal = &Refusal{Kind: RefusalUnbound, File: RuntimesFile, Subject: binding.Run}
				break
			}
			if supportsSystem == nil || !supportsSystem(runtime.System) {
				row.Reason = "runtime system is not supported by this consumer"
				row.Refusal = &Refusal{Kind: RefusalUnsupported, File: RuntimesFile, Subject: string(runtime.System)}
				break
			}
			engineRefs := runtimeEngineReferences(runtime)
			for _, reference := range engineRefs {
				row.Provenance.EngineReferences = append(row.Provenance.EngineReferences, *reference)
				row.Provenance.EngineSource = EnginesFile
				entry, found := engines.Entry(reference.Name)
				if !enginesPresent || !found {
					row.Reason = "referenced engine entry is not present on this machine"
					row.Refusal = &Refusal{Kind: RefusalUnbound, File: EnginesFile, Subject: reference.Name}
					break
				}
				if entry.Kind == "" || entry.Kind != reference.Plugin {
					row.Reason = "engine entry kind does not match the runtime reference"
					row.Refusal = &Refusal{Kind: RefusalUnsupported, File: EnginesFile, Subject: reference.Name}
					break
				}
			}
			if row.Refusal == nil {
				row.State = RowBound
			}
		}
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

// ResolveAt is the strict production resolver. Discovery can show an unbound
// row; a caller requesting that row receives its typed refusal.
func ResolveAt(directory, role string, supportsSystem SystemSupport) (Binding, Provenance, error) {
	result, err := PreflightAt(directory, []string{role}, supportsSystem)
	if err != nil {
		return Binding{}, Provenance{}, err
	}
	if len(result.Rows) != 1 {
		return Binding{}, Provenance{}, &Refusal{Kind: RefusalUnbound, File: ModelsFile, Subject: role}
	}
	row := result.Rows[0]
	if row.Refusal != nil {
		return Binding{}, row.Provenance, row.Refusal
	}
	if row.State != RowBound {
		return Binding{}, row.Provenance, &Refusal{Kind: RefusalUnbound, File: ModelsFile, Subject: role}
	}
	return Binding{Role: row.Role, Run: row.Run, Profile: row.Profile}, row.Provenance, nil
}

func runtimeEngineReferences(runtime Runtime) []*EngineReference {
	result := make([]*EngineReference, 0, len(runtime.Models)+1)
	if runtime.Engine != nil {
		result = append(result, runtime.Engine)
	}
	for _, model := range runtime.Models {
		if model.Engine != nil {
			result = append(result, model.Engine)
		}
	}
	return result
}

func readOptional(directory, file string) ([]byte, bool, error) {
	path := filepath.Join(directory, file)
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

func uniqueRoles(roles []string) []string {
	result := make([]string, 0, len(roles))
	for _, role := range roles {
		if strings.TrimSpace(role) == "" || contains(result, role) {
			continue
		}
		result = append(result, role)
	}
	sort.Strings(result)
	return result
}
