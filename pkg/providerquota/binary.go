package providerquota

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/relux-works/skill-agents-management/internal/launchenv"
)

// BinaryFS is instance-local filesystem evidence. Resolution never uses Stat:
// each link is inspected before its target may be visited.
type BinaryFS interface {
	Getwd() (string, error)
	Lstat(string) (os.FileInfo, error)
	Readlink(string) (string, error)
}
type NativeBinaryFS struct{}

// Use the system cwd call: os.Getwd can stat PWD before a root check.
func (NativeBinaryFS) Getwd() (string, error)              { return syscall.Getwd() }
func (NativeBinaryFS) Lstat(p string) (os.FileInfo, error) { return os.Lstat(p) }
func (NativeBinaryFS) Readlink(p string) (string, error)   { return os.Readlink(p) }

// BinarySpec contains lexical package-layout evidence, never a probed path.
// Empty package fields disable managed-package and shim unwrapping.
type BinarySpec struct {
	Name, ManagedRoot, PlatformPackage, TargetTriple string
}

type binaryResolver struct {
	fs            BinaryFS
	cwd           string
	roots         []string
	rootsResolved bool
	buildingRoots bool
	unresolved    []string
	bases         []string
}

func (r *binaryResolver) absolute(p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.cwd, p)
	}
	return filepath.Clean(p)
}
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (r *binaryResolver) check(p string) error {
	p = r.absolute(p)
	for _, root := range r.roots {
		if within(root, p) {
			return Refuse("protected_root")
		}
	}
	// Until an ancestor is proved, its root may occur under an unknown alias.
	// The root itself is never followed, so its basename survives ancestor
	// canonicalization. Refuse descent through that component anywhere rather
	// than probing a potentially protected parent to learn more evidence.
	barriers := r.unresolved
	if r.buildingRoots {
		barriers = r.roots
	}
	for _, root := range barriers {
		for _, part := range strings.Split(p, string(filepath.Separator)) {
			if part == filepath.Base(root) {
				return Refuse("protected_root")
			}
		}
	}
	return nil
}
func newBinaryResolver(req Request, fs BinaryFS) (binaryResolver, error) {
	if fs == nil {
		return binaryResolver{}, Refuse("filesystem_required")
	}
	cwd, err := fs.Getwd()
	if err != nil || !filepath.IsAbs(cwd) {
		return binaryResolver{}, Refuse("binary_search_invalid")
	}
	r := binaryResolver{fs: fs, cwd: cwd}
	roots := append([]string{}, req.ForbiddenRoots...)
	if req.Context.Identity != nil {
		roots = append(roots, req.Context.Identity.Home)
	}
	// Default roots remain protected even when a harness selects another home.
	if home := launchenv.Value(req.Env, "HOME"); home != "" {
		r.bases = append(r.bases, r.absolute(home))
		for _, name := range []string{".codex", ".claude", ".gemini", filepath.Join(".config", "muse")} {
			roots = append(roots, filepath.Join(home, name))
		}
	}
	for _, name := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "MUSE_HOME", "MUSE_CONFIG_DIR", "MUSE_AUTH_PATH", "MUSE_SESSIONS_DIR"} {
		if value := launchenv.Value(req.Env, name); value != "" {
			roots = append(roots, value)
		}
	}
	if home := launchenv.Value(req.Env, "GEMINI_CLI_HOME"); home != "" {
		roots = append(roots, filepath.Join(home, ".gemini"))
	}
	if config := launchenv.Value(req.Env, "XDG_CONFIG_HOME"); config != "" {
		roots = append(roots, filepath.Join(config, "muse"))
	}
	for _, root := range roots {
		if root != "" {
			r.roots = append(r.roots, r.absolute(root))
			r.bases = append(r.bases, filepath.Dir(r.absolute(root)))
		}
	}
	return r, nil
}

// Learn aliases from HOME and root parents, never the roots themselves. Each
// learned alias protects every root with that prefix immediately. Repeat over
// the bases until no alias is added; input order and lexical lengths do not
// determine which canonical roots are protected. Unknown ancestry retains the
// lexical root and the conservative component barrier in check.
func (r *binaryResolver) prepareRoots() error {
	r.buildingRoots = true
	defer func() { r.buildingRoots = false }()
	for round := 0; round < 40; round++ {
		before := len(r.roots)
		for _, base := range r.bases {
			_, _, err := r.resolveChecked(base)
			if refusal, ok := err.(*Refusal); ok && refusal.Reason != "protected_root" {
				return err
			}
		}
		if len(r.roots) == before {
			// A blocked parent is protected by inclusion when a known root
			// covers it. Otherwise retain a barrier for insufficient evidence.
			for _, root := range append([]string{}, r.roots...) {
				parent := filepath.Dir(root)
				_, _, err := r.resolveChecked(parent)
				if err != nil && !os.IsNotExist(err) {
					covered := false
					for _, known := range r.roots {
						covered = covered || within(known, parent)
					}
					if !covered {
						r.unresolved = append(r.unresolved, root)
					}
				}
			}
			if len(r.roots) == before {
				r.rootsResolved = true
				return nil
			}
		}
	}
	return Refuse("protected_root") // Bounded fixed point; evidence is insufficient.
}

// Register prefix equivalence before any descent through the new target. Both
// raw and canonical forms remain protected. Close over already learned roots
// so aliases discovered in either order protect the same descendants.
func (r *binaryResolver) learnAlias(from, to string) error {
	from, to = r.absolute(from), r.absolute(to)
	for round := 0; round < 40; round++ {
		before := len(r.roots)
		for _, root := range append([]string{}, r.roots...) {
			for _, pair := range [][2]string{{from, to}, {to, from}} {
				if !within(pair[0], root) {
					continue
				}
				rel, _ := filepath.Rel(pair[0], root)
				alias := filepath.Join(pair[1], rel)
				found := false
				for _, known := range r.roots {
					found = found || known == alias
				}
				if !found {
					if len(r.roots) >= 4096 {
						return Refuse("protected_root")
					}
					r.roots = append(r.roots, alias)
				}
			}
		}
		if len(r.roots) == before {
			return nil
		}
	}
	return Refuse("protected_root")
}

func (r *binaryResolver) resolve(candidate string) (string, os.FileInfo, error) {
	if err := r.check(candidate); err != nil {
		return "", nil, err
	}
	if !r.rootsResolved {
		if err := r.prepareRoots(); err != nil {
			return "", nil, err
		}
	}
	return r.resolveChecked(candidate)
}

// resolve checks the entire candidate before touching any component, then
// checks every resolved link target (including directory links) before descent.
// Forty hops bound cycles. Refusals are never treated as a missing candidate.
func (r *binaryResolver) resolveChecked(candidate string) (string, os.FileInfo, error) {
	candidate = r.absolute(candidate)
	for hops := 0; ; {
		if err := r.check(candidate); err != nil {
			return "", nil, err
		}
		volume := filepath.VolumeName(candidate)
		prefix := volume + string(filepath.Separator)
		parts := strings.Split(strings.TrimPrefix(candidate, prefix), string(filepath.Separator))
		restart := false
		var info os.FileInfo
		for i, part := range parts {
			if part == "" {
				continue
			}
			prefix = filepath.Join(prefix, part)
			if err := r.check(prefix); err != nil {
				return "", nil, err
			}
			var err error
			info, err = r.fs.Lstat(prefix)
			if err != nil {
				return "", nil, err
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			if hops >= 40 {
				return "", nil, Refuse("symlink_limit")
			}
			hops++
			if err := r.check(prefix); err != nil {
				return "", nil, err
			}
			target, err := r.fs.Readlink(prefix)
			if err != nil {
				return "", nil, err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(prefix), target)
			}
			if err := r.learnAlias(prefix, target); err != nil {
				return "", nil, err
			}
			// Check the target itself as well as the full remaining candidate: '..'
			// in the remainder must not hide a protected link target.
			if err := r.check(target); err != nil {
				return "", nil, err
			}
			candidate = filepath.Join(append([]string{target}, parts[i+1:]...)...)
			if err := r.check(candidate); err != nil {
				return "", nil, err
			}
			restart = true
			break
		}
		if !restart {
			return candidate, info, nil
		}
	}
}
func (r *binaryResolver) executable(candidate string) (string, bool, error) {
	resolved, info, err := r.resolve(candidate)
	if err != nil {
		if _, ok := err.(*Refusal); ok {
			return "", false, err
		}
		return "", false, nil
	}
	return resolved, info != nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0, nil
}
func (r *binaryResolver) packageBinary(root string, spec BinarySpec) (string, error) {
	if root == "" || spec.PlatformPackage == "" || spec.TargetTriple == "" {
		return "", nil
	}
	exe := spec.Name
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	for _, candidate := range []string{
		filepath.Join(root, "node_modules", spec.PlatformPackage, "vendor", spec.TargetTriple, "bin", exe),
		filepath.Join(root, "vendor", spec.TargetTriple, "bin", exe),
	} {
		_, ok, err := r.executable(candidate)
		if err != nil {
			return "", err
		}
		if ok {
			return r.absolute(candidate), nil
		}
	}
	return "", nil
}

// ResolveBinary is quota-only resolution. It checks all lexical candidates
// and follows links only through checked targets; it never invokes a harness.
func ResolveBinary(req Request, spec BinarySpec, fs BinaryFS) (string, error) {
	r, err := newBinaryResolver(req, fs)
	if err != nil {
		return "", err
	}
	if spec.ManagedRoot != "" {
		if err := r.check(spec.ManagedRoot); err != nil {
			return "", err
		}
		binary, err := r.packageBinary(spec.ManagedRoot, spec)
		if err != nil || binary != "" {
			return binary, err
		}
	}
	candidates := []string{}
	if strings.ContainsRune(spec.Name, filepath.Separator) {
		candidates = append(candidates, spec.Name)
	} else {
		path, ok := launchenv.Lookup(req.Env, "PATH")
		if !ok {
			return "", Refuse("path_required")
		}
		for _, dir := range filepath.SplitList(path) {
			if dir == "" {
				dir = r.cwd
			}
			candidates = append(candidates, filepath.Join(dir, spec.Name))
		}
	}
	for _, candidate := range candidates {
		resolved, ok, err := r.executable(candidate)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		if filepath.Base(resolved) == spec.Name+".js" && filepath.Base(filepath.Dir(resolved)) == "bin" {
			binary, err := r.packageBinary(filepath.Dir(filepath.Dir(resolved)), spec)
			if err != nil || binary != "" {
				return binary, err
			}
		}
		return r.absolute(candidate), nil
	}
	return "", Refuse("binary_not_found")
}
