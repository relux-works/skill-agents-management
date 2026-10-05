package agentic

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Native launch-boundary adapter.
//
// CheckLaunchBoundary repeats, immediately before a start, the launcher's
// native pre-exec boundary: the provider binary identity, the codex MCP layer
// probe, and the prompt/system-prompt boundary (curator-agent-launcher
// internal/execution, internal/composition, internal/systemprompt). The
// tb-sessiond daemon and the launcher do not call this API yet; launcher
// migration to it follows in a separate leaf and is not wired yet. This API is
// available for consumers so a future daemon or launcher call re-checks
// through one module-owned entry point instead of recomposing the checks or
// importing another repository's internal package (hosted-launch-contract-r2.md
// section 5, d4-phase1-daemon-plan.md section 5).
//
// The boundary port is line-cited against curator-agent-launcher at fbcdbaf0.
// Refusal codes and message bytes are identical to the launcher's so a consumer
// splitting on ": " observes the same code the launcher's diagnostics layer
// reports. The MCP admission legs are split by provenance: the fragment
// mcp.path validator ports the launcher's fragment admission
// (internal/fragment/fragment.go), while the inline --mcp-config validator is
// the module's own mcp-config-json grammar (internal/mcpjson/mcpjson.go:96-153,
// byte-identical at v0.5.37), kept as an explicitly requested inline validator
// distinct from the launcher's native reachable boundary (launcher
// internal/plan/plan.go:74-77 at fbcdbaf0 keeps Composition unset, so the
// reachable curator run path carries no inline Composition to validate).
//
// This file owns no exec and performs no filesystem writes. Filesystem reads
// are exactly the launcher's boundary reads: provider binary stats, the codex
// MCP layer probe, and the Pi prompt probes. The MCP admission validators
// (fragment mcp.path, inline --mcp-config JSON grammar) are pure string
// validation and perform no reads; a future daemon admits every other fact
// (full fragment sections, descriptor metadata) through the existing module
// validators, never through this adapter.
const (
	// LaunchBoundaryCodeExecMissing transcribes the launcher's
	// exec_provider_missing (diagnostics.CodeExecMissing): the provider
	// binary is absent, not executable, or not resolvable.
	LaunchBoundaryCodeExecMissing = "exec_provider_missing"
	// LaunchBoundaryCodeMCPLayerMissing transcribes composition's
	// mcp_layer_missing: the codex MCP layer path does not exist.
	LaunchBoundaryCodeMCPLayerMissing = "mcp_layer_missing"
	// LaunchBoundaryCodeMCPLayerUnreadable transcribes composition's
	// mcp_layer_unreadable: the codex MCP layer path exists but cannot be
	// read as a regular file.
	LaunchBoundaryCodeMCPLayerUnreadable = "mcp_layer_unreadable"
	// LaunchBoundaryCodeSyspromptUnavailable transcribes systemprompt's
	// sysprompt_channel_unavailable: no non-file channel matches the
	// requested system-prompt intent.
	LaunchBoundaryCodeSyspromptUnavailable = "sysprompt_channel_unavailable"
	// LaunchBoundaryCodeSyspromptUnreadable transcribes systemprompt's
	// sysprompt_file_unreadable: a probed prompt file cannot be read as a
	// regular file.
	LaunchBoundaryCodeSyspromptUnreadable = "sysprompt_file_unreadable"
)

// LaunchBoundaryBinaryError refuses a provider binary the boundary cannot
// resolve to an executable regular file. It renders byte-identically to the
// launcher's execution.go Run refusal.
type LaunchBoundaryBinaryError struct {
	Binary string
	Err    error
}

func (e *LaunchBoundaryBinaryError) Error() string {
	return fmt.Sprintf("%s: %s: install the provider executable and make it available on the launch PATH: %v", LaunchBoundaryCodeExecMissing, e.Binary, e.Err)
}

func (e *LaunchBoundaryBinaryError) Unwrap() error { return e.Err }

// Code reports the stable launcher diagnostic code.
func (e *LaunchBoundaryBinaryError) Code() string { return LaunchBoundaryCodeExecMissing }

// LaunchBoundaryMCPError refuses a codex MCP layer that is absent or cannot
// be read as a regular file. It renders byte-identically to the launcher's
// composition.LayerError.
type LaunchBoundaryMCPError struct {
	Code string
	Path string
	Err  error
}

func (e *LaunchBoundaryMCPError) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Path, e.Err)
}

func (e *LaunchBoundaryMCPError) Unwrap() error { return e.Err }

// LaunchBoundaryPromptError refuses a system-prompt selection or file probe
// failure. It renders byte-identically to the launcher's systemprompt.Refusal,
// including the empty quoted path on channel-unavailable refusals.
type LaunchBoundaryPromptError struct {
	Code string
	Path string
	Err  error
}

func (e *LaunchBoundaryPromptError) Error() string {
	return fmt.Sprintf("%s: %q: %v", e.Code, e.Path, e.Err)
}

func (e *LaunchBoundaryPromptError) Unwrap() error { return e.Err }

// LaunchBoundaryProcess carries the final process facts the boundary
// re-checks. Env is the FULL composed child environment: bare binary names
// resolve against its PATH, never against ambient process state.
type LaunchBoundaryProcess struct {
	Binary  string
	WorkDir string
	Env     []string
}

// LaunchBoundarySystemPrompt carries the validated prompt projection and the
// caller-owned intent. Channels are the complete adapter-declared descriptor
// list from the admitted fragment; Intent is "" for no opt-in. An absent
// fragment section with an explicit opt-in is spelled as a non-nil value
// carrying the Intent with no Channels, which refuses channel-unavailable;
// a nil SystemPrompt always selects nothing.
type LaunchBoundarySystemPrompt struct {
	Path     string
	Channels []CuratorChannelDescriptor
	Intent   CuratorSystemPromptIntent
}

// LaunchBoundaryPolicy carries the admitted launch facts the boundary
// re-checks. Environment is the launcher environment id ("claude_code",
// "codex_cli", "opencode", "pi", "muse"). MCPLayerPath is the codex MCP layer
// path and is probed only for the codex_cli environment with a non-empty
// path. MCPAdmission carries the MCP facts the admission validators
// re-check; a nil admission skips them. Home is the managed home the Pi file
// probes read under. ProfileName feeds warning text only. A nil SystemPrompt
// is an absent section.
type LaunchBoundaryPolicy struct {
	Environment  string
	MCPLayerPath string
	MCPAdmission *LaunchBoundaryMCPAdmission
	Home         string
	ProfileName  string
	SystemPrompt *LaunchBoundarySystemPrompt
}

// LaunchBoundaryReport carries non-refusal boundary output. Warnings are the
// launcher warning lines without a trailing LF; every execution mode must
// emit them before launching. ResolvedBinary is the absolute executable path
// the binary check resolved; consumers exec that path rather than
// re-resolving.
type LaunchBoundaryReport struct {
	Warnings       []string
	ResolvedBinary string
}

// CheckLaunchBoundary repeats the launcher's native pre-exec boundary in SPEC
// section 4.6 order: the binary check first, then the codex MCP layer probe,
// then the prompt/system-prompt boundary. The first failure is the one
// reported. Before the boundary legs it repeats the MCP admission validators
// in launcher order (fragment mcp.path admission first, then the inline
// grammar), so an admission fault is reported before any boundary fault. The
// fragment leg ports the launcher's resolve-time admission; the inline leg is
// the module's own explicitly requested validator (see the package header and
// launchboundary_admission.go for provenance). Inputs are already-admitted
// launch facts; this entry point performs no exec and no filesystem writes.
func CheckLaunchBoundary(process LaunchBoundaryProcess, policy LaunchBoundaryPolicy) (LaunchBoundaryReport, error) {
	// Launcher order: cmd/curator-run/main.go admits the fragment
	// (resolver.Resolve) before the spawn-plane build (plan.Build), and
	// internal/execution/execution.go:161-173 runs executable(), then
	// Value.CheckLaunchBoundary(), then the Boundary callback
	// (systemprompt.PrepareLaunch bound in cmd/curator-run/main.go),
	// reporting the first failure.
	if err := checkLaunchBoundaryMCPConfigPath(policy.Environment, policy.MCPAdmission); err != nil {
		return LaunchBoundaryReport{}, err
	}
	if err := checkLaunchBoundaryMCPInline(policy.MCPAdmission); err != nil {
		return LaunchBoundaryReport{}, err
	}
	var binary string
	if resolved, err := launchBoundaryExecutable(process.Binary, process.WorkDir, process.Env); err != nil {
		return LaunchBoundaryReport{}, err
	} else {
		binary = resolved
	}
	if err := checkLaunchBoundaryMCPLayer(policy.Environment, policy.MCPLayerPath); err != nil {
		return LaunchBoundaryReport{}, err
	}
	var warnings []string
	if probed, err := prepareLaunchBoundaryPrompt(policy); err != nil {
		return LaunchBoundaryReport{}, err
	} else {
		warnings = probed
	}
	return LaunchBoundaryReport{Warnings: warnings, ResolvedBinary: binary}, nil
}

// launchBoundaryExecutable ports internal/execution/execution.go:202-233. A
// bare name resolves against the FULL composed environment's PATH, not an
// unrelated parent PATH. Relative entries and explicit paths resolve against
// WorkDir. It frames the typed refusal itself (launcher execution.go:166) so
// every embedded cause is an untainted filesystem error.
func launchBoundaryExecutable(binary, workdir string, env []string) (string, error) {
	if strings.ContainsRune(binary, '/') {
		if resolved, err := resolveLaunchBoundaryExecutablePath(binary, workdir); err != nil {
			return "", &LaunchBoundaryBinaryError{Binary: binary, Err: err}
		} else {
			return resolved, nil
		}
	}
	for _, entry := range env {
		if path, ok := strings.CutPrefix(entry, "PATH="); ok {
			for _, dir := range filepath.SplitList(path) {
				if found, err := resolveLaunchBoundaryExecutablePath(filepath.Join(dir, binary), workdir); err == nil {
					return found, nil
				}
			}
		}
	}
	// Transcribes os/exec.ErrNotFound's message ("executable file not found
	// in $PATH") without importing os/exec, which the module's exec-free
	// boundary forbids. Identity differs from the launcher's (errors.Is
	// against exec.ErrNotFound cannot hold here); the refusal text is
	// identical.
	return "", &LaunchBoundaryBinaryError{Binary: binary, Err: errors.New("executable file not found in $PATH")}
}

// resolveLaunchBoundaryExecutablePath ports the check closure at
// internal/execution/execution.go:203-219 as a named function: join
// non-absolute paths against WorkDir, absolutize, and require a regular file
// with any execute bit. It reports only standard-library errors; the caller
// frames the typed refusal.
func resolveLaunchBoundaryExecutablePath(path, workdir string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(workdir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", os.ErrPermission
	}
	return abs, nil
}

// checkLaunchBoundaryMCPLayer ports composition.Value.CheckLaunchBoundary
// (internal/composition/probe.go:32-64) with its composition gating
// (internal/composition/composition.go:94-97): only the codex_cli environment
// engages a layer, and an empty layer is unengaged. The launcher's fail
// closure (probe.go:37) is inlined at each return so every refusal site is
// explicit. The probe is read-only: it never removes flags, repairs files,
// or falls back. A replacement after this check remains a TOCTOU bound; the
// caller must minimize that window, not treat this as an open handle.
func checkLaunchBoundaryMCPLayer(environment, layer string) error {
	if environment != "codex_cli" || layer == "" {
		return nil
	}
	path := layer
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerMissing, Path: path, Err: err}
		}
		return &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerUnreadable, Path: path, Err: err}
	}
	info, err := os.Stat(path)
	if err != nil {
		return &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerUnreadable, Path: path, Err: err}
	}
	if !info.Mode().IsRegular() {
		return &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerUnreadable, Path: path, Err: errors.New("not a regular file")}
	}
	f, err := os.Open(path)
	if err != nil {
		return &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerUnreadable, Path: path, Err: err}
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerUnreadable, Path: path, Err: err}
	}
	if !info.Mode().IsRegular() {
		return &LaunchBoundaryMCPError{Code: LaunchBoundaryCodeMCPLayerUnreadable, Path: path, Err: errors.New("opened file is not regular")}
	}
	return nil
}

// launchBoundarySelection is the ported systemprompt.Selection: the applied
// channel plus the prompt path. The launcher's Selection also carries argv,
// but PrepareLaunch's caller discards it (only warnings are consumed), so the
// port elides argv and keeps the warning inputs. Env is empty in registry
// revision 1.
type launchBoundarySelection struct {
	channel *CuratorChannelDescriptor
	path    string
}

// selectLaunchBoundaryPrompt ports systemprompt.Select
// (internal/systemprompt/systemprompt.go:44-76) over the module's validated
// Curator descriptor projection. It performs no I/O. File channels cannot opt
// in: only a flag channel with a path argument, or a codex config-key
// channel, satisfies an intent.
func selectLaunchBoundaryPrompt(prompt *LaunchBoundarySystemPrompt, environment string) (launchBoundarySelection, error) {
	var intent CuratorSystemPromptIntent
	if prompt != nil {
		intent = prompt.Intent
	}
	if intent == "" {
		return launchBoundarySelection{}, nil
	}
	if prompt != nil && (intent == CuratorSystemPromptAppend || intent == CuratorSystemPromptReplace) {
		for i := range prompt.Channels {
			c := &prompt.Channels[i]
			if c.Semantics != intent || c.Kind == CuratorDescriptorFile {
				continue
			}
			s := launchBoundarySelection{channel: c, path: prompt.Path}
			switch c.Kind {
			// The launcher also composes Pi flag argv here; the boundary
			// caller discards it, so only the channel match matters.
			case CuratorDescriptorFlag:
				if c.Argument != CuratorArgumentPath {
					continue
				}
			case CuratorDescriptorConfigKey:
				if environment != "codex_cli" {
					continue
				}
			default:
				continue
			}
			return s, nil
		}
	}
	return launchBoundarySelection{}, &LaunchBoundaryPromptError{Code: LaunchBoundaryCodeSyspromptUnavailable, Err: fmt.Errorf("no non-file system-prompt channel for %q", intent)}
}

// launchBoundaryObservation records a readable regular managed-home file, not
// application. It ports systemprompt.Observation.
type launchBoundaryObservation struct {
	Filename  string
	Semantics CuratorSystemPromptIntent
}

// probeLaunchBoundaryPromptFiles ports systemprompt.ProbeFiles
// (internal/systemprompt/systemprompt.go:87-106): the closed registry set is
// probed regardless of SystemPrompt presence. Absence returns no observation;
// errors (including dangling links) refuse. Symlinks to readable regular
// files are accepted. No contents are consumed.
func probeLaunchBoundaryPromptFiles(environment, home string) ([]launchBoundaryObservation, error) {
	if environment != "pi" {
		return nil, nil
	}
	files := []launchBoundaryObservation{
		{Filename: "APPEND_SYSTEM.md", Semantics: CuratorSystemPromptAppend},
		{Filename: "SYSTEM.md", Semantics: CuratorSystemPromptReplace},
	}
	var observed []launchBoundaryObservation
	for _, file := range files {
		var present bool
		if isPresent, err := readableLaunchBoundaryPromptFile(filepath.Join(home, file.Filename), true); err != nil {
			return nil, err
		} else {
			present = isPresent
		}
		if present {
			observed = append(observed, file)
		}
	}
	return observed, nil
}

// readableLaunchBoundaryPromptFile ports systemprompt.readable
// (internal/systemprompt/systemprompt.go:108-138). The launcher's fail
// closure (systemprompt.go:109) is inlined at each return.
func readableLaunchBoundaryPromptFile(path string, allowAbsent bool) (bool, error) {
	if _, err := os.Lstat(path); err != nil {
		if allowAbsent && errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, &LaunchBoundaryPromptError{Code: LaunchBoundaryCodeSyspromptUnreadable, Path: path, Err: err}
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, &LaunchBoundaryPromptError{Code: LaunchBoundaryCodeSyspromptUnreadable, Path: path, Err: err}
	}
	if !info.Mode().IsRegular() {
		return false, &LaunchBoundaryPromptError{Code: LaunchBoundaryCodeSyspromptUnreadable, Path: path, Err: fmt.Errorf("not a regular file")}
	}
	// Open only after the regular-file check, so stable FIFOs cannot block.
	file, err := os.Open(path)
	if err != nil {
		return false, &LaunchBoundaryPromptError{Code: LaunchBoundaryCodeSyspromptUnreadable, Path: path, Err: err}
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return false, &LaunchBoundaryPromptError{Code: LaunchBoundaryCodeSyspromptUnreadable, Path: path, Err: err}
	}
	if !info.Mode().IsRegular() {
		return false, &LaunchBoundaryPromptError{Code: LaunchBoundaryCodeSyspromptUnreadable, Path: path, Err: fmt.Errorf("opened object is not a regular file")}
	}
	return true, nil
}

// prepareLaunchBoundaryPrompt ports systemprompt.PrepareLaunch
// (internal/systemprompt/systemprompt.go:151-166): select, freshly validate
// Pi's polymorphic flag path and managed-home files, then format warnings.
// Callers must not cache its result across launches. It does not attest
// project discovery or prevent changes between this probe and the native
// tool's subsequent open.
func prepareLaunchBoundaryPrompt(policy LaunchBoundaryPolicy) ([]string, error) {
	var s launchBoundarySelection
	if selected, err := selectLaunchBoundaryPrompt(policy.SystemPrompt, policy.Environment); err != nil {
		return nil, err
	} else {
		s = selected
	}
	if policy.Environment == "pi" && s.channel != nil && s.channel.Kind == CuratorDescriptorFlag {
		if _, err := readableLaunchBoundaryPromptFile(s.path, false); err != nil {
			return nil, err
		}
	}
	var observed []launchBoundaryObservation
	if probed, err := probeLaunchBoundaryPromptFiles(policy.Environment, policy.Home); err != nil {
		return nil, err
	} else {
		observed = probed
	}
	return formatLaunchBoundaryPromptWarnings(policy.ProfileName, s, observed), nil
}

const launchBoundaryCacheWarning = " A custom system prefix can change request caching and billing: the default may use shared prompt caching; a custom prefix forms its own cache prefix."

// formatLaunchBoundaryPromptWarnings ports systemprompt.FormatWarnings
// (internal/systemprompt/systemprompt.go:172-198). It is pure. Native
// arguments remain opaque, so only the selected launcher flag establishes
// suppression.
func formatLaunchBoundaryPromptWarnings(profile string, s launchBoundarySelection, observed []launchBoundaryObservation) []string {
	var lines []string
	if s.channel != nil && s.channel.Kind != "" {
		line := fmt.Sprintf("warning: profile %q: applied %s/%s system-prompt channel", profile, s.channel.Kind, s.channel.Semantics)
		if s.channel.Kind == CuratorDescriptorFlag {
			line += fmt.Sprintf(" %q", s.channel.Flag)
		}
		line += "."
		if s.channel.Semantics == CuratorSystemPromptReplace {
			line += " Replacement discards the tool's built-in system behavior entirely."
		}
		lines = append(lines, line+launchBoundaryCacheWarning)
	}
	for _, file := range observed {
		line := fmt.Sprintf("warning: profile %q: observed file/%s %q in managed home; ", profile, file.Semantics, file.Filename)
		if s.channel != nil && s.channel.Kind == CuratorDescriptorFlag && s.channel.Semantics == file.Semantics {
			line += fmt.Sprintf("discovery suppressed by applied launcher flag %q.", s.channel.Flag)
		} else {
			line += "conditional native-discovery candidate, subject to native flags and trusted-project precedence."
			if file.Semantics == CuratorSystemPromptReplace {
				line += " If selected, replacement discards the tool's built-in system behavior entirely."
			}
		}
		lines = append(lines, line+launchBoundaryCacheWarning)
	}
	return lines
}
