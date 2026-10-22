package codex

import (
	"sort"
	"strings"
	"unicode"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// allowedLocalProviderSettings is the CLOSED allow-list of Codex `-c` keys a
// local-provider launch may carry through LocalProviderBinding.Settings —
// never a deny-list. After exact plan-owned keys have been dropped, every
// remaining key not present here is refused as unsupported.
//
// The two admitted keys are boolean display switches documented under Codex's
// config reference ("Hide or surface reasoning events"): hide_agent_reasoning
// suppresses reasoning events in both the TUI and `codex exec` output, and
// show_raw_agent_reasoning surfaces raw reasoning content on stderr when the
// model emits it — content a consumer that captures stderr may persist in
// logs or transcripts. Both change only what the harness prints about a run —
// the output shape — never which provider answers, which model runs, which
// tools execute, what the sandbox permits, what approvals are skipped, what
// the network reaches, or which credentials are read. That is why they, and
// only they, are admitted.
//
// The refused families this closed list exists to hold out, each pinned by a
// row of TestCarriedSettingsRefuseBeforeAnyFileIsMaterialised:
//
//   - credential keys (api_key, env_key, auth, bearer tokens): a carried
//     secret would rebind authentication after the plugin validated an
//     auth-free provider entry.
//   - provider keys (model_provider, model_providers.*, model_catalog_json):
//     the binding's ID, snapshot and validated catalog already select the
//     provider; a second spelling is a second writer.
//   - unowned model keys: only settings actually emitted by the model
//     selection fragments are dropped, never arbitrary model_* keys.
//   - sandbox keys (sandbox_mode, sandbox_permissions,
//     sandbox_workspace_write.*): the exec posture is the sealed argv's own.
//   - approval keys (approval_policy): same posture, same reason.
//   - network keys (*.network_access and its companions): reach is the
//     managed network scope's, never a carried override's.
//   - tool keys (mcp_servers.*, tools.*, features.*): tools enter through
//     the composition and descriptor channels the seal already binds.
//
// It is a var, not a const table, so the build-tagged narrowing mutant can
// admit exactly one refused key and the witness must fail. Production code
// never mutates it.
var allowedLocalProviderSettings = map[string]bool{
	"hide_agent_reasoning":     true,
	"show_raw_agent_reasoning": true,
}

// The narrowing-mutant seams below are false in production. The build-tagged
// mutant file sets exactly one per mutant run; each weakens one gate for one
// member only, and the named witness must fail. Production code never sets
// them.
var (
	// settingsMutantAdmitSpacedKey weakens the shape gate for the single
	// leading-space key the spaced-key row carries. The allow-list still
	// refuses it, so the witness fails on the kind change (unsupported
	// where malformed is required), proving the shape gate classifies.
	settingsMutantAdmitSpacedKey = false
	// settingsMutantAdmitCapitalizedBool weakens the value grammar for the
	// single capitalised boolean the capitalised-bool row carries.
	settingsMutantAdmitCapitalizedBool = false
	// settingsMutantAdmitDuplicate weakens the duplicate gate so a second
	// entry for one key is carried instead of refused.
	settingsMutantAdmitDuplicate = false
	// settingsMutantDeferModelToArgv weakens the pre-plan ordering for the
	// single unowned model key: the pre-plan boundary skips it and the
	// Argv-side gate still refuses it. With an unavailable binary the
	// deferred key meets the binary error instead of the typed refusal,
	// so the ordering witness fails; every other refused class still
	// refuses at the pre-plan boundary.
	settingsMutantDeferModelToArgv = false
)

// AllowedLocalProviderSettings returns the sorted allow-list of carried
// local-provider setting keys. It is a detached copy: mutating the result
// cannot admit a key.
func AllowedLocalProviderSettings() []string {
	keys := make([]string, 0, len(allowedLocalProviderSettings))
	for key := range allowedLocalProviderSettings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// carriedLocalSettingsArgv validates request-carried local-provider settings
// against the module's own policy and renders the admitted ones as `-c`
// key=value pairs, sorted by key so operator order never affects the sealed
// bytes. Plan-owned keys have already been split out by the shared
// classifier; they never enter the carried argv. Nil/empty renders nothing.
//
// The gate order is load-bearing: shape first (a malformed key or value is a
// malformed request, not a policy decision), then the closed allow-list (any
// other key — credential, provider, unowned model, sandbox, approval,
// network, tool or unknown — is unsupported), then the boolean value grammar
// (exactly "true" or "false", emitted unquoted as TOML booleans), then the
// duplicate check (a second entry for one key is a second writer, even when
// it carries the same value). BuildPlan's pre-plan boundary and
// localProviderArgs both call this — the first before any
// filesystem-dependent dispatch, the second as defence in depth before its
// first read — so a refusal precedes any materialised file on both the
// ID-only and the snapshot paths.
func carriedLocalSettingsArgv(settings []agentic.LocalProviderSetting, providerID string) ([]string, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	seen := make(map[string]string, len(settings))
	for _, setting := range settings {
		key := setting.Key
		if (key == "" || key != strings.TrimSpace(key) || strings.IndexFunc(key, unicode.IsSpace) >= 0 || strings.IndexFunc(key, unicode.IsControl) >= 0) && !(settingsMutantAdmitSpacedKey && key == " hide_agent_reasoning") {
			return nil, localSettingsRefusal(agentic.LocalProviderMalformed, providerID)
		}
		if !allowedLocalProviderSettings[key] {
			return nil, localSettingsRefusal(agentic.LocalProviderUnsupported, providerID)
		}
		if setting.Value != "true" && setting.Value != "false" && !(settingsMutantAdmitCapitalizedBool && setting.Value == "True") {
			return nil, localSettingsRefusal(agentic.LocalProviderMalformed, providerID)
		}
		if _, duplicate := seen[key]; duplicate && !settingsMutantAdmitDuplicate {
			return nil, localSettingsRefusal(agentic.LocalProviderConflicting, providerID)
		}
		seen[key] = setting.Value
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	argv := make([]string, 0, 2*len(keys))
	for _, key := range keys {
		argv = append(argv, "-c", key+"="+seen[key])
	}
	return argv, nil
}

// validateCarriedLocalSettingsPrePlan is the production pre-planning
// boundary BuildPlan dispatches before any filesystem-dependent work. In
// production it enforces the identical policy (the argv it would render is
// discarded; Argv re-renders it as defence in depth). Under the
// defer-model-to-argv narrowing mutant it skips the unowned model entry, so
// exactly that class falls through to binary resolution and the Argv-side
// gate — every other refused class still refuses here.
func validateCarriedLocalSettingsPrePlan(settings []agentic.LocalProviderSetting, providerID string) error {
	if len(settings) == 0 {
		return nil
	}
	if settingsMutantDeferModelToArgv {
		kept := make([]agentic.LocalProviderSetting, 0, len(settings))
		for _, setting := range settings {
			if setting.Key == "model_reasoning_summary" {
				continue
			}
			kept = append(kept, setting)
		}
		if len(kept) == 0 {
			return nil
		}
		settings = kept
	}
	if _, err := carriedLocalSettingsArgv(settings, providerID); err != nil {
		return err
	}
	return nil
}

// localSettingsRefusal builds a typed refusal for a request-carried setting.
// Unlike the config.toml refusals it names no file: the setting rode the
// request, not a file. The key and value never enter the error — a value may
// carry a secret, and the typed kind already tells the operator whether the
// request was malformed, unadmitted by policy, or doubly set.
func localSettingsRefusal(kind agentic.LocalProviderRefusalKind, providerID string) error {
	return &agentic.LocalProviderRefusal{Kind: kind, Subject: providerID}
}

// sealedLocalModelSettings derives ownership from the same pure fragments
// Args emits. Provider/catalog, auth, sandbox, network and tool keys are never
// model companions. An absent effort/tier has no writer and owns no key.
func sealedLocalModelSettings(req agentic.LaunchRequest, mode agentic.LaunchMode) map[string]bool {
	owned := map[string]bool{}
	if mode != agentic.LaunchModeExec && mode != agentic.LaunchModeDryRun {
		return owned
	}
	args := appendReasoningAndTier(appendModelSelection(nil, req, mode), req)
	for i := 0; i+1 < len(args); i += 2 {
		switch args[i] {
		case "-m", "--model":
			if strings.TrimSpace(args[i+1]) != "" {
				owned["model"] = true
			}
		case "-c":
			key, _, _ := strings.Cut(args[i+1], "=")
			owned[key] = true
		}
	}
	return owned
}

// splitPlanOwnedLocalSettings is shared by pre-plan validation, argv and
// reporting. Only exact keys emitted by the model fragments are dropped;
// their values are neither inspected nor retained in the report. Malformed
// keys cannot match the sealed set and still meet the normal shape gate.
func splitPlanOwnedLocalSettings(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]agentic.LocalProviderSetting, []string) {
	if req.LocalProvider == nil || len(req.LocalProvider.Settings) == 0 {
		return nil, nil
	}
	owned := sealedLocalModelSettings(req, mode)
	kept := make([]agentic.LocalProviderSetting, 0, len(req.LocalProvider.Settings))
	dropped := map[string]bool{}
	for _, setting := range req.LocalProvider.Settings {
		if owned[setting.Key] {
			dropped[setting.Key] = true
			continue
		}
		kept = append(kept, setting)
	}
	var keys []string
	for key := range dropped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return kept, keys
}
