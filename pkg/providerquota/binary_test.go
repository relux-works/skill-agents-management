package providerquota

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Entirely synthetic filesystem: protected paths are never created or visited.
type traceBinaryFS struct {
	cwd         string
	touched     []string
	links       map[string]string
	executables map[string]bool
}
type binaryInfo struct {
	path string
	mode os.FileMode
}

func (i binaryInfo) Name() string               { return filepath.Base(i.path) }
func (i binaryInfo) Size() int64                { return 0 }
func (i binaryInfo) Mode() os.FileMode          { return i.mode }
func (i binaryInfo) ModTime() time.Time         { return time.Time{} }
func (i binaryInfo) IsDir() bool                { return i.mode.IsDir() }
func (i binaryInfo) Sys() any                   { return nil }
func (f *traceBinaryFS) Getwd() (string, error) { return f.cwd, nil }
func (f *traceBinaryFS) Lstat(p string) (os.FileInfo, error) {
	f.touched = append(f.touched, p)
	mode := os.ModeDir | 0700
	if _, ok := f.links[p]; ok {
		mode = os.ModeSymlink | 0700
	}
	if f.executables[p] {
		mode = 0700
	}
	return binaryInfo{p, mode}, nil
}
func (f *traceBinaryFS) Readlink(p string) (string, error) {
	f.touched = append(f.touched, p)
	return f.links[p], nil
}
func assertNoProtectedTouches(t *testing.T, f *traceBinaryFS, roots ...string) {
	t.Helper()
	for _, p := range f.touched {
		for _, root := range roots {
			if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
				t.Fatalf("touched protected path %q", p)
			}
		}
	}
}
func TestBinaryResolutionProtectedRoots(t *testing.T) {
	for _, tc := range []struct{ name, root, env string }{
		{"codex-default", "/fixture/home/.codex", ""},
		{"claude-default", "/fixture/home/.claude", ""},
		{"agy-default", "/fixture/home/.gemini", ""},
		{"muse-default", "/fixture/home/.config/muse", ""},
		{"codex-selected", "/fixture/selected", "CODEX_HOME=/fixture/selected"},
		{"claude-selected", "/fixture/selected", "CLAUDE_CONFIG_DIR=/fixture/selected"},
		{"agy-selected", "/fixture/selected/.gemini", "GEMINI_CLI_HOME=/fixture/selected"},
		{"muse-config-selected", "/fixture/selected", "MUSE_CONFIG_DIR=/fixture/selected"},
		{"muse-selected", "/fixture/selected", "MUSE_HOME=/fixture/selected"},
		{"muse-auth", "/fixture/selected/auth.json", "MUSE_AUTH_PATH=/fixture/selected/auth.json"},
		{"muse-sessions", "/fixture/selected", "MUSE_SESSIONS_DIR=/fixture/selected"},
		{"muse-xdg", "/fixture/selected/muse", "XDG_CONFIG_HOME=/fixture/selected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &traceBinaryFS{cwd: "/fixture/cwd"}
			req := Request{Env: []string{"HOME=/fixture/home", "PATH=" + tc.root + ":/fixture/allowed", tc.env}}
			_, err := ResolveBinary(req, BinarySpec{Name: "fixture"}, f)
			requireReason(t, err, "protected_root")
			assertNoProtectedTouches(t, f, tc.root)
			if len(f.touched) != 0 {
				t.Fatal("lexically protected candidate caused IO")
			}
		})
	}
}
func TestBinaryResolutionEmptyPathProtectedCwd(t *testing.T) {
	root := "/fixture/protected"
	f := &traceBinaryFS{cwd: root}
	_, err := ResolveBinary(Request{ForbiddenRoots: []string{root}, Env: []string{"PATH=:/fixture/allowed"}}, BinarySpec{Name: "fixture"}, f)
	requireReason(t, err, "protected_root")
	assertNoProtectedTouches(t, f, root)
	if len(f.touched) != 0 {
		t.Fatal("empty PATH entry caused IO in protected cwd")
	}
}
func TestBinaryResolutionSymlinkProtectedTarget(t *testing.T) {
	for _, target := range []string{"/fixture/protected/fixture", "../protected/fixture"} {
		f := &traceBinaryFS{cwd: "/fixture/cwd", links: map[string]string{"/fixture/allowed/fixture": target}}
		_, err := ResolveBinary(Request{ForbiddenRoots: []string{"/fixture/protected"}, Env: []string{"PATH=/fixture/allowed:/fixture/protected"}}, BinarySpec{Name: "fixture"}, f)
		requireReason(t, err, "protected_root")
		assertNoProtectedTouches(t, f, "/fixture/protected")
	}
}
func TestBinaryResolutionDirectoryAndPackageLinks(t *testing.T) {
	for _, tc := range []struct {
		name, link, target string
		spec               BinarySpec
	}{
		{"directory", "/fixture/allowed", "/fixture/protected", BinarySpec{Name: "fixture"}},
		{"managed", "/fixture/package/node_modules", "/fixture/protected", BinarySpec{Name: "codex", ManagedRoot: "/fixture/package", PlatformPackage: "platform", TargetTriple: "triple"}},
		{"derived", "/fixture/package/node_modules", "/fixture/protected", BinarySpec{Name: "codex", PlatformPackage: "platform", TargetTriple: "triple"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &traceBinaryFS{cwd: "/fixture/cwd", links: map[string]string{tc.link: tc.target}, executables: map[string]bool{"/fixture/package/bin/codex.js": true}}
			if tc.name == "derived" {
				f.links["/fixture/allowed/codex"] = "/fixture/package/bin/codex.js"
			}
			_, err := ResolveBinary(Request{ForbiddenRoots: []string{"/fixture/protected"}, Env: []string{"PATH=/fixture/allowed"}}, tc.spec, f)
			requireReason(t, err, "protected_root")
			assertNoProtectedTouches(t, f, "/fixture/protected")
		})
	}
}
func TestBinaryResolutionBoundsSymlinkHops(t *testing.T) {
	f := &traceBinaryFS{cwd: "/fixture", links: map[string]string{"/fixture/allowed/fixture": "fixture"}}
	_, err := ResolveBinary(Request{Env: []string{"PATH=/fixture/allowed"}}, BinarySpec{Name: "fixture"}, f)
	requireReason(t, err, "symlink_limit")
	if len(f.touched) > 250 {
		t.Fatal("unbounded symlink traversal")
	}
}

func TestBinaryResolutionProtectedRootAncestorAlias(t *testing.T) {
	f := &traceBinaryFS{cwd: "/fixture/protected", links: map[string]string{"/alias": "/fixture"}}
	_, err := ResolveBinary(Request{ForbiddenRoots: []string{"/alias/protected"}, Env: []string{"PATH=:/fixture/allowed"}}, BinarySpec{Name: "fixture"}, f)
	requireReason(t, err, "protected_root")
	assertNoProtectedTouches(t, f, "/alias/protected", "/fixture/protected")
}

// The first case is the reviewer's exact alias/nested-root trace. The other
// orderings move the alias to a selected-home parent and reverse the lexical
// lengths. All roots are synthetic; no harness directory is created.
func TestRound3ProtectedRootAliasFixedPoint(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   []string
		links map[string]string
		roots []string
	}{
		{"review-exact", []string{"HOME=/long-fixture-home-alias", "MUSE_HOME=/f/.codex/nested", "PATH=/fixture/bin"}, map[string]string{"/long-fixture-home-alias": "/f"}, []string{"/f/.codex", "/f/.claude", "/f/.gemini", "/f/.config/muse"}},
		{"selected-parent-first", []string{"HOME=/fixture/home", "CODEX_HOME=/long-fixture-parent-alias/private", "MUSE_HOME=/f/private/nested", "PATH=/fixture/bin"}, map[string]string{"/long-fixture-parent-alias": "/f"}, []string{"/f/private"}},
		{"short-alias-long-target", []string{"HOME=/a", "CLAUDE_CONFIG_DIR=/long-fixture-home/.gemini/nested", "MUSE_HOME=/a/.gemini/nested/deeper", "PATH=/fixture/bin"}, map[string]string{"/a": "/long-fixture-home"}, []string{"/long-fixture-home/.codex", "/long-fixture-home/.claude", "/long-fixture-home/.gemini", "/long-fixture-home/.config/muse"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &traceBinaryFS{cwd: "/fixture/cwd", links: tc.links, executables: map[string]bool{"/fixture/bin/fixture": true}}
			binary, err := ResolveBinary(Request{Env: tc.env}, BinarySpec{Name: "fixture"}, f)
			assertNoProtectedTouches(t, f, tc.roots...)
			if err != nil || binary != "/fixture/bin/fixture" {
				t.Fatalf("safe candidate was not resolved: %q, %v", binary, err)
			}
			// A nested selected parent can be skipped by inclusion, but its
			// canonical location must still reject a search candidate.
			f.touched = nil
			env := append([]string{}, tc.env...)
			for i, entry := range env {
				if strings.HasPrefix(entry, "PATH=") {
					env[i] = "PATH=" + tc.roots[0]
				}
			}
			_, err = ResolveBinary(Request{Env: env}, BinarySpec{Name: "fixture"}, f)
			requireReason(t, err, "protected_root")
			assertNoProtectedTouches(t, f, tc.roots...)
		})
	}
}

func TestRound3UnprovedRootAncestryRefuses(t *testing.T) {
	// An unrelated parent sharing a protected basename is insufficient alias
	// evidence. Keep its lexical root and reject possibly covered candidates.
	f := &traceBinaryFS{cwd: "/fixture/cwd", executables: map[string]bool{"/elsewhere/leaf/fixture": true}}
	_, err := ResolveBinary(Request{ForbiddenRoots: []string{"/a/private", "/b/private/leaf"}, Env: []string{"PATH=/elsewhere/leaf"}}, BinarySpec{Name: "fixture"}, f)
	requireReason(t, err, "protected_root")
	assertNoProtectedTouches(t, f, "/a/private", "/b/private")
}
