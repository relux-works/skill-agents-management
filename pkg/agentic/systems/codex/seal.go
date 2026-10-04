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
)

// ExportSealedData implements agentic.ExecSealExporter. It reports the
// bound binary, exact argv and catalog artifact from verifier memory and
// performs no I/O: exporting never re-reads the artifact it reports, so a
// swapped catalog still exports the ORIGINAL digest and refuses at import
// or verification time instead of silently resealing. System and integrity
// digest are stamped by agentic.ExportSeal.
func (seal *catalogExecSeal) ExportSealedData() agentic.SealedData {
	return agentic.SealedData{
		Binary: seal.binary,
		Argv:   append([]string(nil), seal.argv...),
		Artifacts: []agentic.SealedArtifact{{
			Name:   sealedCatalogName,
			Path:   seal.path,
			Digest: sealedDigestPrefix + seal.digest,
		}},
	}
}

// VerifySealedArtifacts implements agentic.ExecArtifactVerifier. It
// re-verifies the sealed catalog file against the ORIGINAL digest with the
// same checks the in-process verifier applies.
func (seal *catalogExecSeal) VerifySealedArtifacts() error {
	return verifyLaunchCatalog(seal.path, seal.digest, seal.providerID)
}

// ImportSealedData implements agentic.ExecSealImporter. It rebuilds the
// catalog verifier the in-process sealer would have built: the same binary,
// the same exact argv, and the same artifact digest — verified against the
// file on disk right now, so an already-swapped catalog refuses at import
// instead of minting a verifier that only fails later. Structural defects
// refuse as malformed with the same sanitized shape as every other local
// catalog refusal.
func (*System) ImportSealedData(data agentic.SealedData) (agentic.ExecPlanVerifier, error) {
	if len(data.Artifacts) != 1 || data.Artifacts[0].Name != sealedCatalogName {
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
	return &catalogExecSeal{path: artifact.Path, digest: digest, binary: data.Binary, argv: append([]string(nil), data.Argv...)}, nil
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
