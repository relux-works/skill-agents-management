// Command launchcontext-mutants narrows the Curator launch-context gates in
// isolated source copies and requires each named BuildPlan behavior test to go
// red. It never edits the working module.
package main

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/relux-works/skill-agents-management/internal/refusalscan"
)

type replacement struct {
	file   string
	before string
	after  string
}

type downstreamGuardProof struct {
	file        string
	function    string
	guard       string
	line        int
	returned    string
	exemption   string
	description string
}

type mutant struct {
	name              string
	gate              string
	member            string
	narrows           string
	file              string
	replacements      []replacement
	astFunction       string
	astGuard          string
	astSiteLine       int
	astSiteReturn     string
	astMutationKind   string
	validatorMemberID string
	exemption         string
	testPackage       string
	testName          string
	runPattern        string
	failureText       string
	preflightPkg      string
	preflightName     string
	preflightMatch    string
	downstreamProofs  []downstreamGuardProof
	requiredPlugins   []string
}

func main() {
	tableOutput := flag.String("table-out", "", "write the killed mutant table to this Markdown path")
	start := flag.Int("start", 0, "zero-based candidate offset for bounded sequential runs")
	limit := flag.Int("limit", 0, "maximum candidates to execute; zero means all remaining candidates")
	selectedName := flag.String("name", "", "execute one registered mutant by exact name")
	validatorsOnly := flag.Bool("validators-only", false, "execute only AST-derived curator validator members")
	flag.Parse()
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	taskScratch := filepath.Join(root, ".temp", "launch-context-mutants")
	if err := os.MkdirAll(taskScratch, 0o700); err != nil {
		fatal(err)
	}
	temporary, err := os.MkdirTemp(taskScratch, "launchcontext-mutants-")
	if err != nil {
		fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(temporary); err != nil {
			fatal(fmt.Errorf("remove isolated mutant tree: %w", err))
		}
	}()
	if err := copyTree(root, temporary); err != nil {
		fatal(err)
	}
	gitDir, err := commandOutput(root, "git", "rev-parse", "--absolute-git-dir")
	if err != nil {
		fatal(fmt.Errorf("locate worktree git directory: %w", err))
	}
	gitIndex, err := commandOutput(root, "git", "rev-parse", "--git-path", "index")
	if err != nil {
		fatal(fmt.Errorf("locate worktree git index: %w", err))
	}

	candidates := append(narrowingMutants(), hostedResumeMutants()...)
	fmt.Println("ACCOUNTING | a validator member counts as killed only when each plugin fails a named BuildPlan subtest in its own go test process | managed-home homeVariable mutants additionally spot-check both plugin failures | matrix restriction check is best-effort; t.Skip and aliased plugin conditions are blind spots (TASK-260930-3txv44)")
	generated, err := generatedCuratorConflictMutants(root)
	if err != nil {
		fatal(err)
	}
	candidates = append(candidates, generated...)
	var validatorMutants []mutant
	matched := false
	for _, candidate := range candidates {
		if candidate.name == *selectedName {
			matched = true
		}
	}
	if *selectedName == "" || !matched || *validatorsOnly {
		validatorMutants, err = generatedCuratorValidatorMutants(root, taskScratch, gitDir, gitIndex)
		if err != nil {
			fatal(err)
		}
		candidates = append(candidates, validatorMutants...)
	}
	if *validatorsOnly {
		candidates = validatorMutants
	}
	if *selectedName != "" {
		var selected []mutant
		for _, candidate := range candidates {
			if candidate.name == *selectedName {
				selected = append(selected, candidate)
			}
		}
		if len(selected) != 1 {
			fatal(fmt.Errorf("mutant name %q resolves to %d members", *selectedName, len(selected)))
		}
		candidates = selected
	}
	if *start < 0 || *start > len(candidates) || *limit < 0 {
		fatal(fmt.Errorf("invalid candidate range start=%d limit=%d for %d candidates", *start, *limit, len(candidates)))
	}
	end := len(candidates)
	if *limit > 0 && *start+*limit < end {
		end = *start + *limit
	}
	fmt.Printf("CANDIDATE_PART | total=%d | start=%d | end=%d | count=%d\n", len(candidates), *start, end, end-*start)
	candidates = candidates[*start:end]
	originals := map[string][]byte{}
	killed := 0
	equivalent := 0
	var killedRows []string
	for _, candidate := range candidates {
		if candidate.astFunction != "" {
			if _, found := originals[candidate.file]; !found {
				body, err := os.ReadFile(filepath.Join(temporary, candidate.file))
				if err != nil {
					fatal(err)
				}
				originals[candidate.file] = body
			}
		}
		for _, change := range candidate.replacements {
			sourceFile := change.file
			if sourceFile == "" {
				sourceFile = candidate.file
			}
			if _, found := originals[sourceFile]; found {
				continue
			}
			body, err := os.ReadFile(filepath.Join(temporary, sourceFile))
			if err != nil {
				fatal(err)
			}
			originals[sourceFile] = body
		}
	}

	for _, candidate := range candidates {
		for file, body := range originals {
			if err := os.WriteFile(filepath.Join(temporary, file), body, 0o600); err != nil {
				fatal(err)
			}
		}
		if candidate.astFunction != "" {
			path := filepath.Join(temporary, candidate.file)
			var err error
			switch candidate.astMutationKind {
			case "bool-return-allow":
				err = applyASTBooleanReturnMutation(path, candidate.astFunction, candidate.astSiteLine, candidate.astSiteReturn, candidate.exemption, true)
			case "bool-return-exempt":
				err = applyASTBooleanReturnMutation(path, candidate.astFunction, candidate.astSiteLine, candidate.astSiteReturn, candidate.exemption, false)
			case "":
				if candidate.astSiteLine != 0 {
					err = applyASTNarrowingAtSite(path, candidate.astFunction, candidate.astGuard, candidate.astSiteLine, candidate.astSiteReturn, candidate.exemption)
				} else {
					err = applyASTNarrowing(path, candidate.astFunction, candidate.astGuard, candidate.exemption)
				}
			default:
				err = fmt.Errorf("unknown AST mutation kind %q", candidate.astMutationKind)
			}
			if err != nil {
				fatal(fmt.Errorf("apply generated mutant %q: %w", candidate.name, err))
			}
		} else {
			for _, change := range candidate.replacements {
				sourceFile := change.file
				if sourceFile == "" {
					sourceFile = candidate.file
				}
				path := filepath.Join(temporary, sourceFile)
				body, err := os.ReadFile(path)
				if err != nil {
					fatal(err)
				}
				mutated := string(body)
				if count := strings.Count(mutated, change.before); count != 1 {
					fatal(fmt.Errorf("mutant %q expected exactly one source site in %s, found %d", candidate.name, sourceFile, count))
				}
				mutated = strings.Replace(mutated, change.before, change.after, 1)
				if err := os.WriteFile(path, []byte(mutated), 0o600); err != nil {
					fatal(err)
				}
			}
		}

		if candidate.preflightPkg != "" {
			output, exitCode := runGoTest(temporary, root, gitDir, gitIndex, candidate.preflightPkg, candidate.preflightMatch)
			if exitCode != 0 {
				fatal(fmt.Errorf("mutant %q broke source guard preflight (exit %d):\n%s", candidate.name, exitCode, output))
			}
			fmt.Printf("PREFLIGHT GREEN | exit=0 | %s | test=%s\n", candidate.name, candidate.preflightName)
		}

		output, exitCode := runCandidateTests(temporary, root, gitDir, gitIndex, candidate)
		if err := validateMutantNamedTestExecution(candidate, output); err != nil {
			fatal(err)
		}
		if exitCode == 0 || candidate.validatorMemberID != "" && validatePerPluginKills(candidate, output) != nil {
			if candidate.validatorMemberID != "" {
				proof, proofOutput, proofErr := proveValidatorEquivalentMutant(temporary, root, gitDir, gitIndex, candidate)
				if proofErr != nil {
					fatal(fmt.Errorf("prove validator mutant %q against a mechanically disabled downstream guard: %w", candidate.name, proofErr))
				}
				if proof != nil {
					namedTest := mutantFailureTestName(candidate, proofOutput)
					if namedTest == "" || candidate.failureText != "" && !strings.Contains(proofOutput, candidate.failureText) {
						fatal(fmt.Errorf("equivalence proof for %q did not fail its named BuildPlan negative test %q:\n%s", candidate.name, candidate.testName, proofOutput))
					}
					gate := candidate.gate
					if gate == "" {
						gate = candidate.file
					}
					member := candidate.member
					if member == "" {
						member = candidate.narrows
					}
					description := candidate.narrows + "; equivalent only with downstream guard disabled: " + proof.description
					fmt.Printf("EQUIVALENT PROVED | exit=1 | gate=%s | member=%s | mutant=%s | downstream=%s | test=%s\n", gate, member, candidate.name, proof.description, namedTest)
					killedRows = append(killedRows, fmt.Sprintf("| equivalent | %s | %s | %s | %s | %s |", markdownCell(gate), markdownCell(member), markdownCell(candidate.name), markdownCell(description), markdownCell(namedTest)))
					equivalent++
					continue
				}
			}
			fmt.Fprintf(os.Stderr, "GENERATOR ERROR: narrowing witness was not killed by a named BuildPlan negative test | gate=%s | member=%s | mutant=%s | target=%s:%d guard=%s exemption=%s | narrows=%s | test=%s\n%s", candidate.gate, candidate.member, candidate.name, candidate.astFunction, candidate.astSiteLine, candidate.astGuard, candidate.exemption, candidate.narrows, candidate.testName, output)
			os.Exit(1)
		}
		if !strings.Contains(output, "--- FAIL: "+candidate.testName) {
			fatal(fmt.Errorf("mutant %q failed without named test %q (exit %d):\n%s", candidate.name, candidate.testName, exitCode, output))
		}
		if candidate.failureText != "" && !strings.Contains(output, candidate.failureText) {
			fatal(fmt.Errorf("mutant %q did not fail for its targeted assertion %q (exit %d):\n%s", candidate.name, candidate.failureText, exitCode, output))
		}
		gate := candidate.gate
		if gate == "" {
			gate = candidate.file
		}
		member := candidate.member
		if member == "" {
			member = candidate.narrows
		}
		namedTest := candidate.testName
		if candidate.validatorMemberID != "" {
			failedNames := mutantFailureTestNames(candidate, output)
			if len(failedNames) == 0 {
				fatal(fmt.Errorf("mutant %q failed without a named BuildPlan negative subtest (%s)", candidate.name, candidate.runPattern))
			}
			if err := validatePerPluginKills(candidate, output); err != nil {
				fatal(err)
			}
			namedTest = strings.Join(failedNames, ", ")
		}
		fmt.Printf("KILLED | exit=%d | gate=%s | member=%s | mutant=%s | test=%s\n", exitCode, gate, member, candidate.name, namedTest)
		killedRows = append(killedRows, fmt.Sprintf("| killed | %s | %s | %s | %s | %s |", markdownCell(gate), markdownCell(member), markdownCell(candidate.name), markdownCell(candidate.narrows), markdownCell(namedTest)))
		killed++
	}
	fmt.Printf("SUMMARY | mutants=%d | killed=%d | equivalent=%d | survived=0\n", len(candidates), killed, equivalent)
	if *tableOutput != "" {
		content := "<!-- Accounting: a validator member counts as killed only when each plugin fails a named BuildPlan subtest in its own go test process. Managed-home homeVariable mutants additionally spot-check both plugin failures. The curator validator matrix restriction check is best-effort; t.Skip and aliased plugin conditions are blind spots (TASK-260930-3txv44). -->\n\n| Result | Gate | Member | Mutant | Narrowing / downstream guard | Named test |\n| --- | --- | --- | --- | --- | --- |\n" + strings.Join(killedRows, "\n") + "\n"
		if err := os.MkdirAll(filepath.Dir(*tableOutput), 0o700); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*tableOutput, []byte(content), 0o600); err != nil {
			fatal(fmt.Errorf("write mutation table: %w", err))
		}
	}
}

func validateMutantNamedTestExecution(candidate mutant, output string) error {
	if candidate.runPattern != "" && strings.Contains(output, "[no tests to run]") {
		return fmt.Errorf("completeness check: mutant %q selected no named BuildPlan negative test (%s)", candidate.name, candidate.runPattern)
	}
	if candidate.validatorMemberID != "" {
		selected := false
		for _, line := range strings.Split(output, "\n") {
			if !strings.HasPrefix(line, "=== RUN   ") {
				continue
			}
			matched, err := mutantRunPatternMatches(candidate.runPattern, strings.TrimPrefix(line, "=== RUN   "))
			if err != nil {
				return fmt.Errorf("completeness check: mutant %q has invalid named test pattern: %w", candidate.name, err)
			}
			if matched {
				selected = true
				break
			}
		}
		if !selected {
			return fmt.Errorf("completeness check: mutant %q selected no named BuildPlan negative subtest (%s)", candidate.name, candidate.runPattern)
		}
	}
	return nil
}

// proveValidatorEquivalentMutant accepts a surviving narrowing only when a
// second AST-located boolean guard in a direct caller is mechanically narrowed
// for the same witness and the same named BuildPlan test then fails.
func proveValidatorEquivalentMutant(temporary, sourceRoot, gitDir, gitIndex string, candidate mutant) (*downstreamGuardProof, string, error) {
	if len(candidate.downstreamProofs) == 0 {
		return nil, "", nil
	}
	if candidate.file == "" {
		return nil, "", fmt.Errorf("validator mutant %q has no source file for an independent downstream-guard check", candidate.name)
	}
	primarySource, err := os.ReadFile(filepath.Join(sourceRoot, candidate.file))
	if err != nil {
		return nil, "", fmt.Errorf("read unmutated validator source %s: %w", candidate.file, err)
	}
	originals := make(map[string][]byte)
	primaryMutantSource, err := os.ReadFile(filepath.Join(temporary, candidate.file))
	if err != nil {
		return nil, "", fmt.Errorf("read narrowed validator source %s: %w", candidate.file, err)
	}
	originals[candidate.file] = primaryMutantSource
	for _, proof := range candidate.downstreamProofs {
		if _, found := originals[proof.file]; found {
			continue
		}
		body, err := os.ReadFile(filepath.Join(temporary, proof.file))
		if err != nil {
			return nil, "", fmt.Errorf("read downstream proof source %s: %w", proof.file, err)
		}
		originals[proof.file] = body
	}
	defer func() {
		for file, body := range originals {
			_ = os.WriteFile(filepath.Join(temporary, file), body, 0o600)
		}
	}()

	for _, proof := range candidate.downstreamProofs {
		for file, body := range originals {
			if err := os.WriteFile(filepath.Join(temporary, file), body, 0o600); err != nil {
				return nil, "", fmt.Errorf("restore downstream proof source %s: %w", file, err)
			}
		}
		// First run the claimed guard mutation by itself. A guard that already
		// makes the named test fail is not evidence that it masks the primary
		// mutant; only a green solo run followed by a red combined run proves
		// that dependency.
		if err := os.WriteFile(filepath.Join(temporary, candidate.file), primarySource, 0o600); err != nil {
			return nil, "", fmt.Errorf("restore primary source for independent downstream check %s: %w", candidate.file, err)
		}
		path := filepath.Join(temporary, proof.file)
		if err := applyDownstreamNarrowing(path, proof); err != nil {
			fmt.Printf("EQUIVALENCE CANDIDATE REJECTED | mutant=%s | downstream=%s | reason=AST mutation did not resolve: %v\n", candidate.name, proof.description, err)
			continue
		}
		independentOutput, independentExit := runCandidateTests(temporary, sourceRoot, gitDir, gitIndex, candidate)
		if validateErr := validateMutantNamedTestExecution(candidate, independentOutput); validateErr != nil {
			fmt.Printf("EQUIVALENCE CANDIDATE REJECTED | mutant=%s | downstream=%s | reason=independent guard run: %s\n", candidate.name, proof.description, strings.ReplaceAll(validateErr.Error(), "\n", " "))
			continue
		}
		if independentExit != 0 || strings.Contains(independentOutput, "panic:") || strings.Contains(independentOutput, "runtime error:") {
			fmt.Printf("EQUIVALENCE CANDIDATE REJECTED | mutant=%s | downstream=%s | reason=downstream guard alone does not preserve the named test (exit=%d)\n", candidate.name, proof.description, independentExit)
			continue
		}
		for file, body := range originals {
			if err := os.WriteFile(filepath.Join(temporary, file), body, 0o600); err != nil {
				return nil, "", fmt.Errorf("restore primary mutant source %s: %w", file, err)
			}
		}
		if err := applyDownstreamNarrowing(path, proof); err != nil {
			fmt.Printf("EQUIVALENCE CANDIDATE REJECTED | mutant=%s | downstream=%s | reason=combined AST mutation did not resolve: %v\n", candidate.name, proof.description, err)
			continue
		}
		output, exitCode := runCandidateTests(temporary, sourceRoot, gitDir, gitIndex, candidate)
		if validateErr := validateMutantNamedTestExecution(candidate, output); validateErr != nil {
			fmt.Printf("EQUIVALENCE CANDIDATE REJECTED | mutant=%s | downstream=%s | reason=%s\n", candidate.name, proof.description, strings.ReplaceAll(validateErr.Error(), "\n", " "))
			continue
		}
		if strings.Contains(output, "panic:") || strings.Contains(output, "runtime error:") {
			fmt.Printf("EQUIVALENCE CANDIDATE REJECTED | mutant=%s | downstream=%s | reason=secondary mutation caused a runtime panic\n", candidate.name, proof.description)
			continue
		}
		if downstreamMutationProvesEquivalence(independentExit, exitCode) && mutantFailureTestName(candidate, output) != "" &&
			(candidate.failureText == "" || strings.Contains(output, candidate.failureText)) && validatePerPluginKills(candidate, output) == nil {
			return &proof, output, nil
		}
	}
	return nil, "", nil
}

func downstreamMutationProvesEquivalence(independentExit, combinedExit int) bool {
	return independentExit == 0 && combinedExit != 0
}

func mutantRunPatternMatches(runPattern, testPath string) (bool, error) {
	patterns := strings.Split(runPattern, "/")
	parts := strings.Split(testPath, "/")
	if len(parts) < len(patterns) {
		return false, nil
	}
	for index, part := range parts {
		pattern := ".*"
		if index < len(patterns) {
			pattern = patterns[index]
		}
		matcher, err := regexp.Compile(pattern)
		if err != nil {
			return false, err
		}
		if !matcher.MatchString(part) {
			return false, nil
		}
	}
	return true, nil
}

func mutantFailureTestName(candidate mutant, output string) string {
	names := mutantFailureTestNames(candidate, output)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func mutantFailureTestNames(candidate mutant, output string) []string {
	var names []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(line, "--- FAIL: ") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "--- FAIL: "))
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if candidate.validatorMemberID != "" {
			matched, err := mutantRunPatternMatches(candidate.runPattern, name)
			if err == nil && matched {
				names = append(names, name)
			}
		} else if strings.HasPrefix(name, candidate.testName) {
			return []string{name}
		}
	}
	return names
}

func markdownCell(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}

// runCandidateTests isolates each validator plugin in a separate process.
// A nonzero aggregate is not a kill: the caller also requires named failures
// for every plugin, so an unexecuted or surviving plugin fails closed.
func runCandidateTests(root, sourceRoot, gitDir, gitIndex string, candidate mutant) (string, int) {
	if candidate.validatorMemberID == "" {
		output, code := runGoTestMode(root, sourceRoot, gitDir, gitIndex, candidate.testPackage, candidate.runPattern, candidate.runPattern != "")
		evidenceRoot := filepath.Join(sourceRoot, ".temp", "launch-context-mutants", "process-evidence")
		if err := os.MkdirAll(evidenceRoot, 0700); err != nil {
			fatal(err)
		}
		evidence, err := os.CreateTemp(evidenceRoot, processEvidencePrefix(candidate.name)+"-*.log")
		if err != nil {
			fatal(err)
		}
		record := fmt.Sprintf("MUTANT_PROCESS | mutant=%s | exit=%d | package=%s | pattern=%s\n", candidate.name, code, candidate.testPackage, candidate.runPattern)
		if _, err := evidence.WriteString(record + output); err != nil {
			fatal(err)
		}
		if err := evidence.Close(); err != nil {
			fatal(err)
		}
		fmt.Printf("%sPROCESS_LOG | %s\n", record, evidence.Name())
		return output, code
	}
	var output strings.Builder
	exit := 0
	for _, plugin := range candidate.requiredPlugins {
		pattern, err := validatorPluginPattern(candidate.runPattern, plugin)
		if err != nil {
			fatal(err)
		}
		text, code := runGoTestMode(root, sourceRoot, gitDir, gitIndex, candidate.testPackage, pattern, true)
		record := fmt.Sprintf("PLUGIN_PROCESS | mutant=%s | plugin=%s | exit=%d | pattern=%s\n", candidate.name, plugin, code, pattern)
		fmt.Fprintf(&output, "%s%s", record, text)
		evidenceRoot := filepath.Join(sourceRoot, ".temp", "launch-context-mutants", "process-evidence")
		if err := os.MkdirAll(evidenceRoot, 0700); err != nil {
			fatal(err)
		}
		evidence, err := os.CreateTemp(evidenceRoot, processEvidencePrefix(candidate.name)+"-"+plugin+"-*.log")
		if err != nil {
			fatal(err)
		}
		if _, err := evidence.WriteString(record + text); err != nil {
			fatal(err)
		}
		if err := evidence.Close(); err != nil {
			fatal(err)
		}
		singlePattern := candidate
		singlePattern.runPattern = pattern
		fmt.Printf("%sPLUGIN_FAILURES | %s | %s | names=%s | log=%s\n", record, candidate.name, plugin, strings.Join(mutantFailureTestNames(singlePattern, text), ","), filepath.Base(evidence.Name()))
		single := candidate
		single.runPattern = pattern
		if err := validateMutantNamedTestExecution(single, text); err != nil {
			fatal(err)
		}
		if code != 0 {
			exit = code
		}
	}
	if len(candidate.requiredPlugins) == 0 {
		fatal(fmt.Errorf("validator %q has no required plugins", candidate.name))
	}
	return output.String(), exit
}

func validatorPluginPattern(pattern, plugin string) (string, error) {
	parts := strings.Split(pattern, "/")
	if len(parts) == 1 {
		parts = append(parts, "^(claude|codex)$")
	}
	if len(parts) < 2 {
		return "", fmt.Errorf("validator pattern has no plugin segment: %q", pattern)
	}
	matched, err := regexp.MatchString(parts[1], plugin)
	if err != nil || !matched {
		return "", fmt.Errorf("validator pattern %q excludes plugin %q", pattern, plugin)
	}
	parts[1] = "^" + regexp.QuoteMeta(plugin) + "$"
	return strings.Join(parts, "/"), nil
}

func runGoTest(root, sourceRoot, gitDir, gitIndex, packageName, testPattern string) (string, int) {
	return runGoTestMode(root, sourceRoot, gitDir, gitIndex, packageName, testPattern, false)
}

func runGoTestMode(root, sourceRoot, gitDir, gitIndex, packageName, testPattern string, verbose bool) (string, int) {
	arguments := []string{"test", "-mod=mod"}
	if verbose {
		arguments = append(arguments, "-v")
	}
	arguments = append(arguments, packageName, "-count=1", "-run", testPattern)
	command := exec.Command("go", arguments...)
	command.Dir = root
	command.Env = isolatedGoEnv(os.Environ(), sourceRoot, root, gitDir, gitIndex)
	output, err := command.CombinedOutput()
	if err == nil {
		return string(output), 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return string(output), exitError.ExitCode()
	}
	fatal(fmt.Errorf("go test could not start: %w\n%s", err, output))
	return "", -1
}

func isolatedGoEnv(environment []string, sourceRoot, workTree, gitDir, gitIndex string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, value := range environment {
		if strings.HasPrefix(value, "TASK_BOARD_DIR=") {
			continue
		}
		if strings.HasPrefix(value, "GOWORK=") {
			continue
		}
		if strings.HasPrefix(value, "GIT_DIR=") || strings.HasPrefix(value, "GIT_WORK_TREE=") || strings.HasPrefix(value, "GIT_INDEX_FILE=") {
			continue
		}
		result = append(result, value)
	}
	return append(result, "GOWORK=off", "GIT_DIR="+gitDir, "GIT_WORK_TREE="+workTree, "GIT_INDEX_FILE="+gitIndex)
}

func commandOutput(directory, command string, arguments ...string) (string, error) {
	process := exec.Command(command, arguments...)
	process.Dir = directory
	output, err := process.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", command, strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && excludedDirectory(relative) {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() && !entry.IsDir() {
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, body, info.Mode().Perm())
	})
}

func excludedDirectory(path string) bool {
	first, _, _ := strings.Cut(filepath.ToSlash(path), "/")
	switch first {
	case ".git", ".temp", ".task-board", "node_modules", "vendor", "DerivedData":
		return true
	default:
		return false
	}
}

type catalogCoverage struct {
	testName      string
	generated     bool
	outOfContract string
}

type catalogMutation struct {
	name          string
	file          string
	function      string
	guard         string
	returned      string
	occurrence    int
	hasReturned   bool
	hasOccurrence bool
	member        string
	exemption     string
	testName      string
	runPattern    string
	failureText   string
	narrows       string
}

func generatedCuratorConflictMutants(root string) ([]mutant, error) {
	catalogPath := filepath.Join(root, "pkg", "agentic", "curator_refusal_guard_test.go")
	fileSet := token.NewFileSet()
	catalogFile, err := parser.ParseFile(fileSet, catalogPath, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse Curator refusal catalog: %w", err)
	}
	coverageRows, err := parseCatalogSlice(catalogFile, "curatorRefusalCoverageTable")
	if err != nil {
		return nil, err
	}
	sites, err := refusalscan.Discover(root)
	if err != nil {
		return nil, fmt.Errorf("derive refusal sites from the full agentic source tree: %w", err)
	}
	coverage := make(map[string]catalogCoverage, len(coverageRows))
	for _, row := range coverageRows {
		file, err := requiredString(row, "file")
		if err != nil {
			return nil, err
		}
		function, err := requiredString(row, "function")
		if err != nil {
			return nil, err
		}
		guard, err := requiredString(row, "guard")
		if err != nil {
			return nil, err
		}
		returned, err := requiredString(row, "returned")
		if err != nil {
			return nil, err
		}
		testName := row["testName"]
		outOfContract := row["outOfContract"]
		occurrence, err := strconv.Atoi(row["occurrence"])
		if err != nil {
			return nil, fmt.Errorf("parse occurrence for %s :: %s: %w", file, function, err)
		}
		key := catalogSiteKey(file, function, guard, returned, occurrence)
		if testName == "" && outOfContract == "" {
			return nil, fmt.Errorf("coverage row for %s has neither a BuildPlan test nor an out-of-contract clause", key)
		}
		if !containsSiteKey(sites, key) {
			return nil, fmt.Errorf("coverage row does not resolve to a refusal site in the full source enumeration: %s", key)
		}
		if _, exists := coverage[key]; exists {
			return nil, fmt.Errorf("duplicate Curator refusal coverage row %q", key)
		}
		coverage[key] = catalogCoverage{testName: testName, generated: row["generated"] == "true", outOfContract: outOfContract}
	}
	for _, row := range refusalscan.HostedResumeCoverage() {
		key := row.Site.Key()
		if _, exists := coverage[key]; exists {
			return nil, fmt.Errorf("duplicate hosted refusal row %s", key)
		}
		coverage[key] = catalogCoverage{testName: row.TestName}
	}
	for _, site := range sites {
		if _, ok := coverage[site.Key()]; !ok {
			return nil, fmt.Errorf("unmapped typed refusal return in the full package-tree mutant catalog: %s", site.Key())
		}
	}
	for key := range coverage {
		if !containsSiteKey(sites, key) {
			return nil, fmt.Errorf("stale refusal coverage row against the full source enumeration: %s", key)
		}
	}

	mutationRows, err := parseCatalogSlice(catalogFile, "curatorConflictMutationMembers")
	if err != nil {
		return nil, err
	}
	result := make([]mutant, 0, len(mutationRows))
	seenNames := make(map[string]bool, len(mutationRows))
	memberCount := make(map[string]int)
	for _, row := range mutationRows {
		mutation, err := parseCatalogMutation(row)
		if err != nil {
			return nil, err
		}
		site, err := resolveCatalogSite(mutation, sites)
		if err != nil {
			return nil, fmt.Errorf("generated mutant %q: %w", mutation.name, err)
		}
		key := site.Key()
		coverageRow, ok := coverage[key]
		if !ok || !coverageRow.generated {
			return nil, fmt.Errorf("generated mutant %q does not map to a generated refusal site: %s", mutation.name, key)
		}
		if strings.SplitN(mutation.testName, "/", 2)[0] != coverageRow.testName {
			return nil, fmt.Errorf("generated mutant %q names test %q outside mapped test %q", mutation.name, mutation.testName, coverageRow.testName)
		}
		if seenNames[mutation.name] {
			return nil, fmt.Errorf("duplicate generated mutant name %q", mutation.name)
		}
		seenNames[mutation.name] = true
		memberCount[key]++
		result = append(result, mutant{
			name: mutation.name, gate: site.Locator(), member: mutation.member, narrows: mutation.narrows,
			file: filepath.Join("pkg", "agentic", site.File), astFunction: site.Function,
			astGuard: site.Guard, exemption: mutation.exemption, testPackage: "./pkg/agentic",
			testName: mutation.testName, runPattern: mutation.runPattern, failureText: mutation.failureText,
		})
	}
	for key, row := range coverage {
		if row.generated && memberCount[key] == 0 {
			return nil, fmt.Errorf("generated Curator refusal site has no narrowing mutant members: %s", key)
		}
	}
	return result, nil
}

type curatorValidatorClassMember struct {
	identity         string
	siteKey          string
	testName         string
	subtestPattern   string
	targetFile       string
	targetFunction   string
	targetGuard      string
	targetLine       int
	targetReturn     string
	mutationKind     string
	exemption        string
	member           string
	narrows          string
	downstreamProofs []downstreamGuardProof
}

type parsedSource struct {
	fset *token.FileSet
	file *ast.File
}

func parseSource(root, relative string) (*parsedSource, error) {
	fset := token.NewFileSet()
	path := filepath.Join(root, relative)
	parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse source %s: %w", relative, err)
	}
	return &parsedSource{fset: fset, file: parsed}, nil
}

func callName(call *ast.CallExpr) string {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		return function.Name
	case *ast.SelectorExpr:
		return function.Sel.Name
	default:
		return ""
	}
}

func generatedCuratorValidatorMutants(root, taskScratch, gitDir, gitIndex string) ([]mutant, error) {
	sites, err := refusalscan.Discover(root)
	if err != nil {
		return nil, fmt.Errorf("enumerate the complete agentic source tree for curator validator mutants: %w", err)
	}
	var validatorSites []refusalscan.Site
	for _, site := range sites {
		if site.File == "curator_context.go" {
			validatorSites = append(validatorSites, site)
		}
	}
	if len(validatorSites) == 0 {
		return nil, fmt.Errorf("the hermetic source enumeration found no curator_context.go refusal sites")
	}

	catalogPath := filepath.Join(root, "pkg", "agentic", "curator_refusal_guard_test.go")
	catalog, err := parser.ParseFile(token.NewFileSet(), catalogPath, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse curator validator coverage catalog: %w", err)
	}
	rows, err := parseCatalogSlice(catalog, "curatorRefusalCoverageTable")
	if err != nil {
		return nil, err
	}
	coverage := make(map[string]string)
	for _, row := range rows {
		if row["file"] != "curator_context.go" {
			continue
		}
		testName := row["testName"]
		if testName == "" {
			return nil, fmt.Errorf("curator validator coverage row has no named BuildPlan test: %s :: %s", row["function"], row["guard"])
		}
		occurrence, err := strconv.Atoi(row["occurrence"])
		if err != nil {
			return nil, fmt.Errorf("parse curator validator occurrence for %s :: %s: %w", row["function"], row["guard"], err)
		}
		key := catalogSiteKey(row["file"], row["function"], row["guard"], row["returned"], occurrence)
		if _, exists := coverage[key]; exists {
			return nil, fmt.Errorf("duplicate curator validator mapping %s", key)
		}
		coverage[key] = testName
	}
	for _, site := range validatorSites {
		if _, ok := coverage[site.Key()]; !ok {
			return nil, fmt.Errorf("completeness check: curator validator site has no mapped BuildPlan negative test: %s", site.Key())
		}
	}
	for key := range coverage {
		found := false
		for _, site := range validatorSites {
			if site.Key() == key {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("stale curator validator mapping against the hermetic source enumeration: %s", key)
		}
	}
	if err := proveUnmappedSourceFileAttack(root, taskScratch, gitDir, gitIndex); err != nil {
		return nil, err
	}
	availableTests, err := curatorBuildPlanTestFunctions(root)
	if err != nil {
		return nil, err
	}
	parsedFiles, err := curatorValidatorPackageSources(root)
	if err != nil {
		return nil, err
	}
	functions := curatorValidatorFunctions(parsedFiles)
	members, err := deriveCuratorValidatorClassMembers(root, validatorSites, coverage, functions, parsedFiles)
	if err != nil {
		return nil, err
	}
	pluginNames, err := curatorValidatorPluginNames(root)
	if err != nil {
		return nil, err
	}
	result := make([]mutant, 0, len(members))
	for index, member := range members {
		result = append(result, mutant{
			name: fmt.Sprintf("curator-validator-%04d-%s", index+1, slugName(member.member)),
			gate: strings.TrimSuffix(member.siteKey, " :: "+member.targetReturn), member: member.member,
			narrows: member.narrows, file: member.targetFile,
			astFunction: member.targetFunction, astGuard: member.targetGuard,
			astSiteLine: member.targetLine, astSiteReturn: member.targetReturn,
			astMutationKind: member.mutationKind, validatorMemberID: member.identity,
			exemption: member.exemption, testPackage: "./pkg/agentic",
			testName: member.testName, runPattern: curatorValidatorRunPattern(member),
			failureText: "BuildPlan", downstreamProofs: member.downstreamProofs,
			requiredPlugins: append([]string(nil), pluginNames...),
		})
	}
	if err := validateValidatorMutantCompleteness(members, result, availableTests); err != nil {
		return nil, err
	}
	fmt.Printf("CURATOR_ENUMERATED | refusal_sites_tree=%d | refusal_sites_validator=%d | ast_derived_members=%d | validator_mutants=%d\n", len(sites), len(validatorSites), len(members), len(result))
	return result, nil
}

func curatorValidatorPluginNames(root string) ([]string, error) {
	path := filepath.Join(root, "pkg", "agentic", "curator_context_acceptance_test.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse Curator BuildPlan plugin matrix: %w", err)
	}
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.VAR {
			continue
		}
		for _, specification := range group.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				if name.Name != "curatorPlugins" || index >= len(value.Values) {
					continue
				}
				plugins, ok := value.Values[index].(*ast.CompositeLit)
				if !ok {
					return nil, fmt.Errorf("curatorPlugins is not a composite literal")
				}
				var names []string
				for _, element := range plugins.Elts {
					plugin, ok := element.(*ast.CompositeLit)
					if !ok {
						continue
					}
					for _, field := range plugin.Elts {
						pair, ok := field.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, keyIsIdent := pair.Key.(*ast.Ident)
						if !keyIsIdent || key.Name != "name" {
							continue
						}
						literal, ok := pair.Value.(*ast.BasicLit)
						if !ok || literal.Kind != token.STRING {
							return nil, fmt.Errorf("curatorPlugins contains a non-literal plugin name")
						}
						pluginName, err := strconv.Unquote(literal.Value)
						if err != nil {
							return nil, err
						}
						names = append(names, pluginName)
						break
					}
				}
				if len(names) == 0 {
					return nil, fmt.Errorf("curatorPlugins has no literal plugin names")
				}
				return names, nil
			}
		}
	}
	return nil, fmt.Errorf("curatorPlugins matrix was not found in %s", path)
}

func deriveCuratorValidatorClassMembers(root string, sites []refusalscan.Site, coverage map[string]string, functions map[string]*ast.FuncDecl, parsedFiles map[string]*parsedSource) ([]curatorValidatorClassMember, error) {
	var reasons []string
	for _, source := range parsedFiles {
		ast.Inspect(source.file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || callName(call) != "curatorMalformed" || len(call.Args) == 0 {
				return true
			}
			if literal, ok := call.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
				if reason, err := strconv.Unquote(literal.Value); err == nil && !containsString(reasons, reason) {
					reasons = append(reasons, reason)
				}
			}
			return true
		})
	}
	sort.Strings(reasons)

	var members []curatorValidatorClassMember
	seen := make(map[string]bool)
	appendMember := func(site refusalscan.Site, testName string, member curatorValidatorClassMember) {
		member.siteKey = site.Key()
		member.testName = testName
		member.downstreamProofs = append(deriveBooleanHelperDownstreamProofs(member, parsedFiles), deriveSameFunctionScalarDownstreamProofs(member, parsedFiles)...)
		member.identity = validatorSourceMemberIdentity(member)
		if !seen[member.identity] {
			seen[member.identity] = true
			members = append(members, member)
		}
	}
	appendSiteMembers := func(owner refusalscan.Site, target refusalscan.Site, testName string) error {
		source := parsedFiles[filepath.Join("pkg", "agentic", filepath.FromSlash(target.File))]
		if source == nil {
			return fmt.Errorf("source AST is missing for refusal site %s", target.Key())
		}
		branch, switchNode := curatorSiteBranches(source.fset, source.file, target)
		switch {
		case branch != nil:
			for index, exemption := range conditionMemberExemptions(source.fset, branch.Cond) {
				appendMember(owner, testName, curatorValidatorClassMember{
					targetFile:     filepath.Join("pkg", "agentic", filepath.FromSlash(target.File)),
					targetFunction: target.Function, targetGuard: target.Guard,
					targetLine: target.Line, targetReturn: target.Return,
					exemption: exemption.expression,
					member:    fmt.Sprintf("%s: clause %d (%s)", target.Locator(), index+1, exemption.label),
					narrows:   "admits only the AST-derived rejected condition member " + exemption.expression,
				})
			}
		case switchNode != nil:
			tag := normalizedNode(source.fset, switchNode.Tag)
			for _, member := range curatorSwitchMemberWitnesses(source.fset, switchNode) {
				appendMember(owner, testName, curatorValidatorClassMember{
					targetFile:     filepath.Join("pkg", "agentic", filepath.FromSlash(target.File)),
					targetFunction: target.Function, targetGuard: target.Guard,
					targetLine: target.Line, targetReturn: target.Return,
					exemption: tag + " == " + strconv.Quote(member.value),
					member:    "unlisted switch value derived from " + member.label + ": " + strconv.Quote(member.value),
					narrows:   "admits only the AST-derived unlisted value " + strconv.Quote(member.value),
				})
			}
		case target.Guard == "unconditional" && target.Function == "curatorMalformed":
			for _, reason := range reasons {
				appendMember(owner, testName, curatorValidatorClassMember{
					targetFile:     filepath.Join("pkg", "agentic", filepath.FromSlash(target.File)),
					targetFunction: target.Function, targetGuard: target.Guard,
					targetLine: target.Line, targetReturn: target.Return,
					exemption: "reason == " + strconv.Quote(reason),
					member:    "malformed reason " + strconv.Quote(reason),
					narrows:   "admits only the caller-provided malformed reason " + strconv.Quote(reason),
				})
			}
		default:
			return fmt.Errorf("cannot derive a narrowing member for refusal site %s", target.Key())
		}
		return nil
	}
	for _, owner := range sites {
		testName, ok := coverage[owner.Key()]
		if !ok {
			return nil, fmt.Errorf("refusal site has no named BuildPlan test during AST class derivation: %s", owner.Key())
		}
		if err := appendSiteMembers(owner, owner, testName); err != nil {
			return nil, err
		}
		if owner.Guard == "unconditional" && owner.Function == "curatorMalformed" {
			continue
		}

		source := parsedFiles[filepath.Join("pkg", "agentic", filepath.FromSlash(owner.File))]
		_, targetNode, targetReturn, _ := curatorSiteTarget(source.fset, source.file, owner)
		roots := callsFromNodes(targetNode, targetReturn)
		reachable := reachableCuratorFunctions(roots, functions)
		for _, name := range reachable {
			for _, helperMember := range curatorBooleanHelperMembers(parsedFiles, functions[name]) {
				if !curatorHelperMemberChangesSite(owner, targetNode, helperMember, reachable, functions, parsedFiles) {
					continue
				}
				appendMember(owner, testName, helperMember)
			}
		}
		if owner.File == "curator_context.go" && owner.Function == "ValidateCuratorContext" &&
			owner.Guard == "if err != nil" && owner.Occurrence == 1 {
			channelMembers, err := curatorUnselectedPromptChannelMembers(root, owner, testName)
			if err != nil {
				return nil, err
			}
			for _, channelMember := range channelMembers {
				appendMember(owner, testName, channelMember)
			}
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].identity < members[j].identity })
	return members, nil
}

type curatorPromptIntentConstant struct {
	name  string
	value string
}

func curatorUnselectedPromptChannelMembers(root string, site refusalscan.Site, testName string) ([]curatorValidatorClassMember, error) {
	validatorPath := filepath.Join("pkg", "agentic", "curator_context.go")
	validator, err := parseSource(root, validatorPath)
	if err != nil {
		return nil, err
	}
	intents, err := curatorPromptIntentConstants(validator.file)
	if err != nil {
		return nil, err
	}
	consumerFiles, err := curatorPromptChannelConsumerFiles(root)
	if err != nil {
		return nil, err
	}
	if len(consumerFiles) == 0 {
		return nil, fmt.Errorf("cannot derive unselected system-prompt channels: no registered-system source has an apply*CuratorContext loop that skips nonmatching descriptors")
	}
	for _, relativePath := range consumerFiles {
		pluginSource, err := parseSource(root, relativePath)
		if err != nil {
			return nil, fmt.Errorf("parse Curator plugin selection logic in %s: %w", relativePath, err)
		}
		if !curatorSourceSkipsNonmatchingPromptChannels(pluginSource) {
			return nil, fmt.Errorf("cannot derive unselected system-prompt channels: %s has no apply*CuratorContext loop skipping descriptor.Semantics != context.SystemPrompt.Intent", relativePath)
		}
	}

	result := make([]curatorValidatorClassMember, 0, len(intents))
	for _, intent := range intents {
		result = append(result, curatorValidatorClassMember{
			targetFile:     validatorPath,
			targetFunction: site.Function,
			targetGuard:    site.Guard,
			targetLine:     site.Line,
			targetReturn:   site.Return,
			exemption:      "descriptor.Semantics == " + intent.name + " && context.SystemPrompt.Intent != " + intent.name,
			member:         "unselected system-prompt channel " + intent.value,
			narrows:        "admits malformed descriptors only for AST-derived unselected channel " + strconv.Quote(intent.value),
			subtestPattern: "system-prompt-unselected-" + regexp.QuoteMeta(intent.value) + "-.*",
		})
	}
	return result, nil
}

// curatorPromptChannelConsumerFiles discovers plugin sources from the
// package tree instead of spelling plugin IDs a second time in the generator.
// Only sources that declare an apply*CuratorContext function participate in
// the channel-selection dimension.
func curatorPromptChannelConsumerFiles(root string) ([]string, error) {
	systemsRoot := filepath.Join(root, "pkg", "agentic", "systems")
	entries, err := os.ReadDir(systemsRoot)
	if err != nil {
		return nil, fmt.Errorf("enumerate registered-system sources for Curator prompt channels: %w", err)
	}
	var consumers []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		relativePath := filepath.Join("pkg", "agentic", "systems", entry.Name(), "context.go")
		pluginSource, err := parseSource(root, relativePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("parse registered-system source %s: %w", relativePath, err)
		}
		for _, declaration := range pluginSource.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || function.Name == nil {
				continue
			}
			if strings.HasPrefix(function.Name.Name, "apply") && strings.HasSuffix(function.Name.Name, "CuratorContext") {
				consumers = append(consumers, relativePath)
				break
			}
		}
	}
	sort.Strings(consumers)
	return consumers, nil
}

func curatorSourceSkipsNonmatchingPromptChannels(source *parsedSource) bool {
	for _, declaration := range source.file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || function.Name == nil ||
			!strings.HasPrefix(function.Name.Name, "apply") || !strings.HasSuffix(function.Name.Name, "CuratorContext") {
			continue
		}
		if functionSkipsNonmatchingPromptChannels(source.fset, function) {
			return true
		}
	}
	return false
}

func curatorPromptIntentConstants(file *ast.File) ([]curatorPromptIntentConstant, error) {
	var result []curatorPromptIntentConstant
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, specification := range group.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok || len(value.Values) != 1 {
				continue
			}
			typeName, ok := value.Type.(*ast.Ident)
			if !ok || typeName.Name != "CuratorSystemPromptIntent" {
				continue
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING || len(value.Names) != 1 {
				return nil, fmt.Errorf("cannot derive CuratorSystemPromptIntent member from malformed AST declaration")
			}
			member, err := strconv.Unquote(literal.Value)
			if err != nil || member == "" {
				return nil, fmt.Errorf("invalid CuratorSystemPromptIntent member %s", literal.Value)
			}
			result = append(result, curatorPromptIntentConstant{name: value.Names[0].Name, value: member})
		}
	}
	if len(result) < 2 {
		return nil, fmt.Errorf("CuratorSystemPromptIntent AST class has %d members; want at least two channels", len(result))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].name < result[j].name })
	return result, nil
}

func functionSkipsNonmatchingPromptChannels(fset *token.FileSet, function *ast.FuncDecl) bool {
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if found {
			return false
		}
		loop, ok := node.(*ast.RangeStmt)
		if !ok || !strings.Contains(normalizedNode(fset, loop.X), "SystemPrompt.Channels") {
			return true
		}
		ast.Inspect(loop.Body, func(child ast.Node) bool {
			if found {
				return false
			}
			branch, ok := child.(*ast.IfStmt)
			if !ok || !promptSemanticsDiffersFromIntent(fset, branch.Cond) || !blockContainsContinue(branch.Body) {
				return true
			}
			found = true
			return false
		})
		return !found
	})
	return found
}

func promptSemanticsDiffersFromIntent(fset *token.FileSet, expression ast.Expr) bool {
	comparison, ok := expression.(*ast.BinaryExpr)
	if !ok || comparison.Op != token.NEQ {
		return false
	}
	left, leftOK := comparison.X.(*ast.SelectorExpr)
	right, rightOK := comparison.Y.(*ast.SelectorExpr)
	return leftOK && rightOK && left.Sel.Name == "Semantics" && right.Sel.Name == "Intent" &&
		strings.Contains(normalizedNode(fset, comparison.Y), "SystemPrompt.Intent")
}

func blockContainsContinue(block *ast.BlockStmt) bool {
	found := false
	ast.Inspect(block, func(node ast.Node) bool {
		if found || node == nil {
			return !found
		}
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		if branch, ok := node.(*ast.BranchStmt); ok && branch.Tok == token.CONTINUE {
			found = true
			return false
		}
		return true
	})
	return found
}

func curatorValidatorRunPattern(member curatorValidatorClassMember) string {
	rootPattern := "^" + regexp.QuoteMeta(member.testName) + "$"
	if member.testName != "TestBuildPlanCuratorContextRefusesMalformedFragment" {
		return rootPattern
	}
	pluginPattern := "^(claude|codex)$"
	casePattern := ""
	if member.subtestPattern != "" {
		casePattern = member.subtestPattern
	} else if strings.Contains(member.exemption, "validCuratorLockHash") {
		casePattern = "(profile pin invalid|lockhash-.*)"
	} else {
		switch member.targetFunction {
		case "validEnvironmentName":
			casePattern = "(mcp-env-invalid-name-.*|MCP env names (unsorted|duplicate|reserved)|unsorted MCP env names|env-reserved-.*)"
		case "curatorReservedMCPEnv":
			casePattern = "(MCP env names reserved|env-reserved-.*)"
		case "isSortedUniqueCuratorEnvNames":
			casePattern = "(MCP env names (unsorted|duplicate|reserved)|unsorted MCP env names|env-reserved-.*|mcp-env-invalid-name-.*)"
		case "validCuratorIdentifier":
			if member.exemption == "len(runes) == 0" && strings.Contains(member.siteKey, "context.Profile.Name") {
				casePattern = "empty-profile-name-preempted"
			} else {
				casePattern = curatorIdentifierCasePattern(member.siteKey)
			}
		case "validCuratorLockHash":
			casePattern = "(profile pin invalid|lockhash-.*)"
		case "validCuratorAbsolutePath":
			casePattern = curatorAbsolutePathCasePattern(member.siteKey)
		case "validCuratorPortablePath":
			if member.exemption == "utf8.RuneCountInString(value) == 0" {
				if strings.Contains(member.siteKey, "ValidateCuratorContext :: if err != nil") && strings.Contains(member.siteKey, "occurrence 1") {
					casePattern = "system-prompt-descriptor-file-empty"
				} else {
					casePattern = "file descriptor empty filename"
				}
			} else if strings.Contains(member.siteKey, "ValidateCuratorContext :: if err != nil") && strings.Contains(member.siteKey, "occurrence 1") {
				casePattern = "system-prompt-descriptor-file-.*"
			} else {
				casePattern = "(file-fragment-.*|file-reserved-name-.*|file descriptor (malformed|empty filename|absolute path)|portable-path-.*)"
			}
		case "validCuratorFlag":
			casePattern = "(flag-content-.*|flag-boundary-.*|flag (invalid|empty|length-one|argument kind (invalid|empty))|with-.*companion.*|with-repeats.*)"
		case "curatorAlphaNumeric", "curatorIdentifierTail":
			casePattern = curatorIdentifierCasePattern(member.siteKey)
		case "curatorMalformed":
			casePattern = malformedReasonCasePattern(member.member)
		default:
			casePattern = curatorGuardCasePattern(member.siteKey, member.targetGuard)
		}
	}
	casePattern = strings.ReplaceAll(casePattern, " ", "_")
	if casePattern == "" {
		return rootPattern
	}
	return rootPattern + "/" + pluginPattern + "/^" + casePattern + "$"
}

func malformedReasonCasePattern(member string) string {
	reason := strings.TrimPrefix(member, "malformed reason ")
	switch {
	case strings.Contains(reason, "profile pin"):
		return "(profile pin invalid|profile-identifier-.*|lockhash-.*)"
	case strings.Contains(reason, "environment is not"):
		return "(environment-extra|environment-enum-.*)"
	case strings.Contains(reason, "precedence"):
		return "precedence-.*"
	case strings.Contains(reason, "exactly one managed-home"):
		return "env-(two|no)-entries"
	case strings.Contains(reason, "managed-home entry"):
		return "(home-variable-any|home mismatch|home relative|managed-home-path-.*)"
	case strings.Contains(reason, "path_prepend"):
		return "(path-prepend.*|path-prepend-path-.*)"
	case strings.Contains(reason, "pi fragments"):
		return "MCP context on pi"
	case strings.Contains(reason, "MCP path"):
		return "(relative MCP path|empty MCP path|absolute-path-.*|mcp-path-.*)"
	case strings.Contains(reason, "MCP env_names"):
		return "(MCP env names .*|unsorted MCP env names|mcp-env-invalid-name-.*|env-reserved-.*)"
	case strings.Contains(reason, "MCP must carry"):
		return "mcp-(two|no)-channels"
	case strings.Contains(reason, "system-prompt path"):
		return "(sp-relative-path|system-prompt-path-.*)"
	case strings.Contains(reason, "system-prompt descriptor semantics"):
		return "system-prompt descriptor semantics .*"
	case strings.Contains(reason, "MCP descriptors do not carry"):
		return "mcp-semantics(-.*)?"
	case strings.Contains(reason, "flag descriptor"):
		return "(flag-content-.*|flag-boundary-.*|flag (invalid|empty|length-one|argument kind (invalid|empty)|carries .*))"
	case strings.Contains(reason, "argument kind"):
		return "(flag argument kind .*|descriptor-argument-enum-.*)"
	case strings.Contains(reason, "reserved name"):
		return "(name argument .*|name-argument-identifier-.*)"
	case strings.Contains(reason, "name is permitted"):
		return "name on .* argument"
	case strings.Contains(reason, "with must be"):
		return "with-.*"
	case strings.Contains(reason, "with contains"):
		return "with-.*"
	case strings.Contains(reason, "with repeats"):
		return "with-repeats.*"
	case strings.Contains(reason, "another union arm"):
		return "(flag carries .*|flag descriptor union arm mixed)"
	case strings.Contains(reason, "config-key descriptor"):
		return "(config-key-identifier-.*|config-key key empty|config-key carries .*)"
	case strings.Contains(reason, "variable descriptor"):
		return "(variable-identifier-.*|variable value empty|variable carries .*)"
	case strings.Contains(reason, "file descriptor"):
		return "file (fragment|reserved-name|descriptor) .*"
	default:
		return ""
	}
}

func curatorAbsolutePathCasePattern(siteKey string) string {
	switch {
	case strings.Contains(siteKey, "context.SystemPrompt.Path"):
		return "system-prompt-path-.*"
	case strings.Contains(siteKey, "PathPrepend"):
		return "path-prepend-path-.*"
	case strings.Contains(siteKey, "homeVariable"):
		return "managed-home-path-.*"
	case strings.Contains(siteKey, "context.MCP.Path"):
		return "(absolute-path-.*|mcp-path-.*|relative MCP path|empty MCP path)"
	default:
		return "(home relative|home mismatch|path-prepend.*|relative MCP path|empty MCP path|sp-relative-path|absolute-path-.*|managed-home-path-.*|path-prepend-path-.*|mcp-path-.*|system-prompt-path-.*)"
	}
}

func curatorIdentifierCasePattern(siteKey string) string {
	switch {
	case strings.Contains(siteKey, "ValidateCuratorContext :: if err != nil"):
		if strings.Contains(siteKey, "occurrence 1") {
			return "(profile-identifier-.*|system-prompt-descriptor-identifier-.*)"
		}
		return "(profile-identifier-.*|config-key-identifier-.*|config-key key empty)"
	case strings.Contains(siteKey, "descriptor.Key"):
		return "(profile-identifier-.*|config-key-identifier-.*|config-key key empty)"
	case strings.Contains(siteKey, "descriptor.Variable"):
		return "(profile-identifier-.*|variable-identifier-.*|variable value empty)"
	case strings.Contains(siteKey, "descriptor.Name"):
		return "(profile-identifier-.*|name-argument-identifier-.*|name argument (invalid|empty))"
	case strings.Contains(siteKey, "descriptor.Filename"):
		return "(profile-identifier-.*|file-fragment-.*|file-reserved-name-.*|file descriptor (malformed|empty filename))"
	default:
		return "(profile-identifier-.*|profile-name-malformed|empty-profile-name-preempted)"
	}
}

func curatorGuardCasePattern(siteKey, guard string) string {
	switch {
	case strings.Contains(guard, "context.Profile.Name"):
		return "(profile-name-malformed|profile-identifier-.*)"
	case strings.Contains(guard, "context.Profile.LockSHA256"):
		return "(profile pin invalid|lockhash-.*)"
	case strings.Contains(guard, "len(context.Env)"):
		return "env-(two|no)-entries"
	case strings.Contains(guard, "homeVariable"):
		return "(home-variable-any|home mismatch|home relative|managed-home-path-.*)"
	case strings.Contains(guard, "PathPrepend"):
		return "path-prepend.*"
	case strings.Contains(guard, "context.Environment"):
		return "(environment-extra|environment-enum-.*|MCP context on pi)"
	case strings.Contains(guard, "context.MCP.Path"):
		return "(relative MCP path|empty MCP path|absolute-path-.*|mcp-path-.*)"
	case strings.Contains(guard, "EnvNames"):
		return "(MCP env names .*|unsorted MCP env names|mcp-env-invalid-name-.*|env-reserved-.*)"
	case strings.Contains(guard, "context.SystemPrompt.Path"):
		return "(sp-relative-path|system-prompt-path-.*)"
	case strings.Contains(guard, "descriptor.Semantics"):
		return "(system-prompt descriptor semantics .*|mcp-semantics(-.*)?|system-prompt-semantics-.*)"
	case strings.Contains(guard, "descriptor.Flag"):
		return "(flag-content-.*|flag-boundary-.*|flag (invalid|empty|length-one))"
	case strings.Contains(guard, "descriptor.Argument"):
		return "(flag argument kind .*|descriptor-argument-enum-.*)"
	case strings.Contains(guard, "descriptor.Name"):
		return "(name-argument-identifier-.*|name argument .*|name on .* argument|name on .* argument unmapped)"
	case strings.Contains(guard, "descriptor.With") || strings.Contains(guard, "companion"):
		return "with-.*"
	case strings.Contains(guard, "descriptor.Key"):
		return "(config-key-identifier-.*|config-key key empty|config-key carries .*)"
	case strings.Contains(guard, "descriptor.Variable"):
		return "(variable-identifier-.*|variable value empty|variable carries .*)"
	case strings.Contains(guard, "descriptor.Filename"):
		return "(file-fragment-.*|file-reserved-name-.*|file descriptor .*|portable-path-.*)"
	case strings.Contains(guard, "noExtras"):
		if strings.Contains(siteKey, "flag descriptor") {
			return "flag carries .*"
		}
		if strings.Contains(siteKey, "config-key descriptor") {
			return "config-key carries .*"
		}
		if strings.Contains(siteKey, "variable descriptor") {
			return "variable carries .*"
		}
		if strings.Contains(siteKey, "file descriptor") {
			return "file descriptor carries .*"
		}
	}
	return ""
}

func validatorSourceMemberIdentity(member curatorValidatorClassMember) string {
	return strings.Join([]string{
		member.siteKey, member.targetFile, member.targetFunction, member.targetGuard,
		strconv.Itoa(member.targetLine), member.targetReturn, member.mutationKind,
		member.exemption,
	}, "\x00")
}

func curatorSiteBranches(fset *token.FileSet, file *ast.File, site refusalscan.Site) (*ast.IfStmt, *ast.SwitchStmt) {
	function := findFunctionAndBody(file, site.Function)
	if function == nil || function.Body == nil {
		return nil, nil
	}
	var target *ast.ReturnStmt
	ast.Inspect(function.Body, func(node ast.Node) bool {
		statement, ok := node.(*ast.ReturnStmt)
		if ok && fset.Position(statement.Pos()).Line == site.Line && normalizedNode(fset, statement) == strings.Join(strings.Fields(site.Return), " ") {
			target = statement
		}
		return true
	})
	if target == nil {
		return nil, nil
	}
	guard := normalizeGuard(site.Guard)
	if strings.HasPrefix(guard, "if ") {
		want := strings.TrimPrefix(guard, "if ")
		var match *ast.IfStmt
		ast.Inspect(function.Body, func(node ast.Node) bool {
			branch, ok := node.(*ast.IfStmt)
			if ok && normalizedNode(fset, branch.Cond) == want && nodeContains(branch, target) {
				match = branch
			}
			return true
		})
		return match, nil
	}
	if strings.HasPrefix(guard, "switch ") {
		const marker = " case default"
		if !strings.HasSuffix(guard, marker) {
			return nil, nil
		}
		want := strings.TrimPrefix(strings.TrimSuffix(guard, marker), "switch ")
		var match *ast.SwitchStmt
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switchNode, ok := node.(*ast.SwitchStmt)
			if !ok || switchNode.Tag == nil || normalizedNode(fset, switchNode.Tag) != want {
				return true
			}
			for _, raw := range switchNode.Body.List {
				clause, ok := raw.(*ast.CaseClause)
				if ok && clause.List == nil && nodeContains(clause, target) {
					match = switchNode
				}
			}
			return true
		})
		return nil, match
	}
	return nil, nil
}

func curatorSiteTarget(fset *token.FileSet, file *ast.File, site refusalscan.Site) (*ast.FuncDecl, ast.Node, *ast.ReturnStmt, ast.Node) {
	function := findFunctionAndBody(file, site.Function)
	if function == nil || function.Body == nil {
		return nil, nil, nil, nil
	}
	var target *ast.ReturnStmt
	ast.Inspect(function.Body, func(node ast.Node) bool {
		statement, ok := node.(*ast.ReturnStmt)
		if ok && fset.Position(statement.Pos()).Line == site.Line && normalizedNode(fset, statement) == strings.Join(strings.Fields(site.Return), " ") {
			target = statement
		}
		return true
	})
	branch, switchNode := curatorSiteBranches(fset, file, site)
	var selected ast.Node
	if branch != nil {
		selected = branch
	} else if switchNode != nil && target != nil {
		for _, raw := range switchNode.Body.List {
			clause, ok := raw.(*ast.CaseClause)
			if ok && clause.List == nil && nodeContains(clause, target) {
				selected = clause
				break
			}
		}
	}
	return function, selected, target, branch
}

func findFunctionAndBody(file *ast.File, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == name {
			return function
		}
	}
	return nil
}

type conditionMember struct {
	expression string
	label      string
}

func conditionMemberExemptions(fset *token.FileSet, condition ast.Expr) []conditionMember {
	var result []conditionMember
	seen := make(map[string]bool)
	appendMembers := func(members []conditionMember) {
		for _, member := range members {
			if !seen[member.expression] {
				seen[member.expression] = true
				result = append(result, member)
			}
		}
	}
	var visit func(ast.Expr, []ast.Expr)
	visit = func(expression ast.Expr, siblings []ast.Expr) {
		switch node := expression.(type) {
		case *ast.ParenExpr:
			visit(node.X, siblings)
		case *ast.BinaryExpr:
			if node.Op == token.LAND {
				terms := topLevelAndTerms(node)
				appendMembers(orderedRangeInteriorMembers(fset, terms))
				for _, term := range terms {
					visit(term, terms)
				}
				return
			}
			if node.Op == token.LOR {
				visit(node.X, siblings)
				visit(node.Y, siblings)
				return
			}
			appendMembers(conditionTermMembersWithConjuncts(fset, node, siblings))
		default:
			appendMembers(conditionTermMembersWithConjuncts(fset, expression, siblings))
		}
	}
	visit(condition, nil)
	return result
}

func conditionTermMembersWithConjuncts(fset *token.FileSet, term ast.Expr, conjuncts []ast.Expr) []conditionMember {
	members := conditionTermMembers(fset, term)
	for index := range members {
		if !strings.Contains(members[index].label, "HasPrefix class member derived from") {
			continue
		}
		for _, call := range callsNamed(term, "HasPrefix") {
			if len(call.Args) != 2 {
				continue
			}
			literal, ok := call.Args[1].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			prefix, err := strconv.Unquote(literal.Value)
			if err != nil || !strings.Contains(members[index].label, strconv.Quote(prefix)) {
				continue
			}
			witness, ok := prefixWitnessSatisfyingConjuncts(fset, call.Args[0], prefix, conjuncts)
			if ok {
				members[index].expression = normalizedNode(fset, call.Args[0]) + " == " + strconv.Quote(witness)
			}
			break
		}
	}
	return members
}

func prefixWitnessSatisfyingConjuncts(fset *token.FileSet, receiver ast.Expr, prefix string, conjuncts []ast.Expr) (string, bool) {
	receiverText := normalizedNode(fset, receiver)
	exactLength := -1
	for _, conjunct := range conjuncts {
		ast.Inspect(conjunct, func(node ast.Node) bool {
			comparison, ok := node.(*ast.BinaryExpr)
			if !ok || comparison.Op != token.EQL {
				return true
			}
			call, literal := comparison.X.(*ast.CallExpr), comparison.Y.(*ast.BasicLit)
			if !literalIsLenOf(fset, call, receiverText) {
				call, literal = comparison.Y.(*ast.CallExpr), comparison.X.(*ast.BasicLit)
			}
			if !literalIsLenOf(fset, call, receiverText) || literal == nil || literal.Kind != token.INT {
				return true
			}
			if value, err := strconv.Atoi(literal.Value); err == nil {
				exactLength = value
				return false
			}
			return true
		})
	}
	if exactLength <= len(prefix) || exactLength-len(prefix) > 32 {
		return "", false
	}
	witness := []byte(prefix + strings.Repeat("x", exactLength-len(prefix)))
	for position := len(prefix); position < len(witness); position++ {
		found := false
		for candidate := byte(0); candidate < 127; candidate++ {
			witness[position] = candidate
			candidateValue := string(witness)
			if prefixIndexConstraintsAccept(fset, conjuncts, receiverText, candidateValue, position) {
				found = true
				break
			}
		}
		if !found {
			return "", false
		}
	}
	value := string(witness)
	for _, conjunct := range conjuncts {
		accepted, known := conditionHoldsForString(fset, conjunct, receiverText, value)
		if !known || !accepted {
			return "", false
		}
	}
	return value, true
}

func literalIsLenOf(fset *token.FileSet, call *ast.CallExpr, receiver string) bool {
	return call != nil && callName(call) == "len" && len(call.Args) == 1 && normalizedNode(fset, call.Args[0]) == receiver
}

func prefixIndexConstraintsAccept(fset *token.FileSet, conjuncts []ast.Expr, receiver, witness string, index int) bool {
	for _, conjunct := range conjuncts {
		var accepted = true
		ast.Inspect(conjunct, func(node ast.Node) bool {
			comparison, ok := node.(*ast.BinaryExpr)
			if !ok {
				return true
			}
			indexed, ok := comparison.X.(*ast.IndexExpr)
			op := comparison.Op
			boundNode := comparison.Y
			if !ok {
				indexed, ok = comparison.Y.(*ast.IndexExpr)
				if !ok {
					return true
				}
				boundNode = comparison.X
				op = reverseComparison(op)
			}
			if normalizedNode(fset, indexed.X) != receiver {
				return true
			}
			indexLiteral, ok := indexed.Index.(*ast.BasicLit)
			if !ok || indexLiteral.Kind != token.INT {
				return true
			}
			indexValue, err := strconv.Atoi(indexLiteral.Value)
			if err != nil || indexValue != index {
				return true
			}
			boundLiteral, ok := boundNode.(*ast.BasicLit)
			if !ok || boundLiteral.Kind != token.CHAR {
				return true
			}
			bound, err := strconv.Unquote(boundLiteral.Value)
			if err != nil || len(bound) != 1 || index >= len(witness) {
				accepted = false
				return false
			}
			left, right := witness[index], bound[0]
			passes := false
			switch op {
			case token.GEQ:
				passes = left >= right
			case token.GTR:
				passes = left > right
			case token.LEQ:
				passes = left <= right
			case token.LSS:
				passes = left < right
			case token.EQL:
				passes = left == right
			case token.NEQ:
				passes = left != right
			default:
				return true
			}
			if !passes {
				accepted = false
				return false
			}
			return true
		})
		if !accepted {
			return false
		}
	}
	return true
}

func reverseComparison(operator token.Token) token.Token {
	switch operator {
	case token.GEQ:
		return token.LEQ
	case token.GTR:
		return token.LSS
	case token.LEQ:
		return token.GEQ
	case token.LSS:
		return token.GTR
	default:
		return operator
	}
}

func conditionHoldsForString(fset *token.FileSet, expression ast.Expr, receiver, witness string) (bool, bool) {
	switch typed := expression.(type) {
	case *ast.ParenExpr:
		return conditionHoldsForString(fset, typed.X, receiver, witness)
	case *ast.UnaryExpr:
		if typed.Op != token.NOT {
			return false, false
		}
		value, known := conditionHoldsForString(fset, typed.X, receiver, witness)
		return !value, known
	case *ast.BinaryExpr:
		switch typed.Op {
		case token.LAND, token.LOR:
			left, leftKnown := conditionHoldsForString(fset, typed.X, receiver, witness)
			right, rightKnown := conditionHoldsForString(fset, typed.Y, receiver, witness)
			if !leftKnown || !rightKnown {
				return false, false
			}
			if typed.Op == token.LAND {
				return left && right, true
			}
			return left || right, true
		case token.EQL, token.NEQ, token.GEQ, token.GTR, token.LEQ, token.LSS:
			left, leftKnown := scalarForStringWitness(fset, typed.X, receiver, witness)
			right, rightKnown := scalarForStringWitness(fset, typed.Y, receiver, witness)
			if !leftKnown || !rightKnown {
				return false, false
			}
			return compareWitnessScalars(left, right, typed.Op)
		}
	case *ast.CallExpr:
		if callName(typed) == "len" && len(typed.Args) == 1 && normalizedNode(fset, typed.Args[0]) == receiver {
			return true, true
		}
		if len(typed.Args) != 2 || normalizedNode(fset, typed.Args[0]) != receiver {
			return false, false
		}
		literal, ok := typed.Args[1].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return false, false
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return false, false
		}
		switch callName(typed) {
		case "HasPrefix":
			return strings.HasPrefix(witness, value), true
		case "HasSuffix":
			return strings.HasSuffix(witness, value), true
		case "Contains":
			return strings.Contains(witness, value), true
		}
	case *ast.Ident:
		if typed.Name == "true" {
			return true, true
		}
		if typed.Name == "false" {
			return false, true
		}
	}
	return false, false
}

func scalarForStringWitness(fset *token.FileSet, expression ast.Expr, receiver, witness string) (any, bool) {
	switch typed := expression.(type) {
	case *ast.ParenExpr:
		return scalarForStringWitness(fset, typed.X, receiver, witness)
	case *ast.Ident:
		if normalizedNode(fset, typed) == receiver {
			return witness, true
		}
	case *ast.BasicLit:
		switch typed.Kind {
		case token.STRING:
			value, err := strconv.Unquote(typed.Value)
			return value, err == nil
		case token.CHAR:
			value, err := strconv.Unquote(typed.Value)
			if err == nil && len([]rune(value)) == 1 {
				return int([]rune(value)[0]), true
			}
		case token.INT:
			value, err := strconv.Atoi(typed.Value)
			return value, err == nil
		}
	case *ast.CallExpr:
		if callName(typed) == "len" && len(typed.Args) == 1 && normalizedNode(fset, typed.Args[0]) == receiver {
			return len(witness), true
		}
	case *ast.IndexExpr:
		if normalizedNode(fset, typed.X) == receiver {
			index, ok := typed.Index.(*ast.BasicLit)
			if !ok || index.Kind != token.INT {
				return nil, false
			}
			position, err := strconv.Atoi(index.Value)
			if err != nil || position < 0 || position >= len(witness) {
				return nil, false
			}
			return int(witness[position]), true
		}
	}
	return nil, false
}

func compareWitnessScalars(left, right any, operator token.Token) (bool, bool) {
	switch leftValue := left.(type) {
	case string:
		rightValue, ok := right.(string)
		if !ok {
			return false, false
		}
		switch operator {
		case token.EQL:
			return leftValue == rightValue, true
		case token.NEQ:
			return leftValue != rightValue, true
		case token.GEQ:
			return leftValue >= rightValue, true
		case token.GTR:
			return leftValue > rightValue, true
		case token.LEQ:
			return leftValue <= rightValue, true
		case token.LSS:
			return leftValue < rightValue, true
		}
	case int:
		rightValue, ok := right.(int)
		if !ok {
			return false, false
		}
		switch operator {
		case token.EQL:
			return leftValue == rightValue, true
		case token.NEQ:
			return leftValue != rightValue, true
		case token.GEQ:
			return leftValue >= rightValue, true
		case token.GTR:
			return leftValue > rightValue, true
		case token.LEQ:
			return leftValue <= rightValue, true
		case token.LSS:
			return leftValue < rightValue, true
		}
	}
	return false, false
}

func conditionTermMembers(fset *token.FileSet, term ast.Expr) []conditionMember {
	if members := lengthComparisonMembers(fset, term); len(members) != 0 {
		return members
	}
	if members := allowedSetComparisonMembers(fset, term); len(members) != 0 {
		return members
	}
	if members := orderedComparisonMembers(fset, term); len(members) != 0 {
		return members
	}
	if members := stringComparisonMembers(fset, term); len(members) != 0 {
		return members
	}
	if members := literalSetCallMembers(fset, term); len(members) != 0 {
		return members
	}
	if members := noExtrasCallMembers(fset, term); len(members) != 0 {
		return members
	}
	return []conditionMember{{expression: normalizedNode(fset, term), label: normalizedNode(fset, term)}}
}

type orderedConstraint struct {
	valueExpression  string
	sourceExpression string
	bound            int64
	isRune           bool
	lower            bool
}

func orderedRangeInteriorMembers(fset *token.FileSet, atoms []ast.Expr) []conditionMember {
	var constraints []orderedConstraint
	for _, atom := range atoms {
		constraint, ok := orderedConstraintForExpression(fset, atom)
		if ok {
			constraint.sourceExpression = normalizedNode(fset, atom)
			constraints = append(constraints, constraint)
		}
	}
	var result []conditionMember
	seen := make(map[string]bool)
	for _, lower := range constraints {
		if !lower.lower {
			continue
		}
		for _, upper := range constraints {
			if upper.lower || upper.valueExpression != lower.valueExpression || upper.isRune != lower.isRune || upper.bound < lower.bound || upper.bound-lower.bound > 256 {
				continue
			}
			var otherConstraints []string
			for _, atom := range atoms {
				atomText := normalizedNode(fset, atom)
				if atomText != lower.sourceExpression && atomText != upper.sourceExpression {
					otherConstraints = append(otherConstraints, atomText)
				}
			}
			for value := lower.bound; value <= upper.bound; value++ {
				var literal string
				if lower.isRune {
					if value < 0 || value > unicode.MaxRune || (value >= 0xD800 && value <= 0xDFFF) {
						continue
					}
					literal = strconv.QuoteRuneToASCII(rune(value))
				} else {
					literal = strconv.FormatInt(value, 10)
				}
				expression := lower.valueExpression + " == " + literal
				if len(otherConstraints) != 0 {
					expression = "(" + expression + ") && (" + strings.Join(otherConstraints, " && ") + ")"
				}
				if !seen[expression] {
					seen[expression] = true
					result = append(result, conditionMember{expression: expression, label: "range member " + literal + " from AST bounds"})
				}
			}
		}
	}
	return result
}

func orderedConstraintForExpression(fset *token.FileSet, expression ast.Expr) (orderedConstraint, bool) {
	binary, ok := expression.(*ast.BinaryExpr)
	if !ok {
		return orderedConstraint{}, false
	}
	left, right, operator := binary.X, binary.Y, binary.Op
	literal, ok := right.(*ast.BasicLit)
	if !ok {
		literal, ok = left.(*ast.BasicLit)
		if !ok {
			return orderedConstraint{}, false
		}
		right, operator = left, reverseComparison(operator)
	}
	var bound int64
	isRune := literal.Kind == token.CHAR
	if isRune {
		value, err := strconv.Unquote(literal.Value)
		if err != nil || len([]rune(value)) != 1 {
			return orderedConstraint{}, false
		}
		bound = int64([]rune(value)[0])
	} else if literal.Kind == token.INT {
		value, err := strconv.ParseInt(literal.Value, 0, 64)
		if err != nil {
			return orderedConstraint{}, false
		}
		bound = value
	} else {
		return orderedConstraint{}, false
	}
	lower := false
	switch operator {
	case token.GEQ:
		lower = true
	case token.GTR:
		lower, bound = true, bound+1
	case token.LEQ:
	case token.LSS:
		bound--
	default:
		return orderedConstraint{}, false
	}
	return orderedConstraint{valueExpression: normalizedNode(fset, right), bound: bound, isRune: isRune, lower: lower}, true
}

func topLevelAndTerms(expression ast.Expr) []ast.Expr {
	if binary, ok := expression.(*ast.BinaryExpr); ok && binary.Op == token.LAND {
		return append(topLevelAndTerms(binary.X), topLevelAndTerms(binary.Y)...)
	}
	return []ast.Expr{expression}
}

func lengthComparisonMembers(fset *token.FileSet, expression ast.Expr) []conditionMember {
	binary, ok := expression.(*ast.BinaryExpr)
	if !ok {
		return nil
	}
	var call *ast.CallExpr
	var literal *ast.BasicLit
	var op token.Token
	if candidate, ok := binary.X.(*ast.CallExpr); ok {
		call = candidate
		literal, _ = binary.Y.(*ast.BasicLit)
		op = binary.Op
	} else if candidate, ok := binary.Y.(*ast.CallExpr); ok {
		call = candidate
		literal, _ = binary.X.(*ast.BasicLit)
		op = reverseComparison(binary.Op)
	}
	if call == nil || literal == nil || len(call.Args) != 1 || literal.Kind != token.INT {
		return nil
	}
	callName := callName(call)
	if callName != "len" && callName != "RuneCountInString" && callName != "RuneCount" {
		return nil
	}
	bound, err := strconv.Atoi(literal.Value)
	if err != nil {
		return nil
	}
	var lengths []int
	switch op {
	case token.EQL:
		lengths = []int{bound}
	case token.NEQ:
		lengths = []int{bound - 1, bound + 1}
	case token.LSS:
		lengths = []int{bound - 1}
	case token.LEQ:
		lengths = []int{bound}
	case token.GTR:
		lengths = []int{bound + 1}
	case token.GEQ:
		lengths = []int{bound}
	default:
		return nil
	}
	var result []conditionMember
	for _, length := range lengths {
		if length < 0 {
			continue
		}
		expression := normalizedNode(fset, call) + " == " + strconv.Itoa(length)
		result = append(result, conditionMember{expression: expression, label: fmt.Sprintf("length boundary %d from %s", length, normalizedNode(fset, binary))})
	}
	return result
}

func orderedComparisonMembers(fset *token.FileSet, expression ast.Expr) []conditionMember {
	binary, ok := expression.(*ast.BinaryExpr)
	if !ok {
		return nil
	}
	left, right, operator := binary.X, binary.Y, binary.Op
	literal, ok := right.(*ast.BasicLit)
	if !ok {
		literal, ok = left.(*ast.BasicLit)
		if !ok {
			return nil
		}
		left, operator = right, reverseComparison(operator)
	}
	if literal.Kind != token.INT && literal.Kind != token.CHAR {
		return nil
	}
	var bound int64
	if literal.Kind == token.INT {
		parsed, err := strconv.ParseInt(literal.Value, 0, 64)
		if err != nil {
			return nil
		}
		bound = parsed
	} else {
		value, err := strconv.Unquote(literal.Value)
		if err != nil || len([]rune(value)) != 1 {
			return nil
		}
		bound = int64([]rune(value)[0])
	}
	var witness int64
	switch operator {
	case token.LSS:
		witness = bound - 1
	case token.LEQ:
		witness = bound
	case token.GTR:
		witness = bound + 1
	case token.GEQ:
		witness = bound
	default:
		return nil
	}
	var rendered string
	if literal.Kind == token.CHAR {
		if witness < 0 || witness > unicode.MaxRune || (witness >= 0xD800 && witness <= 0xDFFF) {
			return nil
		}
		rendered = strconv.QuoteRuneToASCII(rune(witness))
	} else {
		rendered = strconv.FormatInt(witness, 10)
	}
	return []conditionMember{{
		expression: normalizedNode(fset, left) + " == " + rendered,
		label:      "ordered boundary witness " + rendered + " from " + normalizedNode(fset, binary),
	}}
}

func stringComparisonMembers(fset *token.FileSet, expression ast.Expr) []conditionMember {
	binary, ok := expression.(*ast.BinaryExpr)
	if !ok || (binary.Op != token.EQL && binary.Op != token.NEQ) {
		return nil
	}
	identifier, literal, operator := binary.X, binary.Y, binary.Op
	value, ok := literal.(*ast.BasicLit)
	if !ok || value.Kind != token.STRING {
		value, ok = binary.X.(*ast.BasicLit)
		if ok && value.Kind == token.STRING {
			identifier = binary.Y
		} else if operator == token.NEQ && isStringLikeConstant(literal) {
			constant := normalizedNode(fset, literal)
			return []conditionMember{{
				expression: normalizedNode(fset, identifier) + " == " + strconv.Quote(constant+"__unmapped__"),
				label:      "unlisted enum value derived from " + constant,
			}}
		} else if operator == token.NEQ && isStringLikeConstant(binary.X) && !isStringLikeConstant(binary.Y) {
			constant := normalizedNode(fset, binary.X)
			return []conditionMember{{
				expression: normalizedNode(fset, binary.Y) + " == " + strconv.Quote(constant+"__unmapped__"),
				label:      "unlisted enum value derived from " + constant,
			}}
		} else {
			return nil
		}
	}
	raw, err := strconv.Unquote(value.Value)
	if err != nil {
		return nil
	}
	witness := raw + "__unmapped__"
	if raw == "" {
		witness = "__unmapped__"
	}
	if operator == token.EQL {
		return []conditionMember{{expression: normalizedNode(fset, binary), label: "compared string member " + strconv.Quote(raw)}}
	}
	return []conditionMember{{
		expression: normalizedNode(fset, identifier) + " == " + strconv.Quote(witness),
		label:      "unlisted string member derived from " + strconv.Quote(raw),
	}}
}

func isStringLikeConstant(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.Ident:
		return strings.HasPrefix(expression.Name, "Curator")
	case *ast.SelectorExpr:
		return strings.HasPrefix(expression.Sel.Name, "Curator")
	default:
		return false
	}
}

func allowedSetComparisonMembers(fset *token.FileSet, expression ast.Expr) []conditionMember {
	var result []conditionMember
	for _, call := range callsNamed(expression, "IndexByte") {
		if len(call.Args) != 2 {
			continue
		}
		value, ok := call.Args[1].(*ast.BasicLit)
		if !ok || value.Kind != token.INT {
			continue
		}
		byteValue, err := strconv.ParseInt(value.Value, 0, 64)
		if err != nil || byteValue < 0 || byteValue > 255 {
			continue
		}
		result = append(result, conditionMember{
			expression: "strings.ContainsRune(" + normalizedNode(fset, call.Args[0]) + ", " + strconv.QuoteRuneToASCII(rune(byteValue)) + ")",
			label:      "allowed-set byte member " + strconv.QuoteRuneToASCII(rune(byteValue)),
		})
	}
	return result
}

func literalSetCallMembers(fset *token.FileSet, expression ast.Expr) []conditionMember {
	var result []conditionMember
	for _, callName := range []string{"ContainsAny", "IndexAny"} {
		for _, call := range callsNamed(expression, callName) {
			if len(call.Args) != 2 {
				continue
			}
			literal, ok := call.Args[1].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			characters, err := strconv.Unquote(literal.Value)
			if err != nil {
				continue
			}
			for _, character := range characters {
				result = append(result, conditionMember{
					expression: "strings.ContainsRune(" + normalizedNode(fset, call.Args[0]) + ", " + strconv.QuoteRuneToASCII(character) + ")",
					label:      "allowed-set literal " + strconv.QuoteRuneToASCII(character),
				})
			}
		}
	}
	for _, callName := range []string{"HasPrefix", "HasSuffix", "Contains"} {
		for _, call := range callsNamed(expression, callName) {
			if len(call.Args) != 2 {
				continue
			}
			literal, ok := call.Args[1].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				continue
			}
			negated := expressionNegatesCall(expression, call)
			witness := value + "__unmapped__"
			switch callName {
			case "HasPrefix":
				if negated {
					witness = "__unmapped__" + value
				}
			case "HasSuffix":
				if !negated {
					witness = "__unmapped__" + value
				}
			case "Contains":
				if negated {
					witness = "__unmapped__"
				} else {
					witness = "__unmapped__" + value + "__member__"
				}
			}
			result = append(result, conditionMember{
				expression: normalizedNode(fset, call.Args[0]) + " == " + strconv.Quote(witness),
				label:      callName + " class member derived from " + strconv.Quote(value),
			})
		}
	}
	return result
}

func expressionNegatesCall(expression ast.Expr, target *ast.CallExpr) bool {
	negated := false
	var visit func(ast.Expr, bool) bool
	visit = func(candidate ast.Expr, underNot bool) bool {
		switch typed := candidate.(type) {
		case *ast.UnaryExpr:
			return visit(typed.X, underNot != (typed.Op == token.NOT))
		case *ast.CallExpr:
			if typed == target {
				negated = underNot
				return true
			}
			for _, argument := range typed.Args {
				if visit(argument, underNot) {
					return true
				}
			}
		case *ast.BinaryExpr:
			if visit(typed.X, underNot) || visit(typed.Y, underNot) {
				return true
			}
		case *ast.ParenExpr:
			return visit(typed.X, underNot)
		}
		return false
	}
	visit(expression, false)
	return negated
}

func noExtrasCallMembers(fset *token.FileSet, expression ast.Expr) []conditionMember {
	var result []conditionMember
	for _, call := range callsNamed(expression, "noExtras") {
		if len(call.Args) != 7 {
			continue
		}
		fields := []string{"Flag", "Argument", "Name", "Key", "Variable", "Filename", "With"}
		for index, argument := range call.Args {
			literal, ok := argument.(*ast.Ident)
			if !ok || literal.Name != "true" {
				continue
			}
			member := "descriptor." + fields[index] + " != \"\""
			if fields[index] == "With" {
				member = "descriptor.With != nil"
			}
			result = append(result, conditionMember{expression: member, label: "union field " + fields[index] + " from " + normalizedNode(fset, call)})
		}
	}
	return result
}

func topLevelOrTerms(expression ast.Expr) []ast.Expr {
	if binary, ok := expression.(*ast.BinaryExpr); ok && binary.Op == token.LOR {
		return append(topLevelOrTerms(binary.X), topLevelOrTerms(binary.Y)...)
	}
	return []ast.Expr{expression}
}

func callsNamed(expression ast.Expr, suffix string) []*ast.CallExpr {
	var result []*ast.CallExpr
	ast.Inspect(expression, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && callName(call) == suffix {
			result = append(result, call)
		}
		return true
	})
	return result
}

type switchMemberWitness struct {
	value string
	label string
}

func curatorSwitchMemberWitnesses(fset *token.FileSet, switchNode *ast.SwitchStmt) []switchMemberWitness {
	var result []switchMemberWitness
	seen := make(map[string]bool)
	for _, raw := range switchNode.Body.List {
		clause, ok := raw.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expression := range clause.List {
			value := normalizedNode(fset, expression)
			if literal, ok := expression.(*ast.BasicLit); ok && literal.Kind == token.STRING {
				if unquoted, err := strconv.Unquote(literal.Value); err == nil {
					value = unquoted
				}
			}
			witness := value + "__unmapped__"
			if !seen[witness] {
				seen[witness] = true
				result = append(result, switchMemberWitness{value: witness, label: normalizedNode(fset, expression)})
			}
		}
	}
	if len(result) == 0 {
		result = append(result, switchMemberWitness{value: "__unmapped_value__", label: normalizedNode(fset, switchNode.Tag)})
	}
	return result
}

func curatorValidatorPackageSources(root string) (map[string]*parsedSource, error) {
	files, err := refusalscan.SourceFiles(root)
	if err != nil {
		return nil, fmt.Errorf("enumerate validator helper sources: %w", err)
	}
	result := make(map[string]*parsedSource)
	for _, relative := range files {
		if filepath.Dir(relative) != filepath.Join("pkg", "agentic") {
			continue
		}
		parsed, err := parseSource(root, relative)
		if err != nil {
			return nil, err
		}
		result[relative] = parsed
	}
	return result, nil
}

func curatorValidatorFunctions(sources map[string]*parsedSource) map[string]*ast.FuncDecl {
	result := make(map[string]*ast.FuncDecl)
	for _, source := range sources {
		for _, declaration := range source.file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
				result[function.Name.Name] = function
			}
		}
	}
	return result
}

func curatorBuildPlanTestFunctions(root string) (map[string]bool, error) {
	packageDirectory := filepath.Join(root, "pkg", "agentic")
	result := make(map[string]bool)
	err := filepath.WalkDir(packageDirectory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse BuildPlan test source %s: %w", path, err)
		}
		for _, declaration := range parsed.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && strings.HasPrefix(function.Name.Name, "TestBuildPlan") {
				result[function.Name.Name] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("enumerate named BuildPlan tests: %w", err)
	}
	return result, nil
}

func callsFromNodes(nodes ...ast.Node) []string {
	seen := make(map[string]bool)
	var result []string
	for _, root := range nodes {
		if root == nil {
			continue
		}
		ast.Inspect(root, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := callName(call)
			if name != "" && !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
			return true
		})
	}
	sort.Strings(result)
	return result
}

func reachableCuratorFunctions(roots []string, functions map[string]*ast.FuncDecl) []string {
	seen := make(map[string]bool)
	var result []string
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		function := functions[name]
		if function == nil || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
		queue = append(queue, callsFromNodes(function.Body)...)
	}
	sort.Strings(result)
	return result
}

func curatorBooleanHelperMembers(parsedFiles map[string]*parsedSource, function *ast.FuncDecl) []curatorValidatorClassMember {
	if function == nil || !curatorBooleanFunction(function) {
		return nil
	}
	if function.Name.Name == "curatorReservedMCPEnv" {
		return curatorReservedEnvironmentMembers(parsedFiles, function)
	}
	if function.Name.Name == "curatorAlphaNumeric" {
		return curatorAlphaNumericBoundaryMembers(parsedFiles, function)
	}
	var result []curatorValidatorClassMember
	for _, source := range parsedFiles {
		for _, declaration := range source.file.Decls {
			candidate, ok := declaration.(*ast.FuncDecl)
			if !ok || candidate.Name.Name != function.Name.Name {
				continue
			}
			ast.Inspect(candidate.Body, func(node ast.Node) bool {
				returnStmt, ok := node.(*ast.ReturnStmt)
				if !ok || len(returnStmt.Results) != 1 {
					return true
				}
				if literal, ok := returnStmt.Results[0].(*ast.Ident); ok && (literal.Name == "true" || literal.Name == "false") {
					return true
				}
				for _, member := range booleanReturnMemberExemptions(source.fset, returnStmt.Results[0]) {
					result = append(result, curatorValidatorClassMember{
						targetFile:     sourcePathForFunction(parsedFiles, candidate.Name.Name),
						targetFunction: candidate.Name.Name, targetLine: source.fset.Position(returnStmt.Pos()).Line,
						targetReturn: normalizedNode(source.fset, returnStmt), mutationKind: "bool-return-allow",
						exemption: member.expression, member: candidate.Name.Name + " " + member.label,
						narrows: "admits only the AST-derived boolean-helper member " + member.expression,
					})
				}
				return true
			})
			ast.Inspect(candidate.Body, func(node ast.Node) bool {
				branch, ok := node.(*ast.IfStmt)
				if !ok {
					return true
				}
				returns := boolReturnsInBlock(branch.Body)
				if len(returns) == 0 {
					return true
				}
				for _, returnStmt := range returns {
					for index, exemption := range conditionMemberExemptions(source.fset, branch.Cond) {
						result = append(result, curatorValidatorClassMember{
							targetFile:     sourcePathForFunction(parsedFiles, candidate.Name.Name),
							targetFunction: candidate.Name.Name, targetGuard: "if " + normalizedNode(source.fset, branch.Cond),
							targetLine:   source.fset.Position(returnStmt.Pos()).Line,
							targetReturn: normalizedNode(source.fset, returnStmt),
							exemption:    exemption.expression,
							member:       fmt.Sprintf("%s clause %d (%s)", candidate.Name.Name, index+1, exemption.label),
							narrows:      "admits only the AST-derived helper condition member " + exemption.expression,
						})
					}
				}
				return true
			})
		}
	}
	return result
}

func curatorHelperMemberChangesSite(owner refusalscan.Site, targetNode ast.Node, member curatorValidatorClassMember, reachable []string, functions map[string]*ast.FuncDecl, parsedFiles map[string]*parsedSource) bool {
	// Return-false helper branches are the source of rejected members. If the
	// member has a concrete string witness, retain it only when the AST shows a
	// reachable caller refusal condition changes from true to false when that
	// helper changes from rejecting to accepting the witness.
	if member.mutationKind == "bool-return-allow" {
		if witness, ok := curatorRuneWitnessForMember(functions[member.targetFunction], member.exemption); ok {
			return curatorRuneHelperMemberChangesSite(owner, targetNode, member, witness, reachable, functions, parsedFiles)
		}
		return true
	}
	if member.targetReturn != "return false" || member.mutationKind != "" {
		return true
	}
	helper := functions[member.targetFunction]
	helperSource := parsedFiles[sourcePathForFunction(parsedFiles, member.targetFunction)]
	if helper == nil || helperSource == nil {
		return true
	}
	parameter, witness, ok := curatorStringWitnessForHelperMember(helperSource.fset, helper, member.exemption)
	if !ok {
		return true
	}
	callers := make([]string, 0, len(reachable)+1)
	callers = append(callers, owner.Function)
	for _, name := range reachable {
		if name != owner.Function && !containsString(callers, name) {
			callers = append(callers, name)
		}
	}
	for _, callerName := range callers {
		caller := functions[callerName]
		callerSource := parsedFiles[sourcePathForFunction(parsedFiles, callerName)]
		if caller == nil || callerSource == nil {
			continue
		}
		scopes := []ast.Node{caller.Body}
		if callerName == owner.Function {
			if targetNode == nil {
				continue
			}
			scopes = []ast.Node{targetNode}
		}
		for _, scope := range scopes {
			var calls []*ast.CallExpr
			ast.Inspect(scope, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if isCall && callName(call) == member.targetFunction {
					calls = append(calls, call)
				}
				return true
			})
			for _, call := range calls {
				parameterIndex := stringParameterIndex(helper, parameter)
				if parameterIndex < 0 || parameterIndex >= len(call.Args) {
					continue
				}
				witnessValues := map[string]any{normalizedNode(callerSource.fset, call.Args[parameterIndex]): witness}
				addRangeWitnessValues(callerSource.fset, caller, call.Args[parameterIndex], witness, witnessValues)
				if earlierRefusalMakesCallUnreachable(callerSource.fset, caller, call, witnessValues) {
					continue
				}
				var affects bool
				ast.Inspect(scope, func(node ast.Node) bool {
					branch, isBranch := node.(*ast.IfStmt)
					if !isBranch || !nodeContains(branch.Cond, call) || !helperCallerBranchRefuses(caller, branch.Body) {
						return true
					}
					before, beforeKnown := evaluateValidatorWitnessCondition(callerSource.fset, caller, branch.Cond, call, false, witnessValues, nil)
					after, afterKnown := evaluateValidatorWitnessCondition(callerSource.fset, caller, branch.Cond, call, true, witnessValues, nil)
					if refusalConditionMayBeLifted(before, beforeKnown, after, afterKnown) {
						affects = true
						return false
					}
					return true
				})
				if affects {
					return true
				}
			}
		}
	}
	return false
}

func curatorRuneWitnessForMember(function *ast.FuncDecl, expression string) (rune, bool) {
	if function == nil || function.Type.Params == nil {
		return 0, false
	}
	var parameter string
	for _, field := range function.Type.Params.List {
		typeName, ok := field.Type.(*ast.Ident)
		if !ok || typeName.Name != "rune" || len(field.Names) != 1 {
			continue
		}
		parameter = field.Names[0].Name
		break
	}
	if parameter == "" {
		return 0, false
	}
	parsed, err := parser.ParseExpr(expression)
	if err != nil {
		return 0, false
	}
	var witness rune
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		comparison, ok := node.(*ast.BinaryExpr)
		if !ok || comparison.Op != token.EQL {
			return true
		}
		for _, pair := range [][2]ast.Expr{{comparison.X, comparison.Y}, {comparison.Y, comparison.X}} {
			identifier, isIdentifier := pair[0].(*ast.Ident)
			literal, isLiteral := pair[1].(*ast.BasicLit)
			if !isIdentifier || identifier.Name != parameter || !isLiteral || literal.Kind != token.CHAR {
				continue
			}
			value, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr == nil && len([]rune(value)) == 1 {
				witness = []rune(value)[0]
				found = true
				return false
			}
		}
		return true
	})
	return witness, found
}

func curatorRuneHelperMemberChangesSite(owner refusalscan.Site, targetNode ast.Node, member curatorValidatorClassMember, witness rune, reachable []string, functions map[string]*ast.FuncDecl, parsedFiles map[string]*parsedSource) bool {
	var wrappers []string
	for _, name := range reachable {
		if name == member.targetFunction {
			continue
		}
		wrapper := functions[name]
		wrapperSource := parsedFiles[sourcePathForFunction(parsedFiles, name)]
		if wrapper == nil || wrapperSource == nil || stringParameterName(wrapper) == "" {
			continue
		}
		parameter := stringParameterName(wrapper)
		localValues := helperLocalExpressionValues(wrapper, wrapperSource.fset)
		ast.Inspect(wrapper.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || callName(call) != member.targetFunction {
				return true
			}
			index := runeParameterIndex(functions[member.targetFunction])
			if index < 0 || index >= len(call.Args) {
				return true
			}
			value, ok := stringWitnessForRuneCall(wrapperSource.fset, wrapper, call.Args[index], parameter, localValues, witness, functions, parsedFiles)
			if ok && !containsString(wrappers, wrapper.Name.Name+"\x00"+value) {
				wrappers = append(wrappers, wrapper.Name.Name+"\x00"+value)
			}
			return true
		})
	}
	if len(wrappers) == 0 {
		return true
	}
	callers := make([]string, 0, len(reachable)+1)
	callers = append(callers, owner.Function)
	for _, name := range reachable {
		if name != owner.Function && !containsString(callers, name) {
			callers = append(callers, name)
		}
	}
	for _, encoded := range wrappers {
		parts := strings.SplitN(encoded, "\x00", 2)
		wrapperName, witnessValue := parts[0], parts[1]
		wrapper := functions[wrapperName]
		if wrapper == nil {
			continue
		}
		parameterName := stringParameterName(wrapper)
		parameterIndex := stringParameterIndex(wrapper, parameterName)
		for _, callerName := range callers {
			caller := functions[callerName]
			callerSource := parsedFiles[sourcePathForFunction(parsedFiles, callerName)]
			if caller == nil || callerSource == nil {
				continue
			}
			scopes := []ast.Node{caller.Body}
			if callerName == owner.Function {
				if targetNode == nil {
					continue
				}
				scopes = []ast.Node{targetNode}
			}
			for _, scope := range scopes {
				var calls []*ast.CallExpr
				ast.Inspect(scope, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if ok && callName(call) == wrapperName {
						calls = append(calls, call)
					}
					return true
				})
				for _, call := range calls {
					if parameterIndex < 0 || parameterIndex >= len(call.Args) {
						continue
					}
					values := map[string]any{normalizedNode(callerSource.fset, call.Args[parameterIndex]): witnessValue}
					if earlierRefusalMakesCallUnreachable(callerSource.fset, caller, call, values) {
						continue
					}
					affects := false
					ast.Inspect(scope, func(node ast.Node) bool {
						branch, ok := node.(*ast.IfStmt)
						if !ok || !nodeContains(branch.Cond, call) || !helperCallerBranchRefuses(caller, branch.Body) {
							return true
						}
						before, beforeKnown := evaluateValidatorWitnessCondition(callerSource.fset, caller, branch.Cond, call, false, values, nil)
						after, afterKnown := evaluateValidatorWitnessCondition(callerSource.fset, caller, branch.Cond, call, true, values, nil)
						if refusalConditionMayBeLifted(before, beforeKnown, after, afterKnown) {
							affects = true
							return false
						}
						return true
					})
					if affects {
						return true
					}
				}
			}
		}
	}
	return false
}

func refusalConditionMayBeLifted(before bool, beforeKnown bool, after bool, afterKnown bool) bool {
	if beforeKnown && !before || afterKnown && after {
		return false
	}
	if beforeKnown && afterKnown {
		return before && !after
	}
	return true
}

func stringParameterName(function *ast.FuncDecl) string {
	if function == nil || function.Type.Params == nil {
		return ""
	}
	for _, field := range function.Type.Params.List {
		if typeName, ok := field.Type.(*ast.Ident); ok && typeName.Name == "string" && len(field.Names) == 1 {
			return field.Names[0].Name
		}
	}
	return ""
}

func runeParameterIndex(function *ast.FuncDecl) int {
	if function == nil || function.Type.Params == nil {
		return -1
	}
	index := 0
	for _, field := range function.Type.Params.List {
		for range field.Names {
			typeName, ok := field.Type.(*ast.Ident)
			if ok && typeName.Name == "rune" {
				return index
			}
			index++
		}
	}
	return -1
}

func stringWitnessForRuneCall(fset *token.FileSet, function *ast.FuncDecl, argument ast.Expr, stringParameter string, localValues map[string]string, witness rune, functions map[string]*ast.FuncDecl, parsedFiles map[string]*parsedSource) (string, bool) {
	if function == nil || function.Body == nil {
		return "", false
	}
	if identifier, ok := argument.(*ast.Ident); ok {
		var rangeOverInput bool
		ast.Inspect(function.Body, func(node ast.Node) bool {
			loop, ok := node.(*ast.RangeStmt)
			if !ok {
				return true
			}
			value, ok := loop.Value.(*ast.Ident)
			if !ok || value.Name != identifier.Name {
				return true
			}
			source := normalizedNode(fset, loop.X)
			for local, expanded := range localValues {
				if source == local {
					source = expanded
				}
			}
			if strings.Contains(source, stringParameter) {
				rangeOverInput = true
			}
			return true
		})
		if rangeOverInput {
			return string(witness), true
		}
	}
	expression := normalizedNode(fset, argument)
	for pass := 0; pass < 4; pass++ {
		updated, err := replaceExpressionIdentifiers(expression, localValues)
		if err != nil {
			return "", false
		}
		expression = updated
	}
	parsed, err := parser.ParseExpr(expression)
	if err != nil {
		return "", false
	}
	acceptedRune, hasAcceptedRune := acceptedRuneWitness(functions, parsedFiles)
	if !hasAcceptedRune {
		return "", false
	}
	var result string
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		index, ok := node.(*ast.IndexExpr)
		if !ok || !strings.Contains(normalizedNode(fset, index.X), "[]rune("+stringParameter+")") {
			return true
		}
		if literal, ok := index.Index.(*ast.BasicLit); ok && literal.Kind == token.INT && literal.Value == "0" {
			result, found = string(witness)+string(acceptedRune), true
			return false
		}
		if boundary, ok := index.Index.(*ast.BinaryExpr); ok && boundary.Op == token.SUB {
			if one, ok := boundary.Y.(*ast.BasicLit); ok && one.Kind == token.INT && one.Value == "1" && callNameFromExpr(boundary.X) == "len" {
				result, found = string(acceptedRune)+string(witness), true
				return false
			}
		}
		return true
	})
	return result, found
}

func acceptedRuneWitness(functions map[string]*ast.FuncDecl, parsedFiles map[string]*parsedSource) (rune, bool) {
	function := functions["curatorAlphaNumeric"]
	if function == nil || function.Body == nil || function.Type.Params == nil || len(function.Type.Params.List) == 0 || len(function.Type.Params.List[0].Names) == 0 {
		return 0, false
	}
	parameter := function.Type.Params.List[0].Names[0].Name
	var source *parsedSource
	for _, candidate := range parsedFiles {
		if nodeContains(candidate.file, function) {
			source = candidate
			break
		}
	}
	if source == nil {
		return 0, false
	}
	var expression ast.Expr
	ast.Inspect(function.Body, func(node ast.Node) bool {
		returned, ok := node.(*ast.ReturnStmt)
		if ok && len(returned.Results) == 1 {
			expression = returned.Results[0]
			return false
		}
		return true
	})
	if expression == nil {
		return 0, false
	}
	var candidates []rune
	ast.Inspect(expression, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.CHAR {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil || len([]rune(value)) != 1 {
			return true
		}
		character := []rune(value)[0]
		for _, candidate := range []rune{character - 1, character, character + 1} {
			if candidate >= 0 && candidate <= unicode.MaxRune && (candidate < 0xD800 || candidate > 0xDFFF) && !containsRune(candidates, candidate) {
				candidates = append(candidates, candidate)
			}
		}
		return true
	})
	for _, candidate := range candidates {
		if value, ok := evaluateRuneCondition(source.fset, expression, parameter, candidate); ok && value {
			return candidate, true
		}
	}
	return 0, false
}

func evaluateRuneCondition(fset *token.FileSet, expression ast.Expr, parameter string, witness rune) (bool, bool) {
	switch typed := expression.(type) {
	case *ast.ParenExpr:
		return evaluateRuneCondition(fset, typed.X, parameter, witness)
	case *ast.UnaryExpr:
		if typed.Op == token.NOT {
			value, ok := evaluateRuneCondition(fset, typed.X, parameter, witness)
			return !value, ok
		}
	case *ast.BinaryExpr:
		if typed.Op == token.LAND || typed.Op == token.LOR {
			left, leftOK := evaluateRuneCondition(fset, typed.X, parameter, witness)
			right, rightOK := evaluateRuneCondition(fset, typed.Y, parameter, witness)
			if !leftOK || !rightOK {
				return false, false
			}
			if typed.Op == token.LAND {
				return left && right, true
			}
			return left || right, true
		}
		left, leftOK := runeScalar(fset, typed.X, parameter, witness)
		right, rightOK := runeScalar(fset, typed.Y, parameter, witness)
		if leftOK && rightOK {
			return compareWitnessScalars(int(left), int(right), typed.Op)
		}
	case *ast.Ident:
		if typed.Name == "true" {
			return true, true
		}
		if typed.Name == "false" {
			return false, true
		}
	}
	return false, false
}

func runeScalar(fset *token.FileSet, expression ast.Expr, parameter string, witness rune) (rune, bool) {
	switch typed := expression.(type) {
	case *ast.ParenExpr:
		return runeScalar(fset, typed.X, parameter, witness)
	case *ast.Ident:
		if typed.Name == parameter {
			return witness, true
		}
	case *ast.BasicLit:
		if typed.Kind == token.CHAR {
			value, err := strconv.Unquote(typed.Value)
			if err == nil && len([]rune(value)) == 1 {
				return []rune(value)[0], true
			}
		}
	}
	_ = fset
	return 0, false
}

func stringParameterIndex(function *ast.FuncDecl, name string) int {
	if function == nil || function.Type.Params == nil {
		return -1
	}
	index := 0
	for _, field := range function.Type.Params.List {
		for _, parameter := range field.Names {
			if parameter.Name == name {
				return index
			}
			index++
		}
	}
	return -1
}

func curatorStringWitnessForHelperMember(fset *token.FileSet, helper *ast.FuncDecl, expression string) (string, string, bool) {
	if helper == nil || helper.Type.Params == nil {
		return "", "", false
	}
	var parameter string
	for _, field := range helper.Type.Params.List {
		if typeName, ok := field.Type.(*ast.Ident); !ok || typeName.Name != "string" {
			continue
		}
		if len(field.Names) != 1 {
			return "", "", false
		}
		parameter = field.Names[0].Name
		break
	}
	if parameter == "" {
		return "", "", false
	}
	localValues := helperLocalExpressionValues(helper, fset)
	for pass := 0; pass < 4; pass++ {
		updated, err := replaceExpressionIdentifiers(expression, localValues)
		if err != nil {
			return "", "", false
		}
		expression = updated
	}
	parsed, err := parser.ParseExpr(expression)
	if err != nil {
		return "", "", false
	}
	var witness string
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		if found {
			return false
		}
		comparison, ok := node.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if call, ok := comparison.X.(*ast.CallExpr); ok && callName(call) == "len" && len(call.Args) == 1 && expressionHasIdentifier(call.Args[0], parameter) {
			if bound, valid := integerWitnessForComparison(fset, comparison.Op, comparison.Y); valid {
				witness, found = strings.Repeat("a", bound), true
				return false
			}
		}
		if call, ok := comparison.Y.(*ast.CallExpr); ok && callName(call) == "len" && len(call.Args) == 1 && expressionHasIdentifier(call.Args[0], parameter) {
			if bound, valid := integerWitnessForComparison(fset, reverseComparison(comparison.Op), comparison.X); valid {
				witness, found = strings.Repeat("a", bound), true
				return false
			}
		}
		if literal, ok := comparison.Y.(*ast.BasicLit); ok && literal.Kind == token.STRING && expressionHasIdentifier(comparison.X, parameter) && comparison.Op == token.EQL {
			witness, err = strconv.Unquote(literal.Value)
			found = err == nil
			return false
		}
		if literal, ok := comparison.X.(*ast.BasicLit); ok && literal.Kind == token.STRING && expressionHasIdentifier(comparison.Y, parameter) && comparison.Op == token.EQL {
			witness, err = strconv.Unquote(literal.Value)
			found = err == nil
			return false
		}
		return true
	})
	if !found {
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || (callName(call) != "HasPrefix" && callName(call) != "HasSuffix") || len(call.Args) != 2 || !expressionHasIdentifier(call.Args[0], parameter) {
				return true
			}
			literal, ok := call.Args[1].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			prefix, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil {
				return true
			}
			if callName(call) == "HasPrefix" {
				witness = prefix + "MUTANT"
			} else {
				witness = "MUTANT" + prefix
			}
			found = true
			return false
		})
	}
	if !found || len([]rune(witness)) > 4096 {
		return "", "", false
	}
	return parameter, witness, true
}

func expressionHasIdentifier(expression ast.Expr, name string) bool {
	if expression == nil {
		return false
	}
	identifiers := expressionIdentifierSet(expression)
	return identifiers[name]
}

func integerWitnessForComparison(fset *token.FileSet, operator token.Token, expression ast.Expr) (int, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.INT {
		return 0, false
	}
	bound, err := strconv.Atoi(literal.Value)
	if err != nil {
		return 0, false
	}
	switch operator {
	case token.EQL, token.GEQ, token.LEQ:
		return bound, bound >= 0
	case token.GTR:
		return bound + 1, bound >= 0
	case token.LSS:
		return bound - 1, bound > 0
	}
	_ = fset
	return 0, false
}

func addRangeWitnessValues(fset *token.FileSet, function *ast.FuncDecl, argument ast.Expr, witness string, values map[string]any) {
	identifier, ok := argument.(*ast.Ident)
	if !ok || function == nil || function.Body == nil {
		return
	}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		loop, ok := node.(*ast.RangeStmt)
		if !ok {
			return true
		}
		value, ok := loop.Value.(*ast.Ident)
		if !ok || value.Name != identifier.Name {
			return true
		}
		call, ok := loop.X.(*ast.CallExpr)
		if !ok || callName(call) != "Split" || len(call.Args) != 2 {
			return true
		}
		values[normalizedNode(fset, call.Args[0])] = witness
		return true
	})
}

func earlierRefusalMakesCallUnreachable(fset *token.FileSet, function *ast.FuncDecl, call *ast.CallExpr, values map[string]any) bool {
	if function == nil || function.Body == nil {
		return false
	}
	blocked := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok || !nodeContains(block, call) {
			return true
		}
		for index, statement := range block.List {
			if nodeContains(statement, call) {
				for _, prior := range block.List[:index] {
					branch, ok := prior.(*ast.IfStmt)
					if !ok || !helperCallerBranchRefuses(function, branch.Body) {
						continue
					}
					condition, known := evaluateValidatorWitnessCondition(fset, function, branch.Cond, nil, false, values, nil)
					if known && condition {
						blocked = true
						return false
					}
				}
				return false
			}
		}
		return true
	})
	return blocked
}

func helperCallerBranchRefuses(function *ast.FuncDecl, block *ast.BlockStmt) bool {
	if function == nil || block == nil || function.Type.Results == nil || len(function.Type.Results.List) == 0 {
		return false
	}
	lastResult := function.Type.Results.List[len(function.Type.Results.List)-1].Type
	if resultType, ok := lastResult.(*ast.Ident); ok && resultType.Name == "bool" {
		for _, returned := range boolReturnsInBlock(block) {
			if len(returned.Results) == 1 {
				if literal, ok := returned.Results[0].(*ast.Ident); ok && literal.Name == "false" {
					return true
				}
			}
		}
		return false
	}
	for _, returned := range returnsInBlock(block) {
		if len(returned.Results) == 1 {
			if literal, ok := returned.Results[0].(*ast.Ident); ok && literal.Name == "nil" {
				continue
			}
			return true
		}
	}
	return false
}

type nilWitnessValue struct{}

func evaluateValidatorWitnessCondition(fset *token.FileSet, function *ast.FuncDecl, expression ast.Expr, targetCall *ast.CallExpr, targetResult bool, values, locals map[string]any) (bool, bool) {
	switch typed := expression.(type) {
	case *ast.ParenExpr:
		return evaluateValidatorWitnessCondition(fset, function, typed.X, targetCall, targetResult, values, locals)
	case *ast.Ident:
		if typed.Name == "true" {
			return true, true
		}
		if typed.Name == "false" {
			return false, true
		}
		if value, ok := locals[typed.Name]; ok {
			if boolean, ok := value.(bool); ok {
				return boolean, true
			}
		}
	case *ast.UnaryExpr:
		if typed.Op == token.NOT {
			value, known := evaluateValidatorWitnessCondition(fset, function, typed.X, targetCall, targetResult, values, locals)
			return !value, known
		}
	case *ast.BinaryExpr:
		switch typed.Op {
		case token.LAND, token.LOR:
			left, leftKnown := evaluateValidatorWitnessCondition(fset, function, typed.X, targetCall, targetResult, values, locals)
			right, rightKnown := evaluateValidatorWitnessCondition(fset, function, typed.Y, targetCall, targetResult, values, locals)
			if typed.Op == token.LAND {
				if leftKnown && !left || rightKnown && !right {
					return false, true
				}
				if leftKnown && rightKnown {
					return true, true
				}
			} else {
				if leftKnown && left || rightKnown && right {
					return true, true
				}
				if leftKnown && rightKnown {
					return false, true
				}
			}
			return false, false
		case token.EQL, token.NEQ, token.GEQ, token.GTR, token.LEQ, token.LSS:
			left, leftKnown := evaluateValidatorWitnessScalar(fset, function, typed.X, targetCall, targetResult, values, locals)
			right, rightKnown := evaluateValidatorWitnessScalar(fset, function, typed.Y, targetCall, targetResult, values, locals)
			if !leftKnown || !rightKnown {
				return false, false
			}
			_, leftIsNil := left.(nilWitnessValue)
			_, rightIsNil := right.(nilWitnessValue)
			if leftIsNil && rightIsNil {
				return typed.Op == token.EQL || typed.Op == token.LEQ || typed.Op == token.GEQ, true
			}
			if _, leftNil := left.(nilWitnessValue); leftNil {
				return typed.Op == token.NEQ, true
			}
			if _, rightNil := right.(nilWitnessValue); rightNil {
				return typed.Op == token.NEQ, true
			}
			return compareWitnessScalars(left, right, typed.Op)
		}
	case *ast.CallExpr:
		if targetCall != nil && normalizedNode(fset, typed) == normalizedNode(fset, targetCall) {
			return targetResult, true
		}
		if typed.Fun != nil {
			if value, known := evaluateValidatorStringCall(fset, typed, values); known {
				return value, true
			}
		}
		if value, known := evaluateValidatorLocalCall(fset, function, typed, targetCall, targetResult, values, locals); known {
			return value, true
		}
	}
	return false, false
}

func evaluateValidatorWitnessScalar(fset *token.FileSet, function *ast.FuncDecl, expression ast.Expr, targetCall *ast.CallExpr, targetResult bool, values, locals map[string]any) (any, bool) {
	if expression == nil {
		return nil, false
	}
	if value, ok := values[normalizedNode(fset, expression)]; ok {
		return value, true
	}
	switch typed := expression.(type) {
	case *ast.ParenExpr:
		return evaluateValidatorWitnessScalar(fset, function, typed.X, targetCall, targetResult, values, locals)
	case *ast.Ident:
		switch typed.Name {
		case "true":
			return true, true
		case "false":
			return false, true
		case "nil":
			return nilWitnessValue{}, true
		}
		if value, ok := locals[typed.Name]; ok {
			return value, true
		}
	case *ast.BasicLit:
		switch typed.Kind {
		case token.STRING:
			value, err := strconv.Unquote(typed.Value)
			return value, err == nil
		case token.INT:
			value, err := strconv.Atoi(typed.Value)
			return value, err == nil
		case token.CHAR:
			value, err := strconv.Unquote(typed.Value)
			if err == nil && len([]rune(value)) == 1 {
				return int([]rune(value)[0]), true
			}
		}
	case *ast.SelectorExpr:
		if value, ok := values[normalizedNode(fset, typed)]; ok {
			return value, true
		}
		if receiver, ok := typed.X.(*ast.Ident); ok && receiver.Name == "descriptor" {
			if typed.Sel.Name == "With" {
				return nilWitnessValue{}, true
			}
			return "", true
		}
	case *ast.CallExpr:
		if targetCall != nil && normalizedNode(fset, typed) == normalizedNode(fset, targetCall) {
			return targetResult, true
		}
		if name := callName(typed); name == "len" && len(typed.Args) == 1 {
			if value, ok := values[normalizedNode(fset, typed.Args[0])].(string); ok {
				return len([]rune(value)), true
			}
		}
		if name := callName(typed); name == "RuneCountInString" && len(typed.Args) == 1 {
			if value, ok := values[normalizedNode(fset, typed.Args[0])].(string); ok {
				return len([]rune(value)), true
			}
		}
		if value, known := evaluateValidatorLocalCall(fset, function, typed, targetCall, targetResult, values, locals); known {
			return value, true
		}
		if value, known := evaluateValidatorStringCall(fset, typed, values); known {
			return value, true
		}
	case *ast.UnaryExpr:
		if typed.Op == token.NOT {
			value, known := evaluateValidatorWitnessCondition(fset, function, typed.X, targetCall, targetResult, values, locals)
			return !value, known
		}
	}
	return nil, false
}

func evaluateValidatorStringCall(fset *token.FileSet, call *ast.CallExpr, values map[string]any) (bool, bool) {
	if call == nil || len(call.Args) == 0 {
		return false, false
	}
	first, ok := values[normalizedNode(fset, call.Args[0])].(string)
	if !ok || len(call.Args) < 2 {
		return false, false
	}
	literal, ok := call.Args[1].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return false, false
	}
	second, err := strconv.Unquote(literal.Value)
	if err != nil {
		return false, false
	}
	switch callName(call) {
	case "Contains", "ContainsAny":
		return strings.Contains(first, second), true
	case "HasPrefix":
		return strings.HasPrefix(first, second), true
	case "HasSuffix":
		return strings.HasSuffix(first, second), true
	}
	return false, false
}

func evaluateValidatorLocalCall(fset *token.FileSet, function *ast.FuncDecl, call *ast.CallExpr, targetCall *ast.CallExpr, targetResult bool, values, locals map[string]any) (bool, bool) {
	if function == nil || function.Body == nil {
		return false, false
	}
	identifier, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false, false
	}
	var literal *ast.FuncLit
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if assignment, ok := node.(*ast.AssignStmt); ok && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 {
			left, leftOK := assignment.Lhs[0].(*ast.Ident)
			right, rightOK := assignment.Rhs[0].(*ast.FuncLit)
			if leftOK && rightOK && left.Name == identifier.Name {
				literal = right
				return false
			}
		}
		if valueSpec, ok := node.(*ast.ValueSpec); ok && len(valueSpec.Names) == 1 && len(valueSpec.Values) == 1 {
			right, rightOK := valueSpec.Values[0].(*ast.FuncLit)
			if rightOK && valueSpec.Names[0].Name == identifier.Name {
				literal = right
				return false
			}
		}
		return true
	})
	if literal == nil || literal.Type.Params == nil || callArgumentCount(literal.Type.Params) != len(call.Args) {
		return false, false
	}
	localValues := make(map[string]any, len(locals)+len(call.Args))
	for name, value := range locals {
		localValues[name] = value
	}
	argumentIndex := 0
	for _, field := range literal.Type.Params.List {
		for _, name := range field.Names {
			if argumentIndex >= len(call.Args) {
				return false, false
			}
			value, known := evaluateValidatorWitnessScalar(fset, function, call.Args[argumentIndex], targetCall, targetResult, values, locals)
			if !known {
				return false, false
			}
			localValues[name.Name] = value
			argumentIndex++
		}
	}
	for _, statement := range literal.Body.List {
		if returned, ok := statement.(*ast.ReturnStmt); ok && len(returned.Results) == 1 {
			return evaluateValidatorWitnessCondition(fset, function, returned.Results[0], targetCall, targetResult, values, localValues)
		}
	}
	return false, false
}

func callArgumentCount(parameters *ast.FieldList) int {
	if parameters == nil {
		return 0
	}
	count := 0
	for _, field := range parameters.List {
		count += len(field.Names)
	}
	return count
}

func deriveBooleanHelperDownstreamProofs(member curatorValidatorClassMember, parsedFiles map[string]*parsedSource) []downstreamGuardProof {
	if member.mutationKind != "bool-return-allow" && member.mutationKind != "bool-return-exempt" {
		var helper *ast.FuncDecl
		for _, source := range parsedFiles {
			if function := findFunctionAndBody(source.file, member.targetFunction); function != nil {
				helper = function
				break
			}
		}
		if helper == nil || !curatorBooleanFunction(helper) {
			return nil
		}
	}
	exemption, err := parser.ParseExpr(member.exemption)
	if err != nil {
		return nil
	}
	witnessIdentifiers := expressionIdentifierSet(exemption)
	if len(witnessIdentifiers) == 0 {
		return nil
	}
	var result []downstreamGuardProof
	seen := make(map[string]bool)
	for relative, source := range parsedFiles {
		for _, declaration := range source.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || function.Name.Name == member.targetFunction {
				continue
			}
			if !containsString(callsFromNodes(function.Body), member.targetFunction) {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				branch, ok := node.(*ast.IfStmt)
				if !ok || !intersectsIdentifiers(witnessIdentifiers, expressionIdentifierSet(branch.Cond)) {
					return true
				}
				for _, returned := range boolReturnsInBlock(branch.Body) {
					if len(returned.Results) != 1 {
						continue
					}
					literal, ok := returned.Results[0].(*ast.Ident)
					if !ok || literal.Name != "false" {
						continue
					}
					line := source.fset.Position(returned.Pos()).Line
					guard := "if " + normalizedNode(source.fset, branch.Cond)
					description := fmt.Sprintf("%s:%d %s %s (return false)", relative, source.fset.Position(branch.Pos()).Line, function.Name.Name, guard)
					key := fmt.Sprintf("%s\x00%s\x00%d\x00%s", relative, function.Name.Name, line, guard)
					if !seen[key] {
						seen[key] = true
						result = append(result, downstreamGuardProof{
							file: relative, function: function.Name.Name, guard: guard,
							line: line, returned: "return false", exemption: member.exemption, description: description,
						})
					}
				}
				return true
			})
		}
	}
	result = append(result, deriveCallerArgumentDownstreamProofs(member, parsedFiles)...)
	result = append(result, deriveSameFunctionRangeDownstreamProofs(member, parsedFiles)...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].file != result[j].file {
			return result[i].file < result[j].file
		}
		if result[i].line != result[j].line {
			return result[i].line < result[j].line
		}
		return result[i].guard < result[j].guard
	})
	return result
}

func deriveCallerArgumentDownstreamProofs(member curatorValidatorClassMember, parsedFiles map[string]*parsedSource) []downstreamGuardProof {
	var helper *ast.FuncDecl
	var helperFileSet *token.FileSet
	for _, source := range parsedFiles {
		if function := findFunctionAndBody(source.file, member.targetFunction); function != nil {
			helper = function
			helperFileSet = source.fset
			break
		}
	}
	if helper == nil || helper.Type.Params == nil {
		return nil
	}
	localValues := helperLocalExpressionValues(helper, helperFileSet)
	var result []downstreamGuardProof
	seen := make(map[string]bool)
	for relative, source := range parsedFiles {
		for _, declaration := range source.file.Decls {
			caller, ok := declaration.(*ast.FuncDecl)
			if !ok || caller.Body == nil || caller.Name.Name == member.targetFunction {
				continue
			}
			var calls []*ast.CallExpr
			ast.Inspect(caller.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok && callName(call) == member.targetFunction {
					calls = append(calls, call)
				}
				return true
			})
			for _, call := range calls {
				witness, ok := helperWitnessAtCall(source.fset, helper, call, localValues, member.exemption)
				if !ok || len(call.Args) == 0 {
					continue
				}
				argument := normalizedNode(source.fset, call.Args[0])
				ast.Inspect(caller.Body, func(node ast.Node) bool {
					branch, ok := node.(*ast.IfStmt)
					if !ok || conditionCallsFunction(branch.Cond, member.targetFunction) || !expressionContainsSubtree(source.fset, branch.Cond, argument) {
						return true
					}
					for _, returned := range returnsInBlock(branch.Body) {
						line := source.fset.Position(returned.Pos()).Line
						guard := "if " + normalizedNode(source.fset, branch.Cond)
						description := fmt.Sprintf("%s:%d %s %s (%s); witness=%s", relative, source.fset.Position(branch.Pos()).Line, caller.Name.Name, guard, normalizedNode(source.fset, returned), witness)
						key := fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s", relative, caller.Name.Name, line, guard, witness)
						if !seen[key] {
							seen[key] = true
							result = append(result, downstreamGuardProof{
								file: relative, function: caller.Name.Name, guard: guard,
								line: line, returned: normalizedNode(source.fset, returned),
								exemption: witness, description: description,
							})
						}
					}
					return true
				})
			}
		}
	}
	return result
}

func helperLocalExpressionValues(function *ast.FuncDecl, fset *token.FileSet) map[string]string {
	result := make(map[string]string)
	if function == nil || function.Body == nil {
		return result
	}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			if len(statement.Lhs) == 1 && len(statement.Rhs) == 1 {
				if identifier, ok := statement.Lhs[0].(*ast.Ident); ok {
					result[identifier.Name] = normalizedNode(fset, statement.Rhs[0])
				}
			}
		case *ast.ValueSpec:
			if len(statement.Names) == 1 && len(statement.Values) == 1 {
				result[statement.Names[0].Name] = normalizedNode(fset, statement.Values[0])
			}
		}
		return true
	})
	return result
}

func helperWitnessAtCall(fset *token.FileSet, helper *ast.FuncDecl, call *ast.CallExpr, localValues map[string]string, memberExpression string) (string, bool) {
	parameters := helper.Type.Params.List
	argumentByParameter := make(map[string]string)
	argumentIndex := 0
	for _, field := range parameters {
		for _, name := range field.Names {
			if argumentIndex >= len(call.Args) {
				return "", false
			}
			argumentByParameter[name.Name] = normalizedNode(fset, call.Args[argumentIndex])
			argumentIndex++
		}
	}
	if argumentIndex != len(call.Args) {
		return "", false
	}
	witness := memberExpression
	for pass := 0; pass < 4; pass++ {
		updated, err := replaceExpressionIdentifiers(witness, localValues)
		if err != nil {
			return "", false
		}
		witness = updated
	}
	for pass := 0; pass < 4; pass++ {
		updated, err := replaceExpressionIdentifiers(witness, argumentByParameter)
		if err != nil {
			return "", false
		}
		if updated == witness {
			break
		}
		witness = updated
	}
	if _, err := parser.ParseExpr(witness); err != nil {
		return "", false
	}
	return witness, true
}

func replaceExpressionIdentifiers(expression string, replacements map[string]string) (string, error) {
	if len(replacements) == 0 {
		return expression, nil
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseExprFrom(fset, "witness.go", expression, parser.AllErrors)
	if err != nil {
		return "", err
	}
	selectorNames := make(map[token.Pos]bool)
	ast.Inspect(parsed, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			selectorNames[selector.Sel.Pos()] = true
		}
		return true
	})
	type edit struct {
		start int
		end   int
		text  string
	}
	var edits []edit
	ast.Inspect(parsed, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok || selectorNames[identifier.Pos()] {
			return true
		}
		replacement, found := replacements[identifier.Name]
		if !found || replacement == identifier.Name {
			return true
		}
		edits = append(edits, edit{
			start: fset.Position(identifier.Pos()).Offset,
			end:   fset.Position(identifier.End()).Offset,
			text:  replacement,
		})
		return true
	})
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, change := range edits {
		expression = expression[:change.start] + change.text + expression[change.end:]
	}
	return expression, nil
}

func returnsInBlock(block *ast.BlockStmt) []*ast.ReturnStmt {
	var result []*ast.ReturnStmt
	ast.Inspect(block, func(node ast.Node) bool {
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		if returned, ok := node.(*ast.ReturnStmt); ok {
			result = append(result, returned)
		}
		return true
	})
	return result
}

func conditionCallsFunction(expression ast.Expr, functionName string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && callName(call) == functionName {
			found = true
		}
		return !found
	})
	return found
}

func expressionContainsSubtree(fset *token.FileSet, expression ast.Expr, expected string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if node != nil && normalizedNode(fset, node) == expected {
			found = true
		}
		return !found
	})
	return found
}

func deriveSameFunctionRangeDownstreamProofs(member curatorValidatorClassMember, parsedFiles map[string]*parsedSource) []downstreamGuardProof {
	var source *parsedSource
	var function *ast.FuncDecl
	for _, candidateSource := range parsedFiles {
		if candidate := findFunctionAndBody(candidateSource.file, member.targetFunction); candidate != nil {
			source = candidateSource
			function = candidate
			break
		}
	}
	if source == nil || function == nil || member.targetLine == 0 {
		return nil
	}
	witnessInput, emptySegmentWitness := splitEmptySegmentWitnessInput(member.exemption)
	if !emptySegmentWitness {
		return nil
	}
	var result []downstreamGuardProof
	seen := make(map[string]bool)
	ast.Inspect(function.Body, func(node ast.Node) bool {
		loop, ok := node.(*ast.RangeStmt)
		if !ok || source.fset.Position(loop.Pos()).Line <= member.targetLine || !rangeReadsWitness(source.fset, loop.X, witnessInput) {
			return true
		}
		value, ok := loop.Value.(*ast.Ident)
		if !ok || value.Name == "_" {
			return true
		}
		ast.Inspect(loop.Body, func(child ast.Node) bool {
			branch, ok := child.(*ast.IfStmt)
			if !ok || !conditionRejectsEmptyRangeValue(source.fset, branch.Cond, value.Name) {
				return true
			}
			for _, returned := range boolReturnsInBlock(branch.Body) {
				if len(returned.Results) != 1 {
					continue
				}
				literal, ok := returned.Results[0].(*ast.Ident)
				if !ok || literal.Name != "false" {
					continue
				}
				line := source.fset.Position(returned.Pos()).Line
				guard := "if " + normalizedNode(source.fset, branch.Cond)
				description := fmt.Sprintf("%s:%d %s %s (return false); empty range member derives from %s", member.targetFile, source.fset.Position(branch.Pos()).Line, member.targetFunction, guard, normalizedNode(source.fset, loop.X))
				key := fmt.Sprintf("%s\x00%d\x00%s", member.targetFunction, line, guard)
				if !seen[key] {
					seen[key] = true
					result = append(result, downstreamGuardProof{
						file: member.targetFile, function: member.targetFunction, guard: guard,
						line: line, returned: "return false", exemption: member.exemption,
						description: description,
					})
				}
			}
			return true
		})
		return false
	})
	return result
}

func splitEmptySegmentWitnessInput(expression string) (string, bool) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseExprFrom(fset, "empty-witness.go", expression, parser.AllErrors)
	if err != nil {
		return "", false
	}
	binary, ok := parsed.(*ast.BinaryExpr)
	if !ok || binary.Op != token.EQL {
		return "", false
	}
	for _, pair := range [][2]ast.Expr{{binary.X, binary.Y}, {binary.Y, binary.X}} {
		literal, ok := pair[1].(*ast.BasicLit)
		if !ok {
			continue
		}
		if literal.Kind == token.INT && literal.Value == "0" {
			call, ok := pair[0].(*ast.CallExpr)
			if !ok || len(call.Args) != 1 || callName(call) != "RuneCountInString" && callName(call) != "len" {
				continue
			}
			return normalizedNode(fset, call.Args[0]), true
		}
		if literal.Kind == token.STRING {
			value, err := strconv.Unquote(literal.Value)
			if err != nil || (!strings.HasPrefix(value, "/") && !strings.HasSuffix(value, "/") && !strings.Contains(value, "//")) {
				continue
			}
			return normalizedNode(fset, pair[0]), true
		}
	}
	return "", false
}

func rangeReadsWitness(fset *token.FileSet, expression ast.Expr, witnessInput string) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) < 2 {
		return false
	}
	selector, selectorOK := call.Fun.(*ast.SelectorExpr)
	if !selectorOK || selector.Sel.Name != "Split" {
		return false
	}
	packageName, packageOK := selector.X.(*ast.Ident)
	separator, separatorOK := call.Args[1].(*ast.BasicLit)
	if !packageOK || packageName.Name != "strings" || !separatorOK || separator.Kind != token.STRING || separator.Value != `"/"` {
		return false
	}
	return normalizedNode(fset, call.Args[0]) == witnessInput
}

func conditionRejectsEmptyRangeValue(fset *token.FileSet, expression ast.Expr, rangeValue string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		binary, ok := node.(*ast.BinaryExpr)
		if !ok || binary.Op != token.EQL {
			return true
		}
		for _, pair := range [][2]ast.Expr{{binary.X, binary.Y}, {binary.Y, binary.X}} {
			identifier, ok := pair[0].(*ast.Ident)
			literal, literalOK := pair[1].(*ast.BasicLit)
			if ok && identifier.Name == rangeValue && literalOK && literal.Kind == token.STRING && literal.Value == `""` {
				found = true
			}
		}
		return true
	})
	return found && strings.Contains(normalizedNode(fset, expression), rangeValue)
}

func expressionIdentifierSet(expression ast.Expr) map[string]bool {
	result := make(map[string]bool)
	excluded := make(map[token.Pos]bool)
	ast.Inspect(expression, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			excluded[typed.Sel.Pos()] = true
		case *ast.CallExpr:
			switch function := typed.Fun.(type) {
			case *ast.Ident:
				excluded[function.Pos()] = true
			case *ast.SelectorExpr:
				excluded[function.Sel.Pos()] = true
			}
		case *ast.ArrayType:
			if identifier, ok := typed.Elt.(*ast.Ident); ok {
				excluded[identifier.Pos()] = true
			}
		}
		return true
	})
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && !excluded[identifier.Pos()] && identifier.Name != "true" && identifier.Name != "false" {
			result[identifier.Name] = true
		}
		return true
	})
	return result
}

func intersectsIdentifiers(left, right map[string]bool) bool {
	for identifier := range left {
		if right[identifier] {
			return true
		}
	}
	return false
}

func booleanReturnMemberExemptions(fset *token.FileSet, expression ast.Expr) []conditionMember {
	var result []conditionMember
	seen := make(map[string]bool)
	for _, term := range topLevelOrTerms(expression) {
		if callNameFromExpr(term) == "curatorAlphaNumeric" {
			continue
		}
		binary, ok := term.(*ast.BinaryExpr)
		if !ok || (binary.Op != token.EQL && binary.Op != token.NEQ) {
			continue
		}
		left, right := binary.X, binary.Y
		literal, ok := right.(*ast.BasicLit)
		if !ok || literal.Kind != token.CHAR {
			literal, ok = left.(*ast.BasicLit)
			if !ok || literal.Kind != token.CHAR {
				continue
			}
			left = right
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil || len([]rune(value)) != 1 {
			continue
		}
		character := []rune(value)[0]
		witness := character + 1
		if witness < 0 || witness > unicode.MaxRune || (witness >= 0xD800 && witness <= 0xDFFF) {
			continue
		}
		exemption := normalizedNode(fset, left) + " == " + strconv.QuoteRuneToASCII(witness)
		if !seen[exemption] {
			seen[exemption] = true
			result = append(result, conditionMember{expression: exemption, label: "boundary witness derived from " + strconv.QuoteRuneToASCII(character)})
		}
	}
	return result
}

func callNameFromExpr(expression ast.Expr) string {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return ""
	}
	return callName(call)
}

func sourcePathForFunction(parsedFiles map[string]*parsedSource, name string) string {
	for relative, source := range parsedFiles {
		if findFunctionAndBody(source.file, name) != nil {
			return relative
		}
	}
	return ""
}

func curatorBooleanFunction(function *ast.FuncDecl) bool {
	if function.Type.Results == nil || len(function.Type.Results.List) != 1 {
		return false
	}
	identifier, ok := function.Type.Results.List[0].Type.(*ast.Ident)
	return ok && identifier.Name == "bool"
}

func boolReturnsInBlock(block *ast.BlockStmt) []*ast.ReturnStmt {
	var result []*ast.ReturnStmt
	ast.Inspect(block, func(node ast.Node) bool {
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		statement, ok := node.(*ast.ReturnStmt)
		if ok && len(statement.Results) == 1 {
			if literal, ok := statement.Results[0].(*ast.Ident); ok && (literal.Name == "false" || literal.Name == "true") {
				result = append(result, statement)
			}
		}
		return true
	})
	return result
}

func curatorReservedEnvironmentMembers(parsedFiles map[string]*parsedSource, function *ast.FuncDecl) []curatorValidatorClassMember {
	var result []curatorValidatorClassMember
	for relative, source := range parsedFiles {
		candidate := findFunctionAndBody(source.file, function.Name.Name)
		if candidate == nil {
			continue
		}
		var mapKeys []string
		ast.Inspect(candidate.Body, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, element := range literal.Elts {
				kv, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.BasicLit)
				if !ok || key.Kind != token.STRING {
					continue
				}
				if value, err := strconv.Unquote(key.Value); err == nil {
					mapKeys = append(mapKeys, value)
				}
			}
			return true
		})
		sort.Strings(mapKeys)
		ast.Inspect(candidate.Body, func(node ast.Node) bool {
			branch, ok := node.(*ast.IfStmt)
			if !ok || len(boolReturnsInBlock(branch.Body)) == 0 {
				return true
			}
			init, ok := branch.Init.(*ast.AssignStmt)
			if !ok || len(init.Rhs) == 0 {
				return true
			}
			index, ok := init.Rhs[0].(*ast.IndexExpr)
			if !ok {
				return true
			}
			name := normalizedNode(source.fset, index.Index)
			for _, raw := range branch.Body.List {
				returnStmt, ok := raw.(*ast.ReturnStmt)
				if !ok || len(returnStmt.Results) != 1 {
					continue
				}
				literal, ok := returnStmt.Results[0].(*ast.Ident)
				if !ok || literal.Name != "true" {
					continue
				}
				for _, key := range mapKeys {
					result = append(result, curatorValidatorClassMember{
						targetFile: relative, targetFunction: candidate.Name.Name,
						targetGuard:  "if " + normalizedNode(source.fset, branch.Cond),
						targetLine:   source.fset.Position(returnStmt.Pos()).Line,
						targetReturn: normalizedNode(source.fset, returnStmt),
						exemption:    name + " == " + strconv.Quote(key),
						member:       fmt.Sprintf("%s reserved map member %q", candidate.Name.Name, key),
						narrows:      "admits only the AST-enumerated reserved environment name " + strconv.Quote(key),
					})
				}
				break
			}
			return true
		})
		ast.Inspect(candidate.Body, func(node ast.Node) bool {
			returnStmt, ok := node.(*ast.ReturnStmt)
			if !ok || len(returnStmt.Results) != 1 {
				return true
			}
			expression := returnStmt.Results[0]
			calls := callsNamed(expression, "HasPrefix")
			for _, call := range calls {
				if len(call.Args) != 2 {
					continue
				}
				prefix, ok := call.Args[1].(*ast.BasicLit)
				if !ok || prefix.Kind != token.STRING {
					continue
				}
				prefixValue, err := strconv.Unquote(prefix.Value)
				if err != nil {
					continue
				}
				name := normalizedNode(source.fset, call.Args[0])
				value := prefixValue + "MUTANT"
				result = append(result, curatorValidatorClassMember{
					targetFile: relative, targetFunction: candidate.Name.Name,
					targetLine:   source.fset.Position(returnStmt.Pos()).Line,
					targetReturn: normalizedNode(source.fset, returnStmt), mutationKind: "bool-return-exempt",
					exemption: name + " == " + strconv.Quote(value),
					member:    fmt.Sprintf("%s reserved prefix member %q", candidate.Name.Name, value),
					narrows:   "admits only the AST-enumerated reserved prefix witness " + strconv.Quote(value),
				})
			}
			return true
		})
	}
	return result
}

func curatorAlphaNumericBoundaryMembers(parsedFiles map[string]*parsedSource, function *ast.FuncDecl) []curatorValidatorClassMember {
	var result []curatorValidatorClassMember
	for relative, source := range parsedFiles {
		candidate := findFunctionAndBody(source.file, function.Name.Name)
		if candidate == nil {
			continue
		}
		ast.Inspect(candidate.Body, func(node ast.Node) bool {
			returnStmt, ok := node.(*ast.ReturnStmt)
			if !ok || len(returnStmt.Results) != 1 {
				return true
			}
			expression := returnStmt.Results[0]
			var variable string
			var witnesses []rune
			ast.Inspect(expression, func(node ast.Node) bool {
				comparison, ok := node.(*ast.BinaryExpr)
				if !ok || len(comparison.Op.String()) == 0 {
					return true
				}
				identifier, idOK := comparison.X.(*ast.Ident)
				literal, litOK := comparison.Y.(*ast.BasicLit)
				if !idOK || !litOK || literal.Kind != token.CHAR {
					return true
				}
				bound, err := strconv.Unquote(literal.Value)
				if err != nil || len([]rune(bound)) != 1 {
					return true
				}
				value := []rune(bound)[0]
				var witness rune
				switch comparison.Op {
				case token.GEQ:
					witness = value - 1
				case token.GTR:
					witness = value
				case token.LEQ:
					witness = value + 1
				case token.LSS:
					witness = value
				default:
					return true
				}
				if witness < 0 || witness > unicode.MaxRune || (witness >= 0xD800 && witness <= 0xDFFF) {
					return true
				}
				variable = identifier.Name
				if !containsRune(witnesses, witness) {
					witnesses = append(witnesses, witness)
				}
				return true
			})
			for _, witness := range witnesses {
				result = append(result, curatorValidatorClassMember{
					targetFile: relative, targetFunction: candidate.Name.Name,
					targetLine:   source.fset.Position(returnStmt.Pos()).Line,
					targetReturn: normalizedNode(source.fset, returnStmt), mutationKind: "bool-return-allow",
					exemption: variable + " == " + strconv.QuoteRuneToASCII(witness),
					member:    fmt.Sprintf("%s range-bound witness %s", candidate.Name.Name, strconv.QuoteRuneToASCII(witness)),
					narrows:   "admits only one AST-derived character immediately outside a declared alphanumeric range",
				})
			}
			return true
		})
	}
	return result
}

func containsRune(values []rune, target rune) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateValidatorMutantCompleteness(expected []curatorValidatorClassMember, candidates []mutant, availableTests map[string]bool) error {
	want := make(map[string]curatorValidatorClassMember, len(expected))
	for _, member := range expected {
		if _, exists := want[member.identity]; exists {
			return fmt.Errorf("AST class enumeration produced duplicate member %q", member.identity)
		}
		want[member.identity] = member
		if !strings.HasPrefix(member.testName, "TestBuildPlan") || !availableTests[member.testName] {
			return fmt.Errorf("completeness check: AST-derived member has no named BuildPlan negative test: %s -> %s", member.identity, member.testName)
		}
	}
	got := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		identity := candidate.validatorMemberID
		if identity == "" {
			return fmt.Errorf("validator mutant %q has no AST-derived member identity", candidate.name)
		}
		if got[identity] {
			return fmt.Errorf("multiple narrowing mutants were generated for AST-derived member %q", identity)
		}
		got[identity] = true
	}
	for identity := range want {
		if !got[identity] {
			return fmt.Errorf("completeness check: AST-derived validator member has no narrowing mutant: %s", identity)
		}
	}
	for identity := range got {
		if _, exists := want[identity]; !exists {
			return fmt.Errorf("validator mutant was generated for an unenumerated AST member: %s", identity)
		}
	}
	return nil
}

func proveUnmappedSourceFileAttack(root, taskScratch, gitDir, gitIndex string) error {
	attackRoot, err := os.MkdirTemp(taskScratch, "curator-new-source-attack-")
	if err != nil {
		return fmt.Errorf("create isolated new-source attack tree: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(attackRoot); err != nil {
			fatal(fmt.Errorf("remove isolated new-source attack tree: %w", err))
		}
	}()
	if err := copyTree(root, attackRoot); err != nil {
		return fmt.Errorf("copy source tree for new-source attack: %w", err)
	}
	attackPath := filepath.Join(attackRoot, "pkg", "agentic", "zz_new.go")
	const source = `package agentic

func refusalFromReviewAttack() error {
	return ErrCuratorContextMalformed
}
`
	if err := os.WriteFile(attackPath, []byte(source), 0o600); err != nil {
		return fmt.Errorf("add reviewer new-source attack: %w", err)
	}
	output, exitCode := runGoTest(attackRoot, root, gitDir, gitIndex, "./pkg/agentic", "^TestCuratorPluginRefusalSitesHaveNamedBuildPlanCoverage$")
	want := "unmapped typed refusal return in the full agentic source tree: zz_new.go"
	if exitCode == 0 || !strings.Contains(output, want) {
		return fmt.Errorf("reviewer zz_new.go attack was not rejected by the full-tree guard (exit %d):\n%s", exitCode, output)
	}
	fmt.Printf("ATTACK KILLED | exit=%d | zz_new.go returns ErrCuratorContextMalformed | guard=TestCuratorPluginRefusalSitesHaveNamedBuildPlanCoverage\n", exitCode)
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func slugName(value string) string {
	var result strings.Builder
	for _, character := range strings.ToLower(value) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			result.WriteRune(character)
		} else if result.Len() > 0 && !strings.HasSuffix(result.String(), "-") {
			result.WriteByte('-')
		}
	}
	return strings.Trim(result.String(), "-")
}

func normalizeGuard(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "if ") || strings.HasPrefix(raw, "switch ") || raw == "unconditional" {
		return raw
	}
	return "if " + raw
}

func catalogSiteKey(file, function, guard, returned string, occurrence int) string {
	return refusalscan.Site{File: file, Function: function, Guard: normalizeGuard(guard), Return: returned, Occurrence: occurrence}.Key()
}

func containsSiteKey(sites []refusalscan.Site, key string) bool {
	for _, site := range sites {
		if site.Key() == key {
			return true
		}
	}
	return false
}

func resolveCatalogSite(row catalogMutation, sites []refusalscan.Site) (refusalscan.Site, error) {
	guard := normalizeGuard(row.guard)
	var matches []refusalscan.Site
	for _, site := range sites {
		if site.File != row.file || site.Function != row.function || site.Guard != guard {
			continue
		}
		if row.hasReturned && site.Return != row.returned {
			continue
		}
		if row.hasOccurrence && site.Occurrence != row.occurrence {
			continue
		}
		matches = append(matches, site)
	}
	if len(matches) != 1 {
		return refusalscan.Site{}, fmt.Errorf("site locator resolves to %d refusal returns: %s :: %s :: %s", len(matches), row.file, row.function, guard)
	}
	return matches[0], nil
}

func parseCatalogSlice(file *ast.File, variableName string) ([]map[string]string, error) {
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, spec := range general.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok || len(valueSpec.Values) != 1 {
				continue
			}
			found := false
			for _, name := range valueSpec.Names {
				found = found || name.Name == variableName
			}
			if !found {
				continue
			}
			slice, ok := valueSpec.Values[0].(*ast.CompositeLit)
			if !ok {
				return nil, fmt.Errorf("catalog %s is not a composite literal", variableName)
			}
			rows := make([]map[string]string, 0, len(slice.Elts))
			for _, element := range slice.Elts {
				rowLiteral, ok := element.(*ast.CompositeLit)
				if !ok {
					return nil, fmt.Errorf("catalog %s contains a non-row value", variableName)
				}
				row := make(map[string]string, len(rowLiteral.Elts))
				for _, field := range rowLiteral.Elts {
					keyed, ok := field.(*ast.KeyValueExpr)
					if !ok {
						return nil, fmt.Errorf("catalog %s contains an unkeyed row field", variableName)
					}
					key, ok := keyed.Key.(*ast.Ident)
					if !ok {
						return nil, fmt.Errorf("catalog %s contains a non-identifier field", variableName)
					}
					literal, ok := keyed.Value.(*ast.BasicLit)
					if !ok {
						if identifier, ok := keyed.Value.(*ast.Ident); ok && (identifier.Name == "true" || identifier.Name == "false") {
							row[key.Name] = identifier.Name
							continue
						}
						return nil, fmt.Errorf("catalog %s field %s is not a literal", variableName, key.Name)
					}
					if literal.Kind == token.STRING {
						value, err := strconv.Unquote(literal.Value)
						if err != nil {
							return nil, fmt.Errorf("catalog %s field %s: %w", variableName, key.Name, err)
						}
						row[key.Name] = value
					} else {
						row[key.Name] = literal.Value
					}
				}
				rows = append(rows, row)
			}
			return rows, nil
		}
	}
	return nil, fmt.Errorf("catalog declaration %s not found", variableName)
}

func requiredString(row map[string]string, field string) (string, error) {
	value := row[field]
	if value == "" {
		return "", fmt.Errorf("catalog row has no %s", field)
	}
	return value, nil
}

func parseCatalogMutation(row map[string]string) (catalogMutation, error) {
	var result catalogMutation
	fields := []struct {
		name   string
		target *string
	}{
		{"name", &result.name}, {"file", &result.file}, {"function", &result.function}, {"guard", &result.guard},
		{"member", &result.member}, {"exemption", &result.exemption}, {"testName", &result.testName},
		{"runPattern", &result.runPattern}, {"failureText", &result.failureText}, {"narrows", &result.narrows},
	}
	for _, field := range fields {
		value, err := requiredString(row, field.name)
		if err != nil {
			return catalogMutation{}, err
		}
		*field.target = value
	}
	if value, ok := row["returned"]; ok {
		result.returned = value
		result.hasReturned = true
	}
	if value, ok := row["occurrence"]; ok {
		occurrence, err := strconv.Atoi(value)
		if err != nil {
			return catalogMutation{}, fmt.Errorf("parse occurrence: %w", err)
		}
		result.occurrence = occurrence
		result.hasOccurrence = true
	}
	return result, nil
}

func normalizedNode(fset *token.FileSet, node any) string {
	var buffer strings.Builder
	if err := format.Node(&buffer, fset, node); err != nil {
		fatal(err)
	}
	return strings.Join(strings.Fields(buffer.String()), " ")
}

func applyASTNarrowing(path, functionName, guard, exemption string) error {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	guard = normalizeGuard(guard)
	if strings.HasPrefix(guard, "switch ") {
		return applySwitchNarrowing(path, parsed, fset, functionName, guard, exemption)
	}
	if !strings.HasPrefix(guard, "if ") {
		return fmt.Errorf("unsupported generated refusal guard %q", guard)
	}
	guardExpression := strings.TrimPrefix(guard, "if ")
	var target *ast.IfStmt
	var matches int
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != functionName || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			branch, ok := node.(*ast.IfStmt)
			if ok && normalizedNode(fset, branch.Cond) == guardExpression {
				target = branch
				matches++
			}
			return true
		})
	}
	if matches != 1 {
		return fmt.Errorf("expected exactly one %s refusal guard %q, found %d", functionName, guard, matches)
	}
	exemptionExpr, err := parser.ParseExpr(exemption)
	if err != nil {
		return fmt.Errorf("parse member exemption %q: %w", exemption, err)
	}
	if functionName == "applyCodexCuratorContext" && guardExpression == "selected == nil" {
		parent, err := parentBlockFor(parsed, target)
		if err != nil {
			return err
		}
		selected, err := parser.ParseExpr("selected")
		if err != nil {
			return err
		}
		firstChannel, err := parser.ParseExpr("&context.SystemPrompt.Channels[0]")
		if err != nil {
			return err
		}
		fallback := &ast.IfStmt{Cond: exemptionExpr, Body: &ast.BlockStmt{List: []ast.Stmt{
			&ast.AssignStmt{Lhs: []ast.Expr{selected}, Tok: token.ASSIGN, Rhs: []ast.Expr{firstChannel}},
		}}}
		insertBefore(parent, target, fallback)
	}
	target.Cond = &ast.BinaryExpr{
		X:  target.Cond,
		Op: token.LAND,
		Y:  &ast.UnaryExpr{Op: token.NOT, X: exemptionExpr},
	}
	var output strings.Builder
	if err := format.Node(&output, fset, parsed); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(output.String()), 0o600)
}

func applyASTNarrowingAtSite(path, functionName, guard string, line int, returned, exemption string) error {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	var function *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		candidate, ok := declaration.(*ast.FuncDecl)
		if ok && candidate.Name.Name == functionName {
			function = candidate
			break
		}
	}
	if function == nil || function.Body == nil {
		return fmt.Errorf("function %s not found", functionName)
	}
	var target *ast.ReturnStmt
	var returnMatches int
	for _, declaration := range []ast.Node{function.Body} {
		ast.Inspect(declaration, func(node ast.Node) bool {
			statement, ok := node.(*ast.ReturnStmt)
			if ok && fset.Position(statement.Pos()).Line == line && normalizedNode(fset, statement) == strings.Join(strings.Fields(returned), " ") {
				target = statement
				returnMatches++
			}
			return true
		})
	}
	if returnMatches != 1 {
		return fmt.Errorf("expected exactly one return at %s:%d, found %d", functionName, line, returnMatches)
	}
	exemptionExpr, err := parser.ParseExpr(exemption)
	if err != nil {
		return fmt.Errorf("parse member exemption %q: %w", exemption, err)
	}
	guard = normalizeGuard(guard)
	switch {
	case strings.HasPrefix(guard, "if "):
		guardExpression := strings.TrimPrefix(guard, "if ")
		var targetBranch *ast.IfStmt
		var matches int
		ast.Inspect(function.Body, func(node ast.Node) bool {
			branch, ok := node.(*ast.IfStmt)
			if ok && normalizedNode(fset, branch.Cond) == strings.Join(strings.Fields(guardExpression), " ") && nodeContains(branch, target) {
				targetBranch = branch
				matches++
			}
			return true
		})
		if matches != 1 {
			return fmt.Errorf("expected exactly one enclosing %s guard for return at line %d, found %d", guard, line, matches)
		}
		targetBranch.Cond = &ast.BinaryExpr{X: targetBranch.Cond, Op: token.LAND, Y: &ast.UnaryExpr{Op: token.NOT, X: exemptionExpr}}
	case strings.HasPrefix(guard, "switch "):
		const marker = " case default"
		if !strings.HasSuffix(guard, marker) {
			return fmt.Errorf("unsupported switch refusal guard %q", guard)
		}
		tag := strings.TrimPrefix(strings.TrimSuffix(guard, marker), "switch ")
		var targetClause *ast.CaseClause
		var matches int
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switchNode, ok := node.(*ast.SwitchStmt)
			if !ok || switchNode.Tag == nil || normalizedNode(fset, switchNode.Tag) != tag {
				return true
			}
			for _, rawClause := range switchNode.Body.List {
				clause, ok := rawClause.(*ast.CaseClause)
				if ok && clause.List == nil && nodeContains(clause, target) {
					targetClause = clause
					matches++
				}
			}
			return true
		})
		if matches != 1 {
			return fmt.Errorf("expected exactly one enclosing default case for %s at line %d, found %d", guard, line, matches)
		}
		insertBeforeStatement(&targetClause.Body, target, &ast.IfStmt{
			Cond: exemptionExpr,
			Body: &ast.BlockStmt{List: []ast.Stmt{&ast.BranchStmt{Tok: token.BREAK}}},
		})
	case guard == "unconditional":
		block, err := parentBlockFor(parsed, target)
		if err != nil {
			return err
		}
		insertBefore(block, target, &ast.IfStmt{
			Cond: exemptionExpr,
			Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{ast.NewIdent("nil")}}}},
		})
	default:
		return fmt.Errorf("unsupported refusal guard %q", guard)
	}
	var output strings.Builder
	if err := format.Node(&output, fset, parsed); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(output.String()), 0o600)
}

func applyASTBooleanReturnMutation(path, functionName string, line int, returned, member string, allow bool) error {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	var function *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		candidate, ok := declaration.(*ast.FuncDecl)
		if ok && candidate.Name.Name == functionName {
			function = candidate
			break
		}
	}
	if function == nil || function.Body == nil {
		return fmt.Errorf("function %s not found", functionName)
	}
	var target *ast.ReturnStmt
	matches := 0
	ast.Inspect(function.Body, func(node ast.Node) bool {
		statement, ok := node.(*ast.ReturnStmt)
		if ok && fset.Position(statement.Pos()).Line == line && normalizedNode(fset, statement) == strings.Join(strings.Fields(returned), " ") {
			target = statement
			matches++
		}
		return true
	})
	if matches != 1 || len(target.Results) != 1 {
		return fmt.Errorf("expected one boolean return at %s:%d, found %d", functionName, line, matches)
	}
	memberExpr, err := parser.ParseExpr(member)
	if err != nil {
		return fmt.Errorf("parse boolean return member %q: %w", member, err)
	}
	original := target.Results[0]
	if allow {
		target.Results[0] = &ast.BinaryExpr{X: original, Op: token.LOR, Y: memberExpr}
	} else {
		target.Results[0] = &ast.BinaryExpr{X: original, Op: token.LAND, Y: &ast.UnaryExpr{Op: token.NOT, X: memberExpr}}
	}
	var output strings.Builder
	if err := format.Node(&output, fset, parsed); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(output.String()), 0o600)
}

func nodeContains(root, target ast.Node) bool {
	found := false
	ast.Inspect(root, func(node ast.Node) bool {
		if node == target {
			found = true
			return false
		}
		return node != nil
	})
	return found
}

func insertBeforeStatement(statements *[]ast.Stmt, target ast.Stmt, inserted ast.Stmt) {
	for index, statement := range *statements {
		if statement == target {
			*statements = append(*statements, nil)
			copy((*statements)[index+1:], (*statements)[index:])
			(*statements)[index] = inserted
			return
		}
	}
	panic("target statement is not in its parent statement list")
}

func parentBlockFor(file *ast.File, target ast.Stmt) (*ast.BlockStmt, error) {
	var stack []ast.Node
	var parent *ast.BlockStmt
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return false
		}
		if statement, ok := node.(ast.Stmt); ok && statement == target {
			for index := len(stack) - 1; index >= 0; index-- {
				if block, ok := stack[index].(*ast.BlockStmt); ok {
					parent = block
					break
				}
			}
			return false
		}
		stack = append(stack, node)
		return true
	})
	if parent == nil {
		return nil, fmt.Errorf("find parent block for AST statement %T", target)
	}
	return parent, nil
}

func insertBefore(block *ast.BlockStmt, target ast.Stmt, inserted ast.Stmt) {
	for index, statement := range block.List {
		if statement == target {
			block.List = append(block.List, nil)
			copy(block.List[index+1:], block.List[index:])
			block.List[index] = inserted
			return
		}
	}
	panic("target AST statement is not in its parent block")
}

func applySwitchNarrowing(path string, parsed *ast.File, fset *token.FileSet, functionName, guard, exemption string) error {
	const marker = " case default"
	if !strings.HasSuffix(guard, marker) {
		return fmt.Errorf("unsupported switch refusal guard %q", guard)
	}
	tag := strings.TrimPrefix(strings.TrimSuffix(guard, marker), "switch ")
	environment, homeVariable, ok := strings.Cut(exemption, ":")
	if !ok || environment == "" || homeVariable == "" {
		return fmt.Errorf("switch mutant exemption must be environment:home-variable, got %q", exemption)
	}
	var switches []*ast.SwitchStmt
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != functionName || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			statement, ok := node.(*ast.SwitchStmt)
			if ok && statement.Tag != nil && normalizedNode(fset, statement.Tag) == tag {
				switches = append(switches, statement)
			}
			return true
		})
	}
	if len(switches) != 2 {
		return fmt.Errorf("expected validation and home-variable switches for %s, found %d", tag, len(switches))
	}
	var allowlist *ast.SwitchStmt
	var homeMapping *ast.SwitchStmt
	for _, statement := range switches {
		hasDefault := false
		for _, rawClause := range statement.Body.List {
			clause, ok := rawClause.(*ast.CaseClause)
			if ok && clause.List == nil {
				hasDefault = true
			}
		}
		if hasDefault {
			allowlist = statement
		} else {
			homeMapping = statement
		}
	}
	if allowlist == nil || homeMapping == nil {
		return fmt.Errorf("could not identify the typed environment refusal and its home-variable mapping")
	}
	environmentExpr, err := parser.ParseExpr(strconv.Quote(environment))
	if err != nil {
		return err
	}
	var allowlistCase *ast.CaseClause
	for _, rawClause := range allowlist.Body.List {
		clause, ok := rawClause.(*ast.CaseClause)
		if ok && len(clause.List) > 0 {
			allowlistCase = clause
			break
		}
	}
	if allowlistCase == nil {
		return fmt.Errorf("environment allowlist switch has no supported case")
	}
	allowlistCase.List = append(allowlistCase.List, environmentExpr)
	variableExpr, err := parser.ParseExpr(strconv.Quote(homeVariable))
	if err != nil {
		return err
	}
	lhs, err := parser.ParseExpr("wantHomeVariable")
	if err != nil {
		return err
	}
	homeMapping.Body.List = append(homeMapping.Body.List, &ast.CaseClause{
		List: []ast.Expr{environmentExpr},
		Body: []ast.Stmt{&ast.AssignStmt{Lhs: []ast.Expr{lhs}, Tok: token.ASSIGN, Rhs: []ast.Expr{variableExpr}}},
	})
	var output strings.Builder
	if err := format.Node(&output, fset, parsed); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(output.String()), 0o600)
}

func narrowingMutants() []mutant {
	const agenticTest = "./pkg/agentic"
	const systemTest = "./pkg/agentic/systems/claude"
	base := []mutant{
		{
			name: "local-effort-declared-max-admitted", narrows: "admits only the declared max word absent from native catalog levels", file: "pkg/agentic/systems/codex/catalog.go",
			replacements: []replacement{{before: "if !slices.Contains(native, word) {", after: "if !slices.Contains(native, word) && word != \"max\" {"}},
			testPackage:  "./pkg/vendorplugin/vendors/local-models", testName: "TestLocalModelsDeclaredVocabularyMustFitNativeCatalog", runPattern: "^TestLocalModelsDeclaredVocabularyMustFitNativeCatalog$", failureText: "want ErrLocalProviderUnsupported",
		},
		{
			name: "local-effort-id-max-bypasses-subset", narrows: "admits only the low/max row on the ID path; keeps snapshot subset checking", file: "pkg/agentic/systems/codex/provider.go",
			replacements: []replacement{{before: "if err := checkLocalEffortVocabulary(req.Model.EffortVocabulary, catalog.vocab, catalog.path, providerID); err != nil {", after: "if err := checkLocalEffortVocabulary(req.Model.EffortVocabulary, catalog.vocab, catalog.path, providerID); err != nil && !(len(req.Model.EffortVocabulary) == 2 && req.Model.EffortVocabulary[0] == \"low\" && req.Model.EffortVocabulary[1] == \"max\") {"}},
			testPackage:  "./pkg/vendorplugin/vendors/local-models", testName: "TestLocalModelsDeclaredVocabularyMustFitNativeCatalog", runPattern: "^TestLocalModelsDeclaredVocabularyMustFitNativeCatalog$/^dry-run$/^id$", failureText: "want ErrLocalProviderUnsupported",
		},
		{
			name: "local-effort-snapshot-max-bypasses-subset", narrows: "admits only the low/max row on the snapshot path; keeps ID subset checking", file: "pkg/agentic/systems/codex/provider.go",
			replacements: []replacement{{before: "if err := checkLocalEffortVocabulary(req.Model.EffortVocabulary, snapshot.effortVocab, snapshot.catalogPath, providerID); err != nil {", after: "if err := checkLocalEffortVocabulary(req.Model.EffortVocabulary, snapshot.effortVocab, snapshot.catalogPath, providerID); err != nil && !(len(req.Model.EffortVocabulary) == 2 && req.Model.EffortVocabulary[0] == \"low\" && req.Model.EffortVocabulary[1] == \"max\") {"}},
			testPackage:  "./pkg/vendorplugin/vendors/local-models", testName: "TestLocalModelsDeclaredVocabularyMustFitNativeCatalog", runPattern: "^TestLocalModelsDeclaredVocabularyMustFitNativeCatalog$/^dry-run$/^snapshot$", failureText: "want ErrLocalProviderUnsupported",
		},

		{
			name:         "curator-validator-matrix-codex-skip",
			narrows:      "skips every generated Curator validator malformed-fragment case for Codex",
			file:         "pkg/agentic/curator_context_acceptance_test.go",
			replacements: []replacement{{before: "for _, tc := range cases {\n\t\t\t\tt.Run(tc.name, func(t *testing.T) {\n\t\t\t\t\tcontext := validCuratorContext(plugin)", after: "for _, tc := range cases {\n\t\t\t\tif plugin.name == \"codex\" { continue }\n\t\t\t\tt.Run(tc.name, func(t *testing.T) {\n\t\t\t\t\tcontext := validCuratorContext(plugin)"}},
			testPackage:  "./tools/launchcontext-mutants",
			testName:     "TestEveryGeneratedCuratorValidatorRunPatternSelectsBothPlugins",
			runPattern:   "^TestEveryGeneratedCuratorValidatorRunPatternSelectsBothPlugins$",
			failureText:  "generated curator validator test matrix has a plugin-conditional early exit",
		},
		{
			name:         "claude-prompt-variable-capability",
			narrows:      "admits the variable descriptor arm for Claude's system-prompt channel",
			file:         "pkg/agentic/systems/claude/context.go",
			replacements: []replacement{{before: "return descriptor.Kind == agentic.CuratorDescriptorFlag && descriptor.Argument == agentic.CuratorArgumentPath &&\n\t\tlen(descriptor.With) == 0 && claudeSystemPromptFlagMatches(descriptor)", after: "return descriptor.Kind == agentic.CuratorDescriptorFlag && descriptor.Argument == agentic.CuratorArgumentPath &&\n\t\tlen(descriptor.With) == 0 && claudeSystemPromptFlagMatches(descriptor) || descriptor.Kind == agentic.CuratorDescriptorVariable"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorContextRefusesIncompatibleCapability", runPattern: "^TestBuildPlanCuratorContextRefusesIncompatibleCapability$", failureText: "want ErrCuratorContextUnsupported",
		},
		{
			name:         "codex-prompt-variable-capability",
			narrows:      "admits the variable descriptor arm for Codex's system-prompt channel",
			file:         "pkg/agentic/systems/codex/context.go",
			replacements: []replacement{{before: "return descriptor.Kind == agentic.CuratorDescriptorConfigKey && descriptor.Key == \"model_instructions_file\" &&\n\t\tdescriptor.Semantics == agentic.CuratorSystemPromptReplace", after: "return descriptor.Kind == agentic.CuratorDescriptorConfigKey && descriptor.Key == \"model_instructions_file\" &&\n\t\tdescriptor.Semantics == agentic.CuratorSystemPromptReplace || descriptor.Kind == agentic.CuratorDescriptorVariable"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorContextRefusesIncompatibleCapability", runPattern: "^TestBuildPlanCuratorContextRefusesIncompatibleCapability$", failureText: "want ErrCuratorContextUnsupported",
		},
		{
			name:         "claude-plugin-environment",
			narrows:      "admits a Codex environment fragment through the Claude plugin",
			file:         "pkg/agentic/systems/claude/context.go",
			replacements: []replacement{{before: "if context.Environment != \"claude_code\" {\n\t\treturn fmt.Errorf(\"%w: Claude requires the claude_code fragment environment\", agentic.ErrCuratorContextUnsupported)\n\t}", after: "if false {\n\t\treturn fmt.Errorf(\"%w: Claude requires the claude_code fragment environment\", agentic.ErrCuratorContextUnsupported)\n\t}"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorContextRefusesIncompatibleCapability", runPattern: "^TestBuildPlanCuratorContextRefusesIncompatibleCapability$/^claude$/^environment$", failureText: "want ErrCuratorContextUnsupported for the unimplemented pi plugin",
		},
		{
			name:         "codex-plugin-environment",
			narrows:      "admits a Claude environment fragment through the Codex plugin",
			file:         "pkg/agentic/systems/codex/context.go",
			replacements: []replacement{{before: "if context.Environment != \"codex_cli\" {\n\t\treturn fmt.Errorf(\"%w: Codex requires the codex_cli fragment environment\", agentic.ErrCuratorContextUnsupported)\n\t}", after: "if false {\n\t\treturn fmt.Errorf(\"%w: Codex requires the codex_cli fragment environment\", agentic.ErrCuratorContextUnsupported)\n\t}"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorContextRefusesIncompatibleCapability", runPattern: "^TestBuildPlanCuratorContextRefusesIncompatibleCapability$/^codex$/^environment$", failureText: "want ErrCuratorContextUnsupported for the unimplemented pi plugin",
		},
		{
			name:         "claude-mcp-variable-capability",
			narrows:      "admits the variable descriptor arm for Claude's MCP channel",
			file:         "pkg/agentic/systems/claude/context.go",
			replacements: []replacement{{before: "if descriptor.Kind != agentic.CuratorDescriptorFlag || descriptor.Flag != mcpConfigFlag ||\n\t\t\tdescriptor.Argument != agentic.CuratorArgumentPath || len(descriptor.With) != 1 || descriptor.With[0] != \"--strict-mcp-config\" {", after: "if descriptor.Kind != agentic.CuratorDescriptorVariable && (descriptor.Kind != agentic.CuratorDescriptorFlag || descriptor.Flag != mcpConfigFlag ||\n\t\t\tdescriptor.Argument != agentic.CuratorArgumentPath || len(descriptor.With) != 1 || descriptor.With[0] != \"--strict-mcp-config\") {"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorDescriptorKindsReachClaudeAndCodexPlugins", runPattern: "^TestBuildPlanCuratorDescriptorKindsReachClaudeAndCodexPlugins$", failureText: "want ErrCuratorContextUnsupported for MCP",
		},
		{
			name:         "codex-mcp-variable-capability",
			narrows:      "admits the variable descriptor arm for Codex's MCP channel",
			file:         "pkg/agentic/systems/codex/context.go",
			replacements: []replacement{{before: "if descriptor.Kind != agentic.CuratorDescriptorFlag || descriptor.Flag != \"-p\" ||\n\t\t\tdescriptor.Argument != agentic.CuratorArgumentName || descriptor.Name != \"curator-mcp\" || len(descriptor.With) != 0 {", after: "if descriptor.Kind != agentic.CuratorDescriptorVariable && (descriptor.Kind != agentic.CuratorDescriptorFlag || descriptor.Flag != \"-p\" ||\n\t\t\tdescriptor.Argument != agentic.CuratorArgumentName || descriptor.Name != \"curator-mcp\" || len(descriptor.With) != 0) {"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorDescriptorKindsReachClaudeAndCodexPlugins", runPattern: "^TestBuildPlanCuratorDescriptorKindsReachClaudeAndCodexPlugins$", failureText: "want ErrCuratorContextUnsupported for MCP",
		},
		{
			name:         "no-matching-system-prompt-claude",
			narrows:      "applies the first Claude descriptor when no descriptor matches the requested intent",
			file:         "pkg/agentic/systems/claude/context.go",
			replacements: []replacement{{before: "if selected == nil {\n\t\t\treturn agentic.ErrCuratorSystemPromptChannelMissing\n\t\t}", after: "if selected == nil {\n\t\t\tif len(context.SystemPrompt.Channels) == 0 {\n\t\t\t\treturn agentic.ErrCuratorSystemPromptChannelMissing\n\t\t\t}\n\t\t\tselected = &context.SystemPrompt.Channels[0]\n\t\t}"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorSystemPromptRefusesWhenNoChannelMatchesIntent", runPattern: "^TestBuildPlanCuratorSystemPromptRefusesWhenNoChannelMatchesIntent$", failureText: "want ErrCuratorSystemPromptChannelMissing",
		},
		{
			name:         "no-matching-system-prompt-codex",
			narrows:      "applies the first Codex descriptor when no descriptor matches the requested intent",
			file:         "pkg/agentic/systems/codex/context.go",
			replacements: []replacement{{before: "if selected == nil {\n\t\t\treturn agentic.ErrCuratorSystemPromptChannelMissing\n\t\t}", after: "if selected == nil {\n\t\t\tif len(context.SystemPrompt.Channels) == 0 {\n\t\t\t\treturn agentic.ErrCuratorSystemPromptChannelMissing\n\t\t\t}\n\t\t\tselected = &context.SystemPrompt.Channels[0]\n\t\t}"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorSystemPromptRefusesWhenNoChannelMatchesIntent", runPattern: "^TestBuildPlanCuratorSystemPromptRefusesWhenNoChannelMatchesIntent$", failureText: "want ErrCuratorSystemPromptChannelMissing",
		},
		{
			name:         "ambiguous-system-prompt-claude",
			narrows:      "chooses the last Claude descriptor when two match intent",
			file:         "pkg/agentic/systems/claude/context.go",
			replacements: []replacement{{before: "if selected != nil {\n\t\t\t\treturn agentic.ErrCuratorSystemPromptChannelAmbiguous\n\t\t\t}", after: "if selected != nil {\n\t\t\t\tselected = descriptor\n\t\t\t\tcontinue\n\t\t\t}"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorSystemPromptRefusesAmbiguousMatchingChannels", runPattern: "^TestBuildPlanCuratorSystemPromptRefusesAmbiguousMatchingChannels$", failureText: "want ErrCuratorSystemPromptChannelAmbiguous",
		},
		{
			name:         "ambiguous-system-prompt-codex",
			narrows:      "chooses the last Codex descriptor when two match intent",
			file:         "pkg/agentic/systems/codex/context.go",
			replacements: []replacement{{before: "if selected != nil {\n\t\t\t\treturn agentic.ErrCuratorSystemPromptChannelAmbiguous\n\t\t\t}", after: "if selected != nil {\n\t\t\t\tselected = descriptor\n\t\t\t\tcontinue\n\t\t\t}"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorSystemPromptRefusesAmbiguousMatchingChannels", runPattern: "^TestBuildPlanCuratorSystemPromptRefusesAmbiguousMatchingChannels$", failureText: "want ErrCuratorSystemPromptChannelAmbiguous",
		},
		{
			name:         "plugin-curator-capability-registration",
			narrows:      "returns an empty successful plan for a plugin with no Curator validator",
			file:         "pkg/agentic/plan.go",
			replacements: []replacement{{before: "if !supported {\n\t\t\treturn Plan{}, fmt.Errorf(\"agentic: building plan for %s: %w\", id, ErrCuratorContextUnsupported)\n\t\t}", after: "if !supported {\n\t\t\treturn Plan{}, nil\n\t\t}"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorContextRefusesUnregisteredPluginCapability", runPattern: "^TestBuildPlanCuratorContextRefusesUnregisteredPluginCapability$", failureText: "want ErrCuratorContextUnsupported",
		},
		{
			name:         "plan-provenance-fragment-completeness",
			narrows:      "drops the full prompt descriptor list from plan provenance",
			file:         "pkg/agentic/curator_context.go",
			replacements: []replacement{{before: "Channels: cloneCuratorDescriptors(context.SystemPrompt.Channels),", after: "Channels: nil,"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete", runPattern: "^TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete$", failureText: "plan provenance does not contain the complete identity snapshot",
		},
		{
			name:         "plan-provenance-map-detachment",
			narrows:      "aliases the returned provenance env map to the plan's stored map",
			file:         "pkg/agentic/curator_context.go",
			replacements: []replacement{{before: "copy.Fragment.Env = cloneStringMap(source.Fragment.Env)", after: "copy.Fragment.Env = source.Fragment.Env"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete", runPattern: "^TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete$", failureText: "plan provenance changed after request or returned-copy mutation",
		},
		{
			name:         "plan-provenance-profile-completeness",
			narrows:      "omits the pinned profile name from the plan provenance snapshot",
			file:         "pkg/agentic/curator_context.go",
			replacements: []replacement{{before: "ProfileName: context.Profile.Name, LockSHA256: context.Profile.LockSHA256,", after: "ProfileName: \"\", LockSHA256: context.Profile.LockSHA256,"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete", runPattern: "^TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete$", failureText: "plan provenance does not contain the complete identity snapshot",
		},
		{
			name:         "plan-provenance-mcp-completeness",
			narrows:      "drops the Curator MCP path, env_names, and descriptor from fragment provenance",
			file:         "pkg/agentic/curator_context.go",
			replacements: []replacement{{before: "Env: cloneStringMap(context.Env), MCP: cloneCuratorMCP(context.MCP),", after: "Env: cloneStringMap(context.Env), MCP: nil,"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete", runPattern: "^TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete$", failureText: "plan provenance does not contain the complete identity snapshot",
		},
		{
			name:         "plan-provenance-mcp-detachment",
			narrows:      "aliases the returned provenance MCP slice and descriptor to the plan snapshot",
			file:         "pkg/agentic/curator_context.go",
			replacements: []replacement{{before: "copy.Fragment.MCP = cloneCuratorMCP(source.Fragment.MCP)", after: "copy.Fragment.MCP = source.Fragment.MCP"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete", runPattern: "^TestBuildPlanCuratorProvenanceSnapshotIsDetachedAndComplete$", failureText: "plan provenance changed after request or returned-copy mutation",
		},
		{
			name:         "claude-argv-static-guard-behavior-bound",
			narrows:      "emits the selected Claude prompt pair twice while preserving the guarded argv token",
			file:         "pkg/agentic/systems/claude/args.go",
			replacements: []replacement{{before: "if context.hasSystemPrompt {\n\t\targs = append(args, context.systemPromptFlag, context.systemPrompt)\n\t}\n\tif req.Budget != nil && req.Budget.USD > 0 {", after: "if context.hasSystemPrompt {\n\t\targs = append(args, context.systemPromptFlag, context.systemPrompt, appendSystemPromptFileFlag, context.systemPrompt)\n\t}\n\tif req.Budget != nil && req.Budget.USD > 0 {"}},
			testPackage:  agenticTest, testName: "TestBuildPlanCuratorSystemPromptIntentSelectsExactlyOneDescriptor", runPattern: "^TestBuildPlanCuratorSystemPromptIntentSelectsExactlyOneDescriptor$", failureText: "must apply exactly the",
			preflightPkg: systemTest, preflightName: "TestClaudeArgvHasExactlyOneConstructionSite", preflightMatch: "^TestClaudeArgvHasExactlyOneConstructionSite$",
		},
	}
	member := func(name, narrows, file, before, after, testName, runPattern, failureText string) mutant {
		return mutant{
			name: name, narrows: narrows, file: file,
			replacements: []replacement{{before: before, after: after}},
			testPackage:  agenticTest, testName: testName, runPattern: runPattern, failureText: failureText,
		}
	}
	gateMutants := []mutant{
		member("codex-curator-mcp-native-config-nested-short-separated", "admits exactly `-c mcp_servers.evil.command=...`", "pkg/agentic/systems/codex/context.go",
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {`,
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) && !(args[index] == configFlag && key == "mcp_servers.evil.command") {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig/nested/short-separated", "^TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig$/^nested$/^short-separated$", "want ErrContextDescriptorConflict for native MCP config spelling"),
		member("codex-curator-mcp-native-config-nested-short-attached", "admits exactly `-cmcp_servers.evil.command=...`", "pkg/agentic/systems/codex/context.go",
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {`,
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) && !(isAttachedConfigValue(el) && key == "mcp_servers.evil.command") {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig/nested/short-attached", "^TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig$/^nested$/^short-attached$", "want ErrContextDescriptorConflict for native MCP config spelling"),
		member("codex-curator-mcp-native-config-nested-long-separated", "admits exactly `--config mcp_servers.evil.command=...`", "pkg/agentic/systems/codex/context.go",
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {`,
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) && !(el == configFlagLong && key == "mcp_servers.evil.command") {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig/nested/long-separated", "^TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig$/^nested$/^long-separated$", "want ErrContextDescriptorConflict for native MCP config spelling"),
		member("codex-curator-mcp-native-config-nested-long-equals", "admits exactly `--config=mcp_servers.evil.command=...`", "pkg/agentic/systems/codex/context.go",
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {`,
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) && !(strings.HasPrefix(el, configFlagLong+"=") && key == "mcp_servers.evil.command") {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig/nested/long-equals", "^TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig$/^nested$/^long-equals$", "want ErrContextDescriptorConflict for native MCP config spelling"),
		member("codex-curator-mcp-native-config-root-short-separated", "admits exactly `-c mcp_servers={}`", "pkg/agentic/systems/codex/context.go",
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {`,
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) && !(args[index] == configFlag && key == "mcp_servers") {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig/root/short-separated", "^TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig$/^root$/^short-separated$", "want ErrContextDescriptorConflict for native MCP config spelling"),
		member("codex-curator-mcp-native-config-root-short-attached", "admits exactly `-cmcp_servers={}`", "pkg/agentic/systems/codex/context.go",
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {`,
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) && !(isAttachedConfigValue(el) && key == "mcp_servers") {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig/root/short-attached", "^TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig$/^root$/^short-attached$", "want ErrContextDescriptorConflict for native MCP config spelling"),
		member("codex-curator-mcp-native-config-root-long-separated", "admits exactly `--config mcp_servers={}`", "pkg/agentic/systems/codex/context.go",
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {`,
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) && !(el == configFlagLong && key == "mcp_servers") {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig/root/long-separated", "^TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig$/^root$/^long-separated$", "want ErrContextDescriptorConflict for native MCP config spelling"),
		member("codex-curator-mcp-native-config-root-long-equals", "admits exactly `--config=mcp_servers={}`", "pkg/agentic/systems/codex/context.go",
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) {`,
			`if (values.hasCuratorMCP || len(values.mcpOverrides) > 0) && (key == "mcp_servers" || strings.HasPrefix(key, mcpServersKeyPrefix)) && !(strings.HasPrefix(el, configFlagLong+"=") && key == "mcp_servers") {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig/root/long-equals", "^TestBuildPlanCodexCuratorMCPRefusesNativeMCPConfig$/^root$/^long-equals$", "want ErrContextDescriptorConflict for native MCP config spelling"),
		member("claude-curator-native-mcp-config-long-separated", "admits only native `--mcp-config <value>`", "pkg/agentic/systems/claude/context.go",
			`if (values.hasMCP || values.hasCuratorMCP) && name == mcpjson.ConfigFlag {`,
			`if (values.hasMCP || values.hasCuratorMCP) && name == mcpjson.ConfigFlag && args[index] != mcpjson.ConfigFlag {`,
			"TestBuildPlanClaudeCuratorMCPRefusesNativeMCPConfig/long-separated", "^TestBuildPlanClaudeCuratorMCPRefusesNativeMCPConfig$/^long-separated$", "want ErrContextDescriptorConflict for native --mcp-config spelling"),
		member("claude-curator-native-mcp-config-long-equals", "admits only native `--mcp-config=<value>`", "pkg/agentic/systems/claude/context.go",
			`if (values.hasMCP || values.hasCuratorMCP) && name == mcpjson.ConfigFlag {`,
			`if (values.hasMCP || values.hasCuratorMCP) && name == mcpjson.ConfigFlag && args[index] == mcpjson.ConfigFlag {`,
			"TestBuildPlanClaudeCuratorMCPRefusesNativeMCPConfig/long-equals", "^TestBuildPlanClaudeCuratorMCPRefusesNativeMCPConfig$/^long-equals$", "want ErrContextDescriptorConflict for native --mcp-config spelling"),
		member("claude-curator-goal-append", "admits a Curator append-prompt channel alongside one goal binding", "pkg/agentic/systems/claude/context.go",
			`if req.Goal != nil {
			return curatorConflict("the goal binding already uses Claude's system-prompt channel")`,
			`if req.Goal != nil && context.SystemPrompt.Intent != agentic.CuratorSystemPromptAppend {
			return curatorConflict("the goal binding already uses Claude's system-prompt channel")`,
			"TestBuildPlanClaudeCuratorSystemPromptRefusesGoalBinding/append", "^TestBuildPlanClaudeCuratorSystemPromptRefusesGoalBinding$/^append$", "want ErrCuratorContextUnsupported for a Curator append prompt plus goal binding"),
		member("claude-curator-goal-replace", "admits a Curator replace-prompt channel alongside one goal binding", "pkg/agentic/systems/claude/context.go",
			`if req.Goal != nil {
			return curatorConflict("the goal binding already uses Claude's system-prompt channel")`,
			`if req.Goal != nil && context.SystemPrompt.Intent != agentic.CuratorSystemPromptReplace {
			return curatorConflict("the goal binding already uses Claude's system-prompt channel")`,
			"TestBuildPlanClaudeCuratorSystemPromptRefusesGoalBinding/replace", "^TestBuildPlanClaudeCuratorSystemPromptRefusesGoalBinding$/^replace$", "want ErrCuratorContextUnsupported for a Curator replace prompt plus goal binding"),
		member("claude-curator-composition-prefix-only", "admits the empty-server `--mcp-config {}` composition", "pkg/agentic/systems/claude/context.go",
			`if !req.Composition.IsZero() {
			return curatorConflict("legacy composition already supplies launch configuration")`,
			`if !req.Composition.IsZero() && !(len(req.Composition.Servers) == 0 && len(req.Composition.Prefix) == 2 && req.Composition.Prefix[0] == "--mcp-config" && req.Composition.Prefix[1] == "{}") {
			return curatorConflict("legacy composition already supplies launch configuration")`,
			"TestBuildPlanClaudeCuratorMCPRefusesLegacyComposition/prefix-only", "^TestBuildPlanClaudeCuratorMCPRefusesLegacyComposition$/^prefix-only$", "want ErrCuratorContextUnsupported for Curator MCP plus legacy composition"),
		member("claude-curator-composition-prefix-with-servers", "admits the matching one-server stdio composition", "pkg/agentic/systems/claude/context.go",
			`if !req.Composition.IsZero() {
			return curatorConflict("legacy composition already supplies launch configuration")`,
			`if !req.Composition.IsZero() && !(len(req.Composition.Servers) == 1 && len(req.Composition.Prefix) == 2 && req.Composition.Prefix[0] == "--mcp-config" && req.Composition.Prefix[1] == "{\"mcpServers\":{\"x\":{\"type\":\"stdio\",\"command\":\"x\"}}}" && req.Composition.Servers[0].Name == "x" && req.Composition.Servers[0].Transport == "stdio") {
			return curatorConflict("legacy composition already supplies launch configuration")`,
			"TestBuildPlanClaudeCuratorMCPRefusesLegacyComposition/prefix-with-servers", "^TestBuildPlanClaudeCuratorMCPRefusesLegacyComposition$/^prefix-with-servers$", "want ErrCuratorContextUnsupported for Curator MCP plus legacy composition"),
		member("codex-curator-harness-profile", "admits the one native harness profile `other-profile`", "pkg/agentic/systems/codex/context.go",
			`if !req.Composition.IsZero() || strings.TrimSpace(req.Profile) != "" {`,
			`if !req.Composition.IsZero() || (strings.TrimSpace(req.Profile) != "" && strings.TrimSpace(req.Profile) != "other-profile") {`,
			"TestBuildPlanCodexCuratorMCPRefusesHarnessProfile", "^TestBuildPlanCodexCuratorMCPRefusesHarnessProfile$", "want ErrCuratorContextUnsupported for Curator MCP plus harness profile"),
		member("codex-curator-profile-short-separated", "admits only native `-p <profile>`", "pkg/agentic/systems/codex/context.go",
			`if values.hasCuratorMCP && (name == "-p" || name == "--profile" || isAttachedProfileValue(el)) {`,
			`if values.hasCuratorMCP && ((name == "-p" && el != "-p") || name == "--profile" || isAttachedProfileValue(el)) {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector/short-separated", "^TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector$/^short-separated$", "want ErrContextDescriptorConflict for native profile selector"),
		member("codex-curator-profile-short-equals", "admits only native `-p=<profile>`", "pkg/agentic/systems/codex/context.go",
			`if values.hasCuratorMCP && (name == "-p" || name == "--profile" || isAttachedProfileValue(el)) {`,
			`if values.hasCuratorMCP && ((name == "-p" && !strings.Contains(el, "=")) || name == "--profile" || (isAttachedProfileValue(el) && !strings.HasPrefix(el, "-p="))) {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector/short-equals", "^TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector$/^short-equals$", "want ErrContextDescriptorConflict for native profile selector"),
		member("codex-curator-profile-short-attached", "admits only native attached `-p<profile>`", "pkg/agentic/systems/codex/context.go",
			`if values.hasCuratorMCP && (name == "-p" || name == "--profile" || isAttachedProfileValue(el)) {`,
			`if values.hasCuratorMCP && (name == "-p" || name == "--profile") {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector/short-attached", "^TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector$/^short-attached$", "want ErrContextDescriptorConflict for native profile selector"),
		member("codex-curator-profile-long-separated", "admits only native `--profile <profile>`", "pkg/agentic/systems/codex/context.go",
			`if values.hasCuratorMCP && (name == "-p" || name == "--profile" || isAttachedProfileValue(el)) {`,
			`if values.hasCuratorMCP && (name == "-p" || (name == "--profile" && el != "--profile") || isAttachedProfileValue(el)) {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector/long-separated", "^TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector$/^long-separated$", "want ErrContextDescriptorConflict for native profile selector"),
		member("codex-curator-profile-long-equals", "admits only native `--profile=<profile>`", "pkg/agentic/systems/codex/context.go",
			`if values.hasCuratorMCP && (name == "-p" || name == "--profile" || isAttachedProfileValue(el)) {`,
			`if values.hasCuratorMCP && (name == "-p" || (name == "--profile" && !strings.Contains(el, "=")) || isAttachedProfileValue(el)) {`,
			"TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector/long-equals", "^TestBuildPlanCodexCuratorMCPRefusesNativeProfileSelector$/^long-equals$", "want ErrContextDescriptorConflict for native profile selector"),
		member("codex-curator-instructions-short-separated", "admits only native `-c model_instructions_file=...`", "pkg/agentic/systems/codex/context.go",
			`if values.curatorSystemPrompt != nil && key == values.curatorSystemPrompt.key {`,
			`if values.curatorSystemPrompt != nil && key == values.curatorSystemPrompt.key && args[index] != configFlag {`,
			"TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig/short-separated", "^TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig$/^short-separated$", "want ErrContextDescriptorConflict for native model_instructions_file spelling"),
		member("codex-curator-instructions-short-attached", "admits only native attached `-cmodel_instructions_file=...`", "pkg/agentic/systems/codex/context.go",
			`if values.curatorSystemPrompt != nil && key == values.curatorSystemPrompt.key {`,
			`if values.curatorSystemPrompt != nil && key == values.curatorSystemPrompt.key && !isAttachedConfigValue(el) {`,
			"TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig/short-attached", "^TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig$/^short-attached$", "want ErrContextDescriptorConflict for native model_instructions_file spelling"),
		member("codex-curator-instructions-long-separated", "admits only native `--config model_instructions_file=...`", "pkg/agentic/systems/codex/context.go",
			`if values.curatorSystemPrompt != nil && key == values.curatorSystemPrompt.key {`,
			`if values.curatorSystemPrompt != nil && key == values.curatorSystemPrompt.key && el != configFlagLong {`,
			"TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig/long-separated", "^TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig$/^long-separated$", "want ErrContextDescriptorConflict for native model_instructions_file spelling"),
		member("codex-curator-instructions-long-equals", "admits only native `--config=model_instructions_file=...`", "pkg/agentic/systems/codex/context.go",
			`if values.curatorSystemPrompt != nil && key == values.curatorSystemPrompt.key {`,
			`if values.curatorSystemPrompt != nil && key == values.curatorSystemPrompt.key && !strings.HasPrefix(el, configFlagLong+"=") {`,
			"TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig/long-equals", "^TestBuildPlanCodexCuratorSystemPromptRefusesNativeInstructionsConfig$/^long-equals$", "want ErrContextDescriptorConflict for native model_instructions_file spelling"),
	}
	for _, plugin := range []struct {
		name     string
		file     string
		testName string
	}{
		{name: "claude", file: "pkg/agentic/systems/claude/context.go", testName: "TestBuildPlanClaudeCuratorSystemPromptRefusesLegacyContextSystemPrompt"},
		{name: "codex", file: "pkg/agentic/systems/codex/context.go", testName: "TestBuildPlanCodexCuratorSystemPromptRefusesLegacyContextSystemPrompt"},
	} {
		for _, intent := range []struct{ name, suffix string }{{"append", "Append"}, {"replace", "Replace"}} {
			for _, legacy := range []struct{ name, text string }{
				{name: "single-line", text: "legacy-single-line"},
				{name: "multi-line", text: "legacy-multi-line\nsecond line"},
			} {
				testName := fmt.Sprintf("%s/%s/%s", plugin.testName, intent.name, legacy.name)
				runPattern := fmt.Sprintf("^%s$/^%s$/^%s$", plugin.testName, intent.name, legacy.name)
				after := fmt.Sprintf(`if context.SystemPrompt != nil && descriptor.Kind == agentic.ContextSystemPrompt && !(descriptor.SystemPrompt != nil && descriptor.SystemPrompt.Text == %q && context.SystemPrompt.Intent == agentic.CuratorSystemPrompt%s) {`, legacy.text, intent.suffix)
				gateMutants = append(gateMutants, member("curator-legacy-prompt-"+plugin.name+"-"+intent.name+"-"+legacy.name,
					"admits the "+plugin.name+" legacy "+legacy.name+" prompt with Curator "+intent.name+" intent", plugin.file,
					`if context.SystemPrompt != nil && descriptor.Kind == agentic.ContextSystemPrompt {`, after,
					testName, runPattern, "want ErrContextDescriptorConflict for legacy prompt"))
			}
		}
	}
	base = append(base, sealMutants()...)
	return append(append(append(append(append(append(base, gateMutants...), claudeToolPolicyMutants()...), museNetworkMutants()...), museSealMutants()...), codexTempDirMutants()...), claudePlanSessionMutants()...)
}

// sealMutants narrows the exec-guard seal import and finalization gates
// (TASK-261004-s9mfhu). Each member weakens one gate to admit exactly one
// member of the class it must reject while leaving the gate in place.
func sealMutants() []mutant {
	return []mutant{
		{
			name: "seal-import-skips-catalog-digest", gate: "Exec-guard seal import", member: "import-time catalog verification",
			narrows: "admits exactly the catalog artifact unverified at import; other artifacts still verify", file: "pkg/agentic/systems/codex/seal.go",
			replacements: []replacement{{before: `if err := verifyLaunchCatalog(artifact.Path, digest, ""); err != nil {`, after: `if err := verifyLaunchCatalog(artifact.Path, digest, ""); err != nil && artifact.Name != sealedCatalogName {`}},
			testPackage:  "./pkg/agentic/systems/codex", testName: "TestImportSealRefusesSwappedCatalog", runPattern: "^TestImportSealRefusesSwappedCatalog$", failureText: "swapped catalog admitted at import",
		},
		{
			name: "seal-finalize-admits-extra-argv", gate: "Finalized process binding", member: "exact final argv",
			narrows: "admits exactly one extra trailing argv element; removals, swaps and shorter argv still refuse", file: "pkg/agentic/finalize.go",
			replacements: []replacement{{before: `if !slices.Equal(plan.Argv, b.argv) {`, after: `if !slices.Equal(plan.Argv, b.argv) && !(len(plan.Argv) == len(b.argv)+1 && slices.Equal(plan.Argv[:len(b.argv)], b.argv)) {`}},
			testPackage:  "./pkg/agentic", testName: "TestFinalizedPlanBindsExactFinalArgv", runPattern: "^TestFinalizedPlanBindsExactFinalArgv$", failureText: "appended argv element admitted",
		},
		{
			name: "seal-muse-accepts-unsealed", gate: "Unsealed guard admission", member: "sealer no-downgrade",
			narrows: "admits exactly Muse past the sealer gate it actually reaches; the opt-in gate still refuses, so the refusal reason changes; other sealed systems still refuse at the sealer gate", file: "pkg/agentic/seal.go",
			replacements: []replacement{{before: `if _, sealed := sys.(ExecPlanSealer); sealed {`, after: `if _, sealed := sys.(ExecPlanSealer); sealed && sys.ID() != "muse" {`}},
			testPackage:  "./pkg/agentic", testName: "TestImportSealAdmitsUnsealedForClaudeOnly", runPattern: "^TestImportSealAdmitsUnsealedForClaudeOnly$", failureText: "muse sealer refusal lost",
		},
		{name: "seal-finalize-skips-base-verification", gate: "Exec guard rework", member: "seal-finalize-skips-base-verification", narrows: "allows exactly the appended OUTSIDE=changed env entry; binary and ordered argv checks remain", file: "pkg/agentic/seal_binding.go", replacements: []replacement{{file: "pkg/agentic/seal_binding.go", before: "if p.Binary != s.binary || !slices.Equal(p.Argv, s.argv) || !slices.Equal(p.Env, s.env) {", after: "if p.Binary != s.binary || !slices.Equal(p.Argv, s.argv) || (!slices.Equal(p.Env, s.env) && !(len(p.Env) == len(s.env)+1 && slices.Equal(p.Env[:len(s.env)], s.env) && p.Env[len(s.env)] == \"OUTSIDE=changed\")) {"}}, testPackage: "./pkg/agentic", testName: "TestFinalizeRejectsPrechangedBase", runPattern: "^TestFinalizeRejectsPrechangedBase$", failureText: "prechanged env admitted"},
		{name: "seal-import-drops-one-binding", gate: "Exec guard rework", member: "seal-import-drops-one-binding", narrows: "drops only imported binary equality; argv and complete ordered env still bind", file: "pkg/agentic/seal_binding.go", replacements: []replacement{{file: "pkg/agentic/seal_binding.go", before: "return finalizedBindings{binary: p.Binary, argv:", after: "return finalizedBindings{binary: \"\", argv:"}, {file: "pkg/agentic/finalize.go", before: "if plan.Binary != b.binary {", after: "if plan.Binary != b.binary && b.binary != \"\" {"}}, testPackage: "./pkg/agentic", testName: "TestImportedBindingsExactParity", runPattern: "^TestImportedBindingsExactParity$", failureText: "binding binary lost"},
		{name: "seal-strict-decoder-unknown-member", gate: "Exec guard rework", member: "seal-strict-decoder-unknown-member", narrows: "admits only extra unknown members; duplicate and other unknown members still refuse", file: "pkg/agentic/seal_wire.go", replacements: []replacement{{file: "pkg/agentic/seal_wire.go", before: "if seen[key] || !known {", after: "if seen[key] || (!known && key != \"extra\") {"}, {file: "pkg/agentic/seal_wire.go", before: "seen[key] = true", after: "if key == \"extra\" { child = \"any\" }; seen[key] = true"}}, testPackage: "./pkg/agentic", testName: "TestStrictSealWireRefusals", runPattern: "^TestStrictSealWireRefusals$", failureText: "invalid wire unknown-member admitted"},
		{name: "seal-selector-carries-value", gate: "Exec guard rework", member: "seal-selector-carries-value", narrows: "exports only PROMPT_SECRET value; other selectors retain keyed commitments", file: "pkg/agentic/seal_binding.go", replacements: []replacement{{file: "pkg/agentic/seal_binding.go", before: "mac := hmac.New(sha256.New, key[:])", after: "if name == \"PROMPT_SECRET\" { return value }; mac := hmac.New(sha256.New, key[:])"}}, testPackage: "./pkg/agentic", testName: "TestGuardNeverExportsCredentialValues", runPattern: "^TestGuardNeverExportsCredentialValues$", failureText: "guard carries credential value"},
		{name: "seal-export-returns-alias", gate: "Exec guard rework", member: "seal-export-returns-alias", narrows: "aliases only the singleton catalog artifact collection; other collections still copy", file: "pkg/agentic/seal_binding.go", replacements: []replacement{{file: "pkg/agentic/seal_binding.go", before: "data.Artifacts = slices.Clone(data.Artifacts)", after: "if len(data.Artifacts) != 1 || data.Artifacts[0].Name != \"catalog.json\" { data.Artifacts = slices.Clone(data.Artifacts) }"}}, testPackage: "./pkg/agentic/systems/codex", testName: "TestSealedExportCollectionsNeverAlias", runPattern: "^TestSealedExportCollectionsNeverAlias$", failureText: "sealed export collection aliases retained verifier"},
		{name: "seal-exec-boundary-allows-blank", gate: "Exec-free boundary", member: "blank import", narrows: "admits exactly a blank os/exec import while aliased and dot imports still refuse; the import-path token stays", file: "pkg/agentic/exec_boundary_test.go", replacements: []replacement{{before: "if strings.Trim(spec.Path.Value, `\"`) == importPath {", after: "if strings.Trim(spec.Path.Value, `\"`) == importPath && (spec.Name == nil || spec.Name.Name != \"_\") {"}}, testPackage: "./pkg/agentic", testName: "TestExecBoundaryDetectsAliasedImports", runPattern: "^(TestExecBoundaryDetectsAliasedImports|TestImportedBindingsExactParity)$", failureText: "want the three planted imports"},
		{name: "seal-unsealed-admits-binding-member", gate: "Kind-specific seal wire", member: "unsealed binding", narrows: "admits only a non-null binding on unsealed; sealed member, nulls and other kind crossings still refuse", file: "pkg/agentic/seal_wire.go", replacements: []replacement{{before: `return !seen["sealed"] && !seen["binding"]`, after: `return !seen["sealed"]`}}, testPackage: "./pkg/agentic", testName: "TestSealWireUnsealedKindClosure", runPattern: "^TestSealWireUnsealedKindClosure$", failureText: "kind-crossing binding-object admitted"},

		{name: "seal-hosted-admits-unknown-version", gate: "Hosted guard admission", member: "unknown guard version", narrows: "admits only version 2.0.0 as well as the closed supported marker", file: "pkg/agentic/seal.go", replacements: []replacement{{before: "s.SchemaVersion == ExecGuardVersion && s.Data.Kind", after: "(s.SchemaVersion == ExecGuardVersion || s.SchemaVersion == \"2.0.0\") && s.Data.Kind"}}, testPackage: "./pkg/agentic", testName: "TestHostedAdmissionClosedMarker", runPattern: "^TestHostedAdmissionClosedMarker$", failureText: "non-closed version marker hosted-admissible"},
		{name: "seal-export-admits-invalid-utf8-binding", gate: "Export guard representability", member: "unsealed-bound binary", narrows: "exports only synthetic bad-0xff binary from a finalized unsealed guard; all other invalid positions still refuse", file: "pkg/agentic/seal.go", replacements: []replacement{{before: `}
	if !sealRepresentable(seal) {`, after: `}
	copy := seal
	if seal.Data.Kind == SealKindUnsealedBound && seal.Data.Binding != nil && seal.Data.Binding.Binary == "bad-\xff" { copy.Data.Binding = cloneProcessBinding(seal.Data.Binding); copy.Data.Binding.Binary = "binary" }
	if !sealRepresentable(copy) {`}}, testPackage: "./pkg/agentic", testName: "TestExportSealRefusesUnrepresentableEnvelope", runPattern: "^TestExportSealRefusesUnrepresentableEnvelope$", failureText: "invalid finalized export admitted"},
		{name: "seal-typed-import-admits-invalid-utf8", gate: "Typed guard representability", member: "unsealed-bound binary", narrows: "admits only the synthetic bad-0xff binary in an otherwise representable unsealed-bound guard; all other invalid positions still refuse", file: "pkg/agentic/seal.go", replacements: []replacement{{before: `// projection must never validate a different binding than we retain.
	if !sealRepresentable(seal) {`, after: `// projection must never validate a different binding than we retain.
	copy := seal
	if seal.Data.Kind == SealKindUnsealedBound && seal.Data.Binding != nil && seal.Data.Binding.Binary == "bad-\xff" { copy.Data.Binding = cloneProcessBinding(seal.Data.Binding); copy.Data.Binding.Binary = "binary" }
	if !sealRepresentable(copy) {`}}, testPackage: "./pkg/agentic", testName: "TestTypedImportRefusesUnrepresentableStrings", runPattern: "^TestTypedImportRefusesUnrepresentableStrings$", failureText: "typed import invalid UTF-8 binding-binary admitted"},
		{name: "seal-export-collapses-empty-selectors", gate: "Exec guard rework", member: "seal-export-collapses-empty-selectors", narrows: "collapses only empty selector maps to nil, restoring null export; absent maps stay nil and nonempty maps still copy", file: "pkg/agentic/seal.go", replacements: []replacement{{file: "pkg/agentic/seal.go", before: "if selectors == nil {", after: "if len(selectors) == 0 {"}}, testPackage: "./pkg/agentic", testName: "TestFinalizedEmptyEnvRoundTrips", runPattern: "^TestFinalizedEmptyEnvRoundTrips$", failureText: "empty selectors collapsed"},
		{name: "seal-finalize-admits-invalid-utf8-tail", gate: "Exec guard rework", member: "seal-finalize-admits-invalid-utf8-tail", narrows: "admits only the 0xff witness tail token; NUL and every other unrepresentable string still refuse", file: "pkg/agentic/finalize.go", replacements: []replacement{{file: "pkg/agentic/finalize.go", before: `if !guardStringsRepresentable(token) {`, after: `if !guardStringsRepresentable(token) && token != "\xff" {`}}, testPackage: "./pkg/agentic", testName: "TestFinalizePlanRefusesUnrepresentableStrings", runPattern: "^TestFinalizePlanRefusesUnrepresentableStrings$", failureText: "invalid UTF-8 tail admitted"},
		{name: "seal-imported-binary-change-admitted", gate: "Imported process binding", member: "exact bound binary", narrows: "admits only the witness binary; every other binary change still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if plan.Binary != p.binary {`, after: `if plan.Binary != p.binary && plan.Binary != "/synthetic/witness-binary" {`}}, testPackage: "./pkg/agentic", testName: "TestImportedProcessRefusesDrift", runPattern: "^TestImportedProcessRefusesDrift$/^binary-witness$", failureText: "witness binary change admitted"},
		{name: "seal-imported-argv-extra-admitted", gate: "Imported process binding", member: "exact bound argv", narrows: "admits exactly one appended witness argv element; removals, swaps, reorders and other appends still refuse", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if !slices.Equal(plan.Argv, p.argv) {`, after: `if !slices.Equal(plan.Argv, p.argv) && !(len(plan.Argv) == len(p.argv)+1 && slices.Equal(plan.Argv[:len(p.argv)], p.argv) && plan.Argv[len(p.argv)] == "--imported-witness") {`}}, testPackage: "./pkg/agentic", testName: "TestImportedProcessRefusesDrift", runPattern: "^TestImportedProcessRefusesDrift$/^argv-witness$", failureText: "witness argv append admitted"},
		{name: "seal-imported-env-compare-skipped", gate: "Imported process binding", member: "exact bound env", narrows: "skips the env comparison for exactly the appended witness entry; value changes, reorders, duplicates and other appends still refuse", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if !slices.Equal(plan.Env, p.env) {`, after: `if !slices.Equal(plan.Env, p.env) && !(len(plan.Env) == len(p.env)+1 && slices.Equal(plan.Env[:len(p.env)], p.env) && plan.Env[len(p.env)] == "IMPORTED_WITNESS=env-appended") {`}}, testPackage: "./pkg/agentic", testName: "TestImportedProcessRefusesDrift", runPattern: "^TestImportedProcessRefusesDrift$/^env-witness$", failureText: "witness env append admitted"},
		{name: "seal-imported-cwd-change-admitted", gate: "Imported process binding", member: "exact bound cwd", narrows: "admits only the witness cwd; every other cwd change still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if plan.WorkDir != p.workDir {`, after: `if plan.WorkDir != p.workDir && plan.WorkDir != "/synthetic/witness-cwd" {`}}, testPackage: "./pkg/agentic", testName: "TestImportedProcessRefusesDrift", runPattern: "^TestImportedProcessRefusesDrift$/^cwd-witness$", failureText: "witness cwd change admitted"},
		{name: "seal-imported-home-change-admitted", gate: "Imported process binding", member: "exact bound home", narrows: "admits only the witness home; every other home change still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if plan.Home != p.home {`, after: `if plan.Home != p.home && plan.Home != "/synthetic/witness-home" {`}}, testPackage: "./pkg/agentic", testName: "TestImportedProcessRefusesDrift", runPattern: "^TestImportedProcessRefusesDrift$/^home-witness$", failureText: "witness home change admitted"},
		{name: "seal-imported-stdin-change-admitted", gate: "Imported process binding", member: "exact bound stdin", narrows: "admits only the witness stdin attach; other attaches, detaches and byte changes still refuse", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if plan.Stdin.Attached != p.stdin.Attached || !bytes.Equal(plan.Stdin.Bytes, p.stdin.Bytes) {`, after: `if (plan.Stdin.Attached != p.stdin.Attached || !bytes.Equal(plan.Stdin.Bytes, p.stdin.Bytes)) && !(plan.Stdin.Attached && string(plan.Stdin.Bytes) == "imported-witness") {`}}, testPackage: "./pkg/agentic", testName: "TestImportedProcessRefusesDrift", runPattern: "^TestImportedProcessRefusesDrift$/^stdin-attach-witness$", failureText: "witness stdin attach admitted"},
		{name: "seal-imported-verifier-not-called", gate: "Imported process binding", member: "delegate verification", narrows: "skips the imported verifier call for exactly the witness env entry; the exact-process checks and every non-witness delegation still run", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `return p.verifier.VerifyBeforeExec(plan)`, after: `if slices.Contains(plan.Env, "IMPORTED_WITNESS=skip-delegation") { return nil }; return p.verifier.VerifyBeforeExec(plan)`}}, testPackage: "./pkg/agentic/systems/codex", testName: "TestImportedProcessDelegatesToSealedVerifier", runPattern: "^TestImportedProcessDelegatesToSealedVerifier$", failureText: "delegation skipped for witness env"},
		{name: "seal-imported-accessor-returns-alias", gate: "Imported process binding", member: "accessor deep copy", narrows: "aliases only the returned argv slice; env and stdin still copy", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `slices.Clone(p.argv)`, after: `p.argv`}}, testPackage: "./pkg/agentic", testName: "TestImportedProcessAccessorNeverAliases", runPattern: "^TestImportedProcessAccessorNeverAliases$", failureText: "accessor argv aliases bound process"},
		{name: "seal-imported-shape-binary-relative-admitted", gate: "Imported process binding", member: "shape absolute binary", narrows: "admits only the witness relative binary; every other non-absolute binary still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if !filepath.IsAbs(process.Binary) {`, after: `if !filepath.IsAbs(process.Binary) && process.Binary != "witness-relative-binary" {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesNonAbsolutePaths", runPattern: "^TestNewImportedProcessRefusesNonAbsolutePaths$/^relative-binary-witness$", failureText: "witness relative binary admitted"},
		{name: "seal-imported-shape-cwd-relative-admitted", gate: "Imported process binding", member: "shape absolute cwd", narrows: "admits only the witness relative cwd; every other non-absolute cwd still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if !filepath.IsAbs(process.WorkDir) {`, after: `if !filepath.IsAbs(process.WorkDir) && process.WorkDir != "witness-relative-cwd" {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesNonAbsolutePaths", runPattern: "^TestNewImportedProcessRefusesNonAbsolutePaths$/^relative-cwd-witness$", failureText: "witness relative cwd admitted"},
		{name: "seal-imported-shape-home-relative-admitted", gate: "Imported process binding", member: "shape absolute home", narrows: "admits only the witness relative home; every other non-absolute home still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if !filepath.IsAbs(process.Home) {`, after: `if !filepath.IsAbs(process.Home) && process.Home != "witness-relative-home" {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesNonAbsolutePaths", runPattern: "^TestNewImportedProcessRefusesNonAbsolutePaths$/^relative-home-witness$", failureText: "witness relative home admitted"},
		{name: "seal-imported-shape-argv0-admitted", gate: "Imported process binding", member: "shape argv excludes argv[0]", narrows: "admits argv[0] only for the witness binary; every other argv[0] still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if len(process.Argv) > 0 && process.Argv[0] == process.Binary {`, after: `if len(process.Argv) > 0 && process.Argv[0] == process.Binary && process.Binary != "/synthetic/witness-argv0" {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesArgvZero", runPattern: "^TestNewImportedProcessRefusesArgvZero$/^argv0-witness$", failureText: "witness argv[0] admitted"},
		{name: "seal-imported-shape-env-no-equals-admitted", gate: "Imported process binding", member: "shape env well-formed", narrows: "admits only the witness entry without equals; every other malformed entry still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if !found {`, after: `if !found && entry != "WITNESS_NO_EQUALS" {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesMalformedEnv", runPattern: "^TestNewImportedProcessRefusesMalformedEnv$/^no-equals-witness$", failureText: "witness env entry without value admitted"},
		{name: "seal-imported-shape-env-bad-name-admitted", gate: "Imported process binding", member: "shape env name valid", narrows: "admits only the witness empty name; every other invalid name still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if !validEnvironmentName(name) {`, after: `if !validEnvironmentName(name) && entry != "=witness-empty-name" {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesMalformedEnv", runPattern: "^TestNewImportedProcessRefusesMalformedEnv$/^empty-name-witness$", failureText: "witness env entry with empty name admitted"},
		{name: "seal-imported-shape-env-nul-admitted", gate: "Imported process binding", member: "shape env NUL-free", narrows: "admits only the witness NUL entry; every other NUL entry still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if strings.IndexByte(entry, 0) >= 0 {`, after: "if strings.IndexByte(entry, 0) >= 0 && entry != \"WITNESS_NUL=bad\\x00value\" {"}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesMalformedEnv", runPattern: "^TestNewImportedProcessRefusesMalformedEnv$/^nul-value-witness$", failureText: "witness env entry with NUL admitted"},
		{name: "seal-imported-shape-env-duplicate-admitted", gate: "Imported process binding", member: "shape env unique names", narrows: "admits a duplicate only for the witness name; every other duplicate still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if seen[name] {`, after: `if seen[name] && name != "WITNESS_DUP" {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesMalformedEnv", runPattern: "^TestNewImportedProcessRefusesMalformedEnv$/^duplicate-witness$", failureText: "witness duplicate env name admitted"},
		{name: "seal-imported-shape-stdin-detached-admitted", gate: "Imported process binding", member: "shape stdin representation", narrows: "admits detached bytes only for the witness payload; every other detached-bytes shape still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if !process.Stdin.Attached && len(process.Stdin.Bytes) > 0 {`, after: `if !process.Stdin.Attached && len(process.Stdin.Bytes) > 0 && string(process.Stdin.Bytes) != "witness-detached" {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesDetachedStdinWithBytes", runPattern: "^TestNewImportedProcessRefusesDetachedStdinWithBytes$/^detached-bytes-witness$", failureText: "witness detached stdin admitted"},
		{name: "seal-imported-binding-gated-on-exporter", gate: "Imported process binding", member: "seal binding exporter-independent", narrows: "re-gates the seal-binding check on the optional exporter; verifier-only mismatches are admitted while exporter mismatches still refuse", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if hasBinding && (process.Binary != bound.binary || !slices.Equal(process.Argv, bound.argv)) {`, after: `if _, isExporter := verifier.(ExecSealExporter); isExporter && hasBinding && (process.Binary != bound.binary || !slices.Equal(process.Argv, bound.argv)) {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesMismatchWithoutExporter", runPattern: "^TestNewImportedProcessRefusesMismatchWithoutExporter$", failureText: "verifier-only argv mismatch err"},
		{name: "seal-imported-binding-binary-admitted", gate: "Imported process binding", member: "seal binding binary", narrows: "admits only the witness sealed binary at construction; every other sealed binding mismatch still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if hasBinding && (process.Binary != bound.binary || !slices.Equal(process.Argv, bound.argv)) {`, after: `if hasBinding && (process.Binary != bound.binary || !slices.Equal(process.Argv, bound.argv)) && process.Binary != "/synthetic/witness-seal-binary" {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesMismatchedBinding", runPattern: "^TestNewImportedProcessRefusesMismatchedBinding$/^binary-witness$", failureText: "witness sealed binary mismatch admitted"},
		{name: "seal-imported-binding-argv-admitted", gate: "Imported process binding", member: "seal binding argv", narrows: "admits only the witness sealed argv token at construction; every other sealed binding mismatch still refuses", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `if hasBinding && (process.Binary != bound.binary || !slices.Equal(process.Argv, bound.argv)) {`, after: `if hasBinding && (process.Binary != bound.binary || !slices.Equal(process.Argv, bound.argv)) && !slices.Contains(process.Argv, "--witness-seal-argv") {`}}, testPackage: "./pkg/agentic", testName: "TestNewImportedProcessRefusesMismatchedBinding", runPattern: "^TestNewImportedProcessRefusesMismatchedBinding$/^argv-witness$", failureText: "witness sealed argv mismatch admitted"},
		{name: "seal-imported-muse-verifier-not-called", gate: "Imported process binding", member: "Muse delegate verification", narrows: "skips the imported verifier call for exactly the Muse witness env entry; every non-witness delegation still runs", file: "pkg/agentic/imported_process.go", replacements: []replacement{{before: `return p.verifier.VerifyBeforeExec(plan)`, after: `if slices.Contains(plan.Env, "MUSE_WITNESS=skip-delegation") { return nil }; return p.verifier.VerifyBeforeExec(plan)`}}, testPackage: "./pkg/agentic/systems/muse", testName: "TestImportedProcessDelegatesToMuseVerifier", runPattern: "^TestImportedProcessDelegatesToMuseVerifier$", failureText: "delegation skipped for witness env"},
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// Keep the process boundary in the evidence. Aggregate failures cannot lend a
// missing or green plugin another plugin's kill.
func validatePerPluginKills(candidate mutant, output string) error {
	if candidate.validatorMemberID == "" {
		return nil
	}
	if len(candidate.requiredPlugins) == 0 {
		return fmt.Errorf("validator %q has no required plugins", candidate.name)
	}
	records := strings.Split(output, "PLUGIN_PROCESS | ")
	for _, plugin := range candidate.requiredPlugins {
		found := false
		for _, record := range records[1:] {
			header, body, ok := strings.Cut(record, "\n")
			if !ok || !strings.Contains(header, "mutant="+candidate.name+" | plugin="+plugin+" | exit=1 |") {
				continue
			}
			single := candidate
			pattern, err := validatorPluginPattern(candidate.runPattern, plugin)
			if err != nil {
				return err
			}
			single.runPattern = pattern
			for _, name := range mutantFailureTestNames(single, body) {
				parts := strings.Split(name, "/")
				if len(parts) >= 2 && parts[1] == plugin {
					found = true
				}
			}
		}
		if !found {
			return fmt.Errorf("validator %q was not killed by a named negative subtest in a separate failing process for %s", candidate.name, plugin)
		}
	}
	return nil
}

func processEvidencePrefix(name string) string {
	if len(name) <= 100 {
		return name
	}
	digest := sha256.Sum256([]byte(name))
	return name[:100] + "-" + fmt.Sprintf("%x", digest[:6])
}

// Scalar validator witnesses can be masked by a later refusal in the same
// function, including a different field of the same descriptor. Generate the
// witness-limited candidates mechanically; solo/combined real plugin runs
// decide whether any candidate actually proves a downstream bound.
func deriveSameFunctionScalarDownstreamProofs(member curatorValidatorClassMember, files map[string]*parsedSource) []downstreamGuardProof {
	source := files[member.targetFile]
	if source == nil {
		return nil
	}
	function := findFunctionAndBody(source.file, member.targetFunction)
	if function == nil || curatorBooleanFunction(function) {
		return nil
	}
	witness, err := parser.ParseExpr(member.exemption)
	if err != nil {
		return nil
	}
	identifiers := expressionIdentifierSet(witness)
	readsParameter := false
	if function.Type.Params != nil {
		for _, field := range function.Type.Params.List {
			for _, name := range field.Names {
				if identifiers[name.Name] {
					readsParameter = true
				}
			}
		}
	}
	if !readsParameter {
		return nil
	}
	var proofs []downstreamGuardProof
	ast.Inspect(function.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		branch, ok := n.(*ast.IfStmt)
		if !ok || source.fset.Position(branch.Pos()).Line <= member.targetLine {
			return true
		}
		guard := "if " + normalizedNode(source.fset, branch.Cond)
		for _, returned := range returnsInBlock(branch.Body) {
			if len(returned.Results) != 1 || normalizedNode(source.fset, returned.Results[0]) == "nil" {
				continue
			}
			line := source.fset.Position(returned.Pos()).Line
			proofs = append(proofs, downstreamGuardProof{file: member.targetFile, function: member.targetFunction, guard: guard, line: line, returned: normalizedNode(source.fset, returned), exemption: member.exemption, description: fmt.Sprintf("%s:%d %s %s (%s); witness=%s", member.targetFile, source.fset.Position(branch.Pos()).Line, member.targetFunction, guard, normalizedNode(source.fset, returned), member.exemption)})
		}
		return true
	})
	return proofs
}

// Formatting a primary mutation may move later lines. Re-resolve the exact
// function/guard/return triple, requiring uniqueness rather than guessing the
// shifted location. The original line stays in the reported proof identity.
func applyDownstreamNarrowing(path string, proof downstreamGuardProof) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return err
	}
	function := findFunctionAndBody(file, proof.function)
	if function == nil {
		return fmt.Errorf("downstream function %s absent", proof.function)
	}
	var lines []int
	ast.Inspect(function.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		branch, ok := n.(*ast.IfStmt)
		if !ok || "if "+normalizedNode(fset, branch.Cond) != normalizeGuard(proof.guard) {
			return true
		}
		for _, returned := range returnsInBlock(branch.Body) {
			if normalizedNode(fset, returned) == strings.Join(strings.Fields(proof.returned), " ") {
				lines = append(lines, fset.Position(returned.Pos()).Line)
			}
		}
		return true
	})
	if len(lines) != 1 {
		return fmt.Errorf("downstream function/guard/return triple is not unique: %d matches", len(lines))
	}
	return applyASTNarrowingAtSite(path, proof.function, proof.guard, lines[0], proof.returned, proof.exemption)
}

// Each member weakens one policy class while leaving the gate in place.
// These run production BuildPlan assertions, not a stripping-wrapper counter.
func claudeToolPolicyMutants() []mutant {
	policy := func(name, file, before, after, testName, pattern, message, narrows string) mutant {
		return mutant{name: "claude-toolpolicy-" + name, file: file, gate: "Claude AskUserQuestion policy", member: name, narrows: narrows,
			replacements: []replacement{{before: before, after: after}}, testPackage: "./pkg/agentic/systems/claude", testName: testName, runPattern: pattern, failureText: message, preflightPkg: "./pkg/agentic/systems/claude", preflightName: "TestClaudeArgvHasExactlyOneConstructionSite", preflightMatch: "^TestClaudeArgvHasExactlyOneConstructionSite$"}
	}
	return []mutant{
		policy("dryrun-deny", "pkg/agentic/systems/claude/args.go",
			"args = append(args, disallowedToolsDenial)\n\n\tif req.Goal == nil", "if mode != agentic.LaunchModeDryRun {args = append(args, disallowedToolsDenial)}\n\n\tif req.Goal == nil",
			"TestAskUserQuestionDeniedForDryRun", "^TestAskUserQuestionDeniedForDryRun$", "dry run spells a different grammar", "omits denial only for dry-run; exec and interactive retain it"),
		policy("caller-read-dropped", "pkg/agentic/systems/claude/args.go",
			"rest := append([]string(nil), req.NativeArgs...)", "rest := append([]string(nil), req.NativeArgs...); for i, token := range rest {if token == disallowedToolsFlag+\"=Read\" {rest = append(rest[:i],rest[i+1:]...);break}}",
			"TestReviewerCallerOccurrencesPreserved", "^TestReviewerCallerOccurrencesPreserved$", "caller bytes changed", "drops only caller attached Read deny occurrence"),
		policy("allow-bare-skipped", "pkg/agentic/systems/claude/args.go",
			"findReenabledTool(req.NativeArgs); found {", "findReenabledTool(req.NativeArgs); found && tool != deniedToolAskUserQuestion {",
			"TestReEnablingAskUserQuestionIsRefused/native/separate_value", "^TestReEnablingAskUserQuestionIsRefused$/^native$/^separate_value$", "want ErrDeniedToolReEnabled", "admits bare allow member while retaining qualified re-enable checks"),
		policy("settings-bare-skipped", "pkg/agentic/systems/claude/toolpolicy.go",
			"if trimmed := ecmaTrim(rule); nativeToolName(trimmed) == deniedToolAskUserQuestion {", "if trimmed := ecmaTrim(rule); nativeToolName(trimmed) == deniedToolAskUserQuestion && trimmed != deniedToolAskUserQuestion {",
			"TestAskUserQuestionSettingsPolicy/native/inline/exact", "^TestAskUserQuestionSettingsPolicy$/^native$/^inline$/^exact$", "want typed settings re-enable refusal", "admits bare settings allow while retaining qualified rules"),
		policy("settings-unreadable-admitted", "pkg/agentic/systems/claude/toolpolicy.go",
			"if kind != \"\" {", "if kind != \"\" && kind != agentic.SettingsPolicyUnreadable {",
			"TestAskUserQuestionSettingsReadFailure/missing", "^TestAskUserQuestionSettingsReadFailure$/^missing$", "want unreadable settings refusal", "treats unreadable effective source as absent; invalid JSON still refuses"),
		policy("settings-invalid-admitted", "pkg/agentic/systems/claude/toolpolicy.go",
			"if kind != \"\" {", "if kind != \"\" && kind != agentic.SettingsPolicyInvalid {",
			"TestAskUserQuestionSettingsPolicy/native/inline/invalid_json", "^TestAskUserQuestionSettingsPolicy$/^native$/^inline$/^invalid_json$", "want settings invalid refusal", "admits invalid JSON while retaining unreadable source refusal"),
		policy("scalar-value-classified", "pkg/agentic/systems/claude/nativegrammar.go",
			"if i+1 < len(args) {", "if i+1 < len(args) && name != appendSystemPromptFlag {",
			"TestAskUserQuestionScalarOwnership/--append-system-prompt/--allowedTools=AskUserQuestion", "^TestAskUserQuestionScalarOwnership$/^--append-system-prompt$/^--allowedTools=AskUserQuestion$", "scalar value classified", "misclassifies dash-leading append-system-prompt value; other scalar ownership remains"),
		policy("trimspace-restored", "pkg/agentic/systems/claude/toolpolicy.go",
			"rule := ecmaTrim(current.String())", "rule := strings.TrimSpace(current.String())",
			"TestAskUserQuestionECMAScriptRuleTokenization/native/allow_BOM_prefix", "^TestAskUserQuestionECMAScriptRuleTokenization$/^native$/^allow_BOM_prefix$", "want typed re-enable refusal", "restores Go TrimSpace in list tokenization; the BOM allow is admitted while ASCII rules still refuse"),
		policy("settings-rule-trim-restored", "pkg/agentic/systems/claude/toolpolicy.go",
			"if trimmed := ecmaTrim(rule); nativeToolName(trimmed) == deniedToolAskUserQuestion {", "if trimmed := strings.TrimSpace(rule); nativeToolName(trimmed) == deniedToolAskUserQuestion {",
			"TestAskUserQuestionSettingsECMAScriptRules/native/settings_BOM_rule_refused", "^TestAskUserQuestionSettingsECMAScriptRules$/^native$/^settings_BOM_rule_refused$", "want typed settings re-enable refusal", "restores Go TrimSpace for settings allow rules; the BOM settings rule is admitted while ASCII rules still refuse"),
		policy("settings-inline-trim-restored", "pkg/agentic/systems/claude/toolpolicy.go",
			"trimmed := ecmaTrim(value)", "trimmed := strings.TrimSpace(value)",
			"TestAskUserQuestionSettingsECMAScriptRules/native/settings_inline_BOM_wrapped_refused", "^TestAskUserQuestionSettingsECMAScriptRules$/^native$/^settings_inline_BOM_wrapped_refused$", "want typed settings re-enable refusal", "restores Go TrimSpace for inline-JSON detection; BOM-wrapped inline settings read as a file path while plain inline JSON still parses"),
		policy("eager-settings-dropped", "pkg/agentic/systems/claude/toolpolicy.go",
			"eagerVal, eagerOK := lastSettingsValue(eagerSettingsValues(args))", "eagerVal, eagerOK := \"\", false",
			"TestAskUserQuestionEagerSettingsAmbiguity/native/eager_only_separate_cn", "^TestAskUserQuestionEagerSettingsAmbiguity$/^native$/^eager_only_separate_cn$", "want typed settings ambiguity refusal", "drops the eager settings scan; combined-short eager sources never disagree while Commander-visible sources still refuse"),
		policy("ambiguity-skipped", "pkg/agentic/systems/claude/toolpolicy.go",
			"if eagerOK != cmdOK || (eagerOK && eagerVal != cmdVal) {\n\t\treturn \"\", false, settingsPolicyRefusal(agentic.SettingsPolicyAmbiguous)\n\t}", "if eagerOK != cmdOK || (eagerOK && eagerVal != cmdVal) {\n\t\tif eagerOK && !cmdOK {\n\t\t\treturn \"\", false, nil\n\t\t}\n\t\treturn \"\", false, settingsPolicyRefusal(agentic.SettingsPolicyAmbiguous)\n\t}",
			"TestAskUserQuestionEagerSettingsAmbiguity/native/eager_only_separate_cn", "^TestAskUserQuestionEagerSettingsAmbiguity$/^native$/^eager_only_separate_cn$", "want typed settings ambiguity refusal", "admits eager-only parser disagreements; other disagreements still refuse ambiguous"),
		policy("conditional-deny", "pkg/agentic/systems/claude/args.go",
			"args = append(args, disallowedToolsDenial)\n\trest := append([]string(nil), req.NativeArgs...)", "hasCallerDenial := false\n\tfor _, token := range req.NativeArgs {\n\t\tif strings.Contains(token, deniedToolAskUserQuestion) {\n\t\t\thasCallerDenial = true\n\t\t}\n\t}\n\tif !hasCallerDenial {\n\t\targs = append(args, disallowedToolsDenial)\n\t}\n\trest := append([]string(nil), req.NativeArgs...)",
			"TestAskUserQuestionDenialUnconditionalBesideCallerValues/--disallowedTools=AskUserQuestion", "^TestAskUserQuestionDenialUnconditionalBesideCallerValues$/^--disallowedTools=AskUserQuestion$", "module denial missing beside caller denial", "restores conditional emission; caller denials suppress the module denial while other inputs keep it"),
	}
}
