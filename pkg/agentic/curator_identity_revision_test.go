package agentic_test

import (
	"errors"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Revision compatibility of Curator fragment identity: a Revision-only drift
// between accepted revisions is the same identity, so a v2-recorded session
// resumes on an otherwise identical v3 fragment instead of refusing stale.
// Every case drives the production entry point BuildPlan, which reaches
// ValidateCuratorContextIdentity from the ExpectedProvenance gate in buildPlan.

func TestBuildPlanCuratorIdentityAcceptsRevisionOnlyV2ToV3(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			context.Revision = agentic.CuratorLaunchFragmentV2
			context.Permissions = validAcceptanceCuratorPermissions()
			plan, err := buildCuratorPlan(t, plugin, context)
			if err != nil {
				t.Fatalf("initial BuildPlan: %v", err)
			}
			stored, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok {
				t.Fatal("initial plan has no Curator provenance")
			}
			if stored.Fragment.Revision != agentic.CuratorLaunchFragmentV2 {
				t.Fatalf("stored revision = %q, want v2", stored.Fragment.Revision)
			}
			context.Revision = agentic.CuratorLaunchFragmentV3
			context.ExpectedProvenance = &stored
			resumed, err := buildCuratorPlan(t, plugin, context)
			if err != nil {
				t.Fatalf("BuildPlan across v2->v3: %v, want resume", err)
			}
			current, ok := resumed.CuratorContextProvenanceSnapshot()
			if !ok || current.Fragment.Revision != agentic.CuratorLaunchFragmentV3 {
				t.Fatalf("resumed provenance revision = %q, want v3", current.Fragment.Revision)
			}
		})
	}
}

func TestBuildPlanCuratorIdentityAcceptsSameRevisionResume(t *testing.T) {
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			for _, tc := range []struct {
				name     string
				revision string
			}{
				{name: "v1", revision: agentic.CuratorLaunchFragmentV1},
				{name: "v2", revision: agentic.CuratorLaunchFragmentV2},
			} {
				t.Run(tc.name, func(t *testing.T) {
					context := validCuratorContext(plugin)
					context.Revision = tc.revision
					if tc.revision == agentic.CuratorLaunchFragmentV2 {
						context.Permissions = validAcceptanceCuratorPermissions()
					}
					plan, err := buildCuratorPlan(t, plugin, context)
					if err != nil {
						t.Fatalf("initial BuildPlan: %v", err)
					}
					stored, ok := plan.CuratorContextProvenanceSnapshot()
					if !ok {
						t.Fatal("initial plan has no Curator provenance")
					}
					context.ExpectedProvenance = &stored
					if _, err := buildCuratorPlan(t, plugin, context); err != nil {
						t.Fatalf("BuildPlan same-revision resume: %v, want accept", err)
					}
				})
			}
		})
	}
}

func TestBuildPlanMuseCuratorIdentityAcceptsV3ToV3(t *testing.T) {
	context := validMuseCuratorContext()
	plan, err := buildMuseCuratorPlan(t, context, museV3HomeParent)
	if err != nil {
		t.Fatalf("initial BuildPlan: %v", err)
	}
	stored, ok := plan.CuratorContextProvenanceSnapshot()
	if !ok {
		t.Fatal("initial plan has no Curator provenance")
	}
	context.ExpectedProvenance = &stored
	if _, err := buildMuseCuratorPlan(t, context, museV3HomeParent); err != nil {
		t.Fatalf("BuildPlan v3->v3: %v, want resume", err)
	}
}

func TestBuildPlanMuseCuratorIdentityRefusesStoredPreV3Revision(t *testing.T) {
	// A current pre-v3 muse fragment never reaches identity: the validator
	// refuses it first. Identity fails closed on the stored side instead, so
	// a recorded pre-v3 muse fragment never resumes — not even v2-to-v2.
	for _, tc := range []struct {
		name     string
		revision string
	}{
		{name: "v2", revision: agentic.CuratorLaunchFragmentV2},
		{name: "v1", revision: agentic.CuratorLaunchFragmentV1},
		{name: "unaccepted", revision: "launch-env-fragment-v9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			context := validMuseCuratorContext()
			plan, err := buildMuseCuratorPlan(t, context, museV3HomeParent)
			if err != nil {
				t.Fatalf("initial BuildPlan: %v", err)
			}
			stored, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok {
				t.Fatal("initial plan has no Curator provenance")
			}
			stored.Fragment.Revision = tc.revision
			context.ExpectedProvenance = &stored
			_, err = buildMuseCuratorPlan(t, context, museV3HomeParent)
			if !errors.Is(err, agentic.ErrCuratorContextStale) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorContextStale for a stored %s muse fragment", err, tc.name)
			}
		})
	}
}

func TestBuildPlanCuratorIdentityRefusesSubstantiveDeltaAcrossRevision(t *testing.T) {
	// The env case mutates the current fragment Env (the request Home follows
	// it, so validation passes and only identity bites); the home case tampers
	// the stored provenance summary instead, proving that arm still bites
	// across a revision change.
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			cases := []struct {
				name   string
				mutate func(*agentic.CuratorContext, *agentic.CuratorContextProvenance)
			}{
				{name: "env", mutate: func(context *agentic.CuratorContext, _ *agentic.CuratorContextProvenance) {
					context.Env[plugin.homeVariable] = "/managed/other-home"
				}},
				{name: "permissions", mutate: func(context *agentic.CuratorContext, _ *agentic.CuratorContextProvenance) {
					context.Permissions = &agentic.CuratorPermissions{Mode: agentic.CuratorPermissionNative, Locked: true, Source: agentic.CuratorPermissionSourceGlobal}
				}},
				{name: "home", mutate: func(_ *agentic.CuratorContext, stored *agentic.CuratorContextProvenance) {
					stored.ManagedHome = "/managed/other-home"
				}},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					context := validCuratorContext(plugin)
					context.Revision = agentic.CuratorLaunchFragmentV2
					context.Permissions = validAcceptanceCuratorPermissions()
					plan, err := buildCuratorPlan(t, plugin, context)
					if err != nil {
						t.Fatalf("initial BuildPlan: %v", err)
					}
					stored, ok := plan.CuratorContextProvenanceSnapshot()
					if !ok {
						t.Fatal("initial plan has no Curator provenance")
					}
					context.Revision = agentic.CuratorLaunchFragmentV3
					tc.mutate(&context, &stored)
					context.ExpectedProvenance = &stored
					_, err = buildCuratorPlan(t, plugin, context)
					if !errors.Is(err, agentic.ErrCuratorContextStale) {
						t.Fatalf("BuildPlan err = %v, want ErrCuratorContextStale for a %s delta across v2->v3", err, tc.name)
					}
				})
			}
		})
	}
}

func TestBuildPlanCuratorIdentityRefusesUnacceptedStoredRevision(t *testing.T) {
	// The validator owns the CURRENT side: a request carrying an unknown
	// revision never reaches identity
	// (TestBuildPlanCuratorContextRefusesUnknownFragmentRevision). Identity
	// owns the STORED side: provenance is data, not a validated request, so
	// an unaccepted revision there reads as stale.
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			context.Revision = agentic.CuratorLaunchFragmentV2
			context.Permissions = validAcceptanceCuratorPermissions()
			plan, err := buildCuratorPlan(t, plugin, context)
			if err != nil {
				t.Fatalf("initial BuildPlan: %v", err)
			}
			stored, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok {
				t.Fatal("initial plan has no Curator provenance")
			}
			stored.Fragment.Revision = "launch-env-fragment-v9"
			context.ExpectedProvenance = &stored
			_, err = buildCuratorPlan(t, plugin, context)
			if !errors.Is(err, agentic.ErrCuratorContextStale) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorContextStale for an unaccepted stored revision", err)
			}
		})
	}
}

func TestBuildPlanCuratorIdentityRefusesV1RecordedSessionOnV2(t *testing.T) {
	// The identity holds Permissions nil for v1 and set for v2/v3, so a
	// v1-recorded session offered v2 is never "otherwise equal": the
	// permissions member itself is the substantive delta. The rule follows
	// from the data, not from revision names.
	for _, plugin := range curatorPlugins {
		t.Run(plugin.name, func(t *testing.T) {
			context := validCuratorContext(plugin)
			plan, err := buildCuratorPlan(t, plugin, context)
			if err != nil {
				t.Fatalf("initial BuildPlan: %v", err)
			}
			stored, ok := plan.CuratorContextProvenanceSnapshot()
			if !ok {
				t.Fatal("initial plan has no Curator provenance")
			}
			if stored.Fragment.Permissions != nil {
				t.Fatalf("stored v1 permissions = %#v, want nil", stored.Fragment.Permissions)
			}
			context.Revision = agentic.CuratorLaunchFragmentV2
			context.Permissions = validAcceptanceCuratorPermissions()
			context.ExpectedProvenance = &stored
			_, err = buildCuratorPlan(t, plugin, context)
			if !errors.Is(err, agentic.ErrCuratorContextStale) {
				t.Fatalf("BuildPlan err = %v, want ErrCuratorContextStale for a v1 session offered v2", err)
			}
		})
	}
}
