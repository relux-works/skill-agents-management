#!/usr/bin/env python3
"""Cross-story isolation check on the MERGED tree.

Each mutant narrows exactly one story's half of the shared guard file. The
claim under test is not "the suite goes red" — it is that the RIGHT test goes
red and its neighbour from the other story stays green. Two stories extending
one guard file could otherwise mask each other: a mutant that reds *something*
proves nothing about which rule is still load-bearing.

Runs in a throwaway copy; the reviewed worktree is never mutated.
"""
from __future__ import annotations
import pathlib, re, shutil, subprocess, sys, tempfile

ROOT = pathlib.Path(__file__).resolve().parents[3]
GUARD = "pkg/agentic/singlesource_guard_test.go"

# tests owned by each story, as they stand on the merged tree
LIMIT_TESTS = [
    "TestSingleSourceAllowlistHasNoUnusedEntries",
    "TestSingleSourceAllowlistEntriesCarryAReason",
    "TestSingleSourceAllowlistDoesNotExemptTheRestOfItsFile",
    "TestSingleSourceAllowlistedSitesAreReportedWhenNotExempt",
]
VENDOR_TESTS = [
    "TestSingleSourceGuardHomesSplitByKind",
    "TestEveryVendorHasExactlyOneBindingFile",
    "TestSingleSourceGuardHomesAreDistinctFacts",
]
ALL = LIMIT_TESTS + VENDOR_TESTS

MUTANTS = [
    # (name, edits, must-fail, must-stay-green)
    (
        "limit: point an exemption at a file that has no such site (stale entry)",
        [(GUARD,
          'singleSourceAllowlistKey("pkg/providerlimits/ladder.go", "var legacyLadderRuntimes")',
          'singleSourceAllowlistKey("pkg/providerlimits/nowhere.go", "var legacyLadderRuntimes")')],
        {"TestSingleSourceAllowlistHasNoUnusedEntries"},
        set(VENDOR_TESTS),
    ),
    (
        "limit: widen an exemption's reason to a label",
        [(GUARD,
          're:(singleSourceAllowlistKey\\("pkg/providerlimits/groups\\.go", "func HasClassifier"\\): )"" \\+\\n\\t\\t"[^\\n]*",',
          r'\1"a broker list",')],
        {"TestSingleSourceAllowlistEntriesCarryAReason"},
        set(VENDOR_TESTS),
    ),
    (
        "vendor: a vendor loses its binding-file home",
        [(GUARD,
          '"google models":    "pkg/vendorplugin/vendors/google/models.go",',
          '"google models":    "pkg/vendorplugin/vendors/google/nowhere.go",')],
        {"TestSingleSourceGuardHomesAreDistinctFacts", "TestEveryVendorHasExactlyOneBindingFile"},
        set(LIMIT_TESTS),
    ),
    (
        "vendor: two vendors share one binding file",
        [(GUARD,
          '"openai":    "pkg/vendorplugin/vendors/openai/models.go",',
          '"openai":    "pkg/vendorplugin/vendors/anthropic/models.go",')],
        {"TestEveryVendorHasExactlyOneBindingFile"},
        set(LIMIT_TESTS),
    ),
    (
        "vendor: an id-spelling home keyed as a legal Go identifier",
        [(GUARD,
          '"anthropic models": "pkg/vendorplugin/vendors/anthropic/models.go",',
          '"anthropicmodels":  "pkg/vendorplugin/vendors/anthropic/models.go",')],
        {"TestSingleSourceGuardHomesSplitByKind"},
        set(LIMIT_TESTS),
    ),
]

RUN = "^(" + "|".join(ALL) + ")$"


def results(work: pathlib.Path) -> dict[str, str]:
    proc = subprocess.run(
        ["go", "test", "-mod=mod", "./pkg/agentic/", "-run", RUN, "-count=1", "-v"],
        cwd=work, capture_output=True, text=True,
    )
    out = proc.stdout + proc.stderr
    seen: dict[str, str] = {}
    for line in out.splitlines():
        m = re.match(r"\s*--- (PASS|FAIL|SKIP): ([A-Za-z0-9_]+)(/|\s|$)", line)
        if m and m.group(2) in ALL:
            # a subtest failure marks its parent; keep the strongest verdict
            if m.group(2) not in seen or m.group(1) == "FAIL":
                seen[m.group(2)] = m.group(1)
    return seen, proc.returncode, out


def main() -> int:
    bad = []
    with tempfile.TemporaryDirectory() as scratch:
        base = pathlib.Path(scratch) / "base"
        shutil.copytree(ROOT, base,
                        ignore=shutil.ignore_patterns(".git", ".temp", ".task-board"),
                        symlinks=True)
        seen, code, out = results(base)
        print(f"baseline: exit {code}")
        for t in ALL:
            print(f"  {seen.get(t, 'MISSING'):8s} {t}")
        if code != 0 or any(seen.get(t) != "PASS" for t in ALL):
            print("BASELINE NOT GREEN — results would be meaningless")
            print(out[-3000:])
            return 1
        print()

        for name, edits, must_fail, must_pass in MUTANTS:
            work = pathlib.Path(scratch) / "m"
            if work.exists():
                shutil.rmtree(work)
            shutil.copytree(base, work, symlinks=True)
            ok = True
            for rel, old, new in edits:
                p = work / rel
                text = p.read_text(encoding="utf-8")
                if old.startswith("re:"):
                    pat = old[3:]
                    if re.search(pat, text) is None:
                        print(f"ANCHOR NOT FOUND (regex): {name}\n  {pat[:80]!r}")
                        bad.append((name, "anchor not found — harness is stale"))
                        ok = False
                        break
                    text = re.sub(pat, new, text, count=1)
                else:
                    if old not in text:
                        print(f"ANCHOR NOT FOUND: {name}\n  {old[:80]!r}")
                        bad.append((name, "anchor not found — harness is stale"))
                        ok = False
                        break
                    text = text.replace(old, new, 1)
                p.write_text(text, encoding="utf-8")
            if not ok:
                continue
            seen, code, out = results(work)
            print(f"### {name}   (suite exit {code})")
            for t in ALL:
                print(f"  {seen.get(t, 'MISSING'):8s} {t}")
            for t in must_fail:
                if seen.get(t) != "FAIL":
                    bad.append((name, f"{t} was expected to FAIL and did not ({seen.get(t)})"))
            for t in must_pass:
                if seen.get(t) != "PASS":
                    bad.append((name, f"{t} belongs to the OTHER story and did not stay green ({seen.get(t)})"))
            print()

    if bad:
        print("PROBLEMS:")
        for n, why in bad:
            print(f"  - {n}: {why}")
        return 1
    print("every mutant redded exactly its own story's rule; no cross-masking")
    return 0


if __name__ == "__main__":
    sys.exit(main())
