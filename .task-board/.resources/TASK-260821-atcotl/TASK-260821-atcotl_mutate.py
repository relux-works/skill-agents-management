#!/usr/bin/env python3
"""Apply one gate mutation, run the tests that must go red, restore the file.

Every gate this task adds ships a negative test. A negative test is worth
nothing until it has been DEMONSTRATED to fail with the gate removed or
narrowed, so this script does that mechanically and records the exit codes.
"""
import subprocess, sys, os, json

MUTANTS = {
  # --- AC2: the dependency-direction refusal ------------------------------
  "ac2-delete": ("pkg/vendorplugin/registry.go",
    '''			if _, ok := r.systems.Lookup(system); !ok {''',
    '''			if false {'''),
  "ac2-narrow-first-model-only": ("pkg/vendorplugin/registry.go",
    '''		for _, system := range model.Systems {
			if _, ok := r.systems.Lookup(system); !ok {''',
    '''		for _, system := range model.Systems {
			if _, ok := r.systems.Lookup(system); !ok && model.ID == models[0].ID {'''),
  "ac2-bypass-nil-agentic-registry": ("pkg/vendorplugin/registry.go",
    '''	if r.systems == nil {
		return fmt.Errorf("%w: refusing every vendor rather than admitting one whose declared systems nobody checked", ErrNoAgenticRegistry)
	}''',
    '''	if r.systems == nil {
		return nil
	}'''),
  # --- F2 collision policy, both directions -------------------------------
  "f2-conflict-admitted": ("pkg/vendorplugin/registry.go",
    '''		if existing.SameBinding(declaration) {
			return nil
		}''',
    '''		if true {
			return nil
		}'''),
  "f2-match-refused": ("pkg/vendorplugin/registry.go",
    '''		if existing.SameBinding(declaration) {
			return nil
		}''',
    '''		if false {
			return nil
		}'''),
  "f2-last-write-wins": ("pkg/vendorplugin/runtime.go",
    '''	return d.ID == other.ID && d.System == other.System && d.Vendor == other.Vendor''',
    '''	return d.ID == other.ID'''),
  # --- the description that cannot be silently empty ----------------------
  "description-empty-admitted": ("pkg/vendorplugin/vendor.go",
    '''	if strings.TrimSpace(string(d)) == "" {
		return fmt.Errorf("%w: the field says what the model is best used for, and an operator choosing between models reads it", ErrDescriptionEmpty)
	}''',
    '''	if false {
		return fmt.Errorf("%w: unreachable", ErrDescriptionEmpty)
	}'''),
  "description-narrowed-to-nil-only": ("pkg/vendorplugin/vendor.go",
    '''	if strings.TrimSpace(string(d)) == "" {''',
    '''	if string(d) == "" {'''),
  # --- evidence-based ranking ---------------------------------------------
  "rank-without-evidence-admitted": ("pkg/vendorplugin/vendor.go",
    '''	if len(r.Basis) == 0 {''',
    '''	if false {'''),
  # --- the availability verdict gate --------------------------------------
  "availability-healthy-without-checking": ("pkg/vendorplugin/availability.go",
    '''		if len(a.Checked) == 0 {
			return fmt.Errorf("%w: healthy with nothing checked; a health claim that read no source is a guess", ErrAvailabilityInvalid)
		}''',
    '''		if false {
			return fmt.Errorf("%w: unreachable", ErrAvailabilityInvalid)
		}'''),
  "availability-gate-not-called-from-production": ("pkg/vendorplugin/spawn.go",
    '''	if err := verdict.Validate(); err != nil {
		return Availability{}, fmt.Errorf("%w: vendor %s: %w", ErrVendorContract, id, err)
	}''',
    '''	if false {
		return Availability{}, nil
	}'''),
  # --- effort: no default is injected anywhere ----------------------------
  "effort-recommended-substituted": ("pkg/vendorplugin/spawn.go",
    '''		if effort == "" {
			return "", fmt.Errorf("%w: model %q under runtime %s accepts %v and the vendor recommends %q; supply one of them",
				ErrEffortMissing, model.ID, runtime.ID, model.Effort.Vocabulary, model.Effort.Recommended)
		}''',
    '''		if effort == "" {
			return model.Effort.Recommended, nil
		}'''),
  "effort-vocabulary-unchecked": ("pkg/vendorplugin/spawn.go",
    '''		if !model.Effort.Accepts(effort) {''',
    '''		if false {'''),
  # --- a vendor may add, never redirect -----------------------------------
  "fidelity-not-checked": ("pkg/vendorplugin/spawn.go",
    '''	if err := checkLaunchFidelity(runtime, model, effort, req, launch); err != nil {
		return agentic.Plan{}, err
	}''',
    '''	if false {
		return agentic.Plan{}, nil
	}'''),
  "fidelity-narrowed-to-system-only": ("pkg/vendorplugin/spawn.go",
    '''	if launch.Model.ID != string(model.ID) {''',
    '''	if false {'''),
  # --- the model must declare the runtime's harness -----------------------
  "model-system-pairing-unchecked": ("pkg/vendorplugin/spawn.go",
    '''	if !model.DrivenBy(runtime.SystemID) {''',
    '''	if false {'''),
  # --- an unresolved vendor must not resolve ------------------------------
  "unresolved-vendor-resolves": ("pkg/vendorplugin/registry.go",
    '''	if !declaration.VendorResolved() {''',
    '''	if false {'''),
  # --- the single-source guard extension ----------------------------------
  "guard-extension-removed": ("pkg/agentic/singlesource_guard_test.go",
    '''	"VendorID":  "pkg/vendorplugin/registry.go",
	"RuntimeID": "pkg/vendorplugin/registry.go",''',
    ''''''),
  "guard-homes-flattened-to-an-allowlist": ("pkg/agentic/singlesource_guard_test.go",
    '''	misplaced := func(key, file string) bool { return homes[key] != file }''',
    '''	misplaced := func(key, file string) bool {
		for _, home := range homes {
			if home == file {
				return false
			}
		}
		return true
	}'''),
}

def run(pkg, pattern):
    cmd = ["go", "test", "-mod=mod", pkg, "-count=1"]
    if pattern:
        cmd += ["-run", pattern]
    env = dict(os.environ); env.pop("TASK_BOARD_DIR", None)
    p = subprocess.run(cmd, capture_output=True, text=True, env=env)
    return p.returncode, p.stdout + p.stderr

def main():
    results = []
    for name, (path, old, new) in MUTANTS.items():
        src = open(path).read()
        if old not in src:
            print(f"!! {name}: anchor not found in {path}")
            sys.exit(2)
        open(path, "w").write(src.replace(old, new, 1))
        try:
            pkg = "./pkg/agentic/" if path.startswith("pkg/agentic") else "./pkg/vendorplugin/"
            code, out = run(pkg, None)
        finally:
            open(path, "w").write(src)
        failing = [l for l in out.splitlines() if l.startswith("--- FAIL") or l.startswith("    --- FAIL")]
        results.append({"mutant": name, "file": path, "exit": code, "failing_tests": failing[:8], "count": len(failing)})
        status = "RED (good)" if code != 0 else "GREEN (BAD: nothing caught it)"
        print(f"{name}: exit={code} {status}; {len(failing)} failing test(s)")
        for line in failing[:6]:
            print("   ", line.strip())
    json.dump(results, open(".temp/TASK-260821-atcotl/mutant-results.json", "w"), indent=2)
    survivors = [r["mutant"] for r in results if r["exit"] == 0]
    if survivors:
        print("\nSURVIVORS (gates with no negative test behind them):", survivors)
        sys.exit(1)
    print("\nAll mutants killed.")

main()
