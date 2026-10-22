package codex

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const carriedSettingsSealSelector = "codex_carried_settings"

func cloneCarriedSettingsReport(report *agentic.LocalProviderSettingsStatus) *agentic.LocalProviderSettingsStatus {
	if report == nil {
		return nil
	}
	copy := *report
	copy.DroppedKeys = slices.Clone(copy.DroppedKeys)
	return &copy
}

// Bind presence, exact names (including slice shape), and note without putting
// the report in guard JSON. Length framing binds raw string bytes, avoiding
// JSON's replacement of invalid UTF-8. Absence emits no selector for parity.
func carriedSettingsSealDigest(report *agentic.LocalProviderSettingsStatus) string {
	if report == nil {
		return ""
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "codex.carried-settings.v1\x00%t:%d:", report.DroppedKeys == nil, len(report.DroppedKeys))
	for _, name := range report.DroppedKeys {
		fmt.Fprintf(hash, "%d:%s", len(name), name)
	}
	fmt.Fprintf(hash, "%d:%s", len(report.Note), report.Note)
	return fmt.Sprintf("sha256:%x", hash.Sum(nil))
}

func (seal *catalogExecSeal) carriedSettingsDigest() string {
	if seal.carriedSettings == nil {
		return seal.importedCarriedSettingsDigest
	}
	return carriedSettingsSealDigest(seal.carriedSettings)
}

func (seal *catalogExecSeal) verifyCarriedSettings(report *agentic.LocalProviderSettingsStatus) error {
	if carriedSettingsSealDigest(report) != seal.carriedSettingsDigest() {
		return localCatalogRefusal(agentic.LocalProviderConflicting, seal.path, seal.providerID)
	}
	return nil
}
