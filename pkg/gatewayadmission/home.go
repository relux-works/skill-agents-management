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
		found := false
		for _, entry := range entries {
			// EvalSymlinks already resolved links; do not select a different alias.
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return "", nil, agentic.ErrCodexGatewayRefused
			}
			if os.SameFile(target, info) {
				canonical = filepath.Join(canonical, entry.Name())
				found = true
				break
			}
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
