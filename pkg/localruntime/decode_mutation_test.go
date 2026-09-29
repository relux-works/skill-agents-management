package localruntime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type decoderMutation struct {
	name        string
	weakens     string
	runPattern  string
	failingTest string
	transform   func(string) (string, error)
}

func TestGeneratedDecoderMutantsAreKilled(t *testing.T) {
	packageDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(packageDir, "..", ".."))
	scratchRoot := filepath.Join(repoRoot, ".temp", "BUG-260930-1jgzhe")
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		t.Fatalf("create mutation evidence directory: %v", err)
	}
	decodeSource, err := os.ReadFile(filepath.Join(packageDir, "decode.go"))
	if err != nil {
		t.Fatalf("read decoder source: %v", err)
	}
	mutations := []decoderMutation{
		{
			name:        "restore RFC3339 string type",
			weakens:     "Admit the one RFC3339 string supplied by the refusal test.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^string$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/string",
			transform:   restoreRFC3339StringDecoder,
		},
		{
			name:        "swap seconds and microseconds",
			weakens:     "Swap the seconds and microseconds components while keeping the conversion active.",
			runPattern:  "^TestCLIStatusReaderDecodesStartTimeAsExactUnixInstant$",
			failingTest: "TestCLIStatusReaderDecodesStartTimeAsExactUnixInstant",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source,
					"return time.Unix(*start.Seconds, int64(*start.Microseconds)*1000), nil",
					"return time.Unix(int64(*start.Microseconds), *start.Seconds*1000), nil")
			},
		},
		{
			name:        "scale microseconds as nanoseconds",
			weakens:     "Use microseconds directly as nanoseconds, losing sub-millisecond precision.",
			runPattern:  "^TestCLIStatusReaderDecodesStartTimeAsExactUnixInstant$",
			failingTest: "TestCLIStatusReaderDecodesStartTimeAsExactUnixInstant",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source,
					"int64(*start.Microseconds)*1000",
					"int64(*start.Microseconds)")
			},
		},
		{
			name:        "admit missing runtime start_time",
			weakens:     "Replace a missing runtime.start_time member with the zero instant.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^missing_start_time$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/missing_start_time",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source,
					"if wr.StartTime == nil {\n\t\treturn 0, time.Time{}, fmt.Errorf(\"%w: missing field %q\", ErrDecodeFailure, \"runtime.start_time\")\n\t}",
					"if wr.StartTime == nil {\n\t\tzeroSeconds, zeroMicroseconds := int64(0), int32(0)\n\t\twr.StartTime = &wireProcessStartTime{Seconds: &zeroSeconds, Microseconds: &zeroMicroseconds}\n\t}")
			},
		},
		{
			name:        "admit missing seconds member",
			weakens:     "Narrow the missing-seconds guard to admit the absent member as zero.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^missing_seconds$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/missing_seconds",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source,
					"if start.Seconds == nil {\n\t\treturn time.Time{}, fmt.Errorf(\"%w: missing field %q\", ErrDecodeFailure, \"runtime.start_time.seconds\")\n\t}",
					"if start.Seconds == nil {\n\t\tzero := int64(0)\n\t\tstart.Seconds = &zero\n\t}")
			},
		},
		{
			name:        "admit missing microseconds member",
			weakens:     "Narrow the missing-microseconds guard to admit the absent member as zero.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^missing_microseconds$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/missing_microseconds",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source,
					"if start.Microseconds == nil {\n\t\treturn time.Time{}, fmt.Errorf(\"%w: missing field %q\", ErrDecodeFailure, \"runtime.start_time.microseconds\")\n\t}",
					"if start.Microseconds == nil {\n\t\tzero := int32(0)\n\t\tstart.Microseconds = &zero\n\t}")
			},
		},
		{
			name:        "admit seconds exactly 1.5",
			weakens:     "Narrow the integer-seconds schema to admit only the tested decimal value 1.5.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^non-integer_seconds$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/non-integer_seconds",
			transform:   admitOneDecimalSeconds,
		},
		{
			name:        "admit microseconds exactly 1.5",
			weakens:     "Narrow the integer-microseconds schema to admit only the tested decimal value 1.5.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^non-integer_microseconds$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/non-integer_microseconds",
			transform:   admitOneDecimalMicroseconds,
		},
		{
			name:        "admit negative seconds -1",
			weakens:     "Narrow the non-negative-seconds guard so it admits exactly -1.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^negative_seconds$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/negative_seconds",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source, "if *start.Seconds < 0 {", "if *start.Seconds < -1 {")
			},
		},
		{
			name:        "admit microseconds -1",
			weakens:     "Narrow the lower-bound guard so it admits exactly -1 microsecond.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^microseconds_below_zero$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/microseconds_below_zero",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source, "if *start.Microseconds < 0 {", "if *start.Microseconds < -1 {")
			},
		},
		{
			name:        "admit microseconds 1000000",
			weakens:     "Narrow the upper-bound guard so it admits exactly 1000000 microseconds.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^microseconds_above_range$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/microseconds_above_range",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source, "if *start.Microseconds > 999999 {", "if *start.Microseconds > 1000000 {")
			},
		},
		{
			name:        "drop negative-seconds range check",
			weakens:     "Disable the negative-seconds guard, admitting the full negative range.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^negative_seconds$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/negative_seconds",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source, "if *start.Seconds < 0 {", "if false && *start.Seconds < 0 {")
			},
		},
		{
			name:        "drop microseconds lower range check",
			weakens:     "Disable the lower-bound guard, admitting every negative microsecond value.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^microseconds_below_zero$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/microseconds_below_zero",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source, "if *start.Microseconds < 0 {", "if false && *start.Microseconds < 0 {")
			},
		},
		{
			name:        "drop microseconds upper range check",
			weakens:     "Disable the upper-bound guard, admitting every value above 999999.",
			runPattern:  "^TestCLIStatusReaderRejectsInvalidStartTime$/^microseconds_above_range$",
			failingTest: "TestCLIStatusReaderRejectsInvalidStartTime/microseconds_above_range",
			transform: func(source string) (string, error) {
				return replaceDecoderText(source, "if *start.Microseconds > 999999 {", "if false && *start.Microseconds > 999999 {")
			},
		},
	}

	var table strings.Builder
	table.WriteString("| Mutant | Weakened behavior | Named test that fails | Result |\n")
	table.WriteString("| --- | --- | --- | --- |\n")
	for _, mutation := range mutations {
		mutantDir, err := os.MkdirTemp(scratchRoot, "decoder-mutant-")
		if err != nil {
			t.Fatalf("create %s mutant directory: %v", mutation.name, err)
		}
		mutantSource, err := mutation.transform(string(decodeSource))
		if err != nil {
			t.Fatalf("generate %s mutant: %v", mutation.name, err)
		}
		if err := copyLocalruntimePackage(packageDir, mutantDir, mutantSource); err != nil {
			t.Fatalf("prepare %s mutant: %v", mutation.name, err)
		}

		command := exec.Command("go", "test", "-run", mutation.runPattern, "-count=1")
		command.Dir = mutantDir
		output, commandErr := command.CombinedOutput()
		var exitErr *exec.ExitError
		if commandErr == nil || !errors.As(commandErr, &exitErr) || !strings.Contains(string(output), "--- FAIL: "+mutation.failingTest) {
			t.Errorf("%s was not killed by %s (error=%v):\n%s", mutation.name, mutation.failingTest, commandErr, output)
			fmt.Fprintf(&table, "| %s | %s | %s | survived or wrong failure |\n", mutation.name, mutation.weakens, mutation.failingTest)
			continue
		}
		fmt.Fprintf(&table, "| %s | %s | %s (exit %d) | killed |\n", mutation.name, mutation.weakens, mutation.failingTest, exitErr.ExitCode())
		if err := os.RemoveAll(mutantDir); err != nil {
			t.Fatalf("remove %s mutant scratch: %v", mutation.name, err)
		}
	}

	evidencePath := filepath.Join(scratchRoot, "mutation-table.md")
	if err := os.WriteFile(evidencePath, []byte(table.String()), 0o644); err != nil {
		t.Fatalf("write mutation evidence: %v", err)
	}
}

func restoreRFC3339StringDecoder(source string) (string, error) {
	tick := string(rune(96))
	beforeType := "*wireProcessStartTime " + tick + "json:\"start_time\"" + tick
	afterType := "string " + tick + "json:\"start_time\"" + tick
	mutated, err := replaceDecoderText(source, beforeType, afterType)
	if err != nil {
		return "", err
	}
	oldBlock := strings.Join([]string{
		"\tif wr.StartTime == nil {",
		"\t\treturn 0, time.Time{}, fmt.Errorf(\"%w: missing field %q\", ErrDecodeFailure, \"runtime.start_time\")",
		"\t}",
		"\tparsed, err := decodeProcessStartTime(*wr.StartTime)",
		"\tif err != nil {",
		"\t\treturn 0, time.Time{}, err",
		"\t}",
	}, "\n")
	newBlock := strings.Join([]string{
		"\tparsed, err := time.Parse(time.RFC3339, wr.StartTime)",
		"\tif err != nil {",
		"\t\treturn 0, time.Time{}, fmt.Errorf(\"%w: field %q.start_time is not an RFC3339 timestamp: %v\", ErrDecodeFailure, \"runtime\", err)",
		"\t}",
	}, "\n")
	return replaceDecoderText(mutated, oldBlock, newBlock)
}

func admitOneDecimalSeconds(source string) (string, error) {
	mutated, err := replaceDecoderText(source, "Seconds      *int64", "Seconds      *float64")
	if err != nil {
		return "", err
	}
	mutated, err = replaceDecoderText(mutated,
		"if *start.Seconds < 0 {",
		"if float64(int64(*start.Seconds)) != *start.Seconds && *start.Seconds != 1.5 {\n\t\treturn time.Time{}, fmt.Errorf(\"%w: field %q is not an integer\", ErrDecodeFailure, \"runtime.start_time.seconds\")\n\t}\n\tif *start.Seconds < 0 {")
	if err != nil {
		return "", err
	}
	return replaceDecoderText(mutated,
		"return time.Unix(*start.Seconds, int64(*start.Microseconds)*1000), nil",
		"return time.Unix(int64(*start.Seconds), int64(*start.Microseconds)*1000), nil")
}

func admitOneDecimalMicroseconds(source string) (string, error) {
	mutated, err := replaceDecoderText(source, "Microseconds *int32", "Microseconds *float64")
	if err != nil {
		return "", err
	}
	return replaceDecoderText(mutated,
		"if *start.Microseconds < 0 {",
		"if float64(int64(*start.Microseconds)) != *start.Microseconds && *start.Microseconds != 1.5 {\n\t\treturn time.Time{}, fmt.Errorf(\"%w: field %q is not an integer\", ErrDecodeFailure, \"runtime.start_time.microseconds\")\n\t}\n\tif *start.Microseconds < 0 {")
}

func replaceDecoderText(source, before, after string) (string, error) {
	if strings.Count(source, before) != 1 {
		return "", fmt.Errorf("expected one source fragment, found %d for %q", strings.Count(source, before), before)
	}
	return strings.Replace(source, before, after, 1), nil
}

func copyLocalruntimePackage(sourceDir, targetDir, mutatedDecoder string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || entry.Name() == "decode.go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(targetDir, entry.Name()), data, 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(targetDir, "decode.go"), []byte(mutatedDecoder), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(targetDir, "go.mod"), []byte("module localruntime-mutant\n\ngo 1.25.5\n"), 0o644); err != nil {
		return err
	}
	fixtureDir := filepath.Join(targetDir, "testdata")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		return err
	}
	fixture, err := os.ReadFile(filepath.Join(sourceDir, "testdata", "status-v0.1.0-runtime-present.json"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(fixtureDir, "status-v0.1.0-runtime-present.json"), fixture, 0o644)
}
