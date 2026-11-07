package codex

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Only the exact gateway release admits across zero/native/yolo permissions; supported permission rows confer no gateway qualification.
func TestGatewayExactReleaseAdmission(t *testing.T) {
	for _, release := range []string{"0.164.0", "", "0.153.2", "0.163.0", "0.165.0", "0.166.0", " 0.164.0", "0.164.0 "} {
		for _, permission := range []agentic.PermissionMode{"", agentic.PermissionModeNative, agentic.PermissionModeYolo} {
			t.Run(release+"/"+string(permission), func(t *testing.T) {
				r := managedPrimaryRequest(t)
				r.ToolRelease = release
				r.PermissionMode = permission
				g := (&System{}).CodexGatewayBasis(r, agentic.LaunchModeInteractive)
				err := g.Admit(agentic.CodexGatewayInputs{}, r.Env)
				if release != "0.164.0" {
					if !errors.Is(err, agentic.ErrCodexGatewayRefused) {
						t.Fatalf("unqualified release admitted: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				p := requireManagedPlan(t, r)
				a, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := gatewayFinalizeFixture(a, gatewayBinding(t, p)); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// Native equality is an independent gate: two individually supported native suffixes cannot be substituted for each other.
func TestGatewayNativeInputEquality(t *testing.T) {
	p := gatewayPlan(t, []string{"--no-alt-screen"})
	if _, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{}); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
		t.Fatalf("different supported native suffix admitted: %v", err)
	}
	if _, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{NativeArgs: []string{"--no-alt-screen"}}); err != nil {
		t.Fatal(err)
	}
}

// Parent selectors and repeated display switches refuse at policy admission, while their unmodified native/environment controls admit.
func TestGatewayParentEnvironmentAndRepeatedSuffix(t *testing.T) {
	for _, kind := range []string{"openai", "codex", "repeat"} {
		t.Run(kind, func(t *testing.T) {
			r := managedPrimaryRequest(t)
			r.ToolRelease = "0.164.0"
			control := requireManagedPlan(t, r)
			if _, err := gatewayAdmitFixture(control, agentic.CodexGatewayInputs{}); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "openai":
				r.Env = append(r.Env, "OPENAI_BASE_URL=http://other")
			case "codex":
				r.Env = append(r.Env, "CODEX_REMOTE_URL=http://other")
			case "repeat":
				r.NativeArgs = []string{"--no-alt-screen", "--no-alt-screen"}
			}
			p := requireManagedPlan(t, r)
			if _, err := gatewayAdmitFixture(p, agentic.CodexGatewayInputs{NativeArgs: r.NativeArgs}); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Fatalf("forbidden parent/native input admitted: %v", err)
			}
		})
	}
}

// Each conflicting route refuses both policy admission and canonical rendering; an ordinary gateway request succeeds beside it.
func TestGatewayOtherRoutes(t *testing.T) {
	for _, kind := range []string{"local", "remote", "network", "gateway"} {
		t.Run(kind, func(t *testing.T) {
			r := managedPrimaryRequest(t)
			r.ToolRelease = "0.164.0"
			p := requireManagedPlan(t, r)
			b := gatewayBinding(t, p)
			g := (&System{}).CodexGatewayBasis(r, agentic.LaunchModeInteractive)
			if err := g.Admit(agentic.CodexGatewayInputs{}, p.Env); err != nil {
				t.Fatal(err)
			}
			if _, err := g.Render(b, p.Argv); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "local":
				r.LocalProvider = &agentic.LocalProviderBinding{}
			case "remote":
				r.CodexRemoteClient = &agentic.CodexRemoteClientTarget{}
			case "gateway":
				r.CodexGateway = &b
			case "network":
				r.Network.Patch.Unset = []string{"HTTP_PROXY"}
			}
			g = (&System{}).CodexGatewayBasis(r, agentic.LaunchModeInteractive)
			if err := g.Admit(agentic.CodexGatewayInputs{}, p.Env); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
				t.Fatalf("other route admitted: %v", err)
			}
			if kind != "gateway" {
				if _, err := g.Render(b, p.Argv); !errors.Is(err, agentic.ErrCodexGatewayRefused) {
					t.Fatalf("other route rendered: %v", err)
				}
			}
		})
	}
}

// Each URL/header/path predicate has an otherwise valid binding; the whole URL must equal its canonical re-rendering.
func TestGatewayBindingCanonicalPredicates(t *testing.T) {
	p := gatewayPlan(t, nil)
	base := gatewayBinding(t, p)
	for _, u := range []string{"http://127.0.0.1:1/chatgpt/backend-api/codex", "http://127.0.0.2:1/chatgpt/backend-api/codex", "http://[::1]:1/chatgpt/backend-api/codex", "http://[::ffff:127.0.0.1]:1/chatgpt/backend-api/codex"} {
		b := base
		b.BaseURL = u
		if !validGatewayBinding(b) {
			t.Fatalf("valid canonical URL refused: %s", u)
		}
	}
	for name, mutate := range map[string]func(*agentic.CodexGatewayBinding){
		"loopback":          func(b *agentic.CodexGatewayBinding) { b.BaseURL = "http://8.8.8.8:1/chatgpt/backend-api/codex" },
		"scheme":            func(b *agentic.CodexGatewayBinding) { b.BaseURL = "https://127.0.0.1:1/chatgpt/backend-api/codex" },
		"userinfo":          func(b *agentic.CodexGatewayBinding) { b.BaseURL = "http://u:p@127.0.0.1:1/chatgpt/backend-api/codex" },
		"port-zero":         func(b *agentic.CodexGatewayBinding) { b.BaseURL = "http://127.0.0.1:0/chatgpt/backend-api/codex" },
		"port-large":        func(b *agentic.CodexGatewayBinding) { b.BaseURL = "http://127.0.0.1:65536/chatgpt/backend-api/codex" },
		"trailing-query":    func(b *agentic.CodexGatewayBinding) { b.BaseURL += "?" },
		"trailing-fragment": func(b *agentic.CodexGatewayBinding) { b.BaseURL += "#" },
		"uppercase-scheme":  func(b *agentic.CodexGatewayBinding) { b.BaseURL = strings.Replace(b.BaseURL, "http:", "HTTP:", 1) },
		"relative-binary":   func(b *agentic.CodexGatewayBinding) { b.ExecutablePath = "relative/codex" },
		"unclean-binary":    func(b *agentic.CodexGatewayBinding) { b.ExecutablePath = filepath.Dir(b.ExecutablePath) + "/./codex" },
	} {
		t.Run(name, func(t *testing.T) {
			if !validGatewayBinding(base) {
				t.Fatal("valid binding refused")
			}
			bad := base
			mutate(&bad)
			if validGatewayBinding(bad) {
				t.Fatal("invalid binding predicate accepted")
			}
		})
	}
	for _, header := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Host", "Content-Length", "Transfer-Encoding", "Connection", "X-Api-Key"} {
		t.Run(header, func(t *testing.T) {
			if !validGatewayBinding(base) {
				t.Fatal("valid binding refused")
			}
			b := base
			b.HeaderName = header
			if validGatewayBinding(b) {
				t.Fatal("denied header accepted")
			}
		})
	}
}
