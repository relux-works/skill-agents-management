package claude

import (
	"context"
	"encoding/json"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Node is used only to execute the installed pinned parser functions verbatim;
// reimplementing those in a test would share the assumption being compared.
// No Claude session, configuration or credential store is accessed.
func TestPinnedClaudeNativeGrammarComparison(t *testing.T) {
	path, err := exec.LookPath(executableName)
	if err != nil {
		t.Skip("pinned Claude not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(version)) != "2.1.288 (Claude Code)" {
		t.Skipf("not the pinned release: %s", version)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable for pinned native parser comparison")
	}
	help, err := exec.CommandContext(ctx, path, "--help").Output()
	if err != nil {
		t.Fatal(err)
	}
	// Derive the native option rows from help, independently of the production map.
	nativeArities := map[string]string{}
	for _, line := range strings.Split(strings.Split(string(help), "Commands:")[0], "\n") {
		if !strings.HasPrefix(line, "  -") {
			continue
		}
		grammar := strings.Split(strings.TrimSpace(line), "  ")[0]
		arity := "none"
		if strings.Contains(grammar, "<") {
			arity = "required"
		} else if strings.Contains(grammar, "[") {
			arity = "optional"
		}
		if strings.Contains(grammar, "...") {
			arity = "variadic"
		}
		for _, token := range strings.Fields(grammar) {
			token = strings.TrimSuffix(token, ",")
			if strings.HasPrefix(token, "-") {
				nativeArities[token] = arity
			}
		}
	}
	for name, arity := range nativeArities {
		want := map[string]optionArity{"none": optionNone, "required": optionRequired, "optional": optionOptional, "variadic": optionVariadic}[arity]
		if got, ok := claudeOptionArities[name]; !ok || got != want {
			t.Fatalf("pinned help arity %s=%s, production %v (present=%v)", name, arity, got, ok)
		}
	}
	type sample struct {
		Args []string
		Deny []string
	}
	var cases []sample
	for name, arity := range nativeArities {
		if arity == "required" || arity == "optional" || arity == "variadic" {
			cases = append(cases, sample{Args: []string{name, "--allowedTools=AskUserQuestion"}})
			if strings.HasPrefix(name, "--") {
				cases = append(cases, sample{Args: []string{name + "=--allowedTools=AskUserQuestion"}})
			}
		}
	}
	for _, args := range [][]string{
		{"--allowedTools", "Read", "AskUserQuestion"},
		{"--allowedTools=Read", "AskUserQuestion"},
		{"--append-system-prompt", "--", "--allowedTools=AskUserQuestion"},
		{"--settings={}", "--settings", `{"permissions":{"allow":["Read"]}}`},
		{"--", "--allowedTools=AskUserQuestion"},
		{"-n--allowedTools=AskUserQuestion"},
		{"-cn--allowedTools=AskUserQuestion"},
		{"--disallowedTools=Bash(", "--disallowedTools=Read"},
	} {
		cases = append(cases, sample{Args: args})
	}
	for _, args := range [][]string{
		{"--disallowedTools=Bash(", "--disallowedTools=Read"},
		{"--disallowed-tools", "Bash(git *) Edit", "--disallowedTools=Read"},
		{"--disallowedTools=Read,AskUserQuestion"},
		{"--disallowedTools", "AskUserQuestion(*)"},
	} {
		req := interactiveRequest(tempSlot(t))
		req.NativeArgs = args
		argv := argvFor(t, req, agentic.LaunchModeInteractive)
		var values []string
		for _, option := range claudeOptions(argv) {
			if isDisallowedToolsFlag(option.name) {
				values = append(values, option.values...)
			}
		}
		cases = append(cases, sample{Args: args, Deny: values})
	}
	input, err := json.Marshal(struct {
		Path    string
		Arities map[string]string
		Cases   []sample
	}{path, nativeArities, cases})
	if err != nil {
		t.Fatal(err)
	}
	// Anchors are release-specific: absence fails the pinned check, not a fallback
	// to an invented tokenizer. The fake host records only native option events.
	const script = `
const fs=require('fs');const input=JSON.parse(fs.readFileSync(0,'utf8'));
const bytes=fs.readFileSync(input.Path).toString('latin1');
const start=bytes.indexOf('parseOptions(t){',170000000), end=bytes.indexOf('opts(){',start);
const first=bytes.indexOf('function Fp(',170000000), last=bytes.indexOf('function p(',first);
if(start<0||end<0||first<0||last<0)throw Error('pinned native anchors missing');
const parse=new Function('return ({'+bytes.slice(start,end)+'}).parseOptions')();
const Fp=new Function(bytes.slice(first,last)+';return Fp;')();
const output=input.Cases.map(sample=>{
 const events=[];const host={_combineFlagAndOptionalValue:true,
 _findOption(name){const arity=input.Arities[name];if(!arity)return null;
 return {required:arity==='required'||arity==='variadic',optional:arity==='optional',variadic:arity==='variadic',name(){return name}}},
 emit(name,value){events.push({name:name.slice(7),values:value===undefined||value===null?[]:[value]})},optionMissingArgument(){throw Error('missing argument')},_getHelpCommand(){return null},_findCommand(){return null}};
 parse.call(host,sample.Args);
 // Fold successive variadic events as Commander does for an occurrence.
 const folded=[];for(const event of events){let previous=folded[folded.length-1];
 if(previous&&previous.name===event.name&&input.Arities[event.name]==='variadic')previous.values.push(...event.values);else folded.push({name:event.name,values:[...event.values]})};
 const values=folded.filter(x=>x.name==='--disallowedTools'||x.name==='--disallowed-tools').flatMap(x=>x.values);
 return {options:events,original:Fp(values),planned:sample.Deny?Fp(sample.Deny):null};
});process.stdout.write(JSON.stringify(output));`
	command := exec.CommandContext(ctx, node, "-e", script)
	command.Stdin = strings.NewReader(string(input))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pinned parser comparison: %v: %s", err, output)
	}
	var results []struct {
		Options []struct {
			Name   string
			Values []string
		}
		Original, Planned []string
	}
	if err := json.Unmarshal(output, &results); err != nil {
		t.Fatal(err)
	}
	for i, result := range results {
		var got []claudeOption
		for _, option := range claudeOptions(cases[i].Args) {
			if len(option.values) == 0 {
				got = append(got, option)
			} else {
				for _, value := range option.values {
					event := option
					event.values = []string{value}
					got = append(got, event)
				}
			}
		}
		if len(got) != len(result.Options) {
			t.Fatalf("native option ownership differs for %q: %#v vs %#v", cases[i].Args, got, result.Options)
		}
		for j, native := range result.Options {
			if got[j].name != native.Name || strings.Join(got[j].values, "\x00") != strings.Join(native.Values, "\x00") {
				t.Fatalf("native option ownership differs for %q: %#v vs %#v", cases[i].Args, got, result.Options)
			}
		}
		if cases[i].Deny != nil {
			planned := result.Planned
			// Remove only the independently added module denial for the comparison.
			if len(planned) > 0 && planned[0] == deniedToolAskUserQuestion && !reflect.DeepEqual(planned, result.Original) {
				planned = planned[1:]
			}
			if !reflect.DeepEqual(planned, result.Original) {
				t.Fatalf("native caller rule set changed for %q: %q -> %q", cases[i].Args, result.Original, result.Planned)
			}
		}
	}
	t.Logf("pinned native grammar: %d help arities, %d ownership and caller-rule comparisons", len(nativeArities), len(cases))
}

// TestPinnedClaudeNativeListAndSettingsComparison differentially compares the
// refusal-side native surfaces against the installed pinned release: the
// Fp list tokenizer over a Unicode corpus, the NI eager --settings scan over
// an ownership corpus, and the embedded eager skip sets against the binary's
// own sets. Node executes the pinned functions verbatim at test time; the
// archive carries no copied native bundle. Skips without pinned Claude 2.1.288
// or Node. No session, configuration or credential store is accessed.
func TestPinnedClaudeNativeListAndSettingsComparison(t *testing.T) {
	path, err := exec.LookPath(executableName)
	if err != nil {
		t.Skip("pinned Claude not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(version)) != "2.1.288 (Claude Code)" {
		t.Skipf("not the pinned release: %s", version)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable for pinned native comparison")
	}
	deny := `{"permissions":{"allow":["AskUserQuestion"]}}`
	niCases := [][]string{
		{"-cn", "--settings", deny},
		{"-pn", "--settings", deny},
		{"--settings={}", "-cn", "--settings=" + deny},
		{"--settings", deny},
		{"--settings=" + deny},
		{"--settings"},
		{"--settings="},
		{"--", "--settings", deny},
		{"--model", "--settings", deny},
		{"--model", "--settings=" + deny},
		{"--agent-id", "--settings", deny},
		{"-m", "--settings", deny},
		{"--remote-control", "--settings", deny},
		{"--remote-control", "v", "--settings", deny},
		{"--betas", "v", "--settings", deny},
		{"--betas", "--settings", deny},
		{"--betas", "a", "b", "--settings", deny},
		{"-c", "--settings", deny},
		{"-cn", "value"},
		{"--settings", "A", "--settings", "B"},
		{"--settings=A", "--settings", "B"},
		{"--settings", "--"},
		{"-d", "--settings", deny},
		{"-d", "v", "--settings", deny},
		{"-r", "v", "--settings", deny},
		{"--file", "a", "--settings", deny},
		{"--channels", "--settings", deny},
		{"--plugin-dir-no-mcp", "--settings", deny},
		{"--mcp-config", "a", "b", "--settings", deny},
		{"--settings==Y"},
		{"--settingsX=A"},
		{"-settings=X"},
		{"--settings", "A", "--settings"},
		{"-n", "v", "--settings", deny},
		{"-n--settings", deny},
		{"--model=X", "--settings", deny},
		{"-cn", "--settings=" + deny},
		{"--exec", "--settings", deny},
		{"--setting-sources", "a", "--settings", deny},
	}
	fpCases := [][]string{
		{"\ufeffAskUserQuestion"},
		{"\u0085AskUserQuestion"},
		{"Read,\ufeffAskUserQuestion"},
		{"\u00a0AskUserQuestion\u00a0"},
		{"\u2003AskUserQuestion"},
		{"\u1680AskUserQuestion"},
		{"\u2028AskUserQuestion"},
		{"\u2029AskUserQuestion"},
		{"\tAskUserQuestion\n"},
		{"AskUserQuestion", "\ufeffAskUserQuestion"},
		{""},
		{"   "},
		{"\ufeff"},
		{"\t"},
		{"Read,,Edit"},
		{"Read Edit"},
		{"Bash(git log --format=%H,%s)"},
		{"Bash("},
		{"AskUserQuestion(*)"},
		{"\ufeffAskUserQuestion(*)"},
		{"Read\tEdit"},
		{"a,b c"},
		{"(a,b) c"},
		{"a(b,c)d"},
		{"a)b,c"},
		{"((a))"},
		{"a(b"},
		{"a(b,c"},
		{"a)b(c,d"},
		{"\u0085"},
		{" \u0085 "},
		{"\ufeff \ufeff"},
		{"", "Read", ""},
		{"Read, Edit", "AskUserQuestion"},
		{"AskUserQuestion\ufeff", "\u00a0Read"},
	}
	input, err := json.Marshal(struct {
		Path    string
		NICases [][]string
		FpCases [][]string
	}{path, niCases, fpCases})
	if err != nil {
		t.Fatal(err)
	}
	// Anchors are release-specific: absence fails the pinned check, not a
	// fallback to an invented tokenizer or scanner.
	const script = `
const fs=require('fs');const input=JSON.parse(fs.readFileSync(0,'utf8'));
const bytes=fs.readFileSync(input.Path).toString('latin1');
function extract(start,end){const s=bytes.indexOf(start,170000000),e=bytes.indexOf(end,s);
if(s<0||e<0)throw Error('pinned native anchors missing: '+start);return bytes.slice(s,e)}
const Fp=new Function(extract('function Fp(','function p(')+';return Fp')();
const eager=new Function(extract('var i=new Set(["--prefill"','function SC(')+';return {NI:NI,sets:{i:[...i],xar:[...xar],a:[...a]}}')();
const ni=input.NICases.map(args=>eager.NI('--settings',args)??null);
const fp=input.FpCases.map(values=>Fp(values));
process.stdout.write(JSON.stringify({sets:eager.sets,ni:ni,fp:fp}));`
	command := exec.CommandContext(ctx, node, "-e", script)
	command.Stdin = strings.NewReader(string(input))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pinned list/settings comparison: %v: %s", err, output)
	}
	var result struct {
		Sets struct {
			I   []string
			Xar []string
			A   []string
		}
		NI []*string
		Fp [][]string
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	// The embedded eager table must transcribe the binary's sets exactly, in
	// both directions: every embedded row in its native set, and no native
	// member missing from the table.
	nativeScalar := map[string]bool{}
	nativeVariadic := map[string]bool{}
	nativeOptional := map[string]bool{}
	for _, name := range result.Sets.I {
		nativeScalar[name] = true
	}
	for _, name := range result.Sets.Xar {
		nativeVariadic[name] = true
	}
	for _, name := range result.Sets.A {
		nativeOptional[name] = true
	}
	if len(claudeEagerArity) != len(nativeScalar)+len(nativeOptional) {
		t.Fatalf("embedded eager table has %d rows, native sets have %d scalar/variadic plus %d optional",
			len(claudeEagerArity), len(nativeScalar), len(nativeOptional))
	}
	for name, arity := range claudeEagerArity {
		switch arity {
		case eagerVariadic:
			if !nativeScalar[name] || !nativeVariadic[name] {
				t.Fatalf("embedded eager row %s=variadic is not variadic in the pinned sets", name)
			}
		case eagerScalar:
			if !nativeScalar[name] || nativeVariadic[name] {
				t.Fatalf("embedded eager row %s=scalar is not scalar in the pinned sets", name)
			}
		case eagerOptional:
			if !nativeOptional[name] {
				t.Fatalf("embedded eager row %s=optional is not optional in the pinned sets", name)
			}
		default:
			t.Fatalf("embedded eager row %s has unknown class", name)
		}
	}
	seen := map[string]bool{}
	for name := range claudeEagerArity {
		seen[name] = true
	}
	for _, name := range append(append(append([]string{}, result.Sets.I...), result.Sets.Xar...), result.Sets.A...) {
		if !seen[name] {
			t.Fatalf("pinned eager set member %s is missing from the embedded table", name)
		}
	}
	if len(result.NI) != len(niCases) || len(result.Fp) != len(fpCases) {
		t.Fatalf("native comparison returned %d NI and %d Fp results for %d and %d cases",
			len(result.NI), len(result.Fp), len(niCases), len(fpCases))
	}
	for i, args := range niCases {
		value, present := lastSettingsValue(eagerSettingsValues(args))
		native := result.NI[i]
		if present != (native != nil) || (present && value != *native) {
			got, want := "<absent>", "<absent>"
			if present {
				got = value
			}
			if native != nil {
				want = *native
			}
			t.Fatalf("eager settings selection differs for %q: module %q, native %q", args, got, want)
		}
	}
	for i, values := range fpCases {
		var got []string
		for _, value := range values {
			got = append(got, splitToolList(value)...)
		}
		if got == nil {
			got = []string{}
		}
		want := result.Fp[i]
		if want == nil {
			want = []string{}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("rule tokenization differs for %q: module %q, native %q", values, got, want)
		}
	}
	t.Logf("pinned native list/settings: %d eager members, %d NI and %d Fp differential vectors",
		len(claudeEagerArity), len(niCases), len(fpCases))
}
