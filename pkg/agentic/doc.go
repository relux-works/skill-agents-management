// Package agentic defines system contracts and launch planning.
// BuildPlanWithEnvironment returns a PlanWithEnvironment whose OwnedEnv is a
// sorted, independently owned snapshot of ChildEnv(nil, effectiveRequest) after
// request preparation and model alias projection. Consumers must not diff this
// snapshot against a parent environment. BuildPlan retains its existing Plan
// and JSON contract and does not request the additional snapshot.
package agentic
