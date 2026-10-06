//go:build mutants

package timezones

import (
	"bytes"
	"os"
	"time"
)

// Semantic mutants. Each one swaps a package-level seam for a weakened
// behaviour; nothing here names a production source line, so a
// behaviour-neutral rename cannot break a mutant. The mutant to apply comes
// from TZ_MUTANT, set by the driver in mutants_test.go. This file is compiled
// only with -tags mutants.
var mutantHooks = map[string]func(){
	"decoded bytes altered by one byte": func() {
		decodeBundle = func(text string) ([]byte, error) {
			data, err := decodeBundledText(text)
			if err == nil {
				data[len(data)/2] ^= 1
			}
			return data, err
		}
	},
	"loader falls back to time.LoadLocation for an unknown zone": func() {
		unknownZone = func(name string) (*time.Location, error) {
			return time.LoadLocation(name)
		}
	},
	"loader consults time.LoadLocation before the bundle": func() {
		zoneSource = func(name string) (*time.Location, error) {
			if loc, err := time.LoadLocation(name); err == nil {
				return loc, nil
			}
			return bundledZoneSource(name)
		}
	},
	"bundle decoded on every call instead of once": func() {
		bundleSource = func() ([]byte, error) { return decodeBundle(zonesB64) }
	},
	"NUL guard scans only the first 4096 bytes of a file": func() {
		nulIndex = func(data []byte) int {
			return bytes.IndexByte(data[:min(len(data), 4096)], 0)
		}
	},
}

func init() {
	name := os.Getenv(mutantEnv)
	if name == "" {
		return
	}
	apply, ok := mutantHooks[name]
	if !ok {
		panic("unknown mutant " + name)
	}
	apply()
}
