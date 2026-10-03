package codex

import (
	_ "embed"
	"encoding/json"
)

// Native schema is pinned to Codex 0.159 ModelInfo and nested messages. See
// native-schema.json for the public source and README for the accepted subset.
// Unknown fields are ignored by native serde; known unsupported complex fields
// are accepted only absent/null, so none can evade native type validation.
//
//go:embed native-schema.json
var nativeSchemaBytes []byte

type nativeField struct {
	Type     string `json:"type"`
	Required bool   `json:"required"`
}
type nativeSchema struct {
	Structs     map[string]map[string]nativeField `json:"structs"`
	Enums       map[string][]string               `json:"enums"`
	Unsupported []string                          `json:"unsupported"`
}

var catalogSchema = func() nativeSchema {
	var schema nativeSchema
	if err := json.Unmarshal(nativeSchemaBytes, &schema); err != nil {
		panic(err)
	}
	return schema
}()

// The schema walk lives in exact_catalog.go and runs over the single
// occurrence-preserving parse. Map and struct decodes of catalog bytes are
// gone: both collapse duplicate keys, and struct fields fold case, so neither
// can establish what native serde will read.
