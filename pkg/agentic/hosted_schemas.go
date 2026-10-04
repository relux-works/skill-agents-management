package agentic

import "embed"

// HostedSchemaVersion is frozen. Changes to closed shapes require a new version.
const HostedSchemaVersion = "1.0.0"
const ExecGuardSchema = "urn:relux:agents-management:exec-guard"
const ClaudeEffectivePolicySchema = "urn:relux:agents-management:claude-effective-policy"
const ClaudeRestartSchema = "urn:relux:agents-management:claude-restart"

//go:embed schemas/1.0.0/*.json
var hostedSchemas embed.FS

// PinnedHostedSchema returns a fresh copy of a module-owned Draft 2020-12
// schema. No network resolver or caller-provided replacement is involved.
func PinnedHostedSchema(id, version string) ([]byte, bool) {
	if version != HostedSchemaVersion {
		return nil, false
	}
	var name string
	switch id {
	case ExecGuardSchema:
		name = "exec-guard"
	case ClaudeEffectivePolicySchema:
		name = "claude-effective-policy"
	case ClaudeRestartSchema:
		name = "claude-restart"
	default:
		return nil, false
	}
	data, err := hostedSchemas.ReadFile("schemas/1.0.0/" + name + ".json")
	return data, err == nil
}
