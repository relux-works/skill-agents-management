#!/usr/bin/env python3
"""Re-run the ten mutants from TASK-260821-atcotl's review verdict.

Each entry is (file, exact old text, replacement) reconstructed from the diffs
and descriptions in TASK-260821-atcotl_review-verdict.md. A mutant that does
not apply is a harness bug, not a survivor, and is reported as such.
"""
import subprocess, sys, os, json

ROOT = "/tmp/atcotl-mut"

AVAIL = "pkg/vendorplugin/availability.go"
VENDOR = "pkg/vendorplugin/vendor.go"
REG = "pkg/vendorplugin/registry.go"
RUNTIME = "pkg/vendorplugin/runtime.go"
SPAWN = "pkg/vendorplugin/spawn.go"
GUARD = "pkg/agentic/singlesource_guard_test.go"

MUTANTS = [
 ("M1 zero-value Availability becomes healthy", AVAIL,
  '''	AvailabilityUnknown AvailabilityState = iota
	// AvailabilityHealthy means requests can be made right now.
	AvailabilityHealthy''',
  '''	AvailabilityHealthy AvailabilityState = iota
	// AvailabilityUnknown is no longer the zero value.
	AvailabilityUnknown'''),

 ("M2a rank evidence source blank check narrowed to == \"\"", VENDOR,
  '''		if strings.TrimSpace(evidence.Source) == "" {''',
  '''		if evidence.Source == "" {'''),

 ("M2b rank evidence observation blank check narrowed to == \"\"", VENDOR,
  '''		if strings.TrimSpace(evidence.Observation) == "" {''',
  '''		if evidence.Observation == "" {'''),

 ("M3 effort vocabulary widened to EqualFold", VENDOR,
  '''		if declared == trimmed {''',
  '''		if strings.EqualFold(declared, trimmed) {'''),

 ("M4 duplicate rank position admitted", REG,
  '''		if other, taken := seenRanks[model.Rank.Position]; taken {''',
  '''		if other, taken := seenRanks[model.Rank.Position]; taken && false {'''),

 ("M5 ErrRuntimeVendorUnresolved aliased to ...Unregistered", REG,
  '''	ErrRuntimeVendorUnresolved = errors.New("vendorplugin: runtime's vendor was never established")''',
  '''	ErrRuntimeVendorUnresolved = ErrRuntimeVendorUnregistered'''),

 ("M6 CheckAvailability validates state only", SPAWN,
  '''	if err := verdict.Validate(); err != nil {
		return Availability{}, fmt.Errorf("%w: vendor %s: %w", ErrVendorContract, id, err)
	}''',
  '''	if !verdict.State.Valid() {
		return Availability{}, fmt.Errorf("%w: vendor %s: undeclared state", ErrVendorContract, id)
	}'''),

 ("M7 bindingHomes flattened to a flat allowlist", GUARD,
  '''	misplaced := func(key, file string) bool { return homes[key] != file }''',
  '''	misplaced := func(key, file string) bool {
		for _, home := range homes {
			if home == file {
				return false
			}
		}
		return true
	}'''),

 ("M8 Broker.Checked blank check narrowed to == \"\"", RUNTIME,
  '''		if strings.TrimSpace(source) == "" {''',
  '''		if source == "" {'''),

 ("M9 unresolved check moved below the vendor lookup", REG,
  '''	if !declaration.VendorResolved() {
		return Runtime{}, fmt.Errorf("%w: runtime %s was recorded with an unknown broker after checking %v; nothing here will guess one, because the binding keys limit state",
			ErrRuntimeVendorUnresolved, normalized, declaration.Broker.Checked)
	}
	vendor, ok := r.Lookup(declaration.Vendor)
	if !ok {
		return Runtime{}, fmt.Errorf("%w: runtime %s names vendor %q, which no plugin in this binary registers",
			ErrRuntimeVendorUnregistered, normalized, declaration.Vendor)
	}''',
  '''	vendor, ok := r.Lookup(declaration.Vendor)
	if !ok {
		return Runtime{}, fmt.Errorf("%w: runtime %s names vendor %q, which no plugin in this binary registers",
			ErrRuntimeVendorUnregistered, normalized, declaration.Vendor)
	}
	if !declaration.VendorResolved() {
		return Runtime{}, fmt.Errorf("%w: runtime %s was recorded with an unknown broker after checking %v; nothing here will guess one, because the binding keys limit state",
			ErrRuntimeVendorUnresolved, normalized, declaration.Broker.Checked)
	}'''),

 ("M11 Accepts stops trimming (the exported half of the rule)", VENDOR,
  '''	trimmed := strings.TrimSpace(word)''',
  '''	trimmed := word'''),

 ("M12 BuildLaunch stops trimming the requested effort", SPAWN,
  '''	effort := strings.TrimSpace(raw)''',
  '''	effort := raw'''),

 ("M10 unknown may carry a clear time", AVAIL,
  '''	if a.State != AvailabilityLimited && !a.Until.IsZero() {''',
  '''	if a.State != AvailabilityLimited && a.State != AvailabilityUnknown && !a.Until.IsZero() {'''),
]

def restore():
    subprocess.run(["git", "checkout", "-q", "--", "."], cwd=ROOT, check=True)

results = []
for name, rel, old, new in MUTANTS:
    restore()
    path = os.path.join(ROOT, rel)
    src = open(path).read()
    count = src.count(old)
    if count != 1:
        results.append((name, rel, "HARNESS-BUG", f"anchor matched {count} times", []))
        print(f"### {name}: HARNESS BUG — anchor matched {count} times in {rel}")
        continue
    open(path, "w").write(src.replace(old, new))
    proc = subprocess.run(["env", "-u", "TASK_BOARD_DIR", "go", "test", "-mod=mod", "./...", "-count=1"],
                          cwd=ROOT, capture_output=True, text=True)
    out = proc.stdout + proc.stderr
    if proc.returncode == 0:
        results.append((name, rel, "SURVIVOR", "exit 0 — no test noticed", []))
        print(f"### {name}\n    SURVIVOR: exit 0")
    else:
        red = sorted({l.strip().split(" ")[2] for l in out.splitlines()
                      if l.strip().startswith("--- FAIL:")})
        results.append((name, rel, "KILLED", f"exit {proc.returncode}", red))
        print(f"### {name}\n    KILLED: exit {proc.returncode}\n    red: {', '.join(red) if red else '(build failure)'}")
restore()

survivors = [r for r in results if r[2] != "KILLED"]
print(f"\nTOTAL {len(results)} mutants, {len(results)-len(survivors)} KILLED, {len(survivors)} SURVIVORS")
for s in survivors:
    print(f"  SURVIVOR: {s[0]}")
json.dump([{"mutant": n, "file": f, "result": r, "detail": d, "red": red}
           for n, f, r, d, red in results], open("/tmp/atcotl-reviewer-mutants.json", "w"), indent=2)
sys.exit(1 if survivors else 0)
