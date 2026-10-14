package muse

import (
	"io"
	"os"
	"path/filepath"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// curatorRootContextPrefix is curator-spec environments §5.1's stable v2
// contract: curator c643b565, internal/contextmaterialize/contextmaterialize.go
// HeaderTypeLine at :27 and Header() at :153-154. A v3 needs a coordinated change.
const curatorRootContextPrefix = "<!--\ncurator-root-context-v2\n"

// The supplied Muse 1.4.7-R6101.1 probes verified both launch forms. Earlier
// 1.4.1/1.4.2 permission/network evidence says nothing about these options.
const foreignPersonalContextBuild = "1.4.7-R6101.1"
const foreignPersonalContextRelease = "1.4.7"

func suppressForeignPersonalContext(req agentic.LaunchRequest) bool {
	if req.ToolRelease != foreignPersonalContextRelease {
		return false
	}
	if req.FrozenToolBuild != "" && req.FrozenToolBuild != foreignPersonalContextBuild {
		return false
	}
	// Managed network launches replace CFG with a private config root; the
	// request's root context would not be the file Muse reads there.
	if !req.Network.IsZero() {
		return false
	}
	return hasCuratorRootContext(childEnv(req.Env, req))
}

// hasCuratorRootContext reads only the launch environment's effective CFG.
// No HOME fallback or ambient environment can grant this capability. Reading
// prefix+1 bytes bounds I/O and proves the file contains more than the header.
func hasCuratorRootContext(env []string) bool {
	cfg := envValue(env, "XDG_CONFIG_HOME")
	if cfg == "" || !filepath.IsAbs(cfg) {
		return false
	}
	path := filepath.Join(cfg, "muse", "AGENTS.md")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= int64(len(curatorRootContextPrefix)) {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return false
	}
	var prefix [len(curatorRootContextPrefix) + 1]byte
	_, err = io.ReadFull(file, prefix[:])
	return err == nil && string(prefix[:len(curatorRootContextPrefix)]) == curatorRootContextPrefix
}
