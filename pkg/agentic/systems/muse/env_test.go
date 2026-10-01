package muse

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/internal/paritycase"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

func TestMuseParentEnvAllowlistIsPinnedByteForByte(t *testing.T) {
	want := strings.ReplaceAll(`HOME\tLocates Muse's per-user configuration and state.
LANG\tSelects the process locale when no category override is set.
LC_ALL\tSelects the process locale across all locale categories.
LC_COLLATE\tSelects locale-aware string collation.
LC_CTYPE\tSelects locale-aware character classification.
LC_MESSAGES\tSelects the locale used for process messages.
LC_MONETARY\tSelects locale-aware monetary formatting.
LC_NUMERIC\tSelects locale-aware numeric formatting.
LC_TIME\tSelects locale-aware date and time formatting.
LOGNAME\tPreserves the inherited login identity for child tools.
PATH\tResolves Muse and the child tools it starts.
SHELL\tIdentifies the configured shell used by child tooling.
TERM\tLets terminal-aware output select supported control sequences.
TMPDIR\tProvides the platform temporary directory for runtime files.
TZ\tPreserves the configured local time zone for timestamps.
USER\tPreserves the inherited user identity for child tools.
XDG_CACHE_HOME\tSelects the XDG cache directory used by CLI dependencies.
XDG_CONFIG_HOME\tSelects the XDG configuration directory used by Muse and CLI dependencies.
XDG_DATA_HOME\tSelects the XDG data directory used by CLI dependencies.
XDG_RUNTIME_DIR\tSelects the XDG runtime directory used by CLI dependencies.
XDG_STATE_HOME\tSelects the XDG state directory used by CLI dependencies.`, `\t`, "\t")

	lines := make([]string, 0, len(museParentEnvAllowlist))
	for i, rule := range museParentEnvAllowlist {
		if strings.ContainsAny(rule.name+rule.reason, "\r\n") {
			t.Fatalf("allowlist entry %q reason must be one line", rule.name)
		}
		if strings.ContainsAny(rule.name, "*?[]") {
			t.Fatalf("allowlist entry %q is not an exact name", rule.name)
		}
		if i > 0 && museParentEnvAllowlist[i-1].name >= rule.name {
			t.Fatalf("allowlist is not strictly sorted at %q then %q", museParentEnvAllowlist[i-1].name, rule.name)
		}
		lines = append(lines, rule.name+"\t"+rule.reason)
	}
	if got := strings.Join(lines, "\n"); got != want {
		t.Fatalf("Muse parent allowlist changed byte-for-byte:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMuseParentEnvAllowlistContainsNoCredentialShapedName(t *testing.T) {
	if names := credentialShapedAllowlistNames(museParentEnvAllowlist); len(names) != 0 {
		t.Fatalf("credential-shaped names are present in the Muse parent allowlist: %v", names)
	}
}

func credentialShapedAllowlistNames(rules []parentEnvRule) []string {
	var names []string
	for _, rule := range rules {
		if isCredentialShapedParentEnvName(rule.name) {
			names = append(names, rule.name)
		}
	}
	sort.Strings(names)
	return names
}

func TestAllowlistGuardContractMatrix(t *testing.T) {
	// Keep this normative list independent of credentialEnvLongWords so deleting
	// an implementation marker cannot silently shrink the generated contract.
	contractWords := []string{
		"TOKEN", "SECRET", "PASSWORD", "PASSWD", "PASSPHRASE", "PASS", "CREDENTIAL",
		"APIKEY", "PRIVATEKEY", "KEY", "BEARER", "COOKIE", "AUTH", "SESSION", "CERT",
		"SIGNING", "PAT",
	}
	decorations := []struct {
		name  string
		apply func(string) string
	}{
		{name: "bare", apply: func(word string) string { return word }},
		{name: "PREFIX_w", apply: func(word string) string { return "PREFIX_" + word }},
		{name: "w_SUFFIX", apply: func(word string) string { return word + "_SUFFIX" }},
		{name: "PREFIXw", apply: func(word string) string { return "PREFIX" + word }},
		{name: "wSUFFIX", apply: func(word string) string { return word + "SUFFIX" }},
		{name: "w2", apply: func(word string) string { return word + "2" }},
		{name: "w-X", apply: func(word string) string { return word + "-X" }},
		{name: "w.X", apply: func(word string) string { return word + ".X" }},
		{name: "X-w", apply: func(word string) string { return "X-" + word }},
		{name: "lower-case", apply: strings.ToLower},
		{name: "mixed-case", apply: mixedCaseCredentialWord},
	}

	generated := 0
	for _, word := range contractWords {
		for _, decoration := range decorations {
			name := decoration.apply(word)
			want := true
			if word == "PAT" && decoration.name == "wSUFFIX" {
				// PAT as a prefix inside a longer token is the documented bound.
				want = false
			}
			generated++
			t.Run(word+"/"+decoration.name, func(t *testing.T) {
				if got := isCredentialShapedParentEnvName(name); got != want {
					t.Errorf("isCredentialShapedParentEnvName(%q) = %t, want %t", name, got, want)
				}
			})
		}
	}
	if generated != 187 {
		t.Fatalf("generated guard matrix has %d cases, want 187", generated)
	}

	// These names are retained from the rev1, rev2, rev4 and rev5 verdicts.
	// They supplement the generated matrix with exact previously published
	// spellings and the historical non-matches.
	for _, tc := range []struct {
		name string
		want bool
	}{
		{name: "MUSE_APIKEY", want: true},
		{name: "MUSE_AUTHTOKEN", want: true},
		{name: "MUSE_ACCESSTOKEN", want: true},
		{name: "MUSE_PRIVATEKEY", want: true},
		{name: "LC_APIKEY", want: true},
		{name: "MUSE_TOKEN", want: true},
		{name: "MUSE_SECRETS", want: true},
		{name: "MUSE_KEYCHAIN_PATH", want: true},
		{name: "muse_api_key", want: true},
		{name: "MUSE_AUTH", want: true},
		{name: "MUSE_BEARER", want: true},
		{name: "MUSE_PAT", want: true},
		{name: "MUSE_COOKIE", want: true},
		{name: "MUSE_SESSION_ID", want: true},
		{name: "MUSE_SESSION_MANAGER_URL", want: true},
		{name: "MUSE_SESSION_MANAGER_AUTH_TOKEN_ENV", want: true},
		{name: "MUSE_CODEX_APP_SERVER_AUTH_TOKEN_ENV", want: true},
		{name: "SSL_CERT_FILE", want: true},
		{name: "muse_apiKey", want: true},
		{name: "Muse_Secret", want: true},
		{name: "MUSE__TOKEN", want: true},
		{name: "MUSE_TOKEN_", want: true},
		{name: "MUSE__API__KEY__", want: true},
		{name: "MUSE_TOKEN_2", want: true},
		{name: "MUSE_2TOKEN", want: true},
		{name: "MUSE_BEARERTOKEN", want: true},
		{name: "MUSE_SESSIONTOKENS", want: true},
		{name: "MUSE_TOKEN2", want: true},
		{name: "MUSE_SECRET1", want: true},
		{name: "MUSE_PASSWORD2", want: true},
		{name: "MUSE_ACCESSTOKEN2", want: true},
		{name: "LC_SECRET2", want: true},
		{name: "MUSE_OAUTH2", want: true},
		{name: "MUSE_GH_PAT2", want: true},
		{name: "MUSE_TOKENVALUE", want: true},
		{name: "MUSE_SECRETVALUE", want: true},
		{name: "MUSE_PASSWORDFILE", want: true},
		{name: "MUSE_CREDENTIALSFILE", want: true},
		{name: "MUSE_TOKEN-X", want: true},
		{name: "MUSE_SECRET.FILE", want: true},
		{name: "GITHUB_TOKEN2", want: true},
		{name: "X_SECRET1", want: true},
		{name: "API_SECRETVALUE", want: true},
		{name: "GH_TOKENFILE", want: true},
		{name: "AUTHORIZATION", want: true},
		{name: "NPM_TOKEN-X", want: true},
		{name: "NPM_TOKEN", want: true},
		{name: "BEARERTOKENX", want: true},
		{name: "OPENAI_API_KEY", want: true},
		{name: "AWS_SECRET_ACCESS_KEY", want: true},
		{name: "GH_PAT", want: true},
		{name: "SSH_AUTH_SOCK", want: true},
		{name: "MUSE_API_KEY", want: true},
		{name: "MUSE_PASS", want: true},
		{name: "TASK_BOARD_SESSION_ID", want: true},
		{name: "HF_TOKEN", want: true},
		{name: "GOOGLE_APPLICATION_CREDENTIALS", want: true},
		{name: "AWS_ACCESS_KEY_ID", want: true},
		{name: "PGPASSWORD", want: true},
		{name: "MUSE_CERT_PASSPHRASE", want: true},
		{name: "SIGNING_KEY", want: true},
		{name: "github_token2", want: true},
		{name: "Muse-Token", want: true},
		{name: "X.SECRET.1", want: true},
		{name: "MUSE_APIKEY2", want: true},
		{name: "MUSE_KEY-X", want: true},
		{name: "MUSE_KEY.FILE", want: true},
		{name: "MUSE_PASS-X", want: true},
		{name: "MUSE_PASS.FILE", want: true},
		{name: "GH_PAT-X", want: true},
		{name: "GH_PAT.2", want: true},
		{name: "MUSE_PAT.FILE", want: true},
		{name: "MUSE_PAT2-X", want: true},
		{name: "GH-PAT-X", want: true},
		{name: "MUSEKEYFILE", want: true},
		{name: "SSHKEYPATH", want: true},
		{name: "GPGKEYID", want: true},
		{name: "MUSE_", want: false},
		{name: "LC_", want: false},
		{name: "LC_ALLX", want: false},
		{name: "LC_FOO", want: false},
		{name: "HTTPS_PROXY", want: false},
		{name: "MUSE_APP_SERVER_URL", want: false},
		{name: "MUSE_OPENAI_API_BASE", want: false},
		{name: "MUSE_CODEX_HOME", want: false},
		{name: "LANGX", want: false},
		{name: "HOME_", want: false},
		{name: "_HOME", want: false},
		{name: "XDG_CONFIG_HOMEX", want: false},
		{name: "home", want: false},
		{name: "Home", want: false},
		{name: " HOME", want: false},
		{name: "HOME ", want: false},
		{name: "\tHOME", want: false},
		{name: "HOME\t", want: false},
		{name: "HOM", want: false},
		{name: "XHOME", want: false},
		{name: "HOME-NOT", want: false},
		{name: "HOME__", want: false},
		{name: "İHOME", want: false},
		{name: "HOME\u200b", want: false},
		{name: "ΗΟΜΕ", want: false},
		{name: "", want: false},
		{name: "=", want: false},
		{name: "HOME\x00VALUE", want: false},
		{name: "HOME", want: false},
		{name: "LANG", want: false},
		{name: "MUSE_NO_AUTO_UPDATE", want: false},
		{name: "MUSE_CREDS", want: false},
		{name: "DOCKER_CRED", want: false},
		{name: "MUSE_JWT", want: false},
		{name: "SENTRY_DSN", want: false},
		{name: "DATABASE_URL", want: false},
		{name: "MUSE_OTP_SEED", want: false},
	} {
		t.Run("reviewed/"+tc.name, func(t *testing.T) {
			if got := isCredentialShapedParentEnvName(tc.name); got != tc.want {
				t.Errorf("isCredentialShapedParentEnvName(%q) = %t, want %t", tc.name, got, tc.want)
			}
		})
	}

	for _, name := range []string{"XPATY", "PATFILE", "MUSE_PATFILE", "PATH", "PATH_SUFFIX", "PATH2"} {
		t.Run("PAT-bound/"+name, func(t *testing.T) {
			if isCredentialShapedParentEnvName(name) {
				t.Errorf("isCredentialShapedParentEnvName(%q) = true, want false for the documented PAT bound", name)
			}
		})
	}

	if got := credentialShapedAllowlistNames(museParentEnvAllowlist); len(got) != 0 {
		t.Errorf("allowlist guard flagged current allowlist entries: %v", got)
	}
}

func mixedCaseCredentialWord(word string) string {
	return strings.ToLower(word[:1]) + word[1:]
}

func TestMuseParentEnvAllowlistIsAppliedThroughBuildPlan(t *testing.T) {
	req, parentNames := exactAllowlistRegressionRequest(t)
	plan := paritycase.BuildPlan(t, New(), req, agentic.LaunchModeExec)
	if err := assertParentDerivedNamesMatchAllowlist(req.Env, plan.Env); err != nil {
		t.Fatal(err)
	}
	child := paritycase.EnvMap(plan.Env)
	if got := child[agentic.EnvRunID]; got != req.Run.RunID {
		t.Errorf("run context %s = %q, want caller value %q", agentic.EnvRunID, got, req.Run.RunID)
	}
	if got := child[agentic.EnvTaskID]; got != req.Run.TaskID {
		t.Errorf("run context %s = %q, want caller value %q", agentic.EnvTaskID, got, req.Run.TaskID)
	}
	if len(parentNames) < 200 {
		t.Fatalf("regression fixture has only %d adversarial/generated parent names", len(parentNames))
	}
}

// TestNoParentNameOutsideTheExactAllowlistReachesMuseChild is the class
// regression: all parent inputs go through the public BuildPlan entry point.
func TestNoParentNameOutsideTheExactAllowlistReachesMuseChild(t *testing.T) {
	req, parentNames := exactAllowlistRegressionRequest(t)
	plan := paritycase.BuildPlan(t, New(), req, agentic.LaunchModeExec)
	if err := assertParentDerivedNamesMatchAllowlist(req.Env, plan.Env); err != nil {
		t.Fatal(err)
	}
	if got := len(parentNames); got < 200 {
		t.Fatalf("class regression seeded %d parent names, want at least 200", got)
	}
}

func TestCredentialShapedParentEnvNeverReachesMuseChild(t *testing.T) {
	req, _ := exactAllowlistRegressionRequest(t)
	blocked := []string{
		"TASK_BOARD_TOKEN",
		"TASK_BOARD_SESSION_ID",
		"TASK_BOARD_BUILDER_GATEWAY_TOKEN",
		"TASK_BOARD_SESSION_MANAGER_URL",
		"TASK_BOARD_CODEX_APP_SERVER_URL",
		"CLAUDECODE",
		"CODEX_HOME",
		"CODEX_API_KEY",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"RANDOM_PARENT_TOKEN",
		"RANDOM_PARENT_SECRET",
		"RANDOM_PARENT_KEY",
	}
	for _, name := range blocked {
		req.Env = agentic.SetEnvValue(req.Env, name, "synthetic-parent-value")
	}
	plan := paritycase.BuildPlan(t, New(), req, agentic.LaunchModeExec)
	child := paritycase.EnvMap(plan.Env)
	for _, name := range blocked {
		if got, ok := child[name]; ok && got == "synthetic-parent-value" {
			t.Errorf("parent variable %s reached the Muse child", name)
		}
	}
}

// TestEveryMuseLaunchPinsAutoUpdateAfterParentAndCallerValues is retained as
// the update-pin regression name. LaunchRequest.Env is a parent snapshot, not a
// Muse configuration channel; all such MUSE_* values are discarded before
// the final MUSE_NO_AUTO_UPDATE=1 write.
func TestEveryMuseLaunchPinsAutoUpdateAfterParentAndCallerValues(t *testing.T) {
	for _, c := range parityCases {
		t.Run(c.goldenID, func(t *testing.T) {
			_, _, req := prepareParityCase(t, c)
			req.Env = append(req.Env, "MUSE_NO_AUTO_UPDATE=0", "MUSE_NO_AUTO_UPDATE=caller-override")
			plan := paritycase.BuildPlan(t, New(), req, c.mode)
			if got := paritycase.EnvMap(plan.Env)[museNoAutoUpdateEnv]; got != "1" {
				t.Fatalf("%s = %q, want forced value 1", museNoAutoUpdateEnv, got)
			}
		})
	}
}

// The legacy test name is retained for reviewer continuity. Exact-name
// admission now rejects every MUSE_ and LC_ variable, including these names.
func TestCredentialShapedMuseNamesAreNotAdmittedByWildcard(t *testing.T) {
	for _, name := range append(reviewerParentNames(), "MUSE_SESSION_ID", "MUSE_OPENAI_API_BASE") {
		t.Run(name, func(t *testing.T) {
			c := parityCaseFor(t, "muse/exec")
			_, _, req := prepareParityCase(t, c)
			req.Env = agentic.SetEnvValue(req.Env, name, "synthetic-credential")
			plan := paritycase.BuildPlan(t, New(), req, c.mode)
			if got, ok := paritycase.EnvMap(plan.Env)[name]; ok && got == "synthetic-credential" {
				t.Fatalf("parent variable %q reached the Muse child", name)
			}
		})
	}
}

func TestMuseCredentialAllowlistGuardRejectsNarrowingMutant(t *testing.T) {
	for _, name := range []string{"GITHUB_TOKEN2", "AUTHORIZATION", "GH_PAT-X", "MUSEKEYFILE"} {
		t.Run(name, func(t *testing.T) {
			mutated := append(append([]parentEnvRule(nil), museParentEnvAllowlist...), parentEnvRule{
				name:   name,
				reason: "narrowing mutant: admitted a credential-shaped environment name.",
			})
			got := credentialShapedAllowlistNames(mutated)
			if len(got) != 1 || got[0] != name {
				t.Fatalf("credential guard did not detect the one-name allowlist mutant %q: %v", name, got)
			}
		})
	}
}

func TestMuseExactAllowlistRegressionDetectsMUSEPrefixMutant(t *testing.T) {
	assertPrefixAdmissionMutantRejected(t, "MUSE_")
}

func TestMuseExactAllowlistRegressionDetectsLocalePrefixMutant(t *testing.T) {
	assertPrefixAdmissionMutantRejected(t, "LC_")
}

func assertPrefixAdmissionMutantRejected(t *testing.T, prefix string) {
	t.Helper()
	req, _ := exactAllowlistRegressionRequest(t)
	mutant := prefixAdmissionMutant{System: New(), prefix: prefix}
	plan := paritycase.BuildPlan(t, mutant, req, agentic.LaunchModeExec)
	if err := assertParentDerivedNamesMatchAllowlist(req.Env, plan.Env); err == nil {
		t.Fatalf("the exact-name class regression did not detect the %q prefix mutant", prefix)
	}
}

type prefixAdmissionMutant struct {
	agentic.System
	prefix string
}

func (m prefixAdmissionMutant) ChildEnv(parent []string, req agentic.LaunchRequest) ([]string, error) {
	env := filterMuseParentEnv(parent)
	for _, entry := range parent {
		name, value, hasValue := strings.Cut(entry, "=")
		if hasValue && strings.HasPrefix(name, m.prefix) {
			env = agentic.SetEnvValue(env, name, value)
		}
	}
	env = agentic.WithRunContext(env, req)
	return agentic.SetEnvValue(env, museNoAutoUpdateEnv, "1"), nil
}

func exactAllowlistRegressionRequest(t *testing.T) (agentic.LaunchRequest, []string) {
	t.Helper()
	c := parityCaseFor(t, "muse/exec")
	_, _, req := prepareParityCase(t, c)

	names := append([]string(nil), reviewerParentNames()...)
	names = append(names, harnessCredentialNames()...)
	names = append(names, generatedMuseAndLocaleNames()...)
	for _, rule := range museParentEnvAllowlist {
		names = append(names, rule.name)
	}
	names = uniqueSortedNames(names)
	for _, name := range names {
		if name == "PATH" {
			continue // Keep the real fixture PATH so BuildPlan resolves Muse.
		}
		req.Env = agentic.SetEnvValue(req.Env, name, "parent-value-"+name)
	}
	return req, names
}

func assertParentDerivedNamesMatchAllowlist(parent, child []string) error {
	parentMap := paritycase.EnvMap(parent)
	childMap := paritycase.EnvMap(child)
	want := make(map[string]string)
	for _, rule := range museParentEnvAllowlist {
		if value, ok := parentMap[rule.name]; ok {
			want[rule.name] = value
		}
	}
	got := make(map[string]string)
	for name, parentValue := range parentMap {
		if childValue, ok := childMap[name]; ok && childValue == parentValue {
			got[name] = childValue
		}
	}
	if len(got) != len(want) {
		var unexpected, missing []string
		for name := range got {
			if _, ok := want[name]; !ok {
				unexpected = append(unexpected, name)
			}
		}
		for name := range want {
			if _, ok := got[name]; !ok {
				missing = append(missing, name)
			}
		}
		sort.Strings(unexpected)
		sort.Strings(missing)
		return fmt.Errorf("parent-derived child names count = %d, want exactly %d allowlisted names present; unexpected sample=%v missing sample=%v", len(got), len(want), firstEnvNames(unexpected, 8), firstEnvNames(missing, 8))
	}
	for name, value := range want {
		if got[name] != value {
			return fmt.Errorf("parent-derived child value %s = %q, want %q", name, got[name], value)
		}
	}
	return nil
}

func firstEnvNames(names []string, count int) []string {
	if len(names) > count {
		return names[:count]
	}
	return names
}

func reviewerParentNames() []string {
	return []string{
		// Revision 1's admitted, refused, and ambiguous-prefix probes.
		"MUSE_APIKEY", "MUSE_AUTHTOKEN", "MUSE_ACCESSTOKEN", "MUSE_PRIVATEKEY", "LC_APIKEY",
		"MUSE_TOKEN", "MUSE_SECRETS", "MUSE_KEYCHAIN_PATH", "MUSE_AUTH", "MUSE_BEARER", "MUSE_PAT", "MUSE_COOKIE",
		"MUSE_SESSION_ID", "MUSE_SESSION_MANAGER_URL", "MUSE_APP_SERVER_URL", "MUSE_OPENAI_API_BASE", "MUSE_CODEX_HOME",
		"MUSE_API_KEY", "MUSE_SESSION_MANAGER_AUTH_TOKEN_ENV", "MUSE_CODEX_APP_SERVER_AUTH_TOKEN_ENV",
		// Revision 1 case and separator controls.
		"muse_apiKey", "Muse_Secret", "MUSE__TOKEN", "MUSE_TOKEN_", "MUSE__API__KEY__", "MUSE_TOKEN_2",
		"MUSE_2TOKEN", "MUSE_BEARERTOKEN", "MUSE_SESSIONTOKENS", "MUSE_", "LC_", "LC_ALLX", "LC_FOO",
		"HTTPS_PROXY", "SSL_CERT_FILE",
		// Revision 2's complete reproduction set.
		"MUSE_TOKEN2", "MUSE_SECRET1", "MUSE_PASSWORD2", "MUSE_ACCESSTOKEN2", "LC_SECRET2", "MUSE_OAUTH2",
		"MUSE_GH_PAT2", "MUSE_TOKENVALUE", "MUSE_SECRETVALUE", "MUSE_PASSWORDFILE", "MUSE_CREDENTIALSFILE",
		"MUSE_TOKEN-X", "MUSE_SECRET.FILE",
	}
}

func harnessCredentialNames() []string {
	return []string{
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "CODEX_HOME", "CODEX_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN",
		"CLAUDE_CODE_SESSION", "CLAUDE_CODE_ENTRYPOINT", "TASK_BOARD_TOKEN", "TASK_BOARD_SESSION_ID",
		"TASK_BOARD_BUILDER_GATEWAY_TOKEN", "TASK_BOARD_SESSION_MANAGER_URL", "TASK_BOARD_CODEX_APP_SERVER_URL",
		"TASK_BOARD_SESSION_MANAGER_AUTH_TOKEN_ENV", "TASK_BOARD_CODEX_APP_SERVER_AUTH_TOKEN_ENV", "TASK_BOARD_RUN_ID",
		"TASK_BOARD_TASK_ID", "TASK_BOARD_BOARD_DIR", "TASK_BOARD_DELIVERY_GOAL_ID", "GH_TOKEN",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_REGION",
		"RANDOM_PARENT_TOKEN", "RANDOM_PARENT_SECRET", "RANDOM_PARENT_KEY", "RANDOM_PARENT_API_KEY",
	}
}

func generatedMuseAndLocaleNames() []string {
	prefixes := []string{"MUSE_", "muse_", "Muse_", "LC_", "lc_", "Lc_"}
	shapes := []func(int) string{
		func(i int) string { return fmt.Sprintf("GEN%d", i) },
		func(i int) string { return fmt.Sprintf("gEn%d", i) },
		func(i int) string { return fmt.Sprintf("GEN-%d", i) },
		func(i int) string { return fmt.Sprintf("GEN__%d", i) },
		func(i int) string { return fmt.Sprintf("GEN_%d_", i) },
		func(i int) string { return fmt.Sprintf("TOKEN-%d", i) },
	}
	var names []string
	for i := 0; i < 64; i++ {
		for _, prefix := range prefixes {
			for _, shape := range shapes {
				names = append(names, prefix+shape(i))
			}
		}
	}
	return names
}

func uniqueSortedNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, name)
	}
	sort.Strings(unique)
	return unique
}

var _ agentic.System = (*System)(nil)
