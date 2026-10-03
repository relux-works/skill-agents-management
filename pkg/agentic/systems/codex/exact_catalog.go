// The catalog has exactly one decode: the token-level exact-key parse below.
// Native Codex deserializes model metadata with serde, which matches struct
// fields byte-exactly (case-sensitively) and rejects a repeated recognized
// field ("duplicate field"), while unknown fields are ignored. Go's
// encoding/json struct and map decoders disagree on both points: struct
// fields fold case, and both collapse duplicates to the last value. Decoding
// catalog bytes a second time with either semantic would re-admit what native
// refuses (Q1) or override protocol flags through case aliases native ignores
// (Q2). Both the native-validity check and the selected-row carriers derive
// from this single parse; production performs no other JSON decode of
// catalog bytes.
package codex

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

// exactMaxDepth caps object/array nesting accepted by the catalog parse. It
// matches serde_json's default recursion limit (128), so anything refused
// here is also unloadable by native Codex.
const exactMaxDepth = 128

// exactField is one key occurrence in arrival order. Duplicates are kept, not
// collapsed, so a repeated recognized field stays observable.
type exactField struct {
	key   string
	value any
}

// exactObject is one JSON object with ordered, occurrence-preserving fields.
// Values are nil, bool, string, json.Number, []any or *exactObject.
type exactObject struct {
	fields []exactField
}

// exactKeyEqual matches a catalog key against a recognized field name with
// byte-exact, case-sensitive semantics, the way native serde matches struct
// fields. Every recognized-key lookup in this file goes through it.
func exactKeyEqual(key, name string) bool {
	return key == name
}

// exactValidatedCatalogModels is the single catalog decode: it parses data
// once with exact keys, validates the whole catalog against the pinned
// native schema (including duplicate recognized fields at every scope), and
// builds one carrier per row from exact-case keys only. ok is false for
// malformed JSON, over-deep nesting, trailing data, native-invalid rows, and
// duplicate recognized fields; unknown keys are ignored exactly as native
// serde ignores them, including repeated unknown keys.
func exactValidatedCatalogModels(data []byte) ([]codexCatalogModel, bool) {
	root, ok := exactParseCatalog(data)
	if !ok || !exactNativeValid(root) {
		return nil, false
	}
	return exactCatalogModels(root), true
}

// exactParseCatalog decodes one top-level JSON object with a token reader,
// preserving key occurrence and exact case. Numbers keep their literal text
// via UseNumber so integer bounds check the same way as before.
func exactParseCatalog(data []byte) (*exactObject, bool) {
	// Token replaces invalid UTF-8 and unpaired surrogate escapes with U+FFFD.
	// Validate the original spelling before that lossy normalization; the
	// bytes retained by snapshots must be the same bytes native can load.
	if !exactCatalogStringsValid(data) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, false
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, false
	}
	root, ok := exactParseObjectBody(decoder, 1)
	if !ok {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	return root, true
}

func exactParseObjectBody(decoder *json.Decoder, depth int) (*exactObject, bool) {
	if depth > exactMaxDepth {
		return nil, false
	}
	root := &exactObject{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, false
		}
		key, ok := token.(string)
		if !ok {
			return nil, false
		}
		value, ok := exactParseValue(decoder, depth)
		if !ok {
			return nil, false
		}
		root.fields = append(root.fields, exactField{key: key, value: value})
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, false
	}
	if delim, ok := token.(json.Delim); !ok || delim != '}' {
		return nil, false
	}
	return root, true
}

func exactParseArray(decoder *json.Decoder, depth int) ([]any, bool) {
	if depth > exactMaxDepth {
		return nil, false
	}
	var list []any
	for decoder.More() {
		value, ok := exactParseValue(decoder, depth)
		if !ok {
			return nil, false
		}
		list = append(list, value)
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, false
	}
	if delim, ok := token.(json.Delim); !ok || delim != ']' {
		return nil, false
	}
	return list, true
}

func exactParseValue(decoder *json.Decoder, depth int) (any, bool) {
	token, err := decoder.Token()
	if err != nil {
		return nil, false
	}
	if delim, ok := token.(json.Delim); ok {
		switch delim {
		case '{':
			return exactParseObjectBody(decoder, depth+1)
		case '[':
			return exactParseArray(decoder, depth+1)
		default:
			return nil, false
		}
	}
	return token, true
}

// exactNativeValid checks the single parse against the pinned native schema
// over every row. At each typed scope a repeated recognized key is invalid,
// matching native "duplicate field" failures; unknown keys are skipped
// without validation, matching native serde's ignore behavior.
func exactNativeValid(root *exactObject) bool {
	if root == nil {
		return false
	}
	var models any
	found := false
	for _, field := range root.fields {
		if !exactKeyEqual(field.key, "models") {
			continue
		}
		if found {
			return false
		}
		found = true
		models = field.value
	}
	if !found {
		return false
	}
	list, ok := models.([]any)
	if !ok {
		return false
	}
	for _, row := range list {
		obj, ok := row.(*exactObject)
		if !ok || !exactStructValid(obj, "ModelInfo") {
			return false
		}
	}
	return true
}

// exactStructValid validates one object against one pinned struct: duplicate
// recognized keys refuse, required keys must be present exactly under their
// canonical spelling, and present recognized values must fit their types.
func exactStructValid(obj *exactObject, typ string) bool {
	if obj == nil {
		return false
	}
	fields, found := catalogSchema.Structs[typ]
	if !found {
		return false
	}
	seen := make(map[string]bool, len(fields))
	values := make(map[string]any, len(obj.fields))
	for _, field := range obj.fields {
		name, recognized := exactRecognizedField(fields, field.key)
		if !recognized {
			continue
		}
		if seen[name] {
			return false
		}
		seen[name] = true
		values[name] = field.value
	}
	for name, field := range fields {
		value, present := values[name]
		if !present {
			if field.Required {
				return false
			}
			continue
		}
		if !exactValueValid(value, field.Type) {
			return false
		}
	}
	return true
}

// exactRecognizedField returns the canonical schema name for a key, or false
// for unknown keys. Matching is exact and case-sensitive: a case variant of
// a recognized name is unknown, as it is to native serde.
func exactRecognizedField(fields map[string]nativeField, key string) (string, bool) {
	for name := range fields {
		if exactKeyEqual(key, name) {
			return name, true
		}
	}
	return "", false
}

// exactValueValid mirrors the pinned scalar, enum, container and struct
// checks over the single exact parse. Free-form string maps accept any data
// keys (duplicates overwrite as in serde map collection); only their values
// are type-checked.
func exactValueValid(value any, typ string) bool {
	if strings.HasPrefix(typ, "Option<") {
		return value == nil || exactValueValid(value, typ[7:len(typ)-1])
	}
	if strings.HasPrefix(typ, "Vec<") {
		list, ok := value.([]any)
		if !ok {
			return false
		}
		for _, item := range list {
			if !exactValueValid(item, typ[4:len(typ)-1]) {
				return false
			}
		}
		return true
	}
	switch typ {
	case "String":
		_, ok := value.(string)
		return ok
	case "ReasoningEffort":
		text, ok := value.(string)
		return ok && text != ""
	case "bool":
		_, ok := value.(bool)
		return ok
	case "i32", "i64":
		return exactIntValid(value, typ)
	case "u16", "usize":
		return exactUintValid(value, typ)
	case "DateTime<Utc>":
		_, ok := value.(string)
		return ok // native tolerates invalid timestamp strings
	case "std::collections::BTreeMap<String, String>":
		obj, ok := value.(*exactObject)
		if !ok {
			return false
		}
		for _, field := range obj.fields {
			if !exactValueValid(field.value, "String") {
				return false
			}
		}
		return true
	}
	if options, found := catalogSchema.Enums[typ]; found {
		text, ok := value.(string)
		if !ok {
			return false
		}
		for _, option := range options {
			if text == option {
				return true
			}
		}
		return false
	}
	if _, found := catalogSchema.Structs[typ]; !found {
		return false
	}
	obj, ok := value.(*exactObject)
	if !ok {
		return false
	}
	return exactStructValid(obj, typ)
}

func exactIntValid(value any, typ string) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	bits := 64
	if typ == "i32" {
		bits = 32
	}
	_, err := strconv.ParseInt(string(number), 10, bits)
	return err == nil
}

func exactUintValid(value any, typ string) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	bits := 64
	if typ == "u16" {
		bits = 16
	}
	_, err := strconv.ParseUint(string(number), 10, bits)
	return err == nil
}

// exactCatalogModels builds one carrier per row from the single validated
// parse, reading exact-case keys only. Callers run exactNativeValid first;
// mismatches below are unreachable and degrade to zero values the entry
// validator refuses as malformed, never to panics or guessed metadata.
func exactCatalogModels(root *exactObject) []codexCatalogModel {
	if root == nil {
		return nil
	}
	for _, field := range root.fields {
		if !exactKeyEqual(field.key, "models") {
			continue
		}
		list, ok := field.value.([]any)
		if !ok {
			return nil
		}
		models := make([]codexCatalogModel, 0, len(list))
		for _, row := range list {
			obj, ok := row.(*exactObject)
			if !ok {
				continue
			}
			models = append(models, exactCatalogModel(obj))
		}
		return models
	}
	return nil
}

// exactCatalogModel builds one row carrier field by field from exact-case
// keys. A missing key leaves the Go zero value (nil for the protocol
// booleans), which the entry validator reports as malformed.
func exactCatalogModel(obj *exactObject) codexCatalogModel {
	var model codexCatalogModel
	if slug, ok := exactStringField(obj, "slug"); ok {
		model.Slug = slug
	}
	if lite, ok := exactBoolField(obj, "use_responses_lite"); ok {
		value := lite
		model.UseResponsesLite = &value
	}
	if toolMode, ok := exactStringField(obj, "tool_mode"); ok {
		model.ToolMode = toolMode
	}
	if multiAgent, ok := exactStringField(obj, "multi_agent_version"); ok {
		model.MultiAgentVersion = multiAgent
	}
	if search, ok := exactBoolField(obj, "supports_search_tool"); ok {
		value := search
		model.SupportsSearchTool = &value
	}
	if summary, ok := exactBoolField(obj, "supports_reasoning_summary_parameter"); ok {
		value := summary
		model.SupportsReasoningSummaryParameter = &value
	}
	model.SupportedReasoningLevels = exactEffortLevels(obj)
	return model
}

func exactStringField(obj *exactObject, name string) (string, bool) {
	for _, field := range obj.fields {
		if exactKeyEqual(field.key, name) {
			value, ok := field.value.(string)
			return value, ok
		}
	}
	return "", false
}

func exactBoolField(obj *exactObject, name string) (bool, bool) {
	for _, field := range obj.fields {
		if exactKeyEqual(field.key, name) {
			value, ok := field.value.(bool)
			return value, ok
		}
	}
	return false, false
}

// exactEffortLevels reads the effort vocabulary from exact-case effort keys.
// A missing list, a non-object preset or a non-string effort degrades to nil
// or blank words the entry validator refuses as malformed.
func exactEffortLevels(obj *exactObject) []catalogEffortLevel {
	for _, field := range obj.fields {
		if !exactKeyEqual(field.key, "supported_reasoning_levels") {
			continue
		}
		list, ok := field.value.([]any)
		if !ok {
			return nil
		}
		levels := make([]catalogEffortLevel, 0, len(list))
		for _, item := range list {
			preset, ok := item.(*exactObject)
			if !ok {
				return nil
			}
			effort, ok := exactStringField(preset, "effort")
			if !ok {
				return nil
			}
			levels = append(levels, catalogEffortLevel{Effort: effort})
		}
		return levels
	}
	return nil
}
