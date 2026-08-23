package vendorplugin

import "github.com/relux-works/skill-agents-management/pkg/agentic"

// Helpers every vendor plugin needs, declared once here rather than four
// times across the plugins.
//
// Four copies of a deep copy and four copies of a launch translation are the
// shadow-declaration disease with the duplication spread thin enough to look
// harmless: they agree until one of them gains a line. What is NOT shared is
// anything a vendor genuinely owns — its models, its identity, its
// availability — because sharing those is how a plugin boundary stops being
// one.

// CloneModels returns a deep copy of a model list.
//
// Vendor.Models() must be answerable repeatedly with the same content, and a
// caller ranging over the answer must not be able to reach the plugin's own
// table through it. A shallow copy is not enough: Model carries three slices —
// the rank basis, the effort vocabulary and the declared systems — and every
// one of them would otherwise share a backing array with the declaration, so
// `models[0].Systems[0] = "muse"` in any caller would rebind a model for the
// whole process.
//
// Pricing is a POINTER, which is the same hazard one level deeper: a shallow
// copy hands every caller the plugin's own contract, and one caller writing
// through it changes the published price everywhere. Pricing.Clone chases it,
// including the *float64 behind each plan's promotional price.
func CloneModels(models []Model) []Model {
	out := make([]Model, 0, len(models))
	for _, model := range models {
		copied := model
		copied.Rank.Basis = append([]RankEvidence(nil), model.Rank.Basis...)
		copied.Effort.Vocabulary = append([]string(nil), model.Effort.Vocabulary...)
		copied.Systems = append([]agentic.SystemID(nil), model.Systems...)
		copied.Pricing = model.Pricing.Clone()
		out = append(out, copied)
	}
	return out
}

// PassthroughLaunch is the Vendor.Spawn implementation for a vendor that adds
// NOTHING to a launch.
//
// All four ported vendors use it, and that is a finding rather than a
// shortcut. The extraction source keeps every launch-shaping fact on the
// harness side: the configuration home is the agentic system's declared
// HomeEnvVar, the argv grammar and the environment filter are the system's,
// and no adapter in that repository injects an API key, a base URL or an
// account header per broker. So the honest port of "what does anthropic add to
// a claude-code launch" is: nothing observable, and a plugin that invented an
// addition here would be adding behaviour the parity goldens never captured.
//
// The seam stays open. A vendor that later needs its own environment
// implements Spawn itself instead of calling this, and BuildLaunch's fidelity
// check already draws the line for it: a vendor may ADD to a launch and may
// not REDIRECT one.
func PassthroughLaunch(sc SpawnContext) agentic.LaunchRequest {
	return agentic.LaunchRequest{
		System:      sc.Runtime.SystemID,
		Model:       sc.Model.Launchable(),
		Effort:      sc.Effort,
		PromptPath:  sc.Request.PromptPath,
		Prompt:      sc.Request.Prompt,
		WorkDir:     sc.Request.WorkDir,
		Home:        sc.Request.Home,
		Env:         sc.Request.Env,
		Goal:        sc.Request.Goal,
		Budget:      sc.Request.Budget,
		ServiceTier: sc.Request.ServiceTier,
		Composition: sc.Request.Composition,
	}
}
