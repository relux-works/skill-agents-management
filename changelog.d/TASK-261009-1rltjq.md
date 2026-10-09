- Retire the Muse verified-release list: the plugin admits any well-formed
  build the binary attests, parsed through the one `buildid` grammar
  (`ParseBuildID`, `ParsePinnedBinaryName`, `ParseBuildIdentity`,
  `CompareBuildIDs`), and seals the attested full build with its byte
  digest, exact argv, posture and environment identity.

- Resolve Muse yolo from bounded `--help` evidence: the flag maps only when
  the selected binary's help declares it as its own option
  (`ErrPermissionModeUnsupported` otherwise), the seal binds the help
  digests and re-verifies them before every exec, and the public
  no-evidence query reports `ErrMuseHelpEvidenceRequired` for yolo.

- Carry a host-frozen Muse copy through `LaunchRequest.FrozenToolBinary`,
  `FrozenToolBuild` and `FrozenToolSHA256`: a present tuple selects the
  supplied copy for resolution, probing and sealing without PATH fallback,
  and any defect refuses with `ErrMuseFrozenToolInvalid`.

- Bound every Muse version and help probe during the read (64 KiB version,
  1 MiB help), with process-group teardown, bounded drain past child exit
  and typed `ProbeExecutionError` attempt records for host routing.

- Echo the attested `ToolRelease` and `PermissionMode` on `Plan`, and refuse
  a caller-supplied release that disagrees with the attested release with
  `ErrMuseToolReleaseMismatch`.
