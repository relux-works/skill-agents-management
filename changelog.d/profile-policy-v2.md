- Add profile policy value contracts v2 under `observed-process/v3`.
  `ValidateReadings` takes an optional contract version; v3 selects
  `stress-policy/v2` and `restart-policy/v2` for the profile stress and
  restart-supervision facts while every other fact keeps its contract. Both
  policies carry the engine catalog's keys and ranges with exact closed
  decoding, including zero restart delay and an explicit
  `{"configured":false}` form. `observed-process/v2` and the v1 value
  contracts are unchanged; three-argument callers keep v2.
