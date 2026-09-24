package agentic

import "fmt"

// PermissionMapping is the provider mapping for one verified system release
// and interactive permission mode. An empty Flag means the module contributes
// no native argument, as for PermissionModeNative.
type PermissionMapping struct {
	Flag    string
	Grammar PermissionGrammarVersion
}

// PermissionMappingCapability is an optional system capability for resolving
// the module-owned native argument used by an interactive permission mode.
// Implementations must use the same release rows as their permission-policy
// grammar and return no mapping for an unverified release.
type PermissionMappingCapability interface {
	PermissionMapping(toolRelease string, mode PermissionMode) (PermissionMapping, error)
}

// PermissionMapping resolves the registered system's module-owned permission
// mapping without constructing or admitting a launch plan. The plugin verifies
// the exact tool release and supplies its permission grammar alongside the
// mapping. Unknown systems return ErrUnknownSystem; systems without this
// capability return ErrPermissionModeUnsupported; unverified releases and
// unknown modes return their existing typed errors.
func (r *Registry) PermissionMapping(systemID SystemID, toolRelease string, mode PermissionMode) (PermissionMapping, error) {
	system, ok := r.Lookup(systemID)
	if !ok {
		return PermissionMapping{}, fmt.Errorf("%w: %s", ErrUnknownSystem, systemID)
	}
	mapper, ok := system.(PermissionMappingCapability)
	if !ok {
		return PermissionMapping{}, fmt.Errorf("%w: %s has no release-pinned permission mapping", ErrPermissionModeUnsupported, system.ID())
	}
	mapping, err := mapper.PermissionMapping(toolRelease, mode)
	if err != nil {
		return PermissionMapping{}, err
	}
	return mapping, nil
}
