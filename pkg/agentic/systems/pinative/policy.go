package pinative

import "github.com/relux-works/skill-agents-management/pkg/agentic"

// verifiedReleases is this environment's rows of the versioned
// provider-capability table (curator-spec Decision 0018 choice 6).
//
// There is exactly one row: pi 0.84.2, carrying YoloSupported false —
// that release documents no permission-bypass flag, so yolo at the
// verified release is refused as unsupported, and any other release (or
// an empty one) fails closed earlier, as drift. The release string
// matches pi's row because both environments run the same tool
// release; the rows are still per-environment facts (the table keys
// environment AND release), which is why each plugin holds its own.
var verifiedReleases = []agentic.ReleaseCapability{
	{Release: "0.84.2", Grammar: agentic.PermissionGrammarV1, YoloSupported: false},
}
