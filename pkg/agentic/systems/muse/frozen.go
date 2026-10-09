package muse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// ErrMuseFrozenToolInvalid refuses a frozen tool tuple the launch must
// not run: a partial tuple, a non-canonical path, a symlink, a
// non-regular or non-executable file, a filename that disagrees with the
// tuple build, or bytes whose digest disagrees with the tuple SHA-256.
// It never falls back to PATH: an invalid override is a refusal, not a
// request to discover something else.
var ErrMuseFrozenToolInvalid = errors.New("muse: frozen tool tuple is invalid")

// resolveMuseBinary is Muse's ONE binary-resolution path: the supplied
// frozen copy when the request carries the tuple, whatever `muse`
// resolves to on the handed PATH otherwise. It never discovers
// installations, copies files or executes the updater: population is the
// host's job, selection is this function's, and the two are not the
// same. System.ResolveBinary and the interactive yolo argv share it, so
// the planned binary and the probed binary cannot drift apart.
func resolveMuseBinary(req agentic.LaunchRequest) (string, error) {
	if req.FrozenToolBinary == "" && req.FrozenToolBuild == "" && req.FrozenToolSHA256 == "" {
		return resolveBinary(req.Env)
	}
	if err := validateFrozenToolTuple(req.FrozenToolBinary, req.FrozenToolBuild, req.FrozenToolSHA256); err != nil {
		return "", err
	}
	return req.FrozenToolBinary, nil
}

// validateFrozenToolTuple holds the frozen tuple to its contract: all
// three members together, a canonical absolute path, a non-symlink
// regular executable leaf, filename/build agreement through the shared
// pinned-name grammar, and digest agreement with the file's bytes.
func validateFrozenToolTuple(binary, build, sha string) error {
	if binary == "" || build == "" || sha == "" {
		return fmt.Errorf("%w: binary, build and sha256 travel together; a partial tuple selects nothing", ErrMuseFrozenToolInvalid)
	}
	if !filepath.IsAbs(binary) || filepath.Clean(binary) != binary {
		return fmt.Errorf("%w: binary %q is not a canonical absolute path", ErrMuseFrozenToolInvalid, binary)
	}
	info, err := os.Lstat(binary)
	if err != nil {
		return fmt.Errorf("%w: binary %q cannot be statted: %w", ErrMuseFrozenToolInvalid, binary, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: binary %q is a symlink, not the frozen copy itself", ErrMuseFrozenToolInvalid, binary)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: binary %q is not a regular file", ErrMuseFrozenToolInvalid, binary)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%w: binary %q is not executable", ErrMuseFrozenToolInvalid, binary)
	}
	identity, err := ParsePinnedBinaryName(filepath.Base(binary))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMuseFrozenToolInvalid, err)
	}
	if identity.Build != build {
		return fmt.Errorf("%w: filename build %q does not match tuple build %q", ErrMuseFrozenToolInvalid, identity.Build, build)
	}
	if !isLowerHex64(sha) {
		return fmt.Errorf("%w: sha256 %q is not 64 lowercase hex", ErrMuseFrozenToolInvalid, sha)
	}
	digest, err := hashInteractiveSealBinary(binary)
	if err != nil {
		return fmt.Errorf("%w: binary %q cannot be hashed: %w", ErrMuseFrozenToolInvalid, binary, err)
	}
	if digest != sha {
		return fmt.Errorf("%w: binary bytes do not match the tuple sha256", ErrMuseFrozenToolInvalid)
	}
	return nil
}
