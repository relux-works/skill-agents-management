package codex

import (
	"path/filepath"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// Seal artifact labels for the local-provider catalog guard.
const (
	// sealedCatalogName is the stable artifact label for the materialized
	// provider catalog. The on-disk file carries a content digest in its
	// name; the label stays fixed so an importer can demand exactly it.
	sealedCatalogName  = "catalog.json"
	sealedDigestPrefix = "sha256:"
	// sealedCommitmentPrefix is the keyed-commitment shape a TMPDIR
	// selector must carry: the relux.hosted.literal.v1 HMAC the exporter
	// mints through agentic.CommitSealLiteral, never a literal directory.
	sealedCommitmentPrefix = "hmac-sha256:"
)

// ExportSealedData implements agentic.ExecSealExporter. It reports the
// bound binary, exact argv and catalog artifact from verifier memory and
// performs no I/O: exporting never re-reads the artifact it reports, so a
// swapped catalog still exports the ORIGINAL digest and refuses at import
// or verification time instead of silently resealing. System and integrity
// digest are stamped by agentic.ExportSeal.
//
// A bound TMPDIR selection travels as a keyed commitment under the TMPDIR
// selector name, never as the literal directory: the importer commits the
// presented value before comparing. A TempDir-only seal exports no
// artifacts; a catalog seal without a TMPDIR selection exports no
// selectors.
func (seal *catalogExecSeal) ExportSealedData() agentic.SealedData {
	var artifacts []agentic.SealedArtifact
	if seal.path != "" {
		artifacts = []agentic.SealedArtifact{{
			Name:   sealedCatalogName,
			Path:   seal.path,
			Digest: sealedDigestPrefix + seal.digest,
		}}
	}
	var selectors map[string]string
	if seal.tmpdirBound {
		commitment := seal.tmpdir
		if !seal.tmpdirCommitted {
			commitment = agentic.CommitSealLiteral(TempDirEnv, seal.tmpdir)
		}
		selectors = map[string]string{TempDirEnv: commitment}
	}
	return agentic.SealedData{
		Binary:    seal.binary,
		Argv:      append([]string(nil), seal.argv...),
		Artifacts: artifacts,
		Selectors: selectors,
	}
}

// VerifySealedArtifacts implements agentic.ExecArtifactVerifier. It
// re-verifies the sealed catalog file against the ORIGINAL digest with the
// same checks the in-process verifier applies. A TempDir-only seal binds
// no file and re-verifies nothing.
func (seal *catalogExecSeal) VerifySealedArtifacts() error {
	if seal.path == "" {
		return nil
	}
	return verifyLaunchCatalog(seal.path, seal.digest, seal.providerID)
}

// importTempDirOnlySeal rebuilds the verifier for a TempDir-only export:
// no artifacts and a well-formed TMPDIR commitment. Anything else is not
// this sealer's shape and falls through to the catalog malformed refusal,
// so the no-artifact defect keeps its existing sanitized shape.
func importTempDirOnlySeal(data agentic.SealedData) (*catalogExecSeal, bool) {
	if len(data.Artifacts) != 0 {
		return nil, false
	}
	commitment, ok := data.Selectors[TempDirEnv]
	if !ok || !isSealCommitment(commitment) {
		return nil, false
	}
	return &catalogExecSeal{binary: data.Binary, argv: append([]string(nil), data.Argv...), tmpdir: commitment, tmpdirBound: true, tmpdirCommitted: true}, true
}

// isSealCommitment reports whether a selector value has the keyed-commitment
// shape the exporter mints. A literal directory passed as a commitment is
// malformed data, never a value to compare against.
func isSealCommitment(value string) bool {
	return strings.HasPrefix(value, sealedCommitmentPrefix)
}

// ImportSealedData implements agentic.ExecSealImporter. It rebuilds the
// catalog verifier the in-process sealer would have built: the same binary,
// the same exact argv, and the same artifact digest — verified against the
// file on disk right now, so an already-swapped catalog refuses at import
// instead of minting a verifier that only fails later. Structural defects
// refuse as malformed with the same sanitized shape as every other local
// catalog refusal, including a malformed TMPDIR selector on a catalog
// export.
func (*System) ImportSealedData(data agentic.SealedData) (agentic.ExecPlanVerifier, error) {
	if len(data.Artifacts) != 1 || data.Artifacts[0].Name != sealedCatalogName {
		if tmpdirSeal, ok := importTempDirOnlySeal(data); ok {
			return tmpdirSeal, nil
		}
		return nil, localCatalogRefusal(agentic.LocalProviderMalformed, sealedCatalogName, "")
	}
	artifact := data.Artifacts[0]
	if !filepath.IsAbs(artifact.Path) {
		return nil, localCatalogRefusal(agentic.LocalProviderMalformed, sealedCatalogName, "")
	}
	digest, ok := parseSealedDigest(artifact.Digest)
	if !ok {
		return nil, localCatalogRefusal(agentic.LocalProviderMalformed, sealedCatalogName, "")
	}
	if err := verifyLaunchCatalog(artifact.Path, digest, ""); err != nil {
		return nil, err
	}
	seal := &catalogExecSeal{path: artifact.Path, digest: digest, binary: data.Binary, argv: append([]string(nil), data.Argv...)}
	if commitment, ok := data.Selectors[TempDirEnv]; ok {
		if !isSealCommitment(commitment) {
			return nil, localCatalogRefusal(agentic.LocalProviderMalformed, sealedCatalogName, "")
		}
		seal.tmpdir, seal.tmpdirBound, seal.tmpdirCommitted = commitment, true, true
	}
	return seal, nil
}

// parseSealedDigest splits a sha256:<64 hex> artifact digest back into the
// bare hex the catalog verifier compares. Anything else is malformed data,
// never a different hash.
func parseSealedDigest(digest string) (string, bool) {
	hex := strings.TrimPrefix(digest, sealedDigestPrefix)
	if hex == digest || len(hex) != 64 {
		return "", false
	}
	for _, r := range hex {
		digit := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !digit {
			return "", false
		}
	}
	return hex, true
}
