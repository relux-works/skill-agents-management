package pi

import "github.com/relux-works/skill-agents-management/pkg/agentic"

// verifiedReleases is this environment's rows of the versioned
// provider-capability table (curator-spec Decision 0018 choice 6).
//
// There is exactly one row: pi 0.84.2, the release this plugin's
// `--model` grammar was checked against, carrying YoloSupported false —
// that release documents no permission-bypass flag (`--approve` trusts
// project-local files for the run; Decision 0018 records it as not
// equivalent), so yolo at the verified release is refused as
// unsupported. Any other release — and an empty one — fails closed
// earlier, as drift.
//
// This plugin has no release probe to fill ToolRelease with: its binary
// is the agents-infra wrapper, and the wrapper's version is not pi's
// release. The caller passes the release when it established it another
// way, and yolo without one is refused as unverified. Native never
// reads the table either way.
var verifiedReleases = []agentic.ReleaseCapability{
	{Release: "0.84.2", Grammar: agentic.PermissionGrammarV1, YoloSupported: false},
}
