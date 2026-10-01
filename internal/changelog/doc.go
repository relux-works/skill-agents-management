// Package changelog holds the behavioral tests for
// .scripts/changelog-release.sh, the release-time changelog fragment
// aggregator. The tests drive the script itself (the production entry point)
// inside temporary git repositories: --check mode over valid and invalid
// fragments, release mode over ordering, carry-over, removal and every
// refusal, with a byte-exact golden for the produced CHANGELOG.md.
package changelog
