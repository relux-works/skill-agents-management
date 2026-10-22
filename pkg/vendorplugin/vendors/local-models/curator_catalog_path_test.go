package localmodels

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// LoadAt exports the resolved file it parsed across later directory/leaf link
// rotation and HOME changes. Direct and HOME-relative paths remain valid.
// This pins the path target, not immutable file contents or inode replacement.
func TestLoadAtCatalogPathSurvivesSymlinkRetarget(t *testing.T) {
	for _, mode := range []string{"stable-control", "home-relative-control", "directory-link", "file-link"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			originalHome, decoyHome := filepath.Join(root, "original home"), filepath.Join(root, "decoy home")
			original, decoy := filepath.Join(originalHome, ".curator"), filepath.Join(decoyHome, ".curator")
			const originalID = "gguf-sha256:1111111111111111111111111111111111111111111111111111111111111111"
			const decoyID = "gguf-sha256:2222222222222222222222222222222222222222222222222222222222222222"
			originalBody := curatorEnginesMLX + "weights = { id = " + quoteForTOML(originalID) + " }\n"
			decoyBody := curatorEnginesMLX + "weights = { id = " + quoteForTOML(decoyID) + " }\n"
			for _, row := range []struct{ dir, body string }{{original, originalBody}, {decoy, decoyBody}} {
				if err := os.MkdirAll(row.dir, 0o700); err != nil {
					t.Fatal(err)
				}
				writeCuratorFile(t, row.dir, runtimesFileName, curatorPiRuntimes(root))
				writeCuratorFile(t, row.dir, enginesFileName, row.body)
			}
			t.Setenv("HOME", originalHome)
			loadDir, link, retarget := original, "", ""
			switch mode {
			case "home-relative-control":
				home, err := os.UserHomeDir()
				if err != nil {
					t.Fatal(err)
				}
				t.Chdir(home)
				loadDir = ".curator"
			case "directory-link":
				link, retarget = filepath.Join(root, "selected catalog"), decoy
				loadDir = link
				if err := os.Symlink(original, link); err != nil {
					t.Fatal(err)
				}
			case "file-link":
				loadDir = filepath.Join(root, "selected catalog")
				if err := os.Mkdir(loadDir, 0o700); err != nil {
					t.Fatal(err)
				}
				writeCuratorFile(t, loadDir, runtimesFileName, curatorPiRuntimes(root))
				link, retarget = filepath.Join(loadDir, enginesFileName), filepath.Join(decoy, enginesFileName)
				if err := os.Symlink(filepath.Join(original, enginesFileName), link); err != nil {
					t.Fatal(err)
				}
			}
			legacy := filepath.Join(root, "absent-legacy.toml")
			cfg, warning, err := LoadAt(loadDir, legacy)
			if err != nil || warning != nil {
				t.Fatalf("LoadAt: err = %v, warning = %v", err, warning)
			}
			result := resolveWithPaths(loadDir, legacy)
			if result.Err != nil || result.Absent || result.Warning != nil || !reflect.DeepEqual(result.Config, cfg) {
				t.Fatalf("ConfigResult changed: %+v", result)
			}
			if got := cfg.Runtimes[0].Models["qwen-3.8-27b-mlx-8bit"].WeightsID; got != originalID {
				t.Fatalf("parsed weights = %q, want original catalog", got)
			}
			if link != "" {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(retarget, link); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("HOME", decoyHome)
			want, err := filepath.EvalSymlinks(filepath.Join(original, enginesFileName))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.EnginesConfig != want {
				t.Errorf("catalog path drift: got %q, want resolved parsed file %q", cfg.EnginesConfig, want)
			}
			data, err := os.ReadFile(cfg.EnginesConfig)
			if err != nil || string(data) != originalBody {
				t.Errorf("catalog content drift: exported path no longer reads parsed catalog; err = %v", err)
			}
		})
	}
}

// Existing but unusable catalog links must refuse read_failed without legacy
// fallback, partial path exports, warnings, or sensitive path disclosure.
func TestLoadAtCatalogLinkFailuresStayTyped(t *testing.T) {
	for _, mode := range []string{"dangling", "loop", "directory"} {
		t.Run(mode, func(t *testing.T) {
			dir, project := t.TempDir(), t.TempDir()
			writeCuratorFile(t, dir, runtimesFileName, curatorPiRuntimes(project))
			catalog := filepath.Join(dir, enginesFileName)
			target := filepath.Join(dir, "missing-private-target")
			if mode == "loop" {
				target = catalog
			} else if mode == "directory" {
				target = t.TempDir()
			}
			if err := os.Symlink(target, catalog); err != nil {
				t.Fatal(err)
			}
			legacy := filepath.Join(t.TempDir(), legacyFileName)
			if err := os.WriteFile(legacy, []byte(legacyPiTOML(project)), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, warning, err := LoadAt(dir, legacy)
			refusal := requireRefusal(t, err, ErrReadFailed, RefusalReadFailed)
			if refusal.File != enginesFileName || warning != nil || !reflect.DeepEqual(cfg, Config{}) {
				t.Fatalf("failed catalog returned partial metadata: cfg=%+v, warning=%v, err=%v", cfg, warning, err)
			}
			requireSanitized(t, err, dir, target)
			result := resolveWithPaths(dir, legacy)
			if result.Absent || result.Err == nil || !reflect.DeepEqual(result.Config, Config{}) || result.Warning != nil {
				t.Fatalf("read failure changed ConfigResult semantics: %+v", result)
			}
			if strings.Contains(result.Err.Error(), target) {
				t.Fatal("ConfigResult leaked catalog target")
			}
		})
	}
}
