package pi

import (
	"fmt"
	"strings"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// THIS FILE IS THE ONE PLACE PI'S OWN ARGV PREFIX IS SPELLED.
//
// # What is pinned, and what is deliberately not
//
// The architecture decision names the exact shape of `agents-infra pi`'s
// invocation: `["agents-infra", "pi", "--profile", <profile>, "--", <turn
// args>]`. Everything up to and including the bare `--` is pinned here,
// unconditionally, for both launch modes — there is nothing mode-dependent
// about it, unlike every other ported system in this module, because
// nothing about WHICH profile a wrapper attaches to differs between a real
// launch and its dry-run mirror.
//
// `<turn args>` — what actually follows `--` for the real `pi` binary to
// read — is NOT built here. It depends on a pinned `earendil-works/pi`
// binary/docs fixture the architecture decision names as an open item and
// does not supply, and the parity gate that would pin it (adversarial plan
// case 17) explicitly cannot exist before that fixture does. Building a
// plausible-looking turn-argument grammar here without that pin would be
// exactly the forced fit this module's own standing orders refuse: a
// capability claim ("this is pi's real turn-argument contract") with no
// evidence behind it, and a later correction would silently rewrite this
// launch's observable surface. The assignment therefore travels on stdin
// instead, using this module's existing, precedented PromptPath/Prompt
// fallback (stdin.go) — a real transport this module already proves
// elsewhere, not a guess at pi's own wire protocol.
func Args(req agentic.LaunchRequest) ([]string, error) {
	profile := strings.TrimSpace(req.Profile)
	if profile == "" {
		return nil, fmt.Errorf("pi: launch request carries no profile; the local-models vendor's Spawn must resolve one from its own pointer before this system can build argv")
	}
	return []string{"pi", "--profile", profile, "--"}, nil
}
