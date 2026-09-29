package agentic_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/refusalscan"
)

type curatorRefusalCoverageRow struct {
	file          string
	function      string
	guard         string
	returned      string
	occurrence    int
	testName      string
	generated     bool
	outOfContract string
}

// This catalog is intentionally keyed to the production guard expression. The
// AST test below fails closed when a refusal site is added, removed, or moved
// to a different guard without updating its BuildPlan test mapping.
var curatorRefusalCoverageTable = []curatorRefusalCoverageRow{
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if context.Profile.Name == \"\" || context.Profile.LockSHA256 == \"\"", returned: "return ErrCuratorProfileMissing", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMissingProfile"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if !validCuratorIdentifier(context.Profile.Name) || !validCuratorLockHash(context.Profile.LockSHA256)", returned: "return curatorMalformed(\"profile pin is invalid\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if context.Revision != CuratorLaunchFragmentV1", returned: "return fmt.Errorf(\"%w: %q\", ErrCuratorFragmentRevisionUnsupported, context.Revision)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesUnknownFragmentRevision"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "switch context.Environment case default", returned: "return curatorMalformed(\"environment is not a supported typed fragment value\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if (context.Precedence.Winner != \"higher-weight\" && context.Precedence.Winner != \"lower-weight\") || (context.Precedence.Placement != \"winner-last\" && context.Precedence.Placement != \"winner-first\")", returned: "return curatorMalformed(\"precedence is invalid\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if len(context.Env) != 1", returned: "return curatorMalformed(\"exactly one managed-home environment entry is required\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if homeVariable != wantHomeVariable || !validCuratorAbsolutePath(home) || requestHome != home", returned: "return curatorMalformed(\"managed-home entry must match LaunchRequest.Home and its environment\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if !validCuratorAbsolutePath(context.PathPrepend)", returned: "return curatorMalformed(\"path_prepend must be an absolute path\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if context.PathPrepend != \"\"", returned: "return fmt.Errorf(\"%w: path_prepend has no registered root-bound launch mapping\", ErrCuratorContextUnsupported)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesUnsupportedPathPrepend"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if context.Environment == \"pi\"", returned: "return curatorMalformed(\"pi fragments cannot carry MCP context\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if !validCuratorAbsolutePath(context.MCP.Path)", returned: "return curatorMalformed(\"MCP path must be absolute\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if !isSortedUniqueCuratorEnvNames(context.MCP.EnvNames)", returned: "return curatorMalformed(\"MCP env_names must be sorted, unique, and non-reserved\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if len(context.MCP.Channels) != 1", returned: "return curatorMalformed(\"MCP must carry exactly one channel descriptor\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if err != nil", returned: "return err", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if !validCuratorAbsolutePath(context.SystemPrompt.Path)", returned: "return curatorMalformed(\"system-prompt path must be absolute\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if context.SystemPrompt.Intent == \"\"", returned: "return ErrCuratorSystemPromptIntentMissing", occurrence: 0, testName: "TestBuildPlanCuratorSystemPromptIntentIsRequired"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if context.SystemPrompt.Intent != CuratorSystemPromptAppend && context.SystemPrompt.Intent != CuratorSystemPromptReplace", returned: "return ErrCuratorSystemPromptIntentInvalid", occurrence: 0, testName: "TestBuildPlanCuratorSystemPromptIntentRejectsUnknownValue"},
	{file: "curator_context.go", function: "ValidateCuratorContext", guard: "if err != nil", returned: "return err", occurrence: 1, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "curatorMalformed", guard: "unconditional", returned: "return fmt.Errorf(\"%w: %s\", ErrCuratorContextMalformed, reason)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if descriptor.Semantics != CuratorSystemPromptAppend && descriptor.Semantics != CuratorSystemPromptReplace", returned: "return curatorMalformed(\"system-prompt descriptor semantics must be append or replace\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if descriptor.Semantics != \"\"", returned: "return curatorMalformed(\"MCP descriptors do not carry system-prompt semantics\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if descriptor.Flag == \"\" || !validCuratorFlag(descriptor.Flag)", returned: "return curatorMalformed(\"flag descriptor has an invalid flag\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if descriptor.Argument != CuratorArgumentPath && descriptor.Argument != CuratorArgumentContents && descriptor.Argument != CuratorArgumentName", returned: "return curatorMalformed(\"flag descriptor has an invalid argument kind\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if !validCuratorIdentifier(descriptor.Name)", returned: "return curatorMalformed(\"name argument requires a valid reserved name\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if descriptor.Name != \"\"", returned: "return curatorMalformed(\"name is permitted only for a name argument\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if descriptor.With != nil && len(descriptor.With) == 0", returned: "return curatorMalformed(\"with must be absent or contain companion flags\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if !validCuratorFlag(companion)", returned: "return curatorMalformed(\"with contains an invalid companion flag\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if exists", returned: "return curatorMalformed(\"with repeats a companion flag\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if !noExtras(false, false, false, true, true, true, false)", returned: "return curatorMalformed(\"flag descriptor carries fields from another union arm\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if !validCuratorIdentifier(descriptor.Key) || !noExtras(true, true, true, false, true, true, true)", returned: "return curatorMalformed(\"config-key descriptor is malformed\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if !validCuratorIdentifier(descriptor.Variable) || !noExtras(true, true, true, true, false, true, true)", returned: "return curatorMalformed(\"variable descriptor is malformed\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "if !validCuratorPortablePath(descriptor.Filename) || !noExtras(true, true, true, true, true, false, true)", returned: "return curatorMalformed(\"file descriptor is malformed\")", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "curator_context.go", function: "validateCuratorDescriptor", guard: "switch descriptor.Kind case default", returned: "return &UnknownCuratorDescriptorError{Kind: descriptor.Kind}", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesUnknownDescriptorKind"},
	{file: "curator_context.go", function: "ValidateCuratorContextIdentity", guard: "if expected.ProfileName != current.ProfileName || expected.LockSHA256 != current.LockSHA256 || expected.ManagedHomeVariable != current.ManagedHomeVariable || expected.ManagedHome != current.ManagedHome || expected.SystemPromptIntent != current.SystemPromptIntent || !sameCuratorFragmentIdentity(expected.Fragment, current.Fragment)", returned: "return ErrCuratorContextStale", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesStaleIdentity"},
	{file: "plan.go", function: "buildPlan", guard: "if !caps.SupportsLocalProvider", returned: "return Plan{}, &LocalProviderRefusal{Kind: LocalProviderUnsupported, Subject: string(id)}", occurrence: 0, testName: "TestBuildPlanCuratorContextPreservesLocalProviderRefusalPrecedence", generated: true},
	{file: "plan.go", function: "buildPlan", guard: "if strings.TrimSpace(req.LocalProvider.ID) == \"\"", returned: "return Plan{}, &LocalProviderRefusal{Kind: LocalProviderUnbound}", occurrence: 0, testName: "TestBuildPlanCuratorContextPreservesLocalProviderRefusalPrecedence", generated: true},
	{file: "plan.go", function: "buildPlan", guard: "if err != nil", returned: "return Plan{}, fmt.Errorf(\"agentic: building plan for %s: %w\", id, err)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "plan.go", function: "buildPlan", guard: "if err != nil", returned: "return Plan{}, fmt.Errorf(\"agentic: building plan for %s: %w\", id, err)", occurrence: 1, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "plan.go", function: "buildPlan", guard: "if !supported", returned: "return Plan{}, fmt.Errorf(\"agentic: building plan for %s: %w\", id, ErrCuratorContextUnsupported)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesUnregisteredPluginCapability"},
	{file: "plan.go", function: "buildPlan", guard: "if err != nil", returned: "return Plan{}, fmt.Errorf(\"agentic: %s rejected Curator context before planning: %w\", id, err)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesIncompatibleCapability"},
	{file: "plan.go", function: "buildPlan", guard: "if !supported", returned: "return Plan{}, fmt.Errorf(\"agentic: building plan for %s: %w\", id, &ContextDescriptorsUnsupportedError{System: id})", occurrence: 0, testName: "TestBuildPlanRefusesContextDescriptorsWithoutPluginSupport"},
	{file: "systems/claude/args.go", function: "interactiveArgs", guard: "if err != nil", returned: "return nil, fmt.Errorf(\"claude: %w\", err)", occurrence: 0, testName: "TestAGoalBoundLaunchWithoutAnAssignmentFileIsRefused"},
	{file: "systems/claude/context.go", function: "buildContextValues", guard: "if seen[descriptor.Kind]", returned: "return values, contextConflict(descriptor.Kind, \"the channel was supplied more than once\")", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "buildContextValues", guard: "if !req.Composition.IsZero()", returned: "return values, contextConflict(agentic.ContextMCPServers, \"the legacy composition already supplies launch configuration\")", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "buildContextValues", guard: "if err != nil", returned: "return values, invalidContext(agentic.ContextMCPServers, fmt.Sprintf(\"Claude MCP configuration failed its plugin grammar: %v\", err))", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "buildContextValues", guard: "if strings.TrimSpace(text) == \"\"", returned: "return values, invalidContext(agentic.ContextSystemPrompt, \"additional prompt text must not be empty\")", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "buildContextValues", guard: "if req.Goal != nil", returned: "return values, contextConflict(agentic.ContextSystemPrompt, \"the goal binding already uses Claude's appended system-prompt channel\")", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "buildContextValues", guard: "if !req.PermissionMode.IsZero()", returned: "return values, contextConflict(agentic.ContextPermission, \"PermissionMode already supplies the permission channel\")", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "buildContextValues", guard: "if err != nil", returned: "return values, err", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "buildContextValues", guard: "if err != nil", returned: "return values, err", occurrence: 1, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "buildContextValues", guard: "if err != nil", returned: "return values, fmt.Errorf(\"claude: %w\", err)", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "buildContextValues", guard: "if err != nil", returned: "return values, err", occurrence: 2, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "applyClaudeCuratorContext", guard: "if context.Environment != \"claude_code\"", returned: "return fmt.Errorf(\"%w: Claude requires the claude_code fragment environment\", agentic.ErrCuratorContextUnsupported)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesIncompatibleCapability"},
	{file: "systems/claude/context.go", function: "applyClaudeCuratorContext", guard: "if context.MCP != nil && descriptor.Kind == agentic.ContextMCPServers", returned: "return contextConflict(descriptor.Kind, \"Curator context already supplies the MCP channel\")", occurrence: 0, testName: "TestBuildPlanClaudeCuratorMCPRefusesLegacyMCPDescriptor", generated: true},
	{file: "systems/claude/context.go", function: "applyClaudeCuratorContext", guard: "if context.SystemPrompt != nil && descriptor.Kind == agentic.ContextSystemPrompt", returned: "return contextConflict(descriptor.Kind, \"Curator context already supplies the system-prompt channel\")", occurrence: 0, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesLegacyContextSystemPrompt"},
	{file: "systems/claude/context.go", function: "applyClaudeCuratorContext", guard: "if !req.Composition.IsZero()", returned: "return curatorConflict(\"legacy composition already supplies launch configuration\")", occurrence: 0, testName: "TestBuildPlanClaudeCuratorMCPRefusesLegacyComposition"},
	{file: "systems/claude/context.go", function: "applyClaudeCuratorContext", guard: "if descriptor.Kind != agentic.CuratorDescriptorFlag || descriptor.Flag != mcpConfigFlag || descriptor.Argument != agentic.CuratorArgumentPath || len(descriptor.With) != 1 || descriptor.With[0] != \"--strict-mcp-config\"", returned: "return fmt.Errorf(\"%w: Claude's MCP channel is incompatible with the fragment descriptor\", agentic.ErrCuratorContextUnsupported)", occurrence: 0, testName: "TestBuildPlanCuratorDescriptorKindsReachClaudeAndCodexPlugins"},
	{file: "systems/claude/context.go", function: "applyClaudeCuratorContext", guard: "if req.Goal != nil", returned: "return curatorConflict(\"the goal binding already uses Claude's system-prompt channel\")", occurrence: 0, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesGoalBinding"},
	{file: "systems/claude/context.go", function: "applyClaudeCuratorContext", guard: "if selected != nil", returned: "return agentic.ErrCuratorSystemPromptChannelAmbiguous", occurrence: 0, testName: "TestBuildPlanCuratorSystemPromptRefusesAmbiguousMatchingChannels"},
	{file: "systems/claude/context.go", function: "applyClaudeCuratorContext", guard: "if selected == nil", returned: "return agentic.ErrCuratorSystemPromptChannelMissing", occurrence: 0, testName: "TestBuildPlanCuratorSystemPromptRefusesWhenNoChannelMatchesIntent"},
	{file: "systems/claude/context.go", function: "applyClaudeCuratorContext", guard: "if !claudeSystemPromptDescriptorSupported(selected)", returned: "return fmt.Errorf(\"%w: Claude cannot apply a system-prompt descriptor\", agentic.ErrCuratorContextUnsupported)", occurrence: 0, testName: "TestBuildPlanCuratorDescriptorKindsReachClaudeAndCodexPlugins"},
	{file: "systems/claude/context.go", function: "curatorConflict", guard: "unconditional", returned: "return fmt.Errorf(\"%w: %s\", agentic.ErrCuratorContextUnsupported, reason)", occurrence: 0, testName: "TestBuildPlanClaudeCuratorMCPRefusesLegacyComposition"},
	{file: "systems/claude/context.go", function: "encodeClaudeMCP", guard: "if err != nil", returned: "return \"\", nil, invalidContext(agentic.ContextMCPServers, \"MCP configuration could not be encoded\")", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: "if (values.hasMCP || values.hasCuratorMCP) && name == mcpjson.ConfigFlag", returned: "return contextConflict(agentic.ContextMCPServers, \"native arguments already set the MCP configuration\")", occurrence: 0, testName: "TestBuildPlanClaudeCuratorMCPRefusesNativeMCPConfig", generated: true},
	{file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: "if values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == \"--system-prompt\")", returned: "return contextConflict(agentic.ContextSystemPrompt, \"native arguments already set the system prompt\")", occurrence: 0, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags", generated: true},
	{file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: "if values.hasPermission && values.permission == agentic.PermissionModeNative && isClaudePermissionSelector(name)", returned: "return contextConflict(agentic.ContextPermission, \"native arguments already set the permission posture\")", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "contextConflict", guard: "unconditional", returned: "return &agentic.ContextDescriptorConflictError{Channel: channel, Reason: reason}", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/claude/context.go", function: "invalidContext", guard: "unconditional", returned: "return &agentic.InvalidContextDescriptorError{Kind: channel, Reason: reason}", occurrence: 0, testName: "TestClaudeContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/args.go", function: "Args", guard: "if err != nil", returned: "return nil, fmt.Errorf(\"codex: %w\", err)", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "buildContextValues", guard: "if seen[descriptor.Kind]", returned: "return values, contextConflict(descriptor.Kind, \"the channel was supplied more than once\")", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "buildContextValues", guard: "if !req.Composition.IsZero()", returned: "return values, contextConflict(agentic.ContextMCPServers, \"the legacy composition already supplies launch configuration\")", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "buildContextValues", guard: "if strings.TrimSpace(payload.SystemPrompt.Text) == \"\"", returned: "return values, invalidContext(agentic.ContextSystemPrompt, \"additional prompt text must not be empty\")", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "buildContextValues", guard: "if err != nil", returned: "return values, invalidContext(agentic.ContextSystemPrompt, \"additional prompt text could not be encoded\")", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "buildContextValues", guard: "if !req.PermissionMode.IsZero()", returned: "return values, contextConflict(agentic.ContextPermission, \"PermissionMode already supplies the permission channel\")", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "buildContextValues", guard: "if err != nil", returned: "return values, err", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "buildContextValues", guard: "if err != nil", returned: "return values, err", occurrence: 1, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "buildContextValues", guard: "if err != nil", returned: "return values, fmt.Errorf(\"codex: %w\", err)", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "buildContextValues", guard: "if err != nil", returned: "return values, err", occurrence: 2, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: "if context.Environment != \"codex_cli\"", returned: "return fmt.Errorf(\"%w: Codex requires the codex_cli fragment environment\", agentic.ErrCuratorContextUnsupported)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesIncompatibleCapability"},
	{file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: "if context.MCP != nil && descriptor.Kind == agentic.ContextMCPServers", returned: "return contextConflict(descriptor.Kind, \"Curator context already supplies the MCP channel\")", occurrence: 0, testName: "TestBuildPlanCodexCuratorMCPRefusesLegacyMCPDescriptor", generated: true},
	{file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: "if context.SystemPrompt != nil && descriptor.Kind == agentic.ContextSystemPrompt", returned: "return contextConflict(descriptor.Kind, \"Curator context already supplies the system-prompt channel\")", occurrence: 0, testName: "TestBuildPlanCodexCuratorSystemPromptRefusesLegacyContextSystemPrompt"},
	{file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: "if !req.Composition.IsZero() || strings.TrimSpace(req.Profile) != \"\"", returned: "return fmt.Errorf(\"%w: Curator MCP layering conflicts with the request's composition or harness profile\", agentic.ErrCuratorContextUnsupported)", occurrence: 0, testName: "TestBuildPlanCodexCuratorMCPRefusesLegacyComposition", generated: true},
	{file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: "if descriptor.Kind != agentic.CuratorDescriptorFlag || descriptor.Flag != \"-p\" || descriptor.Argument != agentic.CuratorArgumentName || descriptor.Name != \"curator-mcp\" || len(descriptor.With) != 0", returned: "return fmt.Errorf(\"%w: Codex MCP channel is incompatible with the fragment descriptor\", agentic.ErrCuratorContextUnsupported)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: "if selected != nil", returned: "return agentic.ErrCuratorSystemPromptChannelAmbiguous", occurrence: 0, testName: "TestBuildPlanCuratorSystemPromptRefusesAmbiguousMatchingChannels", generated: true},
	{file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: "if selected == nil", returned: "return agentic.ErrCuratorSystemPromptChannelMissing", occurrence: 0, testName: "TestBuildPlanCuratorSystemPromptRefusesWhenNoChannelMatchesIntent", generated: true},
	{file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: "if !codexSystemPromptDescriptorSupported(selected)", returned: "return fmt.Errorf(\"%w: Codex cannot apply this system-prompt descriptor\", agentic.ErrCuratorContextUnsupported)", occurrence: 0, testName: "TestBuildPlanCuratorDescriptorKindsReachClaudeAndCodexPlugins"},
	{file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: "if err != nil", returned: "return fmt.Errorf(\"%w: system-prompt path could not be encoded\", agentic.ErrCuratorContextMalformed)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "systems/codex/context.go", function: "encodeCodexMCP.func-literal@187", guard: "if err != nil", returned: "return invalidContext(agentic.ContextMCPServers, \"MCP configuration value could not be encoded\")", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "encodeCodexMCP", guard: "if err != nil", returned: "return nil, invalidContext(agentic.ContextMCPServers, fmt.Sprintf(\"Codex MCP configuration failed its plugin grammar: %v\", err))", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "rejectNativeContextConflicts", guard: "if values.hasCuratorMCP && (name == \"-p\" || name == \"--profile\" || isAttachedProfileValue(el))", returned: "return contextConflict(agentic.ContextMCPServers, \"native arguments already select a Codex profile\")", occurrence: 0, testName: "TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector"},
	{file: "systems/codex/context.go", function: "rejectNativeContextConflicts", guard: "if values.hasSystemPrompt && key == developerInstructionsConfigKey", returned: "return contextConflict(agentic.ContextSystemPrompt, \"native arguments already set developer instructions\")", occurrence: 0, testName: "TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig"},
	{file: "systems/codex/context.go", function: "rejectNativeContextConflicts", guard: "if values.curatorSystemPrompt != nil && key == values.curatorSystemPrompt.key", returned: "return contextConflict(agentic.ContextSystemPrompt, \"native arguments already set the Curator system-prompt file\")", occurrence: 0, testName: "TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig"},
	{file: "systems/codex/context.go", function: "rejectNativeContextConflicts", guard: "if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == \"mcp_servers\" || strings.HasPrefix(key, mcpServersKeyPrefix))", returned: "return contextConflict(agentic.ContextMCPServers, \"native arguments already set MCP configuration\")", occurrence: 0, testName: "TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig", generated: true},
	{file: "systems/codex/context.go", function: "rejectNativeContextConflicts", guard: "if values.hasPermission && values.permission == agentic.PermissionModeNative && isConflictingConfigKey(key)", returned: "return contextConflict(agentic.ContextPermission, \"native arguments already set the permission posture\")", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "rejectNativeContextConflicts", guard: "if values.hasPermission && values.permission == agentic.PermissionModeNative && isCodexPermissionSelector(name)", returned: "return contextConflict(agentic.ContextPermission, \"native arguments already set the permission posture\")", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "contextConflict", guard: "unconditional", returned: "return &agentic.ContextDescriptorConflictError{Channel: channel, Reason: reason}", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/context.go", function: "invalidContext", guard: "unconditional", returned: "return &agentic.InvalidContextDescriptorError{Kind: channel, Reason: reason}", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/policy.go", function: "scanNativePolicy", guard: "if isConflictingConfigKey(key)", returned: "return &agentic.NativePolicyConflictError{Selector: key, Placement: placement}", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/policy.go", function: "scanNativePolicy", guard: "if isConflictingConfigKey(key)", returned: "return &agentic.NativePolicyConflictError{Selector: key, Placement: agentic.NativePolicyPlacementAttachedShort}", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/policy.go", function: "scanNativePolicy", guard: "if name == \"-a\" || name == \"--ask-for-approval\"", returned: "return &agentic.NativePolicyConflictError{Selector: name, Placement: placement}", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/policy.go", function: "scanNativePolicy", guard: "if name == \"-s\" || name == \"--sandbox\"", returned: "return &agentic.NativePolicyConflictError{Selector: name, Placement: placement}", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/policy.go", function: "scanNativePolicy", guard: "if name == approveForMeFlag", returned: "return &agentic.NativePolicyConflictError{Selector: name, Placement: nativePolicyFlagPlacement(hasValue)}", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "systems/codex/policy.go", function: "scanNativePolicy", guard: "if strings.HasPrefix(name, \"--dangerously-bypass-\")", returned: "return &agentic.NativePolicyConflictError{Selector: name, Placement: nativePolicyFlagPlacement(hasValue)}", occurrence: 0, testName: "TestCodexContextDescriptorRefusalsReachBuildPlan"},
	{file: "context.go", function: "DecodeContextDescriptor", guard: "switch descriptor.Kind case default", returned: "return ContextDescriptorPayload{}, &UnknownContextDescriptorError{Kind: descriptor.Kind}", occurrence: 0, outOfContract: "Acceptance-criteria surface is pkg/agentic/curator_context.go; context descriptor decoding is outside that surface."},
	{file: "system.go", function: "NormalizeSystemID", guard: "if !ok", returned: "return \"\", &InvalidSystemIDError{Raw: raw, Reason: reason}", occurrence: 0, outOfContract: "Acceptance-criteria surface is pkg/agentic/curator_context.go; system ID normalization is outside that surface."},
	{file: "systems/claude/policy.go", function: "scanNativePolicy", guard: "if name == permissionModeFlag", returned: "return &agentic.NativePolicyConflictError{Selector: name, Placement: placement}", occurrence: 0, outOfContract: "Acceptance-criteria surface is pkg/agentic/curator_context.go; Claude native-policy scanning is outside that surface."},
	{file: "systems/codex/provider.go", function: "localProviderArgs", guard: "if strings.TrimSpace(providerID) == \"\"", returned: "return nil, &agentic.LocalProviderRefusal{Kind: agentic.LocalProviderUnbound}", occurrence: 0, outOfContract: "Acceptance-criteria surface is pkg/agentic/curator_context.go; Codex provider resolution is outside that surface."},
	{file: "systems/pi/result.go", function: "errorTurnResult", guard: "unconditional", returned: "return result, &TurnResultError{Class: class, Code: code}", occurrence: 0, outOfContract: "Acceptance-criteria surface is pkg/agentic/curator_context.go; Pi turn-result validation is outside that surface."},
	{file: "context.go", function: "DecodeContextDescriptor.func-literal@153", guard: "unconditional", returned: "return ContextDescriptorPayload{}, &InvalidContextDescriptorError{Kind: descriptor.Kind, Reason: reason}", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; generic descriptor decoding is outside this validator surface."},
	{file: "context.go", function: "DecodeContextDescriptor", guard: "switch descriptor.Kind case default", returned: "return ContextDescriptorPayload{}, &UnknownContextDescriptorError{Kind: descriptor.Kind}", occurrence: 1, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; generic descriptor decoding is outside this validator surface."},
	{file: "context.go", function: "ValidateMCPServers.func-literal@186", guard: "unconditional", returned: "return &InvalidContextDescriptorError{Kind: ContextMCPServers, Reason: reason}", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; generic MCP transport validation is outside this validator surface."},
	{file: "systems/claude/policy.go", function: "scanNativePolicy", guard: "if name == allowDangerouslySkipPermissionsFlag || name == restrictedFlag", returned: "return &agentic.NativePolicyConflictError{Selector: name, Placement: placement}", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Claude native-policy scanning is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "localProviderArgs", guard: "if providerID != strings.TrimSpace(providerID)", returned: "return nil, localProviderRefusal(agentic.LocalProviderMalformed, \"provider id\")", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "localProviderArgs", guard: "if mode != agentic.LaunchModeExec && mode != agentic.LaunchModeDryRun", returned: "return nil, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "localProviderArgs", guard: "if !strings.HasPrefix(providerID, \"local-\")", returned: "return nil, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "localProviderArgs", guard: "if !providerIDPattern.MatchString(providerID)", returned: "return nil, localProviderRefusal(agentic.LocalProviderMalformed, \"provider id\")", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil", returned: "return nil, localProviderRefusal(kind, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "localProviderArgs", guard: "if err != nil || config == nil", returned: "return nil, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "localProviderHome", guard: "if !ok", returned: "return \"\", localProviderRefusal(agentic.LocalProviderUnbound, \"provider home\")", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "localProviderHome", guard: "if !resolvedOK || resolved != root", returned: "return \"\", localProviderRefusal(agentic.LocalProviderConflicting, \"provider home\")", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnbound, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !found", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnbound, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok || strings.TrimSpace(name) == \"\" || strings.TrimSpace(name) != name || strings.IndexFunc(name, unicode.IsControl) >= 0", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok || strings.TrimSpace(baseURL) != baseURL", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if errors.Is(err, errMalformedProviderURL)", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if err != nil", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 1, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if wireAPI != \"responses\"", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !found", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if !ok", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderMalformed, providerID)", occurrence: 2, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if requires", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "resolvePrivateProvider", guard: "if found", returned: "return privateProvider{}, localProviderRefusal(agentic.LocalProviderUnsupported, providerID)", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider resolution is outside this validator surface."},
	{file: "systems/codex/provider.go", function: "localProviderRefusal", guard: "unconditional", returned: "return &agentic.LocalProviderRefusal{Kind: kind, File: \"config.toml\", Subject: subject}", occurrence: 0, outOfContract: "Acceptance-criteria surface is TASK-260929-23jskk curator-context validation; Codex local-provider refusal construction is outside this validator surface."},
	{file: "plan.go", function: "BuildPlan", guard: "unconditional", returned: "return buildPlan(r, req, mode, nil)", occurrence: 0, testName: "TestBuildPlanCuratorContextRefusesMalformedFragment"},
	{file: "systems/agy/agy.go", function: "System.Argv", guard: "unconditional", returned: "return Args(req, mode, s.binary())", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Agy argv delegation is outside this validator surface."},
	{file: "systems/claude/args.go", function: "Args", guard: "switch mode case agentic.LaunchModeInteractive", returned: "return interactiveArgs(req, context)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Claude interactive argv delegation is outside this validator surface."},
	{file: "systems/claude/claude.go", function: "System.Argv", guard: "unconditional", returned: "return Args(req, mode)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Claude argv delegation is outside this validator surface."},
	{file: "systems/codex/codex.go", function: "System.Argv", guard: "unconditional", returned: "return Args(req, mode)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Codex argv delegation is outside this validator surface."},
	{file: "systems/gemini/gemini.go", function: "System.Argv", guard: "unconditional", returned: "return Args(req, mode)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Gemini argv delegation is outside this validator surface."},
	{file: "systems/muse/muse.go", function: "System.Argv", guard: "unconditional", returned: "return Args(req, mode)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Muse argv delegation is outside this validator surface."},
	{file: "systems/pi/args.go", function: "Args", guard: "unconditional", returned: "return args(req, agentic.LaunchModeExec)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Pi argv-mode delegation is outside this validator surface."},
	{file: "systems/pi/args.go", function: "args", guard: "if mode == agentic.LaunchModeInteractive", returned: "return interactiveArgs(req, effective)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Pi interactive argv delegation is outside this validator surface."},
	{file: "systems/pi/pi.go", function: "System.Argv", guard: "unconditional", returned: "return args(req, mode)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Pi argv delegation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "if input.Cleanup != ProcessACleanupNotRequired || input.Exit.Signaled", returned: "return invalidTurnResult()", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "if input.Cleanup != ProcessACleanupSucceeded && input.Cleanup != ProcessACleanupFailed", returned: "return invalidTurnResult()", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "if input.Cleanup == ProcessACleanupFailed", returned: "return errorTurnResult(TurnResultCleanupFailed, TurnCodeCleanupFailed)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "if input.Intervention == TurnInterventionCancel", returned: "return errorTurnResult(TurnResultCancelled, TurnCodeCancelled)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "switch input.Intervention case TurnInterventionCancel, TurnInterventionDeadline", returned: "return errorTurnResult(TurnResultDeadlineExceeded, TurnCodeDeadlineExceeded)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "switch input.Intervention case default", returned: "return invalidTurnResult()", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "if input.StdoutTruncated || len(stdout) > TurnResultMaxStdoutBytes", returned: "return invalidTurnResult()", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "if input.Exit.Code < 0 || input.Exit.Code > 3", returned: "return invalidTurnResult()", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "if err != nil || document.contract != TurnResultContract || document.schemaVersion != TurnResultSchemaVersion", returned: "return invalidTurnResult()", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "if input.Exit.Code != 0 || !document.finalTextPresent || document.errorPresent", returned: "return invalidTurnResult()", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "if document.status != \"error\" || document.finalTextPresent || !document.errorPresent", returned: "return invalidTurnResult()", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "if !ok || input.Exit.Code != expectedExit", returned: "return invalidTurnResult()", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "ValidateTurnResult", guard: "unconditional", returned: "return errorTurnResult(class, document.code)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pi/result.go", function: "invalidTurnResult", guard: "unconditional", returned: "return errorTurnResult(TurnResultInvalid, TurnCodeResultInvalid)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the surface is curator-context validation; Pi turn-result validation is outside this validator surface."},
	{file: "systems/pinative/pinative.go", function: "System.Argv", guard: "unconditional", returned: "return Args(req, mode)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Pi Native argv delegation is outside this validator surface."},
	{file: "systems/qwen/qwen.go", function: "System.Argv", guard: "unconditional", returned: "return Args(req, mode)", occurrence: 0, outOfContract: "Acceptance-criteria clause: the task changes evidence for curator-context validators and preserves accepted plugin behavior; Qwen argv delegation is outside this validator surface."},
}

type curatorNarrowingMutationMember struct {
	name        string
	file        string
	function    string
	guard       string
	returned    string
	occurrence  int
	member      string
	exemption   string
	testName    string
	runPattern  string
	failureText string
	narrows     string
}

// The mutant runner reads this same test-owned matrix, resolves every guard
// through the AST catalog above, and adds a one-member exemption to that guard.
var curatorConflictMutationMembers = []curatorNarrowingMutationMember{
	{name: "codex-curator-composition-prefix-only", file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: `!req.Composition.IsZero() || strings.TrimSpace(req.Profile) != ""`, member: "prefix-only", exemption: `strings.TrimSpace(req.Profile) == "" && len(req.Composition.Servers) == 0 && len(req.Composition.Prefix) > 0`, testName: "TestBuildPlanCodexCuratorMCPRefusesLegacyComposition/prefix-only", runPattern: `^TestBuildPlanCodexCuratorMCPRefusesLegacyComposition$/^prefix-only$`, failureText: "want ErrCuratorContextUnsupported for Curator MCP plus legacy composition", narrows: "admits the prefix-only Codex legacy composition member"},
	{name: "codex-curator-composition-prefix-with-servers", file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: `!req.Composition.IsZero() || strings.TrimSpace(req.Profile) != ""`, member: "prefix-with-servers", exemption: `strings.TrimSpace(req.Profile) == "" && len(req.Composition.Servers) > 0 && len(req.Composition.Prefix) > 0`, testName: "TestBuildPlanCodexCuratorMCPRefusesLegacyComposition/prefix-with-servers", runPattern: `^TestBuildPlanCodexCuratorMCPRefusesLegacyComposition$/^prefix-with-servers$`, failureText: "want ErrCuratorContextUnsupported for Curator MCP plus legacy composition", narrows: "admits the Codex legacy composition member with servers"},
	{name: "claude-curator-native-prompt-system-prompt-separated-append", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "append/system-prompt/separated", exemption: `name == "--system-prompt" && !strings.Contains(args[index], "=") && values.systemPromptFlag == "--append-system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/append/system-prompt/separated", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^append$/^system-prompt$/^separated$`, failureText: "want ErrContextDescriptorConflict for native --system-prompt separated spelling beside Curator append prompt", narrows: "admits only --system-prompt separated with append intent"},
	{name: "claude-curator-native-prompt-system-prompt-equals-append", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "append/system-prompt/equals", exemption: `name == "--system-prompt" && strings.Contains(args[index], "=") && values.systemPromptFlag == "--append-system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/append/system-prompt/equals", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^append$/^system-prompt$/^equals$`, failureText: "want ErrContextDescriptorConflict for native --system-prompt equals spelling beside Curator append prompt", narrows: "admits only --system-prompt equals with append intent"},
	{name: "claude-curator-native-prompt-system-prompt-separated-replace", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "replace/system-prompt/separated", exemption: `name == "--system-prompt" && !strings.Contains(args[index], "=") && values.systemPromptFlag == "--system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/replace/system-prompt/separated", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^replace$/^system-prompt$/^separated$`, failureText: "want ErrContextDescriptorConflict for native --system-prompt separated spelling beside Curator replace prompt", narrows: "admits only --system-prompt separated with replace intent"},
	{name: "claude-curator-native-prompt-system-prompt-equals-replace", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "replace/system-prompt/equals", exemption: `name == "--system-prompt" && strings.Contains(args[index], "=") && values.systemPromptFlag == "--system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/replace/system-prompt/equals", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^replace$/^system-prompt$/^equals$`, failureText: "want ErrContextDescriptorConflict for native --system-prompt equals spelling beside Curator replace prompt", narrows: "admits only --system-prompt equals with replace intent"},
	{name: "claude-curator-native-prompt-append-system-prompt-separated-append", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "append/append-system-prompt/separated", exemption: `name == "--append-system-prompt" && !strings.Contains(args[index], "=") && values.systemPromptFlag == "--append-system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/append/append-system-prompt/separated", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^append$/^append-system-prompt$/^separated$`, failureText: "want ErrContextDescriptorConflict for native --append-system-prompt separated spelling beside Curator append prompt", narrows: "admits only --append-system-prompt separated with append intent"},
	{name: "claude-curator-native-prompt-append-system-prompt-equals-append", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "append/append-system-prompt/equals", exemption: `name == "--append-system-prompt" && strings.Contains(args[index], "=") && values.systemPromptFlag == "--append-system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/append/append-system-prompt/equals", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^append$/^append-system-prompt$/^equals$`, failureText: "want ErrContextDescriptorConflict for native --append-system-prompt equals spelling beside Curator append prompt", narrows: "admits only --append-system-prompt equals with append intent"},
	{name: "claude-curator-native-prompt-append-system-prompt-separated-replace", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "replace/append-system-prompt/separated", exemption: `name == "--append-system-prompt" && !strings.Contains(args[index], "=") && values.systemPromptFlag == "--system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/replace/append-system-prompt/separated", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^replace$/^append-system-prompt$/^separated$`, failureText: "want ErrContextDescriptorConflict for native --append-system-prompt separated spelling beside Curator replace prompt", narrows: "admits only --append-system-prompt separated with replace intent"},
	{name: "claude-curator-native-prompt-append-system-prompt-equals-replace", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "replace/append-system-prompt/equals", exemption: `name == "--append-system-prompt" && strings.Contains(args[index], "=") && values.systemPromptFlag == "--system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/replace/append-system-prompt/equals", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^replace$/^append-system-prompt$/^equals$`, failureText: "want ErrContextDescriptorConflict for native --append-system-prompt equals spelling beside Curator replace prompt", narrows: "admits only --append-system-prompt equals with replace intent"},
	{name: "claude-curator-native-prompt-append-system-prompt-file-separated-append", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "append/append-system-prompt-file/separated", exemption: `name == "--append-system-prompt-file" && !strings.Contains(args[index], "=") && values.systemPromptFlag == "--append-system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/append/append-system-prompt-file/separated", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^append$/^append-system-prompt-file$/^separated$`, failureText: "want ErrContextDescriptorConflict for native --append-system-prompt-file separated spelling beside Curator append prompt", narrows: "admits only --append-system-prompt-file separated with append intent"},
	{name: "claude-curator-native-prompt-append-system-prompt-file-equals-append", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "append/append-system-prompt-file/equals", exemption: `name == "--append-system-prompt-file" && strings.Contains(args[index], "=") && values.systemPromptFlag == "--append-system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/append/append-system-prompt-file/equals", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^append$/^append-system-prompt-file$/^equals$`, failureText: "want ErrContextDescriptorConflict for native --append-system-prompt-file equals spelling beside Curator append prompt", narrows: "admits only --append-system-prompt-file equals with append intent"},
	{name: "claude-curator-native-prompt-append-system-prompt-file-separated-replace", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "replace/append-system-prompt-file/separated", exemption: `name == "--append-system-prompt-file" && !strings.Contains(args[index], "=") && values.systemPromptFlag == "--system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/replace/append-system-prompt-file/separated", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^replace$/^append-system-prompt-file$/^separated$`, failureText: "want ErrContextDescriptorConflict for native --append-system-prompt-file separated spelling beside Curator replace prompt", narrows: "admits only --append-system-prompt-file separated with replace intent"},
	{name: "claude-curator-native-prompt-append-system-prompt-file-equals-replace", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "replace/append-system-prompt-file/equals", exemption: `name == "--append-system-prompt-file" && strings.Contains(args[index], "=") && values.systemPromptFlag == "--system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/replace/append-system-prompt-file/equals", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^replace$/^append-system-prompt-file$/^equals$`, failureText: "want ErrContextDescriptorConflict for native --append-system-prompt-file equals spelling beside Curator replace prompt", narrows: "admits only --append-system-prompt-file equals with replace intent"},
	{name: "claude-curator-native-prompt-system-prompt-file-separated-append", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "append/system-prompt-file/separated", exemption: `name == "--system-prompt-file" && !strings.Contains(args[index], "=") && values.systemPromptFlag == "--append-system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/append/system-prompt-file/separated", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^append$/^system-prompt-file$/^separated$`, failureText: "want ErrContextDescriptorConflict for native --system-prompt-file separated spelling beside Curator append prompt", narrows: "admits only --system-prompt-file separated with append intent"},
	{name: "claude-curator-native-prompt-system-prompt-file-equals-append", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "append/system-prompt-file/equals", exemption: `name == "--system-prompt-file" && strings.Contains(args[index], "=") && values.systemPromptFlag == "--append-system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/append/system-prompt-file/equals", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^append$/^system-prompt-file$/^equals$`, failureText: "want ErrContextDescriptorConflict for native --system-prompt-file equals spelling beside Curator append prompt", narrows: "admits only --system-prompt-file equals with append intent"},
	{name: "claude-curator-native-prompt-system-prompt-file-separated-replace", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "replace/system-prompt-file/separated", exemption: `name == "--system-prompt-file" && !strings.Contains(args[index], "=") && values.systemPromptFlag == "--system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/replace/system-prompt-file/separated", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^replace$/^system-prompt-file$/^separated$`, failureText: "want ErrContextDescriptorConflict for native --system-prompt-file separated spelling beside Curator replace prompt", narrows: "admits only --system-prompt-file separated with replace intent"},
	{name: "claude-curator-native-prompt-system-prompt-file-equals-replace", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `values.hasSystemPrompt && (name == values.systemPromptFlag || name == appendSystemPromptFlag || name == appendSystemPromptFileFlag || name == replaceSystemPromptFileFlag || name == "--system-prompt")`, member: "replace/system-prompt-file/equals", exemption: `name == "--system-prompt-file" && strings.Contains(args[index], "=") && values.systemPromptFlag == "--system-prompt-file"`, testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags/replace/system-prompt-file/equals", runPattern: `^TestBuildPlanClaudeCuratorSystemPromptRefusesNativePromptFlags$/^replace$/^system-prompt-file$/^equals$`, failureText: "want ErrContextDescriptorConflict for native --system-prompt-file equals spelling beside Curator replace prompt", narrows: "admits only --system-prompt-file equals with replace intent"},
	{name: "claude-curator-legacy-mcp-descriptor", file: "systems/claude/context.go", function: "applyClaudeCuratorContext", guard: `context.MCP != nil && descriptor.Kind == agentic.ContextMCPServers`, member: "legacy-mcp-descriptor", exemption: `descriptor.MCP != nil && len(descriptor.MCP.Servers) == 1 && descriptor.MCP.Servers[0].Name == "legacy-mcp-member"`, testName: "TestBuildPlanClaudeCuratorMCPRefusesLegacyMCPDescriptor", runPattern: `^TestBuildPlanClaudeCuratorMCPRefusesLegacyMCPDescriptor$`, failureText: "want ErrContextDescriptorConflict for a legacy MCP descriptor beside Curator MCP", narrows: "admits the named legacy MCP descriptor beside Claude Curator MCP"},
	{name: "codex-curator-legacy-mcp-descriptor", file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: `context.MCP != nil && descriptor.Kind == agentic.ContextMCPServers`, member: "legacy-mcp-descriptor", exemption: `descriptor.MCP != nil && len(descriptor.MCP.Servers) == 1 && descriptor.MCP.Servers[0].Name == "legacy-mcp-member"`, testName: "TestBuildPlanCodexCuratorMCPRefusesLegacyMCPDescriptor", runPattern: `^TestBuildPlanCodexCuratorMCPRefusesLegacyMCPDescriptor$`, failureText: "want ErrContextDescriptorConflict for a legacy MCP descriptor beside Curator MCP", narrows: "admits the named legacy MCP descriptor beside Codex Curator MCP"},
	{name: "curator-claude-local-provider-unsupported", file: "plan.go", function: "buildPlan", guard: `!caps.SupportsLocalProvider`, member: "claude_code Curator fragment plus local-provider binding", exemption: `req.Context != nil && req.Context.Environment == "claude_code"`, testName: "TestBuildPlanCuratorContextPreservesLocalProviderRefusalPrecedence/unsupported-transport-before-channel-validation", runPattern: `^TestBuildPlanCuratorContextPreservesLocalProviderRefusalPrecedence$/^unsupported-transport-before-channel-validation$`, failureText: "want typed local-provider unsupported refusal before Curator validation", narrows: "admits a selected local provider on Claude when a Curator fragment is present"},
	{name: "curator-codex-local-provider-unbound", file: "plan.go", function: "buildPlan", guard: `strings.TrimSpace(req.LocalProvider.ID) == ""`, member: "empty Codex local-provider binding with a Curator environment mismatch", exemption: `req.Context != nil && req.Context.Environment == "claude_code"`, testName: "TestBuildPlanCuratorContextPreservesLocalProviderRefusalPrecedence/unbound-before-incompatible-context", runPattern: `^TestBuildPlanCuratorContextPreservesLocalProviderRefusalPrecedence$/^unbound-before-incompatible-context$`, failureText: "want typed local-provider unbound refusal before Curator capability validation", narrows: "admits an empty Codex provider binding for the mapped cross-plugin Curator context"},
	{name: "codex-system-prompt-ambiguous", file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: `selected != nil`, member: "ambiguous replace channels", exemption: `context.SystemPrompt.Intent == agentic.CuratorSystemPromptReplace`, testName: "TestBuildPlanCuratorSystemPromptRefusesAmbiguousMatchingChannels/codex", runPattern: `^TestBuildPlanCuratorSystemPromptRefusesAmbiguousMatchingChannels$/^codex$`, failureText: "want ErrCuratorSystemPromptChannelAmbiguous", narrows: "admits duplicate matching Codex replace descriptors"},
	{name: "codex-system-prompt-missing", file: "systems/codex/context.go", function: "applyCodexCuratorContext", guard: `selected == nil`, member: "append intent without a matching channel", exemption: `context.SystemPrompt.Intent == agentic.CuratorSystemPromptAppend`, testName: "TestBuildPlanCuratorSystemPromptRefusesWhenNoChannelMatchesIntent/codex", runPattern: `^TestBuildPlanCuratorSystemPromptRefusesWhenNoChannelMatchesIntent$/^codex$`, failureText: "want ErrCuratorSystemPromptChannelMissing", narrows: "admits an append intent with only a replace channel"},
	{name: "claude-native-mcp-config", file: "systems/claude/context.go", function: "rejectNativeContextConflicts", guard: `(values.hasMCP || values.hasCuratorMCP) && name == mcpjson.ConfigFlag`, member: "native MCP config while Curator MCP is active", exemption: `values.hasCuratorMCP && name == mcpjson.ConfigFlag`, testName: "TestBuildPlanClaudeCuratorMCPRefusesNativeMCPConfig/long-separated", runPattern: `^TestBuildPlanClaudeCuratorMCPRefusesNativeMCPConfig$/^long-separated$`, failureText: "want ErrContextDescriptorConflict", narrows: "admits one native --mcp-config spelling with Curator MCP"},
	{name: "codex-native-mcp-root-config", file: "systems/codex/context.go", function: "rejectNativeContextConflicts", guard: `(values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix))`, member: "root native mcp_servers override", exemption: `values.hasCuratorMCP && key == "mcp_servers"`, testName: "TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig/root/long-separated", runPattern: `^TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig$/^root/long-separated$`, failureText: "want ErrContextDescriptorConflict", narrows: "admits the native root mcp_servers override with Curator MCP"},
}

func TestCuratorPluginRefusalSitesHaveNamedBuildPlanCoverage(t *testing.T) {
	moduleRoot := filepath.Clean(filepath.Join(testPackageDirectory(t), "..", ".."))
	sites, err := refusalscan.Discover(moduleRoot)
	if err != nil {
		t.Fatalf("derive refusal sites from the full agentic source tree: %v", err)
	}
	coverage := make(map[string]curatorRefusalCoverageRow, len(curatorRefusalCoverageTable))
	for _, row := range curatorRefusalCoverageTable {
		site, ok := resolveCuratorRefusalMapping(row, sites)
		if !ok {
			t.Errorf("refusal coverage row does not resolve to exactly one return site in the full source enumeration: %s :: %s :: %s :: %s", row.file, row.function, row.guard, row.returned)
			continue
		}
		key := site.Key()
		if _, exists := coverage[key]; exists {
			t.Errorf("duplicate refusal coverage mapping for %s", key)
		}
		coverage[key] = row
	}
	functions := parseAcceptanceTestFunctions(t, filepath.Join(moduleRoot, "pkg", "agentic"))
	seen := make(map[string]bool, len(sites))
	for _, site := range unmappedCuratorRefusalSites(sites, coverage) {
		t.Errorf("unmapped typed refusal return in the full agentic source tree: %s (line %d)", site.Key(), site.Line)
	}
	for _, site := range sites {
		key := site.Key()
		seen[key] = true
		row, ok := coverage[key]
		if !ok || row.outOfContract != "" {
			continue
		}
		if !testReachesBuildPlan(row.testName, functions, map[string]bool{}) {
			t.Errorf("mapped test %s for %s does not reach agentic.BuildPlan", row.testName, key)
		}
	}
	for key, row := range coverage {
		if !seen[key] {
			t.Errorf("stale refusal coverage mapping %s -> %s", key, row.testName)
		}
	}
	namedBuildPlanSites := 0
	for _, row := range coverage {
		if row.testName != "" {
			namedBuildPlanSites++
		}
	}
	t.Logf("refusal-site coverage: %d of %d sites have named BuildPlan tests; sites = recognized refusal constructors and ErrCurator-prefixed sentinels in the two allowed shapes; forwarding through other helpers, named-result bare returns and unprefixed sentinels are out of scope, tracked by TASK-260930-3txv44; other shapes and non-regular files refuse", namedBuildPlanSites, len(sites))

	membersBySite := make(map[string]int)
	memberNames := make(map[string]bool)
	for _, member := range curatorConflictMutationMembers {
		row := curatorRefusalCoverageRow{
			file: member.file, function: member.function, guard: member.guard,
			returned: member.returned, occurrence: member.occurrence,
		}
		site, ok := resolveCuratorRefusalMapping(row, sites)
		if !ok {
			t.Errorf("narrowing mutant %s does not resolve to exactly one enumerated refusal return: %s :: %s :: %s", member.name, member.file, member.function, member.guard)
			continue
		}
		key := site.Key()
		coverageRow, covered := coverage[key]
		if !covered {
			t.Errorf("narrowing mutant %s maps to a return without named test coverage: %s", member.name, key)
			continue
		}
		if !coverageRow.generated {
			t.Errorf("narrowing mutant %s is not marked as generated coverage for %s", member.name, key)
		}
		if member.testName == "" || strings.SplitN(member.testName, "/", 2)[0] != coverageRow.testName {
			t.Errorf("narrowing mutant %s is not killed by the mapped test %s", member.name, coverageRow.testName)
		}
		if _, exists := memberNames[member.name]; exists {
			t.Errorf("duplicate narrowing mutant name %s", member.name)
		}
		memberNames[member.name] = true
		membersBySite[key]++
	}
	for key, row := range coverage {
		if row.generated && membersBySite[key] == 0 {
			t.Errorf("generated refusal site has no narrowing mutant members: %s", key)
		}
	}
}

func TestCuratorRefusalCoverageRejectsUnmappedNewSourceFile(t *testing.T) {
	moduleRoot := t.TempDir()
	newSource := filepath.Join(moduleRoot, "pkg", "agentic", "zz_new.go")
	if err := os.MkdirAll(filepath.Dir(newSource), 0o700); err != nil {
		t.Fatal(err)
	}
	const source = `package agentic
import "errors"
var ErrCuratorContextMalformed = errors.New("malformed typed Curator context")
func refusalFromNewSourceFile() error {
	if true {
		return ErrCuratorContextMalformed
	}
	return nil
}
`
	if err := os.WriteFile(newSource, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	sites, err := refusalscan.Discover(moduleRoot)
	if err != nil {
		t.Fatalf("discover refusal in new source file: %v", err)
	}
	unmapped := unmappedCuratorRefusalSites(sites, nil)
	if len(unmapped) != 1 || unmapped[0].File != "zz_new.go" {
		t.Fatalf("guard unmapped-source scan = %#v, want exactly the zz_new.go refusal site", unmapped)
	}
}

func TestCuratorRefusalCoverageRejectsUnmappedSitesInCoveredFiles(t *testing.T) {
	for _, attack := range []struct {
		name         string
		file         string
		source       string
		coveredGuard string
	}{
		{
			name: "B claude context site",
			file: "pkg/agentic/systems/claude/context.go",
			source: `package claude
import "errors"
var ErrCuratorContextMalformed = errors.New("malformed")
func covered() error { if true { return ErrCuratorContextMalformed }; return nil }
func added() error { if false { return ErrCuratorContextMalformed }; return nil }
`,
			coveredGuard: "if true",
		},
		{
			name: "B2 plan site",
			file: "pkg/agentic/plan.go",
			source: `package agentic
import "errors"
var ErrCuratorContextMalformed = errors.New("malformed")
func covered() error { if true { return ErrCuratorContextMalformed }; return nil }
func added() error { if false { return ErrCuratorContextMalformed }; return nil }
`,
			coveredGuard: "if true",
		},
		{
			name: "D out-of-contract row covers one site",
			file: "pkg/agentic/systems/pi/result.go",
			source: `package pi
import "errors"
var ErrCuratorContextMalformed = errors.New("malformed")
func covered() error { if true { return ErrCuratorContextMalformed }; return nil }
func added() error { if false { return ErrCuratorContextMalformed }; return nil }
`,
			coveredGuard: "if true",
		},
	} {
		t.Run(attack.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, filepath.FromSlash(attack.file))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(attack.source), 0o600); err != nil {
				t.Fatal(err)
			}
			sites, err := refusalscan.Discover(root)
			if err != nil {
				t.Fatalf("discover refusal sites: %v", err)
			}
			var covered map[string]curatorRefusalCoverageRow
			for _, site := range sites {
				if site.Guard == attack.coveredGuard {
					covered = map[string]curatorRefusalCoverageRow{site.Key(): {
						file: attack.file, function: site.Function, guard: attack.coveredGuard,
						returned: site.Return, occurrence: site.Occurrence,
						outOfContract: "only this enumerated site is out of contract",
					}}
					break
				}
			}
			if len(covered) != 1 {
				t.Fatalf("attack fixture has no exact covered site: %#v", sites)
			}
			unmapped := unmappedCuratorRefusalSites(sites, covered)
			if len(unmapped) != 1 || unmapped[0].Function != "added" {
				t.Fatalf("per-site guard accepted an added refusal beside an existing row: %#v", unmapped)
			}
		})
	}
}

func unmappedCuratorRefusalSites(sites []refusalscan.Site, coverage map[string]curatorRefusalCoverageRow) []refusalscan.Site {
	var unmapped []refusalscan.Site
	for _, site := range sites {
		if _, found := coverage[site.Key()]; !found {
			unmapped = append(unmapped, site)
		}
	}
	sort.Slice(unmapped, func(i, j int) bool {
		if unmapped[i].File != unmapped[j].File {
			return unmapped[i].File < unmapped[j].File
		}
		if unmapped[i].Line != unmapped[j].Line {
			return unmapped[i].Line < unmapped[j].Line
		}
		return unmapped[i].Key() < unmapped[j].Key()
	})
	return unmapped
}

func resolveCuratorRefusalMapping(row curatorRefusalCoverageRow, sites []refusalscan.Site) (refusalscan.Site, bool) {
	var matches []refusalscan.Site
	for _, site := range sites {
		guard := row.guard
		if !strings.HasPrefix(guard, "if ") && !strings.HasPrefix(guard, "switch ") && guard != "unconditional" {
			guard = "if " + guard
		}
		if site.File != row.file || site.Function != row.function || site.Guard != guard {
			continue
		}
		if row.returned != "" && site.Return != row.returned {
			continue
		}
		if site.Occurrence != row.occurrence {
			continue
		}
		matches = append(matches, site)
	}
	if len(matches) != 1 {
		return refusalscan.Site{}, false
	}
	return matches[0], true
}

func testPackageDirectory(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get test package directory: %v", err)
	}
	return workingDirectory
}

func parseAcceptanceTestFunctions(t *testing.T, packageDirectory string) map[string]*ast.FuncDecl {
	t.Helper()
	var files []string
	err := filepath.WalkDir(packageDirectory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("find acceptance test files: %v", err)
	}
	sort.Strings(files)
	functions := make(map[string]*ast.FuncDecl)
	for _, path := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse acceptance test file %s: %v", path, err)
		}
		for _, declaration := range parsed.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
				functions[function.Name.Name] = function
			}
		}
	}
	return functions
}

func testReachesBuildPlan(name string, functions map[string]*ast.FuncDecl, visiting map[string]bool) bool {
	if visiting[name] {
		return false
	}
	function := functions[name]
	if function == nil {
		return false
	}
	visiting[name] = true
	defer delete(visiting, name)
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
			if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "agentic" && selector.Sel.Name == "BuildPlan" {
				found = true
				return false
			}
		}
		if direct, ok := call.Fun.(*ast.Ident); ok && direct.Name == "BuildPlan" {
			found = true
			return false
		}
		if helper, ok := call.Fun.(*ast.Ident); ok && testReachesBuildPlan(helper.Name, functions, visiting) {
			found = true
			return false
		}
		return true
	})
	return found
}
