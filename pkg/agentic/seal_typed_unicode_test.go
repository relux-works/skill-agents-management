package agentic_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/agentic/systems/claude"
	"strings"
	"testing"
)

// This public-plugin fixture imports an artifact-free sealed projection as
// well as opting into unsealed guards. It performs no process start or I/O.
type typedUnicodeSystem struct{ *claude.System }

func (typedUnicodeSystem) AcceptUnsealedExecGuard() {}
func (typedUnicodeSystem) ImportSealedData(agentic.SealedData) (agentic.ExecPlanVerifier, error) {
	return typedUnicodeVerifier{}, nil
}

type typedUnicodeVerifier struct{}

func (typedUnicodeVerifier) VerifyBeforeExec(agentic.Plan) error { return nil }
func (typedUnicodeVerifier) VerifySealedArtifacts() error        { return nil }

func typedUnicodeGuard(t *testing.T, sealed bool) (agentic.Seal, agentic.Plan) {
	p := agentic.Plan{Binary: "/synthetic/héllo-世界", Argv: []string{"--name", "e\u0301-🌍"}, Env: []string{"UNICODE=e\u0301-🌍"}}
	sys := &sealRev5EnvSystem{System: claude.New(), binary: p.Binary, argv: p.Argv, hasArgv: true, env: p.Env}
	final, err := agentic.FinalizePlan(sealRev5Build(t, sys), agentic.FinalizeOverlays{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := final.ExportSeal()
	if err != nil {
		t.Fatal(err)
	}
	binding := exported.Data.Binding
	s := agentic.Seal{Schema: agentic.ExecGuardSchema, SchemaVersion: agentic.ExecGuardVersion, Data: agentic.SealData{Kind: agentic.SealKindUnsealedBound, Binding: binding}}
	if sealed {
		s.Data = agentic.SealData{Kind: agentic.SealKindSealed, Sealed: &agentic.SealedData{System: "claude-code", Binary: p.Binary, Argv: append([]string{}, p.Argv...), Artifacts: []agentic.SealedArtifact{{Name: "cátalog", Path: "/synthetic/世界", Digest: "sha256:fixture"}}, Selectors: map[string]string{"sélector": "metadata-🌍"}, Binding: binding}}
		s.Data.Sealed.Digest = sealIntegrityDigest(*s.Data.Sealed)
	}
	return s, p
}

// TestTypedImportRefusesUnrepresentableStrings sweeps every typed string
// position, before marshal can repair it. Recomputed public digests are
// deliberate: they pin representability rather than authentication.
func TestTypedImportRefusesUnrepresentableStrings(t *testing.T) {
	bad := "bad-\xff"
	type mutation struct {
		name   string
		sealed bool
		change func(*agentic.Seal)
	}
	cases := []mutation{
		{"schema", false, func(s *agentic.Seal) { s.Schema = bad }},
		{"version", false, func(s *agentic.Seal) { s.SchemaVersion = bad }},
		{"kind", false, func(s *agentic.Seal) { s.Data.Kind = bad }},
		{"sealed-system", true, func(s *agentic.Seal) { s.Data.Sealed.System = bad }},
		{"sealed-binary", true, func(s *agentic.Seal) { s.Data.Sealed.Binary = bad }},
		{"sealed-argv", true, func(s *agentic.Seal) { s.Data.Sealed.Argv[0] = bad }},
		{"artifact-name", true, func(s *agentic.Seal) { s.Data.Sealed.Artifacts[0].Name = bad }},
		{"artifact-path", true, func(s *agentic.Seal) { s.Data.Sealed.Artifacts[0].Path = bad }},
		{"artifact-digest", true, func(s *agentic.Seal) { s.Data.Sealed.Artifacts[0].Digest = bad }},
		{"sealed-selector-key", true, func(s *agentic.Seal) { s.Data.Sealed.Selectors = map[string]string{bad: "metadata"} }},
		{"sealed-selector-value", true, func(s *agentic.Seal) { s.Data.Sealed.Selectors["sélector"] = bad }},
		{"sealed-digest", true, func(s *agentic.Seal) { s.Data.Sealed.Digest = bad }},
	}
	fields := []struct {
		name   string
		change func(*agentic.ProcessBinding)
	}{
		{"binary", func(b *agentic.ProcessBinding) { b.Binary = bad }},
		{"argv", func(b *agentic.ProcessBinding) { b.Argv[0] = bad }},
		{"env-name", func(b *agentic.ProcessBinding) { b.EnvNames[0] = bad }},
		{"selector-key", func(b *agentic.ProcessBinding) { b.Selectors = map[string]string{bad: "commitment"} }},
		{"selector-value", func(b *agentic.ProcessBinding) { b.Selectors["0"] = bad }},
		{"key-id", func(b *agentic.ProcessBinding) { b.KeyID = bad }},
		{"digest", func(b *agentic.ProcessBinding) { b.Digest = bad }},
	}
	for _, sealed := range []bool{false, true} {
		for _, field := range fields {
			prefix := "binding-"
			if sealed {
				prefix = "nested-binding-"
			}
			cases = append(cases, mutation{prefix + field.name, sealed, func(s *agentic.Seal) {
				b := s.Data.Binding
				if sealed {
					b = s.Data.Sealed.Binding
				}
				field.change(b)
			}})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := typedUnicodeGuard(t, tc.sealed)
			tc.change(&s)
			b := s.Data.Binding
			if tc.sealed {
				b = s.Data.Sealed.Binding
			}
			if !strings.HasSuffix(tc.name, "digest") {
				b.Digest = processBindingDigest(*b)
			}
			if tc.sealed && tc.name != "sealed-digest" {
				s.Data.Sealed.Digest = sealIntegrityDigest(*s.Data.Sealed)
			}
			verifier, err := agentic.ImportSeal(typedUnicodeSystem{claude.New()}, s)
			if !errors.Is(err, agentic.ErrSealMalformed) || verifier != nil {
				t.Fatalf("typed import invalid UTF-8 %s admitted or wrong refusal: %v", tc.name, err)
			}
		})
	}
	t.Logf("typed string positions: %d of %d refused", len(cases), len(cases))
}

func TestTypedImportValidUnicodeRoundTrips(t *testing.T) {
	for _, sealed := range []bool{false, true} {
		s, p := typedUnicodeGuard(t, sealed)
		v, err := agentic.ImportSeal(typedUnicodeSystem{claude.New()}, s)
		if err != nil {
			t.Fatal(err)
		}
		if err = v.VerifyBeforeExec(p); err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := agentic.DecodeSeal(wire)
		if err != nil {
			t.Fatal(err)
		}
		again, err := json.Marshal(decoded)
		if err != nil {
			t.Fatal(err)
		}
		if string(wire) != string(again) {
			t.Fatal("valid Unicode changed through JSON")
		}
		imported, err := agentic.ImportSeal(typedUnicodeSystem{claude.New()}, decoded)
		if err != nil {
			t.Fatal(err)
		}
		if err = imported.VerifyBeforeExec(p); err != nil {
			t.Fatal(err)
		}
	}
}

// Canonical field order mirrors the documented public integrity projection;
// these test helpers deliberately do not provide authentication.
func processBindingDigest(p agentic.ProcessBinding) string {
	p.Digest = ""
	wire, _ := json.Marshal(p)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(wire))
}
func sealIntegrityDigest(p agentic.SealedData) string {
	canonical := struct {
		Binding   *agentic.ProcessBinding  `json:"binding,omitempty"`
		System    string                   `json:"system"`
		Binary    string                   `json:"binary"`
		Argv      []string                 `json:"argv"`
		Artifacts []agentic.SealedArtifact `json:"artifacts"`
		Selectors map[string]string        `json:"selectors,omitempty"`
	}{p.Binding, p.System, p.Binary, p.Argv, p.Artifacts, p.Selectors}
	wire, _ := json.Marshal(canonical)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(wire))
}
