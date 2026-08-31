package agentic

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/relux-works/skill-agents-management/internal/ident"
	"github.com/relux-works/skill-agents-management/pkg/plugin"
)

// PluginKind is declaration data used by the compatibility adapter from the
// historical System registry into the general plugin graph.
const PluginKind plugin.Kind = "agentic-system"

// PlanNodeID identifies one process in a multi-node launch plan.
type PlanNodeID string

// PrimaryPlanNodeID is the stable id of the legacy agent process when a Plan
// is expanded into a multi-node plan.
const PrimaryPlanNodeID PlanNodeID = "agent"

// ProcessPlan is the process-shaped part of a launch node. It deliberately
// matches Plan's observable launch surface without pretending every process is
// an agentic System.
type ProcessPlan struct {
	Mode    LaunchMode
	Binary  string
	Argv    []string
	Env     []string
	Stdin   StdinPayload
	WorkDir string
	Home    string
}

// PlanNode is one typed plugin contribution to a launch. Plugin.Kind stays
// opaque, so engine, sidecar and future node kinds do not grow a core switch.
type PlanNode struct {
	ID        PlanNodeID
	Plugin    plugin.Ref
	DependsOn []PlanNodeID
	Process   ProcessPlan
}

var (
	ErrPlanInvalid           = errors.New("agentic: multi-node plan is invalid")
	ErrDuplicatePlanNode     = errors.New("agentic: plan node id is declared twice")
	ErrPlanDependencyMissing = errors.New("agentic: plan node dependency is missing")
	ErrPlanDependencyCycle   = errors.New("agentic: plan node dependency cycle")
)

// BuildMultiNodePlan preserves primary's legacy fields and adds a validated
// dependency graph. The returned Nodes are ordered dependency-first, which is
// a launch order, not a process executor or supervisor.
func BuildMultiNodePlan(primary Plan, primaryDependencies []PlanNodeID, nodes ...PlanNode) (Plan, error) {
	if len(primary.Nodes) != 0 {
		return Plan{}, fmt.Errorf("%w: primary plan is already multi-node", ErrPlanInvalid)
	}
	primaryNode := PlanNode{
		ID:        PrimaryPlanNodeID,
		Plugin:    plugin.Ref{ID: plugin.ID(primary.System), Kind: PluginKind},
		DependsOn: append([]PlanNodeID(nil), primaryDependencies...),
		Process: ProcessPlan{
			Mode:    primary.Mode,
			Binary:  primary.Binary,
			Argv:    append([]string(nil), primary.Argv...),
			Env:     append([]string(nil), primary.Env...),
			Stdin:   cloneStdin(primary.Stdin),
			WorkDir: primary.WorkDir,
			Home:    primary.Home,
		},
	}
	all := append(append([]PlanNode(nil), nodes...), primaryNode)
	byID := make(map[PlanNodeID]PlanNode, len(all))
	for _, raw := range all {
		node, err := validatePlanNode(raw)
		if err != nil {
			return Plan{}, err
		}
		if _, duplicate := byID[node.ID]; duplicate {
			return Plan{}, fmt.Errorf("%w: %q", ErrDuplicatePlanNode, node.ID)
		}
		byID[node.ID] = node
	}
	for _, id := range sortedPlanNodeIDs(byID) {
		for _, dependency := range byID[id].DependsOn {
			if _, found := byID[dependency]; !found {
				return Plan{}, fmt.Errorf("%w: node %q requires %q", ErrPlanDependencyMissing, id, dependency)
			}
		}
	}
	order, err := planNodeOrder(byID)
	if err != nil {
		return Plan{}, err
	}
	primary.Nodes = make([]PlanNode, 0, len(order))
	for _, id := range order {
		primary.Nodes = append(primary.Nodes, clonePlanNode(byID[id]))
	}
	return primary, nil
}

func validatePlanNode(raw PlanNode) (PlanNode, error) {
	id, reason, ok := ident.Normalize(string(raw.ID))
	if !ok || id != string(raw.ID) {
		return PlanNode{}, fmt.Errorf("%w: node id %q is not normalized: %s", ErrPlanInvalid, raw.ID, reason)
	}
	pluginID, pluginIDReason, pluginIDOK := ident.Normalize(string(raw.Plugin.ID))
	pluginKind, pluginKindReason, pluginKindOK := ident.Normalize(string(raw.Plugin.Kind))
	if !pluginIDOK || pluginID != string(raw.Plugin.ID) {
		return PlanNode{}, fmt.Errorf("%w: node %q has invalid plugin id %q: %s", ErrPlanInvalid, raw.ID, raw.Plugin.ID, pluginIDReason)
	}
	if !pluginKindOK || pluginKind != string(raw.Plugin.Kind) {
		return PlanNode{}, fmt.Errorf("%w: node %q has invalid plugin kind %q: %s", ErrPlanInvalid, raw.ID, raw.Plugin.Kind, pluginKindReason)
	}
	if !raw.Process.Mode.Valid() {
		return PlanNode{}, fmt.Errorf("%w: node %q has invalid launch mode %q", ErrPlanInvalid, raw.ID, raw.Process.Mode)
	}
	if strings.TrimSpace(raw.Process.Binary) == "" {
		return PlanNode{}, fmt.Errorf("%w: node %q has an empty binary", ErrPlanInvalid, raw.ID)
	}
	if !raw.Process.Stdin.Attached && len(raw.Process.Stdin.Bytes) != 0 {
		return PlanNode{}, fmt.Errorf("%w: node %q has stdin bytes but reports no attached stdin", ErrPlanInvalid, raw.ID)
	}
	node := clonePlanNode(raw)
	seen := make(map[PlanNodeID]bool, len(node.DependsOn))
	for i, dependency := range node.DependsOn {
		normalized, dependencyReason, dependencyOK := ident.Normalize(string(dependency))
		if !dependencyOK || normalized != string(dependency) {
			return PlanNode{}, fmt.Errorf("%w: node %q has invalid dependency id %q: %s", ErrPlanInvalid, node.ID, dependency, dependencyReason)
		}
		if seen[dependency] {
			return PlanNode{}, fmt.Errorf("%w: node %q repeats dependency %q", ErrPlanInvalid, node.ID, dependency)
		}
		seen[dependency] = true
		node.DependsOn[i] = PlanNodeID(normalized)
	}
	return node, nil
}

func planNodeOrder(nodes map[PlanNodeID]PlanNode) ([]PlanNodeID, error) {
	const (
		unseen uint8 = iota
		visiting
		visited
	)
	state := make(map[PlanNodeID]uint8, len(nodes))
	stack := make([]PlanNodeID, 0, len(nodes))
	order := make([]PlanNodeID, 0, len(nodes))
	var visit func(PlanNodeID) error
	visit = func(id PlanNodeID) error {
		switch state[id] {
		case visited:
			return nil
		case visiting:
			start := 0
			for i, item := range stack {
				if item == id {
					start = i
					break
				}
			}
			cycle := append(append([]PlanNodeID(nil), stack[start:]...), id)
			return fmt.Errorf("%w: %v", ErrPlanDependencyCycle, cycle)
		}
		state[id] = visiting
		stack = append(stack, id)
		dependencies := append([]PlanNodeID(nil), nodes[id].DependsOn...)
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i] < dependencies[j] })
		for _, dependency := range dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = visited
		order = append(order, id)
		return nil
	}
	for _, id := range sortedPlanNodeIDs(nodes) {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func sortedPlanNodeIDs(nodes map[PlanNodeID]PlanNode) []PlanNodeID {
	ids := make([]PlanNodeID, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func cloneStdin(in StdinPayload) StdinPayload {
	in.Bytes = append([]byte(nil), in.Bytes...)
	return in
}

func clonePlanNode(in PlanNode) PlanNode {
	in.DependsOn = append([]PlanNodeID(nil), in.DependsOn...)
	in.Process.Argv = append([]string(nil), in.Process.Argv...)
	in.Process.Env = append([]string(nil), in.Process.Env...)
	in.Process.Stdin = cloneStdin(in.Process.Stdin)
	return in
}

// Node returns a defensive copy of one launch node.
func (p Plan) Node(id PlanNodeID) (PlanNode, bool) {
	for _, node := range p.Nodes {
		if node.ID == id {
			return clonePlanNode(node), true
		}
	}
	return PlanNode{}, false
}

// NodeIDs returns the dependency-ordered ids of a multi-node plan.
func (p Plan) NodeIDs() []PlanNodeID {
	ids := make([]PlanNodeID, 0, len(p.Nodes))
	for _, node := range p.Nodes {
		ids = append(ids, node.ID)
	}
	return ids
}
