package codex

// representedProviderKeys is the single declaration of provider-table keys
// represented by ProviderSnapshot and providerArgv. Keep it private so callers
// cannot alter the contract; the tagged test hook narrows this seam.
var representedProviderKeys = []string{
	"name",
	"base_url",
	"wire_api",
	"requires_openai_auth",
}

// RepresentedProviderKeys returns a detached copy of the exact, case-sensitive
// [model_providers.<id>] key set represented by ProviderSnapshot and emitted
// by local-provider plans. name and base_url are retained values; wire_api and
// requires_openai_auth are validated constants ("responses" and false).
//
// Consumers can use slices.Contains(RepresentedProviderKeys(), key) and must
// refuse unknown keys themselves when lossless snapshot transport is required.
// This declaration does not change the plugin's existing parsing behavior.
// Top-level model_provider and model_catalog_json are not provider-table keys.
func RepresentedProviderKeys() []string {
	return append([]string(nil), representedProviderKeys...)
}
