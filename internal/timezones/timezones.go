// Package timezones loads the bundled IANA zone data as a pure operation.
// Quota prose parsers must not consult ambient ZONEINFO or open user files.
package timezones

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"errors"
	"io"
	"time"
)

//go:embed data/zoneinfo.zip
var zones []byte

func Load(name string) (*time.Location, error) {
	if name == "UTC" {
		return time.UTC, nil
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
	return nil, errors.New("timezones: unknown zone")
}
