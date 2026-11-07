package gatewayadmission

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// resolveHome uses directory entries rather than case or Unicode normalization:
// only the filesystem can establish which spellings denote the same directory.
func resolveHome(home string) (string, os.FileInfo, error) {
	if !filepath.IsAbs(home) || strings.ContainsAny(home, "\x00\r\n") {
		return "", nil, agentic.ErrCodexGatewayRefused
	}
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", nil, agentic.ErrCodexGatewayRefused
	}
	volume := filepath.VolumeName(resolved)
	canonical := volume + string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(resolved, canonical), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		target, err := os.Stat(filepath.Join(canonical, component))
		if err != nil || !target.IsDir() {
			return "", nil, agentic.ErrCodexGatewayRefused
		}
		entries, err := os.ReadDir(canonical)
		if err != nil {
			return "", nil, agentic.ErrCodexGatewayRefused
		}
		name, found := homeComponent(entries, target)
		if found {
			canonical = filepath.Join(canonical, name)
		}
		if !found {
			return "", nil, agentic.ErrCodexGatewayRefused
		}
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", nil, agentic.ErrCodexGatewayRefused
	}
	return canonical, info, nil
}

// Match by identity, never by guessed case or Unicode normalization.
func homeComponent(entries []os.DirEntry, target os.FileInfo) (string, bool) {
	for _, entry := range entries {
		// EvalSymlinks already resolved links; do not select a different alias.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			// An unrelated sibling can disappear after ReadDir; only a
			// successfully identified target can supply the component.
			continue
		}
		if os.SameFile(target, info) {
			return entry.Name(), true
		}
	}
	return "", false
}
