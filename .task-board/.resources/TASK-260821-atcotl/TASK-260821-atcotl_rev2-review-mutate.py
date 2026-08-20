import subprocess, sys, os, json, shutil

S = "/tmp/atcotl-s2"

MUTANTS = [
  # (id, file, old, new, note)
  ("M1", "pkg/vendorplugin/availability.go",
   "\tAvailabilityUnknown AvailabilityState = iota\n\t// AvailabilityHealthy means requests can be made right now.\n\tAvailabilityHealthy\n",
   "\tAvailabilityHealthy AvailabilityState = iota\n\t// AvailabilityUnknown moved off the zero value.\n\tAvailabilityUnknown\n",
   "zero-value Availability becomes healthy"),

  ("M2", "pkg/vendorplugin/vendor.go",
   "\t\tif strings.TrimSpace(evidence.Source) == \"\" {",
   "\t\tif evidence.Source == \"\" {",
   "rank evidence Source blank check narrowed to == \"\""),

  ("M2b", "pkg/vendorplugin/vendor.go",
   "\t\tif strings.TrimSpace(evidence.Observation) == \"\" {",
   "\t\tif evidence.Observation == \"\" {",
   "rank evidence Observation blank check narrowed to == \"\""),

  ("M3", "pkg/vendorplugin/vendor.go",
   "\t\tif declared == trimmed {",
   "\t\tif strings.EqualFold(declared, trimmed) {",
   "effort vocabulary widened to EqualFold"),

  ("M4", "pkg/vendorplugin/registry.go",
   "\t\tif other, taken := seenRanks[model.Rank.Position]; taken {\n\t\t\treturn fmt.Errorf(\"%w: %s ranks both %q and %q at position %d; a lineup that cannot order itself is not a ranking\",\n\t\t\t\tErrDuplicateRank, id, other, model.ID, model.Rank.Position)\n\t\t}\n",
   "\t\tif _, taken := seenRanks[model.Rank.Position]; taken && false {\n\t\t\treturn fmt.Errorf(\"%w\", ErrDuplicateRank)\n\t\t}\n",
   "duplicate rank position admitted"),

  ("M5", "pkg/vendorplugin/registry.go",
   "\tErrRuntimeVendorUnresolved = errors.New(\"vendorplugin: runtime's vendor was never established\")",
   "\tErrRuntimeVendorUnresolved = ErrRuntimeVendorUnregistered",
   "ErrRuntimeVendorUnresolved aliased to ErrRuntimeVendorUnregistered"),

  ("M6", "pkg/vendorplugin/spawn.go",
   "\tif err := verdict.Validate(); err != nil {\n\t\treturn Availability{}, fmt.Errorf(\"%w: vendor %s: %w\", ErrVendorContract, id, err)",
   "\tif !verdict.State.Valid() {\n\t\treturn Availability{}, fmt.Errorf(\"%w: vendor %s: bad state\", ErrVendorContract, id)",
   "CheckAvailability validates state only"),

  ("M7", "pkg/agentic/singlesource_guard_test.go",
   "var bindingHomes = map[string]string{\n\t\"SystemID\":  \"pkg/agentic/registry.go\",\n\t\"VendorID\":  \"pkg/vendorplugin/registry.go\",\n\t\"RuntimeID\": \"pkg/vendorplugin/registry.go\",\n}",
   "var bindingHomes = func() map[string]string {\n\tflat := []string{\"pkg/agentic/registry.go\", \"pkg/vendorplugin/registry.go\"}\n\t_ = flat\n\treturn map[string]string{\n\t\t\"SystemID\":  \"pkg/agentic/registry.go|pkg/vendorplugin/registry.go\",\n\t\t\"VendorID\":  \"pkg/agentic/registry.go|pkg/vendorplugin/registry.go\",\n\t\t\"RuntimeID\": \"pkg/agentic/registry.go|pkg/vendorplugin/registry.go\",\n\t}\n}()",
   "bindingHomes flattened to a flat allowlist"),

  ("M9", "pkg/vendorplugin/registry.go",
   "\tif !declaration.VendorResolved() {\n\t\treturn Runtime{}, fmt.Errorf(\"%w: runtime %s was recorded with an unknown broker after checking %v; nothing here will guess one, because the binding keys limit state\",\n\t\t\tErrRuntimeVendorUnresolved, normalized, declaration.Broker.Checked)\n\t}\n\tvendor, ok := r.Lookup(declaration.Vendor)\n\tif !ok {\n\t\treturn Runtime{}, fmt.Errorf(\"%w: runtime %s names vendor %q, which no plugin in this binary registers\",\n\t\t\tErrRuntimeVendorUnregistered, normalized, declaration.Vendor)\n\t}\n",
   "\tvendor, ok := r.Lookup(declaration.Vendor)\n\tif !ok {\n\t\treturn Runtime{}, fmt.Errorf(\"%w: runtime %s names vendor %q, which no plugin in this binary registers\",\n\t\t\tErrRuntimeVendorUnregistered, normalized, declaration.Vendor)\n\t}\n\tif !declaration.VendorResolved() {\n\t\treturn Runtime{}, fmt.Errorf(\"%w: runtime %s was recorded with an unknown broker after checking %v; nothing here will guess one, because the binding keys limit state\",\n\t\t\tErrRuntimeVendorUnresolved, normalized, declaration.Broker.Checked)\n\t}\n",
   "unresolved check moved below the vendor lookup"),

  ("M10", "pkg/vendorplugin/availability.go",
   "\tif a.State != AvailabilityLimited && !a.Until.IsZero() {",
   "\tif a.State != AvailabilityLimited && a.State != AvailabilityUnknown && !a.Until.IsZero() {",
   "unknown may carry a clear time"),

  ("M8", "pkg/vendorplugin/runtime.go", None, None, "Broker.Checked blank check narrowed to == \"\""),
]

def run_tests():
    env = dict(os.environ)
    env.pop("TASK_BOARD_DIR", None)
    p = subprocess.run(["go","test","-mod=mod","./...","-count=1"], cwd=S, capture_output=True, text=True, env=env)
    return p.returncode, p.stdout + p.stderr

def restore():
    subprocess.run(["git","checkout","-q","--","."], cwd=S, check=True)

results = []
for mid, f, old, new, note in MUTANTS:
    restore()
    path = os.path.join(S, f)
    src = open(path).read()
    if old is None:
        # M8: narrow the Broker.Checked blank test
        import re
        seg = src
        target = "\tfor i, source := range d.Broker.Checked {"
        idx = src.index(target)
        end = src.index("\n\t}\n", idx)
        block = src[idx:end]
        if "strings.TrimSpace(source) == \"\"" not in block:
            print(f"{mid}: ANCHOR MISS"); results.append((mid,note,"ANCHOR MISS","")); continue
        newblock = block.replace("strings.TrimSpace(source) == \"\"", "source == \"\"")
        src = src[:idx] + newblock + src[end:]
        open(path,"w").write(src)
    else:
        if old not in src:
            print(f"{mid}: ANCHOR MISS in {f}"); results.append((mid,note,"ANCHOR MISS","")); continue
        src = src.replace(old, new, 1)
        open(path,"w").write(src)
    d = subprocess.run(["git","diff","--quiet"], cwd=S)
    if d.returncode == 0:
        print(f"{mid}: NO DIFF"); results.append((mid,note,"NO DIFF","")); restore(); continue
    code, out = run_tests()
    if code == 0:
        print(f"{mid}: *** SURVIVOR *** ({note})")
        results.append((mid,note,"SURVIVOR",""))
    else:
        fails = sorted(set(l.strip() for l in out.splitlines() if l.strip().startswith("--- FAIL")))
        top = [l.replace("--- FAIL: ","").split(" ")[0] for l in fails]
        # keep only top-level test names
        tops = sorted(set(t.split("/")[0] for t in top))
        print(f"{mid}: KILLED (exit {code}) by {', '.join(tops[:6])}{' ...' if len(tops)>6 else ''}")
        results.append((mid,note,"KILLED",", ".join(tops)))
restore()
json.dump([{"id":a,"mutation":b,"result":c,"killed_by":d} for a,b,c,d in results], open("/tmp/atcotl-rerun.json","w"), indent=1)
print("\n=== SUMMARY ===")
for a,b,c,d in results:
    print(f"{a:5} {c:12} {b}")
import subprocess, os, json
S = "/tmp/atcotl-s2"

P = [
 ("P1a","pkg/vendorplugin/registry.go",
  '\tErrUnknownRuntime = errors.New("vendorplugin: no runtime declared")',
  '\tErrUnknownRuntime = ErrRuntimeVendorUnresolved',
  "collapse ErrUnknownRuntime into ErrRuntimeVendorUnresolved (pair NOT used in rev1)"),
 ("P1b","pkg/vendorplugin/registry.go",
  '\tErrRuntimeSystemUnregistered = errors.New("vendorplugin: runtime\'s agentic system is not registered")',
  '\tErrRuntimeSystemUnregistered = ErrUnknownRuntime',
  "collapse ErrRuntimeSystemUnregistered into ErrUnknownRuntime (pair NOT used in rev1)"),
 ("P1c","pkg/vendorplugin/registry.go",
  '\tErrVendorNotRegistered = errors.New("vendorplugin: no vendor registered")',
  '\tErrVendorNotRegistered = ErrRuntimeVendorUnregistered',
  "collapse ErrVendorNotRegistered into ErrRuntimeVendorUnregistered (pair NOT used in rev1)"),
 ("P1d","pkg/vendorplugin/registry.go",
  '\tErrRuntimeVendorUnregistered = errors.New("vendorplugin: runtime\'s vendor is not registered")',
  '\tErrRuntimeVendorUnregistered = ErrUnknownRuntime',
  "collapse ErrRuntimeVendorUnregistered into ErrUnknownRuntime (pair NOT used in rev1)"),
 ("P3","pkg/vendorplugin/spawn.go",
  '\teffort := strings.TrimSpace(raw)',
  '\teffort := raw',
  "BuildLaunch (resolveEffort) stops trimming -> accepted-spellings half must red"),
 ("P4","pkg/vendorplugin/vendor.go",
  '\ttrimmed := strings.TrimSpace(word)',
  '\ttrimmed := word',
  "Accepts stops trimming -> Accepts-level test must red"),
 ("M7f","pkg/agentic/singlesource_guard_test.go",
  '\tmisplaced := func(key, file string) bool { return homes[key] != file }',
  '\tmisplaced := func(key, file string) bool { _ = key; return !spellsIDsFlat(homes, file) }',
  "bindingHomes semantics flattened: any home file may bind ANY key type"),
]

def restore(): subprocess.run(["git","checkout","-q","--","."], cwd=S, check=True)
def run():
    env=dict(os.environ); env.pop("TASK_BOARD_DIR",None)
    p=subprocess.run(["go","test","-mod=mod","./...","-count=1"],cwd=S,capture_output=True,text=True,env=env)
    return p.returncode, p.stdout+p.stderr

out=[]
for pid,f,old,new,note in P:
    restore()
    path=os.path.join(S,f); src=open(path).read()
    if old not in src:
        print(f"{pid}: ANCHOR MISS"); out.append((pid,note,"ANCHOR MISS","")); continue
    src=src.replace(old,new,1)
    if pid=="M7f":
        src=src.replace("func scanSingleSource(", "func spellsIDsFlat(homes map[string]string, file string) bool {\n\tfor _, h := range homes {\n\t\tif h == file {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn false\n}\n\nfunc scanSingleSource(",1)
    open(path,"w").write(src)
    code,o=run()
    if code==0:
        print(f"{pid}: *** SURVIVOR *** {note}"); out.append((pid,note,"SURVIVOR",""))
    else:
        fails=sorted(set(l.strip().replace("--- FAIL: ","").split(" ")[0] for l in o.splitlines() if l.strip().startswith("--- FAIL")))
        tops=sorted(set(t.split("/")[0] for t in fails))
        subs=[t for t in fails if "/" in t]
        print(f"{pid}: KILLED by {', '.join(tops)}")
        for s in subs[:8]: print(f"        sub: {s}")
        out.append((pid,note,"KILLED",", ".join(tops)+" || "+"; ".join(subs[:10])))
restore()
json.dump([{"id":a,"mutation":b,"result":c,"killed_by":d} for a,b,c,d in out],open("/tmp/atcotl-probes.json","w"),indent=1)
import subprocess, os
S="/tmp/atcotl-s2"
P=[
 ("X1","pkg/vendorplugin/vendor.go",'\tif r.Position < 1 {','\tif r.Position < 0 {',"rank position narrowed: 0 admitted"),
 ("X2","pkg/vendorplugin/vendor.go",'\tif len(r.Basis) == 0 {','\tif r.Basis == nil {',"basis emptiness narrowed to nil-only"),
 ("X3","pkg/vendorplugin/runtime.go",None,None,"muse seed given a guessed vendor"),
 ("X4","pkg/vendorplugin/registry.go",
  '\tif !declaration.VendorResolved() {',
  '\tif false && !declaration.VendorResolved() {',
  "unresolved check deleted entirely"),
]
def restore(): subprocess.run(["git","checkout","-q","--","."],cwd=S,check=True)
def run():
    env=dict(os.environ); env.pop("TASK_BOARD_DIR",None)
    p=subprocess.run(["go","test","-mod=mod","./...","-count=1"],cwd=S,capture_output=True,text=True,env=env)
    return p.returncode,p.stdout+p.stderr
for pid,f,old,new,note in P:
    restore(); path=os.path.join(S,f); src=open(path).read()
    if pid=="X3":
        old='\t\tBroker: BrokerProvenance{Checked: []string{runtimeIDSource}},'
        new='\t\tVendor: "anthropic",\n\t\tBroker: BrokerProvenance{Checked: []string{runtimeIDSource}, Found: "guessed"},'
    if old not in src: print(f"{pid}: ANCHOR MISS"); continue
    open(path,"w").write(src.replace(old,new,1))
    code,o=run()
    if code==0: print(f"{pid}: *** SURVIVOR *** {note}")
    else:
        fails=sorted(set(l.strip().replace("--- FAIL: ","").split(" ")[0] for l in o.splitlines() if l.strip().startswith("--- FAIL")))
        tops=sorted(set(t.split("/")[0] for t in fails))
        print(f"{pid}: KILLED by {', '.join(tops[:6])}   [{note}]")
restore()
