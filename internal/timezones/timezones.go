// Package timezones loads the bundled IANA zone data as a pure operation.
// Quota prose parsers must not consult ambient ZONEINFO or open user files.
package timezones

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/base64"
	"errors"
	"io"
	"sync"
	"time"
)

// zonesB64 is the base64 text form of the bundled zoneinfo.zip. The text is
// NUL-free so the module carries no binary blob; it decodes to exactly the
// original zip bytes.
//
//go:embed data/zoneinfo.zip.b64
var zonesB64 string

// The package-level seams below default to the real implementation and are
// never assigned in production code. Tests built with -tags mutants swap them
// (mutants_hooks_test.go) to inject behaviour instead of rewriting source.
var (
	decodeBundle = decodeBundledText
	bundleSource = cachedBundle
	unknownZone  = errUnknownZone
	zoneSource   = bundledZoneSource
)

// decodeBundledText decodes zonesB64; the decoder skips the line breaks the
// encoded file is wrapped with.
func decodeBundledText(text string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(text)
}

// decodedBundle decodes zonesB64 once, on first use.
var decodedBundle = sync.OnceValues(func() ([]byte, error) {
	return decodeBundle(zonesB64)
})

func cachedBundle() ([]byte, error) { return decodedBundle() }

func bundledZip() ([]byte, error) { return bundleSource() }

func errUnknownZone(string) (*time.Location, error) {
	return nil, errors.New("timezones: unknown zone")
}

func Load(name string) (*time.Location, error) {
	if name == "UTC" {
		return time.UTC, nil
	}
	return zoneSource(name)
}

func bundledZoneSource(name string) (*time.Location, error) {
	zones, err := bundledZip()
	if err != nil {
		return nil, err
	}
	reader, err := zip.NewReader(bytes.NewReader(zones), int64(len(zones)))
	if err != nil {
		return nil, err
	}
	for _, file := range reader.File {
		if file.Name == name {
			stream, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer stream.Close()
			data, err := io.ReadAll(stream)
			if err != nil {
				return nil, err
			}
			return time.LoadLocationFromTZData(name, data)
		}
	}
	return unknownZone(name)
}
