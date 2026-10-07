package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestRepresentedProviderKeysMatchSnapshotAndOverrides(t *testing.T) {
	t.Parallel()
	rows := []struct{ key, entryField, snapshotField, value string }{
		{"name", "Name", "name", `"task local provider"`},
		{"base_url", "BaseURL", "baseURL", `"http://127.0.0.1:38171/v1"`},
		{"wire_api", "", "", `"responses"`},
		{"requires_openai_auth", "", "", "false"},
	}
	keys := RepresentedProviderKeys()
	if len(keys) != len(rows) {
		t.Fatalf("represented keys = %q, want exactly %d", keys, len(rows))
	}
	// Every struct field must be classified. A new carried field cannot hide
	// behind a comparison of two manually maintained key lists.
	entryFields := map[string]bool{}
	snapshotFields := map[string]bool{
		"providerID": true, "digest": true, "modelSlug": true,
		"catalogPath": true, "catalogDigest": true, "catalogData": true,
		"effortVocab": true, "sealed": true,
	}
	for _, row := range rows {
		t.Run(row.key, func(t *testing.T) {
			if !slices.Contains(keys, row.key) {
				t.Fatalf("represented keys %q omit %s", keys, row.key)
			}
		})
		if row.entryField != "" {
			entryFields[row.entryField] = true
			snapshotFields[row.snapshotField] = true
		}
	}
	for _, shape := range []struct {
		typ    reflect.Type
		fields map[string]bool
	}{
		{reflect.TypeOf(privateProvider{}), entryFields},
		{reflect.TypeOf(ProviderSnapshot{}), snapshotFields},
	} {
		if shape.typ.NumField() != len(shape.fields) {
			t.Fatalf("%s field count changed without updating the provider contract table", shape.typ)
		}
		for i := 0; i < shape.typ.NumField(); i++ {
			if !shape.fields[shape.typ.Field(i).Name] {
				t.Fatalf("%s field %s is not classified in the provider contract table", shape.typ, shape.typ.Field(i).Name)
			}
		}
	}
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			home := t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			req := providerRequest(t, home, t.TempDir(), "local-story")
			snapshot := mustSnapshot(t, req)
			entry := snapshot.entry()
			for _, row := range rows {
				if row.entryField != "" {
					carried := reflect.ValueOf(snapshot).Elem().FieldByName(row.snapshotField).String()
					if got := reflect.ValueOf(entry).FieldByName(row.entryField).String(); got != carried || carried == "" {
						t.Fatalf("snapshot field %s is not carried to entry field %s", row.snapshotField, row.entryField)
					}
				}
			}
			for _, bound := range []agentic.LaunchRequest{req, withSnapshot(req, snapshot)} {
				plan, err := buildCodexPlanForMode(t, bound, mode)
				if err != nil {
					t.Fatal(err)
				}
				emitted := map[string]string{}
				for i := 0; i+1 < len(plan.Argv); i++ {
					if plan.Argv[i] != "-c" {
						continue
					}
					key, value, ok := strings.Cut(plan.Argv[i+1], "=")
					if ok && strings.HasPrefix(key, "model_providers.local-story.") {
						key = strings.TrimPrefix(key, "model_providers.local-story.")
						if _, duplicate := emitted[key]; duplicate {
							t.Fatalf("duplicate override %s", key)
						}
						emitted[key] = value
					}
				}
				if len(emitted) != len(keys) {
					t.Fatalf("overrides %v do not match declared keys %q", emitted, keys)
				}
				for _, row := range rows {
					if emitted[row.key] != row.value {
						t.Fatalf("override %s = %q, want %q", row.key, emitted[row.key], row.value)
					}
				}
			}
		})
	}
}

func TestRepresentedProviderKeysAreDetachedAndExact(t *testing.T) {
	t.Parallel()
	original := RepresentedProviderKeys()
	changed := RepresentedProviderKeys()
	changed[0] = "env_key"
	if !reflect.DeepEqual(RepresentedProviderKeys(), original) {
		t.Fatal("caller mutated the public contract")
	}
	for _, key := range []string{"env_key", "http_headers", "request_max_retries", "NAME", " name", "model_provider", "model_catalog_json", ""} {
		if slices.Contains(original, key) {
			t.Errorf("unrepresented key %q declared represented", key)
		}
	}
}

// acceptedPlanSystem is a test-only seam at the real plugin entry points. The
// normal witness uses New unchanged; tagged mutants override Argv or ChildEnv
// before BuildPlan consumes and seals their outputs.
var acceptedPlanSystem = func() agentic.System { return New() }

func TestRepresentedProviderKeysLeaveAcceptedPlansByteIdentical(t *testing.T) {
	t.Parallel()
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun} {
		t.Run(mode.String(), func(t *testing.T) {
			home := t.TempDir()
			writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
			req := providerRequest(t, home, t.TempDir(), "local-story")
			snapshot := mustSnapshot(t, req)
			want, err := os.ReadFile(filepath.Join("testdata", "provider-plans-v0.5.56", mode.String()+".json"))
			if err != nil {
				t.Fatal(err)
			}
			for i, bound := range []agentic.LaunchRequest{req, withSnapshot(req, snapshot)} {
				registry := agentic.NewRegistry()
				if err := registry.Register(acceptedPlanSystem()); err != nil {
					t.Fatal(err)
				}
				plan, err := agentic.BuildPlan(registry, bound, mode)
				if err != nil {
					t.Fatal(err)
				}
				got := acceptedProviderPlanBytes(t, req, plan)
				if !bytes.Equal(got, want) {
					t.Fatalf("path %d accepted plan bytes changed from v0.5.56:\nwant %s\ngot %s", i, want, got)
				}
				if err := plan.VerifyBeforeExec(); err != nil {
					t.Fatalf("plan seal: %v", err)
				}
			}
		})
	}
}

// Mask only allocated directory identities, retaining every argv byte, env
// entry, and content-addressed catalog filename. Sort env without deduping it.
func acceptedProviderPlanBytes(t *testing.T, req agentic.LaunchRequest, plan agentic.Plan) []byte {
	t.Helper()
	binDir := codexPolicyEnvValue(req.Env, "PATH")
	replacer := strings.NewReplacer(
		req.Home, "<CODEX_HOME>",
		req.WorkDir, "<WORK_DIR>",
		binDir, "<BIN_DIR>",
		filepath.Dir(req.Home), "<HOME>",
		launchCatalogs.root, "<CATALOG_DIR>",
	)
	argv := append([]string(nil), plan.Argv...)
	env := append([]string(nil), plan.Env...)
	for i := range argv {
		argv[i] = replacer.Replace(argv[i])
	}
	for i := range env {
		env[i] = replacer.Replace(env[i])
	}
	slices.Sort(env)
	data, err := json.MarshalIndent(struct {
		Argv []string `json:"argv"`
		Env  []string `json:"env"`
	}{argv, env}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestAcceptedProviderPlanDriftMutantsKilledByName(t *testing.T) {
	const witness = "TestRepresentedProviderKeysLeaveAcceptedPlansByteIdentical"
	for _, mutant := range []string{"", "wire-api-argv", "extra-child-env"} {
		t.Run(mutant, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "-tags", "providerkeysmutant", "-count=1", "-run", "^"+witness+"$", ".")
			cmd.Env = append(os.Environ(), "CODEX_PROVIDER_KEYS_MUTANT=", "CODEX_ACCEPTED_PLAN_MUTANT="+mutant)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("mutant timed out: %s", out)
			}
			if mutant == "" {
				if err != nil {
					t.Fatalf("control: %v\n%s", err, out)
				}
				return
			}
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "--- FAIL: "+witness) || !strings.Contains(string(out), "accepted plan bytes changed from v0.5.56") || strings.Contains(string(out), "panic:") || strings.Contains(string(out), "[build failed]") {
				t.Fatalf("mutant did not fail its named byte witness: %v\n%s", err, out)
			}
			t.Logf("mutant %s killed by %s; child exit 1\n%s", mutant, witness, out)
		})
	}
}

func TestRepresentedProviderKeyDropMutantKilledByName(t *testing.T) {
	const witness = "TestRepresentedProviderKeysMatchSnapshotAndOverrides"
	for _, mutant := range []string{"", "drop-requires-openai-auth"} {
		t.Run(mutant, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "-tags", "providerkeysmutant", "-count=1", "-run", "^"+witness+"$", ".")
			cmd.Env = append(os.Environ(), "CODEX_PROVIDER_KEYS_MUTANT="+mutant)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("mutant timed out: %s", out)
			}
			if mutant == "" {
				if err != nil {
					t.Fatalf("control: %v\n%s", err, out)
				}
				return
			}
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "--- FAIL: "+witness) || strings.Contains(string(out), "panic:") || strings.Contains(string(out), "[build failed]") || strings.Contains(string(out), "setup failed") {
				t.Fatalf("mutant did not fail its named witness: %v\n%s", err, out)
			}
			t.Logf("mutant %s killed by %s; child exit 1\n%s", mutant, witness, out)
		})
	}
}
