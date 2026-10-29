package runtimecatalog

import (
	"sort"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// Model catalog, schema model-catalog/1.
//
// ~/.curator/catalog.toml lets the operator declare what can be launched as
// plain strings in a vendor > model > reasoning hierarchy, so a new model or
// reasoning word needs no code change in this module or its consumers. The
// file is ADDITIVE to the module's own declarations: it may add models to a
// registered vendor and add effort words to a model that already has an
// effort axis. It never removes, renames or narrows a declaration, and it
// never touches permission grammar, argv spellings or the frozen v2 snapshot.
//
// Admission is unchanged. A catalog row becomes an ordinary vendorplugin.Model,
// so an unknown model id is still ErrUnknownModel and an undeclared effort
// word is still refused by the model's own vocabulary. A row the catalog adds
// is OperatorDeclared, which keeps it out of every Lineup: it is used only
// when a caller names it.
//
// Every word a catalog row accepts must be on the effective effort scale: the
// module's EffortOrder extended by the catalog's [[effort_scale]] entries. That
// is what turns a misspelt word into a refusal instead of a new vocabulary
// entry. Each entry places one new word directly after a word already on the
// scale (the module's own or an earlier entry). An unknown or repeated
// position, or a word already on the scale, refuses the file.

// CatalogFile is the operator model catalog's file name under ~/.curator.
const CatalogFile = "catalog.toml"

// catalogEvidenceSource names the evidence an operator-declared row carries.
// It is provenance, never a score: the row is kept out of every Lineup.
const catalogEvidenceSource = "operator model catalog (catalog.toml)"

// ScaleEntry places one new effort word on the scale, directly after After.
type ScaleEntry struct {
	Word  string
	After string
}

// CatalogEffort is a catalog row's effort axis. A row extending a module
// declaration carries only Accepts.
type CatalogEffort struct {
	Support agentic.EffortSupport
	// SupportSet reports that the row spelled support; the enum's zero value
	// is EffortSupportNone and cannot also mean "not given".
	SupportSet  bool
	Accepts     []string
	Recommended string
}

// CatalogModel is one model row in the catalog.
type CatalogModel struct {
	ID          vendorplugin.ModelID
	Description string
	Systems     []agentic.SystemID
	Aliases     []vendorplugin.ModelID
	Lifecycle   vendorplugin.Lifecycle
	Effort      *CatalogEffort
	// declared lists the keys the row spelled, so an extension of a module
	// row can be told apart from a new declaration.
	declared []string
}

// CatalogVendor groups the catalog rows of one registered vendor.
type CatalogVendor struct {
	ID     vendorplugin.VendorID
	Models []CatalogModel
}

// ModelCatalog is the parsed operator model catalog. The zero value is an
// absent catalog and changes nothing.
type ModelCatalog struct {
	EffortScale []ScaleEntry
	Vendors     []CatalogVendor
}

// LoadModelCatalog reads catalog.toml from the curator directory. A missing
// file is the zero catalog; a file that exists but cannot be read is
// read_failed, never absence.
func LoadModelCatalog(curatorDir string) (ModelCatalog, error) {
	data, present, err := readOptional(curatorDir, CatalogFile)
	if err != nil {
		return ModelCatalog{}, err
	}
	if !present {
		return ModelCatalog{}, nil
	}
	return ParseModelCatalog(data)
}

// ParseModelCatalog parses and validates catalog.toml without consulting a
// registry. Validation against the module's declarations happens in Apply.
func ParseModelCatalog(data []byte) (ModelCatalog, error) {
	document, err := parseDocument(CatalogFile, data)
	if err != nil {
		return ModelCatalog{}, err
	}
	if err := requireSchemaVersion(document, CatalogFile); err != nil {
		return ModelCatalog{}, err
	}
	for key := range document {
		if key != "schema_version" && key != "effort_scale" && key != "vendors" {
			return ModelCatalog{}, refuse(RefusalMalformed, CatalogFile, key)
		}
	}
	var catalog ModelCatalog
	if raw, ok := document["effort_scale"]; ok {
		if catalog.EffortScale, err = parseEffortScale(raw); err != nil {
			return ModelCatalog{}, err
		}
	}
	if raw, ok := document["vendors"]; ok {
		vendors, ok := table(raw)
		if !ok {
			return ModelCatalog{}, refuse(RefusalMalformed, CatalogFile, "vendors")
		}
		for _, rawID := range sortedKeys(vendors) {
			vendor, err := parseCatalogVendor(rawID, vendors[rawID])
			if err != nil {
				return ModelCatalog{}, err
			}
			catalog.Vendors = append(catalog.Vendors, vendor)
		}
	}
	if _, err := catalog.EffectiveEffortScale(); err != nil {
		return ModelCatalog{}, err
	}
	return catalog, nil
}

func parseEffortScale(raw any) ([]ScaleEntry, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, refuse(RefusalMalformed, CatalogFile, "effort_scale")
	}
	entries := make([]ScaleEntry, 0, len(items))
	for _, item := range items {
		fields, ok := table(item)
		if !ok || len(fields) != 2 {
			return nil, refuse(RefusalMalformed, CatalogFile, "effort_scale")
		}
		word, wordOK := fields["word"].(string)
		after, afterOK := fields["after"].(string)
		if !wordOK || !afterOK || !effortWord(word) || !effortWord(after) {
			return nil, refuse(RefusalMalformed, CatalogFile, "effort_scale")
		}
		entries = append(entries, ScaleEntry{Word: word, After: after})
	}
	return entries, nil
}

func parseCatalogVendor(rawID string, raw any) (CatalogVendor, error) {
	id, err := vendorplugin.NormalizeVendorID(rawID)
	if err != nil || string(id) != rawID {
		return CatalogVendor{}, refuse(RefusalMalformed, CatalogFile, "vendors."+rawID)
	}
	fields, ok := table(raw)
	if !ok {
		return CatalogVendor{}, refuse(RefusalMalformed, CatalogFile, "vendors."+rawID)
	}
	for key := range fields {
		if key != "models" {
			return CatalogVendor{}, refuse(RefusalMalformed, CatalogFile, "vendors."+rawID+"."+key)
		}
	}
	models, ok := table(fields["models"])
	if !ok {
		return CatalogVendor{}, refuse(RefusalMalformed, CatalogFile, "vendors."+rawID+".models")
	}
	vendor := CatalogVendor{ID: id}
	for _, modelID := range sortedKeys(models) {
		model, err := parseCatalogModel(rawID, modelID, models[modelID])
		if err != nil {
			return CatalogVendor{}, err
		}
		vendor.Models = append(vendor.Models, model)
	}
	return vendor, nil
}

func parseCatalogModel(vendorID, rawID string, raw any) (CatalogModel, error) {
	subject := vendorID + "/" + rawID
	fields, ok := table(raw)
	if !ok || vendorplugin.ValidateModelID(vendorplugin.ModelID(rawID)) != nil {
		return CatalogModel{}, refuse(RefusalMalformed, CatalogFile, subject)
	}
	model := CatalogModel{ID: vendorplugin.ModelID(rawID)}
	for _, key := range sortedKeys(fields) {
		model.declared = append(model.declared, key)
		value := fields[key]
		switch key {
		case "description":
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return CatalogModel{}, refuse(RefusalMalformed, CatalogFile, subject+".description")
			}
			model.Description = text
		case "lifecycle":
			text, ok := value.(string)
			if !ok || vendorplugin.Lifecycle(text).Validate() != nil {
				return CatalogModel{}, refuse(RefusalMalformed, CatalogFile, subject+".lifecycle")
			}
			model.Lifecycle = vendorplugin.Lifecycle(text)
		case "systems":
			words, ok := stringList(value)
			if !ok || len(words) == 0 {
				return CatalogModel{}, refuse(RefusalMalformed, CatalogFile, subject+".systems")
			}
			for _, word := range words {
				model.Systems = append(model.Systems, agentic.SystemID(word))
			}
		case "aliases":
			words, ok := stringList(value)
			if !ok || len(words) == 0 {
				return CatalogModel{}, refuse(RefusalMalformed, CatalogFile, subject+".aliases")
			}
			for _, word := range words {
				if vendorplugin.ValidateModelID(vendorplugin.ModelID(word)) != nil {
					return CatalogModel{}, refuse(RefusalMalformed, CatalogFile, subject+".aliases")
				}
				model.Aliases = append(model.Aliases, vendorplugin.ModelID(word))
			}
		case "effort":
			effort, err := parseCatalogEffort(subject, value)
			if err != nil {
				return CatalogModel{}, err
			}
			model.Effort = effort
		default:
			return CatalogModel{}, refuse(RefusalMalformed, CatalogFile, subject+"."+key)
		}
	}
	return model, nil
}

func parseCatalogEffort(subject string, raw any) (*CatalogEffort, error) {
	fields, ok := table(raw)
	if !ok || len(fields) == 0 {
		return nil, refuse(RefusalMalformed, CatalogFile, subject+".effort")
	}
	effort := &CatalogEffort{}
	for key, value := range fields {
		switch key {
		case "support":
			text, _ := value.(string)
			switch text {
			case "required":
				effort.Support = agentic.EffortSupportRequired
			case "none":
				effort.Support = agentic.EffortSupportNone
			default:
				return nil, refuse(RefusalMalformed, CatalogFile, subject+".effort.support")
			}
			effort.SupportSet = true
		case "accepts":
			words, ok := stringList(value)
			if !ok || len(words) == 0 {
				return nil, refuse(RefusalMalformed, CatalogFile, subject+".effort.accepts")
			}
			for _, word := range words {
				if !effortWord(word) {
					return nil, refuse(RefusalMalformed, CatalogFile, subject+".effort.accepts")
				}
			}
			effort.Accepts = words
		case "recommended":
			text, ok := value.(string)
			if !ok || !effortWord(text) {
				return nil, refuse(RefusalMalformed, CatalogFile, subject+".effort.recommended")
			}
			effort.Recommended = text
		default:
			return nil, refuse(RefusalMalformed, CatalogFile, subject+".effort."+key)
		}
	}
	return effort, nil
}

// EffectiveEffortScale returns the module's EffortOrder extended by the
// catalog's entries, each placed directly after its After word. It refuses an
// entry whose After is not already on the scale, a word that is already on
// it, and two entries that claim the same position (their order would be
// ambiguous).
func (c ModelCatalog) EffectiveEffortScale() ([]string, error) {
	scale := vendorplugin.EffortOrder()
	claimed := make([]string, 0, len(c.EffortScale))
	for _, entry := range c.EffortScale {
		if indexOf(scale, entry.Word) >= 0 {
			return nil, refuse(RefusalConflicting, CatalogFile, "effort_scale."+entry.Word)
		}
		if contains(claimed, entry.After) {
			return nil, refuse(RefusalConflicting, CatalogFile, "effort_scale."+entry.Word)
		}
		position := indexOf(scale, entry.After)
		if position < 0 {
			return nil, refuse(RefusalMalformed, CatalogFile, "effort_scale."+entry.Word)
		}
		claimed = append(claimed, entry.After)
		next := make([]string, 0, len(scale)+1)
		next = append(next, scale[:position+1]...)
		next = append(next, entry.Word)
		next = append(next, scale[position+1:]...)
		scale = next
	}
	return scale, nil
}

// Apply returns the vendor with this catalog's rows for it merged in. A vendor
// the catalog does not name is returned unchanged. Every refusal is a typed
// Refusal naming the vendor/model, never file contents:
//   - conflicting: a row that redefines a module declaration beyond adding
//     effort words, an extension of a model with no effort axis, or an id or
//     alias that collides with an existing row;
//   - unsupported: an effort word that is not on the effective scale;
//   - malformed: a new row without systems or an effort table, or one that
//     the module's own Model.Validate refuses.
func (c ModelCatalog) Apply(vendor vendorplugin.Vendor) (vendorplugin.Vendor, error) {
	if vendor == nil {
		return vendor, nil
	}
	var entry *CatalogVendor
	for i := range c.Vendors {
		if c.Vendors[i].ID == vendor.ID() {
			entry = &c.Vendors[i]
		}
	}
	if entry == nil || len(entry.Models) == 0 {
		return vendor, nil
	}
	scale, err := c.EffectiveEffortScale()
	if err != nil {
		return nil, err
	}
	models := vendor.Models()
	var added []vendorplugin.Model
	for _, row := range entry.Models {
		subject := string(entry.ID) + "/" + string(row.ID)
		if index := modelIndex(models, row.ID); index >= 0 {
			// An alias row must declare its target's effort exactly, so an
			// extension reaches the target and every alias of it together.
			target := models[index].ID
			if models[index].AliasOf != "" {
				target = models[index].AliasOf
			}
			for i := range models {
				if models[i].ID == target || models[i].AliasOf == target {
					if err := extendModel(&models[i], row, scale, subject); err != nil {
						return nil, err
					}
				}
			}
			continue
		}
		rows, err := declareModel(entry.ID, row, scale, subject)
		if err != nil {
			return nil, err
		}
		for _, declared := range rows {
			if modelIndex(models, declared.ID) >= 0 || modelIndex(added, declared.ID) >= 0 {
				return nil, refuse(RefusalConflicting, CatalogFile, string(entry.ID)+"/"+string(declared.ID))
			}
			added = append(added, declared)
		}
	}
	return &catalogVendor{Vendor: vendor, models: append(models, added...)}, nil
}

// ApplyAll applies the catalog to every vendor and refuses a catalog vendor
// that names no registered vendor: v1 adds models to existing vendor plugins
// only, because a vendor also owns spawn and availability behaviour.
func (c ModelCatalog) ApplyAll(vendors []vendorplugin.Vendor) ([]vendorplugin.Vendor, error) {
	for _, entry := range c.Vendors {
		found := false
		for _, vendor := range vendors {
			if vendor != nil && vendor.ID() == entry.ID {
				found = true
			}
		}
		if !found {
			return nil, refuse(RefusalUnsupported, CatalogFile, "vendors."+string(entry.ID))
		}
	}
	result := make([]vendorplugin.Vendor, 0, len(vendors))
	for _, vendor := range vendors {
		applied, err := c.Apply(vendor)
		if err != nil {
			return nil, err
		}
		result = append(result, applied)
	}
	return result, nil
}

func extendModel(model *vendorplugin.Model, row CatalogModel, scale []string, subject string) error {
	if len(row.declared) != 1 || row.declared[0] != "effort" || row.Effort == nil ||
		row.Effort.SupportSet || row.Effort.Recommended != "" || len(row.Effort.Accepts) == 0 {
		return refuse(RefusalConflicting, CatalogFile, subject)
	}
	if model.Effort.Support != agentic.EffortSupportRequired {
		return refuse(RefusalConflicting, CatalogFile, subject+".effort")
	}
	vocabulary := append([]string(nil), model.Effort.Vocabulary...)
	for _, word := range row.Effort.Accepts {
		if indexOf(scale, word) < 0 {
			return refuse(RefusalUnsupported, CatalogFile, subject+".effort."+word)
		}
		if indexOf(vocabulary, word) < 0 {
			vocabulary = append(vocabulary, word)
		}
	}
	model.Effort.Vocabulary = vocabulary
	return nil
}

func declareModel(vendor vendorplugin.VendorID, row CatalogModel, scale []string, subject string) ([]vendorplugin.Model, error) {
	if len(row.Systems) == 0 || row.Effort == nil || !row.Effort.SupportSet {
		return nil, refuse(RefusalMalformed, CatalogFile, subject)
	}
	for _, word := range row.Effort.Accepts {
		if indexOf(scale, word) < 0 {
			return nil, refuse(RefusalUnsupported, CatalogFile, subject+".effort."+word)
		}
	}
	description := row.Description
	if description == "" {
		description = "Operator-declared model from " + CatalogFile
	}
	lifecycle := row.Lifecycle
	if lifecycle == "" {
		lifecycle = vendorplugin.LifecycleCurrent
	}
	model := vendorplugin.Model{
		ID:          row.ID,
		Description: vendorplugin.UsageDescription(description),
		Rank: vendorplugin.CapabilityRank{Score: 1, Basis: []vendorplugin.RankEvidence{{
			Source:      catalogEvidenceSource,
			Observation: "declared by the operator; not benchmarked, never ranked into a lineup",
		}}},
		Lifecycle: lifecycle,
		Effort: vendorplugin.EffortDeclaration{
			Support:     row.Effort.Support,
			Vocabulary:  append([]string(nil), row.Effort.Accepts...),
			Recommended: row.Effort.Recommended,
		},
		Systems:          append([]agentic.SystemID(nil), row.Systems...),
		Publisher:        string(vendor),
		OperatorDeclared: true,
	}
	if err := model.Validate(); err != nil {
		return nil, refuse(RefusalMalformed, CatalogFile, subject)
	}
	rows := []vendorplugin.Model{model}
	for _, alias := range row.Aliases {
		aliased := vendorplugin.CloneModels([]vendorplugin.Model{model})[0]
		aliased.ID = alias
		aliased.AliasOf = model.ID
		if err := aliased.Validate(); err != nil {
			return nil, refuse(RefusalMalformed, CatalogFile, string(vendor)+"/"+string(alias))
		}
		rows = append(rows, aliased)
	}
	return rows, nil
}

// catalogVendor is a registered vendor with the catalog's rows merged in. Only
// Models changes; spawn and availability stay the vendor's own.
type catalogVendor struct {
	vendorplugin.Vendor
	models []vendorplugin.Model
}

func (v *catalogVendor) Models() []vendorplugin.Model { return vendorplugin.CloneModels(v.models) }

func modelIndex(models []vendorplugin.Model, id vendorplugin.ModelID) int {
	for i, model := range models {
		if model.ID == id {
			return i
		}
	}
	return -1
}

func indexOf(values []string, expected string) int {
	for i, value := range values {
		if value == expected {
			return i
		}
	}
	return -1
}

func effortWord(word string) bool {
	return word != "" && strings.TrimSpace(word) == word && !strings.ContainsAny(word, " \t\r\n")
}

func stringList(raw any) ([]string, bool) {
	items, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	words := make([]string, 0, len(items))
	for _, item := range items {
		word, ok := item.(string)
		if !ok || strings.TrimSpace(word) == "" || indexOf(words, word) >= 0 {
			return nil, false
		}
		words = append(words, word)
	}
	return words, true
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
