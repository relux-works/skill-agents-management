//go:build providersettingsmutant

package codex

import "os"

func init() {
	switch os.Getenv("CODEX_PROVIDER_SETTINGS_MUTANT") {
	case "":
	case "admit-model":
		// Narrow the closed allow-list by exactly one refused-class member:
		// the model-identity key. The gate stays present — every other
		// refused key still refuses — so the witness must fail on this one
		// admission, not on a deleted gate.
		allowedLocalProviderSettings["model"] = true
	case "admit-spaced-key":
		// Weaken the shape gate for exactly one malformed member: the
		// leading-space key. The allow-list still refuses it, so the
		// witness fails on the kind change (unsupported where malformed is
		// required).
		settingsMutantAdmitSpacedKey = true
	case "admit-capitalized-bool":
		// Weaken the value grammar for exactly one non-bool member: the
		// capitalised boolean. Every other non-bool value still refuses.
		settingsMutantAdmitCapitalizedBool = true
	case "admit-duplicate":
		// Weaken the duplicate gate so a second entry for one key is
		// carried instead of refused as conflicting.
		settingsMutantAdmitDuplicate = true
	case "defer-model-to-argv":
		// Weaken the pre-plan ordering for exactly one refused class: the
		// model-identity key skips the pre-plan boundary and still refuses
		// at the Argv-side gate. With an unavailable binary the deferred
		// key meets the binary error instead of the typed refusal, so the
		// ordering witness fails; every other refused class still refuses
		// before binary resolution.
		settingsMutantDeferModelToArgv = true
	default:
		panic("unknown provider-settings mutant")
	}
}
