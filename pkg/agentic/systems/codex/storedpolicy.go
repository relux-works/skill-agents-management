package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

const codexManagedPolicySource = "system or cloud-managed configuration and requirements"

// InspectStoredPolicy reads the user configuration, the configuration file
// in the supplied working directory, and a user-selected profile file. It
// never reads the process environment or resolves provider precedence.
func (*System) InspectStoredPolicy(ctx agentic.StoredPolicyContext) agentic.StoredPolicyInspection {
	result := agentic.StoredPolicyInspection{Support: agentic.StoredPolicySupported}
	home := codexPolicyEnvValue(ctx.Environment, "HOME")
	configRoot := codexPolicyEnvValue(ctx.Environment, "CODEX_HOME")
	if strings.TrimSpace(configRoot) == "" {
		configRoot = ctx.Home
	}
	if strings.TrimSpace(configRoot) == "" && home != "" {
		configRoot = filepath.Join(home, ".codex")
	}

	var root string
	rootOK := false
	if root, rootOK = resolveCodexConfigRoot(configRoot, home, ctx.WorkDir); rootOK {
		userPath := filepath.Join(root, "config.toml")
		userConfig, userReadErr := readCodexConfig(userPath)
		var profileName string
		var userProfileErr error
		if userReadErr != nil {
			result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
				SourcePath: userPath,
				Reason:     codexReadFailure(userReadErr),
			})
			if !errors.Is(userReadErr, os.ErrNotExist) {
				result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
					SourcePath: filepath.Join(root, "<selected-profile>.config.toml"),
					Reason:     agentic.StoredPolicySourceUnknownSelection,
				})
			}
		} else {
			profileName, userProfileErr = codexSelectedProfile(userConfig)
			if userProfileErr != nil {
				result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
					SourcePath: userPath,
					Reason:     agentic.StoredPolicySourceUnparseable,
				})
			} else {
				inspectCodexConfig(&result, userPath, userConfig)
			}
		}

		if projectRoot, ok := resolveCodexProjectRoot(ctx.WorkDir); ok {
			projectPath := filepath.Join(projectRoot, ".codex", "config.toml")
			projectConfig, err := readCodexConfig(projectPath)
			if err != nil {
				result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
					SourcePath: projectPath,
					Reason:     codexReadFailure(err),
				})
			} else {
				inspectCodexConfig(&result, projectPath, projectConfig)
			}
		} else {
			result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
				SourcePath: filepath.Join("<workdir>", ".codex", "config.toml"),
				Reason:     agentic.StoredPolicySourceRootUnavailable,
			})
		}

		if userReadErr == nil {
			if userProfileErr != nil {
				result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
					SourcePath: filepath.Join(root, "<selected-profile>.config.toml"),
					Reason:     agentic.StoredPolicySourceUnknownSelection,
				})
			} else if profileName != "" {
				if profilePath, ok := codexProfilePath(root, profileName); ok {
					profileConfig, err := readCodexConfig(profilePath)
					if err != nil {
						result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
							SourcePath: profilePath,
							Reason:     codexReadFailure(err),
						})
					} else {
						inspectCodexConfig(&result, profilePath, profileConfig)
					}
				} else {
					result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
						SourcePath: filepath.Join(root, "<selected-profile>.config.toml"),
						Reason:     agentic.StoredPolicySourceUnknownSelection,
					})
				}
			}
		}
	} else {
		result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
			SourcePath: "${CODEX_HOME}/config.toml or ${HOME}/.codex/config.toml",
			Reason:     agentic.StoredPolicySourceRootUnavailable,
		})
		if _, ok := resolveCodexProjectRoot(ctx.WorkDir); !ok {
			result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
				SourcePath: filepath.Join("<workdir>", ".codex", "config.toml"),
				Reason:     agentic.StoredPolicySourceRootUnavailable,
			})
		} else {
			projectPath := filepath.Join(filepath.Clean(ctx.WorkDir), ".codex", "config.toml")
			projectConfig, err := readCodexConfig(projectPath)
			if err != nil {
				result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
					SourcePath: projectPath,
					Reason:     codexReadFailure(err),
				})
			} else {
				inspectCodexConfig(&result, projectPath, projectConfig)
			}
		}
	}

	result.SourcesNotInspected = append(result.SourcesNotInspected, agentic.StoredPolicySourceIssue{
		SourcePath: codexManagedPolicySource,
		Reason:     agentic.StoredPolicySourceNotProvided,
	})
	return result
}

var (
	errCodexConfigRoot        = errors.New("codex configuration root is not a TOML table")
	errCodexConfigUnparseable = errors.New("codex configuration is not parseable TOML")
)

func readCodexConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, errors.Join(errCodexConfigUnparseable, err)
	}
	if config == nil {
		return nil, errCodexConfigRoot
	}
	return config, nil
}

func inspectCodexConfig(result *agentic.StoredPolicyInspection, path string, config map[string]any) {
	relaxations, err := codexPolicyRelaxations(config, path)
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

func codexPolicyRelaxations(config map[string]any, sourcePath string) ([]agentic.StoredPolicyRelaxation, error) {
	var relaxations []agentic.StoredPolicyRelaxation
	if value, found := config["approval_policy"]; found {
		if mode, ok := value.(string); ok {
			switch mode {
			case "never":
				relaxations = append(relaxations, codexRelaxation("approval_policy", mode, sourcePath))
			case "on-request", "on-failure", "untrusted":
			default:
				return nil, fmt.Errorf("unrecognized approval_policy value %q", mode)
			}
		} else if !validGranularApprovalPolicy(value) {
			return nil, errors.New("unrecognized approval_policy value")
		}
	}
	if value, found := config["sandbox_mode"]; found {
		mode, ok := value.(string)
		if !ok {
			return nil, errors.New("sandbox_mode is not a string")
		}
		known, relaxing := classifyStoredSandboxMode(mode)
		if !known {
			return nil, fmt.Errorf("unrecognized sandbox_mode value %q", mode)
		}
		if relaxing {
			relaxations = append(relaxations, codexRelaxation("sandbox_mode", mode, sourcePath))
		}
	}
	if value, found := config["sandbox_permissions"]; found && codexPermissionValueIsRelaxing(value) {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("sandbox_permissions cannot be rendered: %w", err)
		}
		relaxations = append(relaxations, codexRelaxation("sandbox_permissions", string(encoded), sourcePath))
	}
	return relaxations, nil
}

func validGranularApprovalPolicy(value any) bool {
	policy, ok := value.(map[string]any)
	if !ok || len(policy) != 1 {
		return false
	}
	granular, ok := policy["granular"].(map[string]any)
	if !ok {
		return false
	}
	for key, value := range granular {
		switch key {
		case "sandbox_approval", "rules", "mcp_elicitations", "request_permissions", "skill_approval":
			if _, ok := value.(bool); !ok {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func codexPermissionValueIsRelaxing(value any) bool {
	switch value := value.(type) {
	case bool:
		return value
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		for _, item := range value {
			if codexPermissionValueIsRelaxing(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range value {
			if codexPermissionValueIsRelaxing(item) {
				return true
			}
		}
	case int64:
		return value > 0
	case float64:
		return value > 0
	}
	return false
}

func codexRelaxation(selector, value, sourcePath string) agentic.StoredPolicyRelaxation {
	return agentic.StoredPolicyRelaxation{Selector: selector, Value: value, SourcePath: sourcePath}
}

func codexSelectedProfile(config map[string]any) (string, error) {
	value, found := config["profile"]
	if !found {
		return "", nil
	}
	name, ok := value.(string)
	if !ok || strings.TrimSpace(name) != name {
		return "", errors.New("codex profile selector is not a string")
	}
	return name, nil
}

func codexProfilePath(root, name string) (string, bool) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, "/\\\x00") {
		return "", false
	}
	return filepath.Join(root, name+".config.toml"), true
}

func codexReadFailure(err error) agentic.StoredPolicySourceReason {
	if errors.Is(err, os.ErrNotExist) {
		return agentic.StoredPolicySourceAbsent
	}
	if errors.Is(err, errCodexConfigRoot) || errors.Is(err, errCodexConfigUnparseable) {
		return agentic.StoredPolicySourceUnparseable
	}
	return agentic.StoredPolicySourceUnreadable
}

func codexPolicyEnvValue(env []string, name string) string {
	value := ""
	for _, entry := range env {
		key, candidate, found := strings.Cut(entry, "=")
		if found && key == name {
			value = candidate
		}
	}
	return value
}

func resolveCodexConfigRoot(raw, home, workDir string) (string, bool) {
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

func resolveCodexProjectRoot(workDir string) (string, bool) {
	if !filepath.IsAbs(workDir) {
		return "", false
	}
	return filepath.Clean(workDir), true
}
