package agentic

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// SealCommitmentKey is ephemeral verification material, conveyed separately
// over private stdin and the authenticated socket, never in guard JSON or logs.
// Cross-process consumers MUST supply the same key to FinalizePlan and ImportSeal.
// The default key lives only in this process; foreign imports refuse, never renew.
type SealCommitmentKey [32]byte

var processCommitmentKey = func() (k SealCommitmentKey) { rand.Read(k[:]); return }()

func selectCommitmentKey(keys []SealCommitmentKey) (SealCommitmentKey, error) {
	if len(keys) > 1 || (len(keys) == 1 && keys[0] == (SealCommitmentKey{})) {
		return SealCommitmentKey{}, fmt.Errorf("%w: commitment key unavailable", ErrSealImportRefused)
	}
	if len(keys) == 1 {
		return keys[0], nil
	}
	return processCommitmentKey, nil
}

// ProcessBinding is a local-only exact-process projection. Its environment
// contains names and keyed commitments only. It is NOT the frozen hosted
// unsealed schema. Integrity digests are transient corruption checks, not
// authentication; trust comes from private stdin plus the authenticated socket.
type ProcessBinding struct {
	Binary    string            `json:"binary"`
	Argv      []string          `json:"argv"`
	EnvNames  []string          `json:"env_names"`
	Selectors map[string]string `json:"selectors"`
	KeyID     string            `json:"key_id"`
	Digest    string            `json:"digest"`
}

func literalCommitment(key SealCommitmentKey, name, value string) string {
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte("relux.hosted.literal.v1\x00" + name + "\x00" + value))
	return fmt.Sprintf("hmac-sha256:%x", mac.Sum(nil))
}

// CommitSealLiteral binds one environment name=value pair under the
// process-local commitment key, in the contract's relux.hosted.literal.v1
// domain. Base (unfinalized) sealed guards use it so literal environment
// values never enter exported guards; finalized guards commit through
// their own process bindings instead. The key never leaves the process:
// importers compare LocalCommitmentKeyID and refuse a guard committed
// under a key they do not hold rather than verifying under the wrong one.
func CommitSealLiteral(name, value string) string {
	return literalCommitment(processCommitmentKey, name, value)
}

// LocalCommitmentKeyID identifies the process-local commitment key. It
// travels in guards so an importer can refuse a guard committed under a
// key it does not hold; the key itself never serializes.
func LocalCommitmentKeyID() string {
	return commitmentKeyID(processCommitmentKey)
}

// CommitSealLiteralWithKey binds one environment name=value pair under the
// caller's explicit commitment key, in the same relux.hosted.literal.v1
// domain as CommitSealLiteral. Finalized sealed guards use it so plugin
// selectors travel under the finalization key: a consumer holding the same
// explicit key verifies in its own process, while a different or missing
// key refuses typed. The key itself never serializes; only its ID travels.
func CommitSealLiteralWithKey(key SealCommitmentKey, name, value string) string {
	return literalCommitment(key, name, value)
}

// CommitmentKeyID identifies an explicit commitment key the same way
// LocalCommitmentKeyID identifies the process-local one: the SHA-256 of
// the key bytes, hex-encoded. Guards carry the ID so an importer can
// refuse a guard committed under a key it was not given.
func CommitmentKeyID(key SealCommitmentKey) string { return commitmentKeyID(key) }
func commitmentKeyID(key SealCommitmentKey) string {
	return fmt.Sprintf("%x", sha256.Sum256(key[:]))
}
func environmentNames(env []string) []string {
	names := make([]string, len(env))
	for i, entry := range env {
		names[i], _, _ = strings.Cut(entry, "=")
	}
	return names
}
func bindFinalProcess(p Plan, key SealCommitmentKey) finalizedBindings {
	b := finalizedBindings{binary: p.Binary, argv: slices.Clone(p.Argv), envNames: environmentNames(p.Env), selectors: make(map[string]string, len(p.Env)), key: key, session: cloneSession(p.Session), sessionBound: true}
	for i, entry := range p.Env {
		name, value, _ := strings.Cut(entry, "=")
		b.selectors[fmt.Sprint(i)] = literalCommitment(key, name, value)
	}
	return b
}
func (b finalizedBindings) export() *ProcessBinding {
	// Required collections serialize as []/{} even when empty, never null.
	p := &ProcessBinding{Binary: b.binary, Argv: append([]string{}, b.argv...), EnvNames: append([]string{}, b.envNames...), Selectors: cloneSealSelectors(b.selectors), KeyID: commitmentKeyID(b.key)}
	p.Digest = processBindingDigest(*p)
	return p
}
func processBindingDigest(p ProcessBinding) string {
	p.Digest = ""
	wire, _ := json.Marshal(p)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(wire))
}
func importProcessBinding(p *ProcessBinding, keys []SealCommitmentKey) (finalizedBindings, error) {
	if p == nil || p.Binary == "" || len(p.EnvNames) != len(p.Selectors) {
		return finalizedBindings{}, fmt.Errorf("%w: invalid process binding", ErrSealMalformed)
	}
	if p.Digest != processBindingDigest(*p) {
		return finalizedBindings{}, fmt.Errorf("%w: process binding digest mismatch", ErrSealTampered)
	}
	var key SealCommitmentKey
	if selected, err := selectCommitmentKey(keys); err != nil {
		return finalizedBindings{}, err
	} else {
		key = selected
	}
	if p.KeyID != commitmentKeyID(key) {
		return finalizedBindings{}, fmt.Errorf("%w: commitment key unavailable", ErrSealImportRefused)
	}
	return finalizedBindings{binary: p.Binary, argv: append([]string{}, p.Argv...), envNames: append([]string{}, p.EnvNames...), selectors: cloneSealSelectors(p.Selectors), key: key}, nil
}
func cloneProcessBinding(p *ProcessBinding) *ProcessBinding {
	if p == nil {
		return nil
	}
	c := *p
	c.Argv = append([]string{}, p.Argv...)
	c.EnvNames = append([]string{}, p.EnvNames...)
	c.Selectors = cloneSealSelectors(p.Selectors)
	return &c
}
func cloneSealedData(data SealedData) SealedData {
	data.Argv = append([]string{}, data.Argv...)
	data.Artifacts = slices.Clone(data.Artifacts)
	if data.Artifacts == nil {
		data.Artifacts = []SealedArtifact{}
	}
	data.Selectors = cloneSealSelectors(data.Selectors)
	data.Binding = cloneProcessBinding(data.Binding)
	return data
}

// processSnapshot is captured by BuildPlan, before the caller can mutate any
// exported plan slices. Artifact checks remain at exec; no disk content is
// adopted or re-digested during finalization.
//
// Session is part of what the snapshot binds: the record is derived from the
// argv, so a Session edited after sealing describes a launch the sealed argv
// does not carry and refuses like any other changed field.
type processSnapshot struct {
	binary    string
	argv, env []string
	session   *PlanSession
}

func snapshotProcess(p Plan) *processSnapshot {
	return &processSnapshot{p.Binary, slices.Clone(p.Argv), slices.Clone(p.Env), cloneSession(p.Session)}
}
func (s *processSnapshot) verify(p Plan) error {
	if p.Binary != s.binary || !slices.Equal(p.Argv, s.argv) || !slices.Equal(p.Env, s.env) {
		return fmt.Errorf("%w: base process changed before finalization", ErrFinalizedProcessChanged)
	}
	if !sessionsEqual(p.Session, s.session) {
		return fmt.Errorf("%w: session record changed before finalization", ErrFinalizedProcessChanged)
	}
	return nil
}
