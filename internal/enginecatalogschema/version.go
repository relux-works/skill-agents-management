// Package enginecatalogschema shares the engine catalog's version contract
// between readers while leaving their refusal types and parsing independent.
package enginecatalogschema

// ValidVersion accepts an omitted schema_version or exactly TOML integer 1.
// It expects a document decoded by go-toml, which represents integers as int64.
func ValidVersion(document map[string]any) bool {
	value, present := document["schema_version"]
	if !present {
		return true
	}
	version, ok := value.(int64)
	return ok && version == 1
}
