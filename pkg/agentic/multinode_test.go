package agentic

import (
	"errors"
	"fmt"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

func testPrimaryPlan() Plan {
	return Plan{
		System:  "pi",
		Mode:    LaunchModeExec,
		Binary:  "/opt/bin/agents-infra",
		Argv:    []string{"pi", "--profile", "local"},
		Env:     []string{"PATH=/usr/bin"},
		Stdin:   StdinPayload{Attached: true, Bytes: []byte("turn")},
		WorkDir: "/work/story",
		Home:    "~/.agents",
	}
}

func testProcess(binary string, argv ...string) ProcessPlan {
	return ProcessPlan{Mode: LaunchModeExec, Binary: binary, Argv: argv}
}

func TestBuildMultiNodePlanCarriesEngineAndSidecarDependencies(t *testing.T) {
	engine := PlanNode{
		ID:      "engine",
		Plugin:  plugin.Ref{ID: "llama-cpp", Kind: "inference-engine"},
		Process: testProcess("/opt/bin/llama-server", "--model", "weights.gguf"),
	}
	sidecar := PlanNode{
		ID:        "metrics",
		Plugin:    plugin.Ref{ID: "metrics", Kind: "sidecar"},
		DependsOn: []PlanNodeID{"engine"},
		Process:   testProcess("/opt/bin/model-metrics", "--target", "engine"),
	}

	plan, err := BuildMultiNodePlan(testPrimaryPlan(), []PlanNodeID{"engine", "metrics"}, engine, sidecar)
	if err != nil {
		t.Fatalf("BuildMultiNodePlan: %v", err)
	}
	if got := fmt.Sprint(plan.NodeIDs()); got != "[engine metrics agent]" {
		t.Fatalf("NodeIDs() = %s, want dependency order [engine metrics agent]", got)
	}
	if plan.Binary != "/opt/bin/agents-infra" || fmt.Sprint(plan.Argv) != "[pi --profile local]" {
		t.Fatalf("legacy primary surface changed: Binary=%q Argv=%v", plan.Binary, plan.Argv)
	}
	primary, ok := plan.Node(PrimaryPlanNodeID)
	if !ok {
		t.Fatal("multi-node plan has no typed primary node")
	}
	if got := fmt.Sprint(primary.DependsOn); got != "[engine metrics]" {
		t.Fatalf("primary.DependsOn = %s, want [engine metrics]", got)
	}
}

func TestBuildMultiNodePlanRefusesAMissingDependency(t *testing.T) {
	node := PlanNode{
		ID:        "engine",
		Plugin:    plugin.Ref{ID: "llama-cpp", Kind: "inference-engine"},
		DependsOn: []PlanNodeID{"weights"},
		Process:   testProcess("/opt/bin/llama-server"),
	}
	_, err := BuildMultiNodePlan(testPrimaryPlan(), []PlanNodeID{"engine"}, node)
	if !errors.Is(err, ErrPlanDependencyMissing) {
		t.Fatalf("BuildMultiNodePlan(missing dependency) error = %v, want ErrPlanDependencyMissing", err)
	}
}

func TestBuildMultiNodePlanRefusesExplicitNodeWithEmptyBinary(t *testing.T) {
	node := PlanNode{
		ID:      "engine",
		Plugin:  plugin.Ref{ID: "llama-cpp", Kind: "inference-engine"},
		Process: testProcess(""),
	}

	_, err := BuildMultiNodePlan(testPrimaryPlan(), []PlanNodeID{"engine"}, node)
	if !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("BuildMultiNodePlan(explicit node with empty binary) error = %v, want ErrPlanInvalid", err)
	}
}

func TestBuildMultiNodePlanRefusesMissingDependenciesFromPrimaryAndLaterEdges(t *testing.T) {
	t.Run("implicit primary node", func(t *testing.T) {
		_, err := BuildMultiNodePlan(testPrimaryPlan(), []PlanNodeID{"missing-engine"})
		if !errors.Is(err, ErrPlanDependencyMissing) {
			t.Fatalf("BuildMultiNodePlan(primary missing dependency) error = %v, want ErrPlanDependencyMissing", err)
		}
	})

	t.Run("later dependency", func(t *testing.T) {
		engine := PlanNode{
			ID:      "engine",
			Plugin:  plugin.Ref{ID: "llama-cpp", Kind: "inference-engine"},
			Process: testProcess("/opt/bin/llama-server"),
		}
		sidecar := PlanNode{
			ID:        "metrics",
			Plugin:    plugin.Ref{ID: "metrics", Kind: "sidecar"},
			DependsOn: []PlanNodeID{"engine", "missing-collector"},
			Process:   testProcess("/opt/bin/model-metrics"),
		}
		_, err := BuildMultiNodePlan(testPrimaryPlan(), []PlanNodeID{"metrics"}, engine, sidecar)
		if !errors.Is(err, ErrPlanDependencyMissing) {
			t.Fatalf("BuildMultiNodePlan(later missing dependency) error = %v, want ErrPlanDependencyMissing", err)
		}
	})
}

func TestBuildMultiNodePlanRefusesDuplicateExplicitAndPrimaryNodeIDs(t *testing.T) {
	makeNode := func(id PlanNodeID) PlanNode {
		return PlanNode{
			ID:      id,
			Plugin:  plugin.Ref{ID: "metrics", Kind: "sidecar"},
			Process: testProcess("/opt/bin/model-metrics"),
		}
	}
	tests := map[string][]PlanNode{
		"two explicit nodes":                           {makeNode("metrics"), makeNode("metrics")},
		"explicit node collides with implicit primary": {makeNode(PrimaryPlanNodeID)},
	}
	for name, nodes := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := BuildMultiNodePlan(testPrimaryPlan(), nil, nodes...)
			if !errors.Is(err, ErrDuplicatePlanNode) {
				t.Fatalf("BuildMultiNodePlan(%s) error = %v, want ErrDuplicatePlanNode", name, err)
			}
		})
	}
}

func TestBuildMultiNodePlanRefusesACycle(t *testing.T) {
	engine := PlanNode{
		ID:        "engine",
		Plugin:    plugin.Ref{ID: "llama-cpp", Kind: "inference-engine"},
		DependsOn: []PlanNodeID{"metrics"},
		Process:   testProcess("/opt/bin/llama-server"),
	}
	sidecar := PlanNode{
		ID:        "metrics",
		Plugin:    plugin.Ref{ID: "metrics", Kind: "sidecar"},
		DependsOn: []PlanNodeID{"engine"},
		Process:   testProcess("/opt/bin/model-metrics"),
	}
	_, err := BuildMultiNodePlan(testPrimaryPlan(), []PlanNodeID{"engine"}, engine, sidecar)
	if !errors.Is(err, ErrPlanDependencyCycle) {
		t.Fatalf("BuildMultiNodePlan(cycle) error = %v, want ErrPlanDependencyCycle", err)
	}
}

func TestBuildMultiNodePlanRefusesASelfCycle(t *testing.T) {
	engine := PlanNode{
		ID:        "engine",
		Plugin:    plugin.Ref{ID: "llama-cpp", Kind: "inference-engine"},
		DependsOn: []PlanNodeID{"engine"},
		Process:   testProcess("/opt/bin/llama-server"),
	}
	_, err := BuildMultiNodePlan(testPrimaryPlan(), []PlanNodeID{"engine"}, engine)
	if !errors.Is(err, ErrPlanDependencyCycle) {
		t.Fatalf("BuildMultiNodePlan(engine -> engine) error = %v, want ErrPlanDependencyCycle", err)
	}
}

func TestBuildMultiNodePlanRefusesAMultiHopCycle(t *testing.T) {
	nodes := []PlanNode{
		{ID: "engine", Plugin: plugin.Ref{ID: "engine", Kind: "inference-engine"}, DependsOn: []PlanNodeID{"metrics"}, Process: testProcess("engine")},
		{ID: "metrics", Plugin: plugin.Ref{ID: "metrics", Kind: "sidecar"}, DependsOn: []PlanNodeID{"collector"}, Process: testProcess("metrics")},
		{ID: "collector", Plugin: plugin.Ref{ID: "collector", Kind: "sidecar"}, DependsOn: []PlanNodeID{"engine"}, Process: testProcess("collector")},
	}
	_, err := BuildMultiNodePlan(testPrimaryPlan(), []PlanNodeID{"engine"}, nodes...)
	if !errors.Is(err, ErrPlanDependencyCycle) {
		t.Fatalf("BuildMultiNodePlan(three-node cycle) error = %v, want ErrPlanDependencyCycle", err)
	}
}
