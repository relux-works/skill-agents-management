# TASK-260821-atcotl: vendor-plugin-contract-and-registry

## Description
The Layer-2 contract: a Vendor plugin interface declaring its models (each with capability rank, usage description, effort vocabulary and recommended effort, and the agentic systems that can drive it), availability as a STRUCTURED verdict (healthy / limited-until / unreachable / unknown, with evidence) rather than a boolean - the local-model resource plane must fit behind it later without an interface break - and spawn with the full parameter surface. The registry enforces the dependency direction: a vendor declaring support for an unregistered agentic system is refused at registration with an error naming both IDs. Runtime = declared (system x vendor) pair with a stable ID; the six historical IDs are seeded as declarations.

## Scope
(define task scope)

## Acceptance Criteria
1. Vendor interface covers models+ranking+descriptions+efforts, structured availability, and the full spawn parameter surface. 2. Registering a vendor naming an unknown agentic system is refused with both IDs in the error. 3. Runtimes resolve as declared pairs; the six historical IDs exist as seed declarations with their bindings from runtimeid. 4. Availability verdict type admits limited-until and unknown without interface change.
