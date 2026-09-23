package pinative

import (
	"errors"
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THIS FILE IS THE ONE PLACE NATIVE PI FLAGS ARE SPELLED.
//
// # The model identity is ALWAYS provider-qualified
//
// Pi 0.84.2 resolves `--model <pattern>` across every provider whose data file
// it ships. A bare `claude-opus-5` exits 1 as "ambiguous across providers";
// `gpt-5.5` appears in six provider files. `anthropic/claude-opus-5` resolves
// to exactly one row (probe pi-probe-provider-qualified-01.log,
// TASK-260908-ggxfte rev2 §5). The provider segment is the runtime's VENDOR,
// which BuildLaunch writes onto LaunchRequest.Vendor after the vendor's Spawn
// and which Pi spells identically (`anthropic`, `openai`, `google` are the
// literal data-file names), so no mapping table exists here.
//
// A request with no Vendor is REFUSED (ErrVendorMissing), never downgraded to
// a bare id: a bare id is the one shape this plugin exists to never emit, and
// a wrong-prefix id (`openai/claude-opus-5`) falls to Pi's custom-model path
// with only a warning — an unverified launch that looks like the one asked for.
//
// # The effort is transport, gated by the installed Pi contract
//
// `--thinking <word>` is emitted exactly when the request carries an effort.
// BuildLaunch has already refused a word outside the row's vocabulary and a
// required-effort row given none, so reaching that line means a value the
// operator chose. The row's vocabulary stays the model's contract (invariant
// 4), but a word installed Pi would drop or clamp is REFUSED here, never
// rewritten (catalog.go, ErrEffortNotNativelySupported): a plan must not
// claim an effort the session will not run. BuildLaunch applies the same
// check earlier, through agentic.EffortAdmitter, where it can also name the
// row's recommendation; this second call closes the direct-plugin path.
//
// # The two modes build the SAME argv
//
// A dry run mirrors the interactive launch byte for byte; there is no prompt
// placeholder because there is no prompt.

// ErrVendorMissing is returned when the launch request names no vendor, so no
// provider-qualified identity can be built.
var ErrVendorMissing = errors.New("pinative: launch request names no vendor; a native Pi model identity is <provider>/<model> and a bare id is ambiguous across providers")

// Args builds the native Pi argv for one launch mode, excluding the binary. It
// is the single construction site.
//
// Yolo is REFUSED here, not mapped (curator-spec Decision 0018's pi row): `pi
// --help` at the pinned 0.84.2 documents no permission-bypass flag — only
// `--approve`/`-a` ("Trust project-local files for this run") and
// `--no-approve`/`-na`, which Decision 0018 records as not equivalent.
func Args(req agentic.LaunchRequest, mode agentic.LaunchMode) ([]string, error) {
	effective, err := req.PermissionMode.Resolve()
	if err != nil {
		return nil, fmt.Errorf("pinative: %w", err)
	}
	if mode != agentic.LaunchModeInteractive && !req.PermissionMode.IsZero() {
		return nil, fmt.Errorf("pinative: %w: permission mode %q is valid only for interactive launches",
			agentic.ErrPermissionModeNotInteractive, strings.TrimSpace(string(req.PermissionMode)))
	}
	if mode != agentic.LaunchModeInteractive && len(req.NativeArgs) != 0 {
		return nil, fmt.Errorf("pinative: %w: %d native argument(s) reach no verbatim suffix outside an interactive launch",
			agentic.ErrNativeArgsNotInteractive, len(req.NativeArgs))
	}
	switch mode {
	case agentic.LaunchModeInteractive, agentic.LaunchModeDryRun:
	default:
		return nil, fmt.Errorf("pinative: unsupported launch mode %s", mode)
	}
	if mode == agentic.LaunchModeInteractive && effective == agentic.PermissionModeYolo {
		// Drift fails closed before support is even asked: an unpinned
		// or newer release, and an empty one, refuse as unverified,
		// and only the verified release reaches the unsupported
		// refusal below.
		if _, err := agentic.LookupReleaseCapability(verifiedReleases, req.ToolRelease); err != nil {
			return nil, fmt.Errorf("pinative: %w", err)
		}
		return nil, fmt.Errorf("pinative: refusing yolo: %w: pi 0.84.2 documents no interactive permission-bypass flag; --approve trusts project-local files for this run and is not equivalent",
			agentic.ErrPermissionModeUnsupported)
	}
	vendor := strings.TrimSpace(req.Vendor)
	if vendor == "" {
		return nil, ErrVendorMissing
	}
	if strings.Contains(vendor, "/") {
		return nil, fmt.Errorf("pinative: vendor %q contains a path separator and cannot prefix a Pi model identity", vendor)
	}
	// BuildPlan substitutes the alias target before Argv is called, so
	// req.Model.ID is the launch identity here. LaunchIdentity() is applied
	// anyway so a caller holding the plugin directly gets the same answer.
	model := req.Model.LaunchIdentity()
	if model == "" {
		return nil, fmt.Errorf("pinative: launch request names no model")
	}
	if strings.Contains(model, "/") {
		return nil, fmt.Errorf("pinative: model %q already carries a provider segment; the identity is qualified here, once, from the runtime's vendor", model)
	}
	args := []string{"--model", vendor + "/" + model}
	if effort := strings.TrimSpace(req.Effort); effort != "" {
		if _, err := checkNativeEffort(vendor, model, effort, nil); err != nil {
			return nil, err
		}
		args = append(args, "--thinking", effort)
	}
	// The verbatim suffix: native forwards the caller's arguments with no
	// inspection (Decision 0018 item 4). Yolo never reaches here — refused
	// above — and dry-run carries none (the scope gate refused them).
	return append(args, nativeArgsSuffix(req)...), nil
}

// nativeArgsSuffix returns the caller's native arguments for the verbatim
// interactive suffix, copied so the plan's argv never aliases the request's
// backing array.
func nativeArgsSuffix(req agentic.LaunchRequest) []string {
	return append([]string{}, req.NativeArgs...)
}
