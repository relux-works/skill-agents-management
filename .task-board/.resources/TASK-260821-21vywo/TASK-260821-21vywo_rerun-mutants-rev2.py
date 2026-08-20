import os, shutil, subprocess, sys, json

ROOT = os.getcwd()
FILES = ["pkg/agentic/singlesource_guard_test.go", "pkg/agentic/registry.go",
         "pkg/agentic/plan.go", "pkg/agentic/system.go"]
BAK = ".temp/TASK-260821-21vywo/rerun-bak"
os.makedirs(BAK, exist_ok=True)
for f in FILES:
    shutil.copy(f, os.path.join(BAK, f.replace("/", "_")))

def restore():
    for f in FILES:
        shutil.copy(os.path.join(BAK, f.replace("/", "_")), f)
    for extra in ["tools/agents-management/cmd/shadow_binding.go",
                  "tools/agents-management/cmd/id_switch.go"]:
        if os.path.exists(extra):
            os.remove(extra)

def patch(path, old, new):
    s = open(path).read()
    assert old in s, f"anchor missing in {path}: {old[:60]!r}"
    open(path, "w").write(s.replace(old, new, 1))

def run(pattern="Test", pkgs="./..."):
    p = subprocess.run(["go", "test", "-mod=mod", pkgs, "-count=1", "-run", pattern, "-v"],
                       capture_output=True, text=True,
                       env={**os.environ, "TASK_BOARD_DIR": ""} )
    fails = sorted({l.split()[2] for l in p.stdout.splitlines()
                    if l.strip().startswith("--- FAIL:")})
    return p.returncode, fails

def plant(path, src):
    open(path, "w").write(src)

results = []
def record(tag, desc, code, fails):
    results.append({"id": tag, "desc": desc, "exit": code, "fails": fails})
    print(f"{tag}: exit={code} fails={len(fails)}")
    for f in fails[:8]:
        print("     ", f)

# --- M0 control ---
code, fails = run()
record("M0", "control, nothing mutated", code, fails)

# --- M1 planted shadow table ---
plant("tools/agents-management/cmd/shadow_binding.go", '''package cmd

import "github.com/relux-works/skill-agents-management/pkg/agentic"

type launchBuilder func() []string

var shadowBindings = map[agentic.SystemID]launchBuilder{}
''')
code, fails = run("TestSingleSourceGuard")
record("M1", "shadow table planted in tools/agents-management/cmd", code, fails)
restore()

# --- M2 planted id switch ---
plant("tools/agents-management/cmd/id_switch.go", '''package cmd

func launchArgs(id string) []string {
	switch id {
	case "claude-code":
		return []string{"-p"}
	case "codex":
		return []string{"exec"}
	}
	return nil
}
''')
code, fails = run("TestSingleSourceGuard")
record("M2", "id switch planted in tools/agents-management/cmd", code, fails)
restore()

G = "pkg/agentic/singlesource_guard_test.go"

# --- M3 const indirection off ---
patch(G, "\tconsts := map[string]string{}\n\tfor pass := 0; pass <= len(queue); pass++ {",
         "\tconsts := map[string]string{}\n\tqueue = nil\n\tfor pass := 0; pass <= len(queue); pass++ {")
code, fails = run("TestSingleSourceGuard")
record("M3", "const indirection no longer resolves", code, fails)
restore()

# --- M4 var indirection off ---
patch(G, "func resolveVarStrings(files []*ast.File, consts map[string]string) map[string][]string {\n\tvars := map[string][]string{}",
         "func resolveVarStrings(files []*ast.File, consts map[string]string) map[string][]string {\n\tvars := map[string][]string{}\n\tif true {\n\t\treturn vars\n\t}")
code, fails = run("TestSingleSourceGuard")
record("M4", "package/function var indirection no longer resolves", code, fails)
restore()

# --- M5 map[SystemID]T only inside composite literals ---
patch(G, "\t\t\t\tcase *ast.MapType:\n\t\t\t\t\tif !tableAllowed && isSystemIDRef(node.Key, systemIDTypes) {",
         "\t\t\t\tcase *ast.MapType:\n\t\t\t\t\tif false && !tableAllowed && isSystemIDRef(node.Key, systemIDTypes) {")
code, fails = run("TestSingleSourceGuard")
record("M5", "map[SystemID]T detected only inside composite literals", code, fails)
restore()

# --- M6 ID() structural rule removed (both spellings) ---
patch(G, "if node.Tag != nil && (isIDCall(node.Tag) || idTagSwitches[node]) && node.Body != nil",
         "if false && node.Tag != nil && (isIDCall(node.Tag) || idTagSwitches[node]) && node.Body != nil")
code, fails = run("TestSingleSourceGuard")
record("M6", "the ID() structural rule removed, both the direct and the local-copy spelling", code, fails)
restore()

# --- M7 allowlist widened to every file ---
patch(G, "\t\ttableAllowed := allowFiles[name]", "\t\ttableAllowed := true\n\t\t_ = allowFiles[name]")
code, fails = run("TestSingleSourceGuard")
record("M7", "allowlist widened to every file", code, fails)
restore()

R = "pkg/agentic/registry.go"

# --- M8 duplicate check removed ---
patch(R, "\tif _, exists := r.systems[id]; exists {", "\tif _, exists := r.systems[id]; false && exists {")
code, fails = run()
record("M8", "Register stops refusing a duplicate id", code, fails)
restore()

# --- M9 normalization skipped ---
patch(R, "\tid, err := NormalizeSystemID(string(raw))\n\tif err != nil {\n\t\treturn fmt.Errorf(\"agentic: registering system: %w\", err)\n\t}",
         "\tid, err := raw, error(nil)\n\tif err != nil {\n\t\treturn fmt.Errorf(\"agentic: registering system: %w\", err)\n\t}")
code, fails = run()
record("M9", "Register skips id normalization", code, fails)
restore()

# --- M10 no-launch-modes check removed ---
patch(R, "\tif len(caps.LaunchModes) == 0 {", "\tif false && len(caps.LaunchModes) == 0 {")
code, fails = run()
record("M10", "Register admits a system declaring no launch modes", code, fails)
restore()

S = "pkg/agentic/system.go"
# --- M11 CanCarry always true ---
patch(S, "\tif s == EffortSupportRequired {\n\t\treturn t == EffortTransportArgv || t == EffortTransportStdin\n\t}",
         "\tif s == EffortSupportRequired {\n\t\treturn true\n\t}")
code, fails = run()
record("M11", "CanCarry returns true for every transport", code, fails)
restore()

P = "pkg/agentic/plan.go"
# --- M12 composition under GrammarNone admitted ---
patch(P, "\t\tif caps.CompositionGrammar == GrammarNone {", "\t\tif false && caps.CompositionGrammar == GrammarNone {")
code, fails = run()
record("M12", "BuildPlan admits a composition under GrammarNone", code, fails)
restore()

# --- M13 detached stdin bytes admitted ---
patch(P, "\tif !stdin.Attached && len(stdin.Bytes) > 0 {", "\tif false && !stdin.Attached && len(stdin.Bytes) > 0 {")
code, fails = run()
record("M13", "BuildPlan admits stdin bytes reported as detached", code, fails)
restore()

# --- M14 empty binary admitted ---
patch(P, "\tif strings.TrimSpace(binary) == \"\" {", "\tif false && strings.TrimSpace(binary) == \"\" {")
code, fails = run()
record("M14", "BuildPlan admits an empty resolved binary", code, fails)
restore()

# --- M15 mode validity but not declared support ---
patch(P, "\tif !mode.Valid() || !caps.SupportsMode(mode) {", "\tif !mode.Valid() {")
code, fails = run()
record("M15", "BuildPlan checks mode validity but not declared support", code, fails)
restore()

# --- M16 required effort with no value admitted ---
patch(P, "\tif req.Model.Effort == EffortSupportRequired && effort == \"\" {",
         "\tif false && req.Model.Effort == EffortSupportRequired && effort == \"\" {")
code, fails = run()
record("M16", "BuildPlan admits a required-effort model with no effort value", code, fails)
restore()

code, fails = run()
record("M0'", "control after restoring all 16", code, fails)
json.dump(results, open(".temp/TASK-260821-21vywo/rerun-mutants.json","w"), indent=1)
