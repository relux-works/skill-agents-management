package codex

import (
	"errors"
	"reflect"
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// BuildPlan preserves native defaults and explicit-model argv for each managed
// interactive shape. Base, finalized and imported seals reject a model flag
// added afterward. These are plan-value checks, not native Codex execution.
func TestManagedNativeDefaultModel(t *testing.T) {
	for _, shape := range []string{"manager", "roots", "primary-app-server", "remote-client"} {
		for _, model := range []string{"", "test-model"} {
			t.Run(shape+"/model="+model, func(t *testing.T) {
				req := managedPrimaryRequest(t)
				req.Model.ID = model
				want := []string{}
				if shape == "manager" || shape == "primary-app-server" {
					manager := t.TempDir()
					req.ManagerToolDir = &manager
				}
				if shape == "roots" || shape == "primary-app-server" {
					req.AdditionalWritableRoots = []string{"/tmp/runtime"}
					want = append(want, "-c", `sandbox_workspace_write.writable_roots=["/tmp/runtime"]`)
				}
				if model != "" {
					want = append(want, "-m", model)
				}
				if shape == "primary-app-server" {
					req.NativeArgs = []string{"app-server"}
					want = append(want, "app-server")
				}
				if shape == "remote-client" {
					req.CodexRemoteClient = &agentic.CodexRemoteClientTarget{Endpoint: "unix:///tmp/manager.sock", ThreadID: managedThread}
					// Remote argv keeps the thread's model selection, even for
					// an explicit request, matching the v0.5.67 contract.
					want = []string{"--remote", "unix:///tmp/manager.sock", "resume", managedThread}
				}
				base := requireManagedPlan(t, req)
				if !reflect.DeepEqual(base.Argv, want) {
					t.Fatalf("managed model argv = %q, want %q", base.Argv, want)
				}
				if base.ModelIdentity != (agentic.ModelIdentity{Requested: model, Launched: model}) {
					t.Fatalf("model identity = %+v", base.ModelIdentity)
				}
				final, err := agentic.FinalizePlan(base, agentic.FinalizeOverlays{}, nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range []agentic.Plan{base, final} {
					if err := p.VerifyBeforeExec(); err != nil {
						t.Fatal(err)
					}
					seal, err := p.ExportSeal()
					if err != nil {
						t.Fatal(err)
					}
					verifier, err := agentic.ImportSeal(New(), seal)
					if err != nil {
						t.Fatal(err)
					}
					if err := verifier.VerifyBeforeExec(p); err != nil {
						t.Fatal(err)
					}
					changed := p
					changed.Argv = append(append([]string(nil), p.Argv...), "-m", "injected-model")
					if err := changed.VerifyBeforeExec(); err == nil {
						t.Fatal("managed model seal admits added model flag")
					}
					if err := verifier.VerifyBeforeExec(changed); err == nil {
						t.Fatal("imported managed model seal admits added model flag")
					}
				}
			})
		}
	}
}

// The native-default exception does not admit worker/spawn modes, ordinary
// interactive requests, or whitespace-only IDs. Each shape has an explicit
// model positive control; rejection returns no partial plan.
func TestManagedNativeDefaultRefusesWorkerModels(t *testing.T) {
	for _, mode := range []agentic.LaunchMode{agentic.LaunchModeExec, agentic.LaunchModeDryRun, agentic.LaunchModeManagedSession, agentic.LaunchModeInteractive} {
		for _, manager := range []bool{false, true} {
			if manager && mode == agentic.LaunchModeInteractive {
				continue
			}
			name := mode.String()
			if manager {
				name += "-with-manager"
			}
			t.Run(name, func(t *testing.T) {
				req := managedPrimaryRequest(t)
				if manager {
					dir := t.TempDir()
					req.ManagerToolDir = &dir
				}
				r := agentic.NewRegistry()
				if err := r.Register(New()); err != nil {
					t.Fatal(err)
				}
				if _, err := agentic.BuildPlan(r, req, mode); err != nil {
					t.Fatalf("explicit model control: %v", err)
				}
				for _, model := range []string{"", "   "} {
					req.Model.ID = model
					p, err := agentic.BuildPlan(r, req, mode)
					if !errors.Is(err, agentic.ErrModelMissing) || !reflect.DeepEqual(p, agentic.Plan{}) {
						t.Fatalf("worker missing model admitted: model=%q err=%v plan=%+v", model, err, p)
					}
				}
			})
		}
	}
	for _, remote := range []bool{false, true} {
		req := managedPrimaryRequest(t)
		manager := t.TempDir()
		req.ManagerToolDir = &manager
		if remote {
			req.CodexRemoteClient = &agentic.CodexRemoteClientTarget{Endpoint: "unix:///tmp/manager.sock", ThreadID: managedThread}
		}
		req.Model.ID = "   "
		if p, err := managedBuild(req); !errors.Is(err, agentic.ErrModelMissing) || !reflect.DeepEqual(p, agentic.Plan{}) {
			t.Fatalf("whitespace managed model admitted: %v", err)
		}
	}
}
