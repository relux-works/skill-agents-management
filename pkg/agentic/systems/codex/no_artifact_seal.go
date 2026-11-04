package codex

import (
	"fmt"
	"slices"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// This provider-owned selector versions explicit absence of catalog/TMPDIR
// artifacts. It does not extend hosted admission or erase carried reports.
const noArtifactSealSelector = "codex-no-artifact"

// Only the owning sealer installs this private basis on an admitted Plan.
// BuildPlan retains its original process snapshot; owning FinalizePlan commits
// the exact composed process without recreating admission from current bytes.
type noArtifactSeal struct {
	catalogExecSeal
	env []string
	// Imported projections require the generic keyed final-process wrapper.
	// A provider selector or caller snapshot alone cannot verify a process.
	imported bool
}

func newNoArtifactSeal(plan agentic.Plan) *noArtifactSeal {
	return &noArtifactSeal{
		catalogExecSeal: catalogExecSeal{binary: plan.Binary, argv: slices.Clone(plan.Argv), carriedSettings: cloneCarriedSettingsReport(plan.CarriedSettings)},
		env:             slices.Clone(plan.Env),
	}
}

func (s *noArtifactSeal) VerifyBeforeExec(plan agentic.Plan) error {
	if s.imported {
		return fmt.Errorf("%w: no-artifact projection requires finalized verification", agentic.ErrSealImportRefused)
	}
	if plan.Binary != s.binary || !slices.Equal(plan.Argv, s.argv) || !slices.Equal(plan.Env, s.env) {
		return fmt.Errorf("%w: no-artifact base process changed", agentic.ErrFinalizedProcessChanged)
	}
	return s.verifyCarriedSettings(plan.CarriedSettings)
}

// The embedded verifier preserves report-presence/digest verification and its
// final-plan-aware check. It owns no file artifact in this explicit branch.
func (s *noArtifactSeal) ExportSealedData() agentic.SealedData {
	data := s.catalogExecSeal.ExportSealedData()
	if data.Selectors == nil {
		data.Selectors = map[string]string{}
	}
	data.Selectors[noArtifactSealSelector] = "v1"
	return data
}

// Only finalized projections import: base exports have no environment
// commitments. Generic ImportSeal validates the binding digest/key and wraps
// this verifier; no public API installs it onto a caller-created base Plan.
func importNoArtifactSeal(data agentic.SealedData) (agentic.ExecPlanVerifier, error) {
	if data.Selectors[noArtifactSealSelector] != "v1" || len(data.Artifacts) != 0 || data.Binding == nil {
		return nil, localCatalogRefusal(agentic.LocalProviderMalformed, sealedCatalogName, "")
	}
	for name, value := range data.Selectors {
		switch name {
		case noArtifactSealSelector:
		case carriedSettingsSealSelector:
			if _, valid := parseSealedDigest(value); !valid {
				return nil, localCatalogRefusal(agentic.LocalProviderMalformed, sealedCatalogName, "")
			}
		default:
			return nil, localCatalogRefusal(agentic.LocalProviderMalformed, sealedCatalogName, "")
		}
	}
	if data.Binary != data.Binding.Binary || !slices.Equal(data.Argv, data.Binding.Argv) {
		return nil, localCatalogRefusal(agentic.LocalProviderMalformed, sealedCatalogName, "")
	}
	if slices.Contains(data.Binding.EnvNames, TempDirEnv) {
		return nil, localCatalogRefusal(agentic.LocalProviderMalformed, sealedCatalogName, "")
	}
	return &noArtifactSeal{catalogExecSeal: catalogExecSeal{binary: data.Binary, argv: slices.Clone(data.Argv), importedCarriedSettingsDigest: data.Selectors[carriedSettingsSealSelector]}, imported: true}, nil
}
