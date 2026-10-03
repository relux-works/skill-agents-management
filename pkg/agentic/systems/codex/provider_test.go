package codex

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func providerRequest(t *testing.T, home, workDir, providerID string) agentic.LaunchRequest {
	t.Helper()
	layout := newStubLayout(t)
	writeStubExecutable(t, layout.binDir, executableName)
	return agentic.LaunchRequest{
		System:        New().ID(),
		Effort:        "low",
		Model:         agentic.Model{ID: "Qwen3.8-27B-Q4_K_M"},
		WorkDir:       workDir,
		Home:          home,
		Env:           append(layout.env, "HOME="+filepath.Dir(home), "CODEX_HOME="+home),
		LocalProvider: &agentic.LocalProviderBinding{ID: providerID},
	}
}

func writeProviderConfig(t *testing.T, home, providerID, baseURL, wireAPI string, requiresAuth bool) string {
	t.Helper()
	// Every success-path local launch needs native catalog metadata for the
	// test slug; failure-path tests fail provider validation first, so the
	// catalog never masks their expected kind.
	catalogName := writeLocalCatalog(t, home, "Qwen3.8-27B-Q4_K_M", []string{"low"})
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create Codex home: %v", err)
	}
	content := "model_provider = \"openai\"\n" +
		"model_catalog_json = \"" + catalogName + "\"\n\n" +
		"[model_providers." + providerID + "]\n" +
		"name = \"task local provider\"\n" +
		"base_url = \"" + baseURL + "\"\n" +
		"wire_api = \"" + wireAPI + "\"\n" +
		"requires_openai_auth = " + map[bool]string{true: "true", false: "false"}[requiresAuth] + "\n"
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write Codex config: %v", err)
	}
	return content
}

func writeProviderConfigBody(t *testing.T, home, body string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create Codex home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write Codex config: %v", err)
	}
}

func buildCodexPlan(t *testing.T, req agentic.LaunchRequest) (agentic.Plan, error) {
	t.Helper()
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("register Codex: %v", err)
	}
	return agentic.BuildPlan(registry, req, agentic.LaunchModeExec)
}

func TestBuildPlanSelectsOnlyTheExplicitPrivateLocalProvider(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)

	localRequest := providerRequest(t, home, workDir, "local-story")
	localPlan, err := buildCodexPlan(t, localRequest)
	if err != nil {
		t.Fatalf("BuildPlan(local provider): %v", err)
	}
	if !containsPair(localPlan.Argv, "-c", `model_provider="local-story"`) {
		t.Fatalf("local argv %q does not explicitly select the bound provider", localPlan.Argv)
	}
	if containsPair(localPlan.Argv, "-c", `model_providers.local-story.requires_openai_auth=true`) {
		t.Fatalf("local argv enables OpenAI account authentication: %q", localPlan.Argv)
	}

	nativeRequest := localRequest
	nativeRequest.LocalProvider = nil
	nativeRequest.Effort = ""
	nativePlan, err := buildCodexPlan(t, nativeRequest)
	if err != nil {
		t.Fatalf("BuildPlan(native subscription): %v", err)
	}
	if containsArgPrefix(nativePlan.Argv, "model_provider=") {
		t.Fatalf("native argv contains a provider override: %q", nativePlan.Argv)
	}
	wantNative := []string{
		"--search", "-a", "never", "exec", "-m", "Qwen3.8-27B-Q4_K_M",
		bypassApprovalsAndSandboxFlag, "--skip-git-repo-check", "-C", workDir, "-",
	}
	if !reflect.DeepEqual(nativePlan.Argv, wantNative) {
		t.Fatalf("native subscription argv = %q, want the unchanged native grammar %q", nativePlan.Argv, wantNative)
	}
}

// TestBuildPlanBindsCodexHomeToTheValidatedLocalProviderHome is the regression
// from review finding F1. Codex resolves HOME/.codex when CODEX_HOME is absent;
// the local-provider launch must pin the validated Home into the child env so
// a same-id provider in the operator's default home cannot contribute auth or
// headers after the plan has been validated.
func TestBuildPlanBindsCodexHomeToTheValidatedLocalProviderHome(t *testing.T) {
	home, operatorHome, workDir := t.TempDir(), t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	decoyHome := filepath.Join(operatorHome, ".codex")
	if err := os.MkdirAll(decoyHome, 0o700); err != nil {
		t.Fatalf("create operator default Codex home: %v", err)
	}
	decoyConfig := `model_provider = "local-story"

[model_providers.local-story]
name = "unvalidated home"
base_url = "http://127.0.0.1:38172/v1"
wire_api = "responses"
requires_openai_auth = false
env_key = "OPENAI_API_KEY"

[model_providers.local-story.http_headers]
X-Unvalidated-Home = "operator-default"
`
	if err := os.WriteFile(filepath.Join(decoyHome, "config.toml"), []byte(decoyConfig), 0o600); err != nil {
		t.Fatalf("write operator default Codex config: %v", err)
	}

	req := providerRequest(t, home, workDir, "local-story")
	req.Env = filterEnvKeys(req.Env, "CODEX_HOME")
	req.Env = agentic.SetEnvValue(req.Env, "HOME", operatorHome)
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatalf("BuildPlan(local provider with no incoming CODEX_HOME): %v", err)
	}
	if got := codexPolicyEnvValue(plan.Env, "CODEX_HOME"); got != home {
		t.Fatalf("BuildPlan child CODEX_HOME = %q, want validated binding home %q", got, home)
	}
	if !containsPair(plan.Argv, "-c", `model_provider="local-story"`) ||
		!containsPair(plan.Argv, "-c", `model_providers.local-story.base_url="http://127.0.0.1:38171/v1"`) {
		t.Fatalf("BuildPlan did not pin the provider validated from the private home: %q", plan.Argv)
	}
}

func TestBuildPlanUsesTwoPrivateProviderBindingsWithOneSharedProjectPolicy(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	projectConfigDir := filepath.Join(project, ".codex")
	if err := os.MkdirAll(projectConfigDir, 0o700); err != nil {
		t.Fatalf("create shared project config: %v", err)
	}
	projectPolicy := []byte("model_reasoning_effort = \"low\"\n")
	projectConfig := filepath.Join(projectConfigDir, "config.toml")
	if err := os.WriteFile(projectConfig, projectPolicy, 0o600); err != nil {
		t.Fatalf("write shared project policy: %v", err)
	}
	firstHome, secondHome := filepath.Join(t.TempDir(), "codex"), filepath.Join(t.TempDir(), "codex")
	writeProviderConfig(t, firstHome, "local-one", "http://127.0.0.1:38171/v1", "responses", false)
	writeProviderConfig(t, secondHome, "local-two", "http://127.0.0.1:38172/v1", "responses", false)

	first, err := buildCodexPlan(t, providerRequest(t, firstHome, project, "local-one"))
	if err != nil {
		t.Fatalf("BuildPlan(first private home): %v", err)
	}
	second, err := buildCodexPlan(t, providerRequest(t, secondHome, project, "local-two"))
	if err != nil {
		t.Fatalf("BuildPlan(second private home): %v", err)
	}
	if !containsPair(first.Argv, "-c", `model_provider="local-one"`) || containsPair(first.Argv, "-c", `model_provider="local-two"`) {
		t.Fatalf("first home did not remain bound to its own provider: %q", first.Argv)
	}
	if !containsPair(second.Argv, "-c", `model_provider="local-two"`) || containsPair(second.Argv, "-c", `model_provider="local-one"`) {
		t.Fatalf("second home did not remain bound to its own provider: %q", second.Argv)
	}
	if first.WorkDir != second.WorkDir || first.WorkDir != project {
		t.Fatalf("shared project workdirs differ: %q / %q", first.WorkDir, second.WorkDir)
	}
	after, err := os.ReadFile(projectConfig)
	if err != nil {
		t.Fatalf("read shared project policy after both plans: %v", err)
	}
	if !reflect.DeepEqual(after, projectPolicy) {
		t.Fatalf("shared project policy changed: %q", after)
	}
}

func TestBuildPlanRefusesLocalProviderBindingAndTransportFailures(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		kind       agentic.LocalProviderRefusalKind
		want       error
		configure  func(t *testing.T, req *agentic.LaunchRequest)
	}{
		{
			name: "absent_private_config",
			kind: agentic.LocalProviderAbsent,
			want: agentic.ErrLocalProviderAbsent,
		},
		{
			name: "private_config_read_failure_is_not_absence",
			kind: agentic.LocalProviderReadFailed,
			want: agentic.ErrLocalProviderReadFailed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				t.Helper()
				if err := os.MkdirAll(req.Home, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(req.Home, "config.toml"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "malformed_private_config",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				t.Helper()
				if err := os.MkdirAll(req.Home, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(req.Home, "config.toml"), []byte("[broken\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "malformed_private_config_after_valid_prefix",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `[model_providers.local-story]
name = "task local provider"
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false
[broken
`)
			},
		},
		{
			name: "provider_home_cannot_be_resolved",
			kind: agentic.LocalProviderUnbound,
			want: agentic.ErrLocalProviderUnbound,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				req.Home = "~"
				req.Env = filterEnvKeys(req.Env, "HOME")
			},
		},
		{
			name: "private_provider_table_missing",
			kind: agentic.LocalProviderUnbound,
			want: agentic.ErrLocalProviderUnbound,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `model_provider = "local-story"
`)
			},
		},
		{
			name: "malformed_provider_id",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				req.LocalProvider = &agentic.LocalProviderBinding{ID: " local-story"}
			},
		},
		{
			name:       "malformed_dotted_provider_id",
			providerID: "local-a.b",
			kind:       agentic.LocalProviderMalformed,
			want:       agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `model_provider = "local-a.b"

[model_providers."local-a.b"]
name = "task local provider"
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false
`)
			},
		},
		{
			name:       "malformed_slash_provider_id",
			providerID: "local-a/b",
			kind:       agentic.LocalProviderMalformed,
			want:       agentic.ErrLocalProviderMalformed,
		},
		{
			name: "malformed_provider_name_control_character",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				t.Helper()
				if err := os.MkdirAll(req.Home, 0o700); err != nil {
					t.Fatal(err)
				}
				config := `model_provider = "openai"

[model_providers.local-story]
name = "Task \u007f provider"
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false
`
				if err := os.WriteFile(filepath.Join(req.Home, "config.toml"), []byte(config), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "malformed_provider_name_leading_control_character",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				t.Helper()
				if err := os.MkdirAll(req.Home, 0o700); err != nil {
					t.Fatal(err)
				}
				// Control at index 0: IndexFunc returns 0, and >= 0 catches
				// it while >= 1 would not (mutant M-provider.go-L151-O4-C1).
				config := "model_provider = \"openai\"\n\n[model_providers.local-story]\nname = \"\\u0001task provider\"\nbase_url = \"http://127.0.0.1:38171/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n"
				if err := os.WriteFile(filepath.Join(req.Home, "config.toml"), []byte(config), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "malformed_provider_entry_type",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `model_providers.local-story = "not a provider table"
`)
			},
		},
		{
			name: "malformed_provider_name_empty",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `[model_providers.local-story]
name = "   "
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false
`)
			},
		},
		{
			name: "malformed_provider_name_empty_string",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `[model_providers.local-story]
name = ""
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false
`)
			},
		},
		{
			name: "malformed_provider_name_type",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `[model_providers.local-story]
name = 123
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false
`)
			},
		},
		{
			name: "malformed_provider_name_trailing_space",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `[model_providers.local-story]
name = "task local provider "
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false
`)
			},
		},
		{
			name: "malformed_base_url_type",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `[model_providers.local-story]
name = "task local provider"
base_url = 123
wire_api = "responses"
requires_openai_auth = false
`)
			},
		},
		{
			name: "malformed_base_url_whitespace",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1 ", "responses", false)
			},
		},
		{
			name: "endpoint_port_above_65535_is_malformed",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:70000/v1", "responses", false)
			},
		},
		{
			name: "endpoint_port_65536_is_malformed",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:65536/v1", "responses", false)
			},
		},
		{
			name: "endpoint_port_integer_overflow_is_malformed",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:999999999999999999999999999999/v1", "responses", false)
			},
		},
		{
			name: "conflicting_Codex_home_binding",
			kind: agentic.LocalProviderConflicting,
			want: agentic.ErrLocalProviderConflicting,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				req.Env = append(req.Env, "CODEX_HOME="+t.TempDir())
			},
		},
		{
			name: "conflicting_Codex_home_binding_with_trailing_slash",
			kind: agentic.LocalProviderConflicting,
			want: agentic.ErrLocalProviderConflicting,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				otherHome := filepath.Join(t.TempDir(), "different-codex-home")
				req.Env = append(req.Env, "CODEX_HOME="+otherHome+string(os.PathSeparator))
			},
		},
		{
			name: "provider_not_bound_in_private_home",
			kind: agentic.LocalProviderUnbound,
			want: agentic.ErrLocalProviderUnbound,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-other", "http://127.0.0.1:38171/v1", "responses", false)
			},
		},
		{
			name: "unsupported_chat_completions_transport",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "chat_completions", false)
			},
		},
		{
			name: "unsupported_legacy_chat_wire_api",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "chat", false)
			},
		},
		{
			name: "account_auth_required_by_local_binding",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", true)
			},
		},
		{
			name: "credential_env_key_is_refused",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				path := filepath.Join(req.Home, "config.toml")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, []byte("env_key = \"OPENAI_API_KEY\"\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "credential_api_key_is_refused",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				path := filepath.Join(req.Home, "config.toml")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, []byte("api_key = \"test-secret\"\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "credential_experimental_bearer_token_is_refused",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
				path := filepath.Join(req.Home, "config.toml")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, []byte("experimental_bearer_token = \"test-secret\"\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "credential_auth_table_is_refused",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `model_provider = "openai"

[model_providers.local-story]
name = "task local provider"
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = false

[model_providers.local-story.auth]
token = "test-secret"
`)
			},
		},
		{
			name: "non_loopback_endpoint",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://192.0.2.17/v1", "responses", false)
			},
		},
		{
			name: "non_loopback_hostname_endpoint",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://example.invalid/v1", "responses", false)
			},
		},
		{
			name: "https_endpoint_is_unsupported",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "https://127.0.0.1:38171/v1", "responses", false)
			},
		},
		{
			name: "unspecified_ipv4_endpoint_is_unsupported",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://0.0.0.0:38171/v1", "responses", false)
			},
		},
		{
			name: "localhost_hostname_is_unsupported",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://localhost:38171/v1", "responses", false)
			},
		},
		{
			name: "endpoint_host_missing_is_malformed",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http:///v1", "responses", false)
			},
		},
		{
			name: "endpoint_port_zero_is_malformed",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:0/v1", "responses", false)
			},
		},
		{
			name: "endpoint_userinfo_is_malformed",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://user:pass@127.0.0.1:38171/v1", "responses", false)
			},
		},
		{
			name: "endpoint_query_is_malformed",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1?token=hidden", "responses", false)
			},
		},
		{
			name: "endpoint_fragment_is_malformed",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1#fragment", "responses", false)
			},
		},
		{
			name: "endpoint_invalid_escape_is_malformed",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "local-story", "http://127.0.0.1:38171/v1%zz", "responses", false)
			},
		},
		{
			name: "requires_openai_auth_is_required",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `model_provider = "openai"

[model_providers.local-story]
name = "task local provider"
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
`)
			},
		},
		{
			name: "requires_openai_auth_must_be_boolean",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `model_provider = "openai"

[model_providers.local-story]
name = "task local provider"
base_url = "http://127.0.0.1:38171/v1"
wire_api = "responses"
requires_openai_auth = "false"
`)
			},
		},
		{
			name: "wire_api_is_required",
			kind: agentic.LocalProviderMalformed,
			want: agentic.ErrLocalProviderMalformed,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfigBody(t, req.Home, `[model_providers.local-story]
name = "task local provider"
base_url = "http://127.0.0.1:38171/v1"
requires_openai_auth = false
`)
			},
		},
		{
			name: "builtin_provider_id_is_not_a_local_binding",
			kind: agentic.LocalProviderUnsupported,
			want: agentic.ErrLocalProviderUnsupported,
			configure: func(t *testing.T, req *agentic.LaunchRequest) {
				writeProviderConfig(t, req.Home, "openai", "http://127.0.0.1:38171/v1", "responses", false)
				req.LocalProvider = &agentic.LocalProviderBinding{ID: "openai"}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home, workDir := t.TempDir(), t.TempDir()
			providerID := test.providerID
			if providerID == "" {
				providerID = "local-story"
			}
			req := providerRequest(t, home, workDir, providerID)
			if test.configure != nil {
				test.configure(t, &req)
			}
			plan, err := buildCodexPlan(t, req)
			if !errors.Is(err, test.want) {
				t.Fatalf("BuildPlan error = %v, want %v", err, test.want)
			}
			var refusal *agentic.LocalProviderRefusal
			if !errors.As(err, &refusal) || refusal.Kind != test.kind {
				t.Fatalf("BuildPlan error = %v, want typed refusal kind %q", err, test.kind)
			}
			if !reflect.DeepEqual(plan, agentic.Plan{}) {
				t.Fatalf("refused launch returned a plan: %#v", plan)
			}
			if strings.Contains(err.Error(), home) || strings.Contains(err.Error(), "192.0.2.17") {
				t.Fatalf("refusal leaked a private path or endpoint: %v", err)
			}
		})
	}
}

// TestLocalProviderPortBoundariesAdmit1And65535 pins the valid port edges:
// 1 and 65535 succeed (mutants M-provider.go-L203-O2-C1 and -O3-C1 refuse or
// admit the wrong edge).
func TestLocalProviderPortBoundariesAdmit1And65535(t *testing.T) {
	t.Parallel()
	for _, port := range []string{"1", "65535"} {
		home, workDir := t.TempDir(), t.TempDir()
		writeProviderConfig(t, home, "local-story", "http://127.0.0.1:"+port+"/v1", "responses", false)
		if _, err := buildCodexPlan(t, providerRequest(t, home, workDir, "local-story")); err != nil {
			t.Fatalf("BuildPlan(port %s) = %v, want success", port, err)
		}
	}
}

func TestBuildPlanRefusesAnEmptyLocalProviderBinding(t *testing.T) {
	t.Parallel()
	req := providerRequest(t, t.TempDir(), t.TempDir(), "")
	plan, err := buildCodexPlan(t, req)
	if !errors.Is(err, agentic.ErrLocalProviderUnbound) {
		t.Fatalf("BuildPlan error = %v, want %v", err, agentic.ErrLocalProviderUnbound)
	}
	if !reflect.DeepEqual(plan, agentic.Plan{}) {
		t.Fatalf("unbound request returned a plan: %#v", plan)
	}
}

// TestPluginArgvRefusesAnEmptyLocalProviderBindingDirectly drives the
// plugin's own empty-ID gate for a caller holding the plugin directly.
// BuildPlan refuses empty bindings before dispatch, so this site is
// unreachable via BuildPlan; without this test the guard has no mapped
// negative for provider.go's blank-binding return.
func TestPluginArgvRefusesAnEmptyLocalProviderBindingDirectly(t *testing.T) {
	t.Parallel()
	req := providerRequest(t, t.TempDir(), t.TempDir(), "   ")
	if _, err := New().Argv(req, agentic.LaunchModeExec); !errors.Is(err, agentic.ErrLocalProviderUnbound) {
		t.Fatalf("Argv(empty provider ID) = %v, want %v", err, agentic.ErrLocalProviderUnbound)
	}
}

func TestBuildPlanRefusesLocalProviderForInteractiveMode(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	req := providerRequest(t, home, workDir, "local-story")
	registry := agentic.NewRegistry()
	if err := registry.Register(New()); err != nil {
		t.Fatalf("register Codex: %v", err)
	}
	plan, err := agentic.BuildPlan(registry, req, agentic.LaunchModeInteractive)
	if !errors.Is(err, agentic.ErrLocalProviderUnsupported) {
		t.Fatalf("BuildPlan(interactive local provider) error = %v, want %v", err, agentic.ErrLocalProviderUnsupported)
	}
	if !reflect.DeepEqual(plan, agentic.Plan{}) {
		t.Fatalf("unsupported interactive binding returned a plan: %#v", plan)
	}
}

func TestBuildPlanPinsLocalProviderAfterAProfileSelection(t *testing.T) {
	t.Parallel()
	home, workDir := t.TempDir(), t.TempDir()
	writeProviderConfig(t, home, "local-story", "http://127.0.0.1:38171/v1", "responses", false)
	req := providerRequest(t, home, workDir, "local-story")
	req.Profile = "private-profile"
	plan, err := buildCodexPlan(t, req)
	if err != nil {
		t.Fatalf("BuildPlan(local provider with profile): %v", err)
	}
	if !containsPair(plan.Argv, "-c", `model_provider="local-story"`) ||
		!containsPair(plan.Argv, "-c", `model_providers.local-story.base_url="http://127.0.0.1:38171/v1"`) {
		t.Fatalf("profile overrides were not pinned back to the private local binding: %q", plan.Argv)
	}
}

func containsPair(args []string, key, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key && args[index+1] == value {
			return true
		}
	}
	return false
}

func containsArgPrefix(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}
