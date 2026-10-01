`zoneinfo.zip` is the public-domain IANA timezone corpus bundled with the
test toolchain go1.26.0 (`lib/time/zoneinfo.zip`). It is embedded so pure
quota parsers do not read ambient `ZONEINFO`, host timezone files, or a home.
Update it deliberately with a toolchain/timezone-data change. It is standard
timezone data, with no harness, account, machine, or credential evidence.
