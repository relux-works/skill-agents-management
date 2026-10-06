`zoneinfo.zip.b64` is the base64 text form (wrapped at 76 columns) of the
public-domain IANA timezone corpus bundled with the test toolchain go1.26.0
(`lib/time/zoneinfo.zip`). It is text so the module carries no NUL-bearing
file; the loader decodes it once into exactly the original zip bytes (sha256
`8f55634d05f8bca1f7bc7c69c5933428c69357e0bdf565e5ba224e3f88ff12e8`, pinned in
`encoding_test.go`). It is embedded so pure quota parsers do not read ambient
`ZONEINFO`, host timezone files, or a home; that is why the loader does not
import `time/tzdata`, whose `time.LoadLocation` path consults them first.
Update it deliberately with a toolchain/timezone-data change:
`base64 -b 76 -i zoneinfo.zip > zoneinfo.zip.b64`, then update the pinned
digest. It is standard timezone data, with no harness, account, machine, or
credential evidence.
