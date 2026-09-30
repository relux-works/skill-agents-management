package vendorplugin

import "github.com/relux-works/skill-agents-management/pkg/agentic"

// The vendor boundary owns two independent copies: admission's reference and
// the vendor callback's input. Preserve nil versus empty collections for fidelity.
func cloneCuratorContext(source *agentic.CuratorContext) *agentic.CuratorContext {
	if source == nil {
		return nil
	}
	out := *source
	out.Env = cloneContextMap(source.Env)
	out.MCP = cloneCuratorMCP(source.MCP)
	if source.SystemPrompt != nil {
		prompt := *source.SystemPrompt
		prompt.Channels = cloneCuratorChannels(prompt.Channels)
		out.SystemPrompt = &prompt
	}
	if source.ExpectedProvenance != nil {
		provenance := *source.ExpectedProvenance
		provenance.Fragment.Env = cloneContextMap(provenance.Fragment.Env)
		provenance.Fragment.MCP = cloneCuratorMCP(provenance.Fragment.MCP)
		if provenance.Fragment.SystemPrompt != nil {
			prompt := *provenance.Fragment.SystemPrompt
			prompt.Channels = cloneCuratorChannels(prompt.Channels)
			provenance.Fragment.SystemPrompt = &prompt
		}
		out.ExpectedProvenance = &provenance
	}
	return &out
}

func cloneCuratorMCP(source *agentic.CuratorMCPContext) *agentic.CuratorMCPContext {
	if source == nil {
		return nil
	}
	out := *source
	out.EnvNames = cloneContextSlice(source.EnvNames)
	out.Channels = cloneCuratorChannels(source.Channels)
	return &out
}

func cloneCuratorChannels(source []agentic.CuratorChannelDescriptor) []agentic.CuratorChannelDescriptor {
	out := cloneContextSlice(source)
	for i := range out {
		out[i].With = cloneContextSlice(source[i].With)
	}
	return out
}

func cloneContextDescriptors(source []agentic.ContextDescriptor) []agentic.ContextDescriptor {
	out := cloneContextSlice(source)
	for i := range out {
		if source[i].MCP != nil {
			mcp := *source[i].MCP
			mcp.Servers = cloneContextSlice(mcp.Servers)
			for j := range mcp.Servers {
				mcp.Servers[j].Args = cloneContextSlice(mcp.Servers[j].Args)
			}
			out[i].MCP = &mcp
		}
		if source[i].SystemPrompt != nil {
			prompt := *source[i].SystemPrompt
			out[i].SystemPrompt = &prompt
		}
		if source[i].Permission != nil {
			permission := *source[i].Permission
			out[i].Permission = &permission
		}
	}
	return out
}

func cloneContextMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func cloneContextSlice[T any](source []T) []T {
	if source == nil {
		return nil
	}
	out := make([]T, len(source))
	copy(out, source)
	return out
}
