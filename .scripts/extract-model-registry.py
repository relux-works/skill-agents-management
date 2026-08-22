#!/usr/bin/env python3
"""Extract the EXTRACTION SOURCE's model registry into a pinnable fixture.

This reads skill-project-management's own sources and emits the facts this
repository's vendor plugins are pinned against:

  * every ``knownModels`` row: id, runtime, broker, agentic systems, the
    ``PolicyRank`` capability evidence, the reasoning-effort vocabulary and the
    recommended effort;
  * the frozen spawn-policy-v2 admission tiers, which are the membership
    authority for a v2 ordered ceiling (``PolicyRank`` deliberately is not);
  * the sha256 of every source file it read, so a fixture can never quietly
    describe a table that has since moved.

It is READ-ONLY with respect to the source checkout: it opens files and writes
nothing there.  Text parsing rather than a Go program is deliberate — the
registry lives in an ``internal/`` package, so any Go reader of it would have to
be created inside the source tree.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys


def read(path: str) -> str:
    with open(path, "r", encoding="utf-8") as handle:
        return handle.read()


def sha256_of(path: str) -> str:
    with open(path, "rb") as handle:
        return "sha256:" + hashlib.sha256(handle.read()).hexdigest()


def split_fields(body: str) -> list[str]:
    """Split one struct literal body on the commas that separate FIELDS.

    Commas inside a quoted string ("...complex code, math, and STEM") or inside
    a nested literal ([]string{"low", "medium"}) separate nothing.
    """
    fields: list[str] = []
    depth = 0
    in_string = False
    escaped = False
    current: list[str] = []
    for char in body:
        if in_string:
            current.append(char)
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                in_string = False
            continue
        if char == '"':
            in_string = True
            current.append(char)
            continue
        if char in "{[(":
            depth += 1
        elif char in "}])":
            depth -= 1
        if char == "," and depth == 0:
            fields.append("".join(current).strip())
            current = []
            continue
        current.append(char)
    tail = "".join(current).strip()
    if tail:
        fields.append(tail)
    return fields


def parse_named_string_slice(expr: str) -> list[str] | None:
    """Parse a []string{"a", "b"} literal; return None for anything else."""
    match = re.fullmatch(r"\[\]string\{(.*)\}", expr.strip(), re.S)
    if not match:
        return None
    inner = match.group(1).strip()
    if not inner:
        return []
    return [json.loads(item.strip()) for item in split_fields(inner)]


def parse_const_strings(source: str, pattern: str) -> dict[str, str]:
    """Collect `Name Type = "value"` constants matching a name pattern."""
    found = {}
    for name, value in re.findall(
        r"^\s*(" + pattern + r")\s+\w+\s*=\s*\"([^\"]*)\"", source, re.M
    ):
        found[name] = value
    return found


def parse_reasoning_effort(source: str) -> dict[str, list[str]]:
    """Read the per-provider and legacy vocabularies out of the package."""
    vocabularies: dict[str, list[str]] = {}

    table = re.search(
        r"var providerVocabulary = map\[Provider\]\[\]string\{(.*?)\n\}", source, re.S
    )
    if not table:
        raise SystemExit("reasoningeffort: providerVocabulary table not found")
    providers = parse_const_strings(source, r"Codex|Qwen|Claude|QwenCodex")
    for line in table.group(1).splitlines():
        line = line.strip()
        match = re.fullmatch(r"(\w+):\s*\{(.*)\},", line)
        if not match:
            continue
        provider = providers.get(match.group(1), match.group(1))
        vocabularies["provider:" + provider] = [
            json.loads(word.strip()) for word in match.group(2).split(",")
        ]

    for name in ("CodexLegacyVocabulary", "ClaudeLegacyNoXhighVocabulary"):
        match = re.search(r"var " + name + r" = (\[\]string\{[^}]*\})", source)
        if not match:
            raise SystemExit(f"reasoningeffort: {name} not found")
        vocabularies["var:" + name] = parse_named_string_slice(match.group(1))

    return vocabularies


def parse_runtime_bindings(builtins: str) -> dict[str, dict[str, str]]:
    """Read runtimeid's frozen (runtime -> broker, agentic system) bindings.

    muse's Broker is the package's own ``BrokerUnknown`` constant rather than a
    string literal, and it is read as such: an unresolved broker is a recorded
    finding in the source, and turning it into an empty literal here would erase
    the distinction the source spent a whole evidence block making.
    """
    unknown = re.search(r"BrokerUnknown\s+BrokerID\s*=\s*\"([^\"]*)\"", builtins)
    unknown_value = unknown.group(1) if unknown else ""
    bindings: dict[str, dict[str, str]] = {}
    for body in re.split(r"\n\t\{", builtins):
        runtime = re.search(r"Runtime:\s*\"([^\"]+)\"", body)
        system = re.search(r"AgenticSystem:\s*\"([^\"]+)\"", body)
        if not runtime or not system:
            continue
        broker = re.search(r"Broker:\s*(?:\"([^\"]*)\"|(BrokerUnknown))", body)
        if broker is None:
            broker_value = ""
        elif broker.group(2):
            broker_value = unknown_value
        else:
            broker_value = broker.group(1)
        bindings[runtime.group(1)] = {
            "broker": broker_value,
            "agentic_system": system.group(1),
        }
    return bindings


def parse_v2_tiers(source: str) -> dict[str, list[list[str]]]:
    """Read the frozen spawn-policy-v2 admission tiers, ascending."""
    table = re.search(
        r"return map\[AgentType\]\[\]\[\]string\{(.*?)\n\t\}\n\}", source, re.S
    )
    if not table:
        raise SystemExit("v2 admission snapshot: rollout tier table not found")
    agents = parse_const_strings(source, r"Agent\w+")
    tiers: dict[str, list[list[str]]] = {}
    current = None
    for line in table.group(1).splitlines():
        line = line.strip()
        header = re.fullmatch(r"(Agent\w+):\s*\{", line)
        if header:
            current = header.group(1)
            tiers[current] = []
            continue
        row = re.fullmatch(r"\{(.*)\},", line)
        if row and current:
            tiers[current].append(
                [json.loads(item.strip()) for item in row.group(1).split(",")]
            )
    return tiers, agents


def parse_known_models(models_source: str, agents, brokers, systems, vocabularies):
    rows = []
    for line in models_source.splitlines():
        stripped = line.strip()
        if not stripped.startswith('{ID: "') or not stripped.endswith("},"):
            continue
        fields = {}
        for field in split_fields(stripped[1:-2]):
            key, _, value = field.partition(":")
            fields[key.strip()] = value.strip()

        runtime = agents[fields["Runtime"]]
        broker = brokers[fields["Broker"]]
        agentic_systems = systems[fields["AgenticSystems"]]

        supported = fields.get("SupportedEfforts", "")
        if not supported:
            efforts = []
        else:
            literal = parse_named_string_slice(supported)
            efforts = literal if literal is not None else vocabularies[supported]

        rows.append(
            {
                "id": json.loads(fields["ID"]),
                "runtime": runtime,
                "broker": broker,
                "agentic_systems": agentic_systems,
                "policy_rank": int(fields["PolicyRank"]),
                "lifecycle": fields.get("Lifecycle", ""),
                "superseded_by": json.loads(fields["SupersededBy"])
                if "SupersededBy" in fields
                else "",
                "description": json.loads(fields["Description"]),
                "reasoning": fields.get("Reasoning", "ReasoningNone"),
                "supported_efforts": efforts,
                "recommended_effort": json.loads(fields["RecommendedEffort"])
                if "RecommendedEffort" in fields
                else "",
                "recommended": fields.get("Recommended", "false") == "true",
            }
        )
    return rows


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, help="skill-project-management checkout")
    parser.add_argument("--out", required=True, help="fixture path to write")
    args = parser.parse_args()

    paths = {
        "models": "tools/board-cli/internal/spawn/models.go",
        "spawn": "tools/board-cli/internal/spawn/spawn.go",
        "v2_snapshot": "tools/board-cli/internal/spawn/v2_admission_snapshot.go",
        "reasoning_effort": "pkg/remoteconfig/reasoningeffort/reasoningeffort.go",
        "runtimeid_builtins": "pkg/remoteconfig/runtimeid/builtins.go",
        "runtimeid": "pkg/remoteconfig/runtimeid/runtimeid.go",
    }
    absolute = {name: os.path.join(args.source, rel) for name, rel in paths.items()}
    for name, path in absolute.items():
        if not os.path.isfile(path):
            raise SystemExit(f"source file for {name} not found: {path}")

    models_source = read(absolute["models"])
    spawn_source = read(absolute["spawn"])
    v2_source = read(absolute["v2_snapshot"])
    effort_source = read(absolute["reasoning_effort"])
    # BrokerUnknown is declared in runtimeid.go and used in builtins.go, so the
    # binding reader is handed both rather than half the package.
    builtins_source = read(absolute["runtimeid_builtins"]) + read(absolute["runtimeid"])

    agents = parse_const_strings(spawn_source, r"Agent\w+")
    bindings = parse_runtime_bindings(builtins_source)
    vocabularies = parse_reasoning_effort(effort_source)

    # models.go binds its per-runtime broker/system vars through runtimeBinding,
    # so resolve those names back to the frozen runtimeid rows rather than
    # restating a second copy of the bindings here.
    brokers: dict[str, str] = {}
    systems: dict[str, list[str]] = {}
    for broker_var, systems_var, agent in re.findall(
        r"(\w+Broker), (\w+AgenticSystems)\s+= runtimeBinding\((Agent\w+)\)",
        models_source,
    ):
        runtime = agents[agent]
        brokers[broker_var] = bindings[runtime]["broker"]
        systems[systems_var] = [bindings[runtime]["agentic_system"]]
    # qwen-codex is deliberately NOT a builtin runtimeid row: it is the source's
    # own operator-declared cross-runtime, so its two vars are literals there.
    literal_broker = re.search(r"(\w+Broker)\s+= runtimeid\.BrokerID\(\"([^\"]+)\"\)", models_source)
    if literal_broker:
        brokers[literal_broker.group(1)] = literal_broker.group(2)
    literal_systems = re.search(
        r"(\w+AgenticSystems) = \[\]runtimeid\.AgenticSystemID\{(.*?)\}", models_source
    )
    if literal_systems:
        systems[literal_systems.group(1)] = [
            json.loads(item.strip()) for item in literal_systems.group(2).split(",")
        ]

    # models.go's own aliases for the reasoningeffort vocabularies.
    for var, provider in re.findall(
        r"var (current\w+ReasoningEfforts) = reasoningeffort\.Vocabulary\(string\(reasoningeffort\.(\w+)\)\)",
        models_source,
    ):
        providers = parse_const_strings(effort_source, r"Codex|Qwen|Claude|QwenCodex")
        vocabularies[var] = vocabularies["provider:" + providers[provider]]
    for var, name in re.findall(
        r"var (\w+ReasoningEfforts\w*) = reasoningeffort\.(\w+)", models_source
    ):
        if "var:" + name in vocabularies:
            vocabularies[var] = vocabularies["var:" + name]

    rows = parse_known_models(models_source, agents, brokers, systems, vocabularies)
    tiers, tier_agents = parse_v2_tiers(v2_source)
    rollout = re.search(
        r'v2AdmissionSnapshotRolloutVersion = "([^"]+)"', v2_source
    )

    commit = subprocess.run(
        ["git", "-C", args.source, "rev-parse", "HEAD"],
        capture_output=True, text=True, check=False,
    ).stdout.strip() or "unknown"

    fixture = {
        "provenance": {
            "source_repo": os.path.abspath(args.source),
            "source_commit": commit,
            "generated_by": ".scripts/capture-model-registry.sh",
            "source_files": {
                name: {"path": rel, "sha256": sha256_of(absolute[name])}
                for name, rel in paths.items()
            },
        },
        "model_count": len(rows),
        "models": rows,
        "v2_admission_snapshot": {
            "rollout_version": rollout.group(1) if rollout else "",
            "tiers": {
                agents[agent]: tier_rows for agent, tier_rows in tiers.items()
            },
        },
    }
    with open(args.out, "w", encoding="utf-8") as handle:
        json.dump(fixture, handle, indent=2, sort_keys=True)
        handle.write("\n")
    print(f"wrote {args.out}: {len(rows)} model rows", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
