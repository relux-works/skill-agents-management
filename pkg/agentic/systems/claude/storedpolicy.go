package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const claudeManagedPolicySource = "managed settings (system, MDM, server, or embedding-host policy)"

// InspectStoredPolicy reads only settings paths derived from the supplied
// launch environment, home and working directory. It reports raw known
// relaxations per file and does not resolve precedence or managed policy.
func (*System) InspectStoredPolicy(ctx agentic.StoredPolicyContext) agentic.StoredPolicyInspection {
	result := agentic.StoredPolicyInspection{Support: agentic.StoredPolicySupported}
	home := storedPolicyEnvValue(ctx.Environment, "HOME")
	configRoot := storedPolicyEnvValue(ctx.Environment, "CLAUDE_CONFIG_DIR")
	if strings.TrimSpace(configRoot) == "" {
		configRoot = ctx.Home
	}
	if strings.TrimSpace(configRoot) == "" && home != "" {
		configRoot = filepath.Join(home, ".claude")
	}

	if root, ok := resolveStoredPolicyRoot(configRoot, home, ctx.WorkDir); ok {
		inspectClaudeSettingsFile(&result, filepath.Join(root, "settings.json"))
	} else {
		result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
			SourcePath: "${CLAUDE_CONFIG_DIR}/settings.json or ${HOME}/.claude/settings.json",
			Reason:     agentic.StoredPolicySourceRootUnavailable,
		})
	}

	if projectRoot, ok := resolveProjectRoot(ctx.WorkDir); ok {
		inspectClaudeSettingsFile(&result, filepath.Join(projectRoot, ".claude", "settings.json"))
		inspectClaudeSettingsFile(&result, filepath.Join(projectRoot, ".claude", "settings.local.json"))
	} else {
		for _, name := range []string{"settings.json", "settings.local.json"} {
			result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
				SourcePath: filepath.Join("<workdir>", ".claude", name),
				Reason:     agentic.StoredPolicySourceRootUnavailable,
			})
		}
	}

	// These sources do not have a path carried by the launch environment and
	// may be delivered outside the local file system. Keep that blind spot
	// visible rather than treating the local file results as the full policy.
	result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
		SourcePath: claudeManagedPolicySource,
		Reason:     agentic.StoredPolicySourceNotProvided,
	})
	return result
}

func inspectClaudeSettingsFile(result *agentic.StoredPolicyInspection, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
			SourcePath: path,
			Reason:     storedPolicyReadFailure(err),
		})
		return
	}

	relaxations, err := parseClaudeStoredPolicy(data, path)
	if err != nil {
		result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
			SourcePath: path,
			Reason:     agentic.StoredPolicySourceUnparseable,
		})
		return
	}
	result.SourcesInspected = append(result.SourcesInspected, path)
	result.Relaxations = append(result.Relaxations, relaxations...)
}

func parseClaudeStoredPolicy(data []byte, sourcePath string) ([]agentic.StoredPolicyRelaxation, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errors.New("claude settings root is not an object")
	}

	rawPermissions, found := root["permissions"]
	if !found {
		return nil, nil
	}
	var permissions map[string]json.RawMessage
	if err := json.Unmarshal(rawPermissions, &permissions); err != nil || permissions == nil {
		return nil, errors.New("claude permissions setting is not an object")
	}

	var relaxations []agentic.StoredPolicyRelaxation
	if rawMode, found := permissions["defaultMode"]; found {
		var mode string
		if err := json.Unmarshal(rawMode, &mode); err != nil || !knownPermissionModes[mode] {
			return nil, errors.New("claude default permission mode is not a known string")
		}
		if mode == "acceptEdits" || mode == "auto" || mode == "bypassPermissions" {
			relaxations = append(relaxations, agentic.StoredPolicyRelaxation{
				Selector: "permissions.defaultMode", Value: mode, SourcePath: sourcePath,
			})
		}
	}
	for _, selector := range []string{"allow", "additionalDirectories"} {
		rawValues, found := permissions[selector]
		if !found {
			continue
		}
		var values []string
		if err := json.Unmarshal(rawValues, &values); err != nil || values == nil {
			return nil, fmt.Errorf("claude permissions.%s is not a string array", selector)
		}
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				continue
			}
			relaxations = append(relaxations, agentic.StoredPolicyRelaxation{
				Selector: "permissions." + selector, Value: value, SourcePath: sourcePath,
			})
		}
	}
	return relaxations, nil
}

func storedPolicyReadFailure(err error) agentic.StoredPolicySourceReason {
	if errors.Is(err, os.ErrNotExist) {
		return agentic.StoredPolicySourceAbsent
	}
	return agentic.StoredPolicySourceUnreadable
}

func storedPolicyEnvValue(env []string, name string) string {
	value := ""
	for _, entry := range env {
		key, candidate, found := strings.Cut(entry, "=")
		if found && key == name {
			value = candidate
		}
	}
	return value
}

func resolveStoredPolicyRoot(raw, home, workDir string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if raw == "~" || strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, "~\\") {
		if home == "" {
			return "", false
		}
		raw = filepath.Join(home, strings.TrimLeft(raw[1:], "/\\"))
	}
	if !filepath.IsAbs(raw) {
		if !filepath.IsAbs(workDir) {
			return "", false
		}
		raw = filepath.Join(workDir, raw)
	}
	return filepath.Clean(raw), true
}

func resolveProjectRoot(workDir string) (string, bool) {
	if !filepath.IsAbs(workDir) {
		return "", false
	}
	return filepath.Clean(workDir), true
}
