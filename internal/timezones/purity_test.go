package timezones

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// bundledZoneBytes returns the raw TZif bytes of a bundled zone.
func bundledZoneBytes(t *testing.T, name string) []byte {
	t.Helper()
	zones, err := bundledZip()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(zones), int64(len(zones)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		data, err := io.ReadAll(stream)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatalf("zone %s not bundled", name)
	return nil
}

func tbilisiOffset(t *testing.T) int {
	t.Helper()
	zone, err := Load("Asia/Tbilisi")
	if err != nil {
		t.Fatal(err)
	}
	_, offset := time.Date(2026, 10, 1, 12, 0, 0, 0, zone).Zone()
	return offset
}

func TestLoadIgnoresABogusZoneinfoPath(t *testing.T) {
	t.Setenv("ZONEINFO", filepath.Join(t.TempDir(), "does", "not", "exist.zip"))
	if got := tbilisiOffset(t); got != 4*3600 {
		t.Fatalf("Asia/Tbilisi offset = %d with bogus ZONEINFO", got)
	}
	if _, err := Load("Missing/Zone"); err == nil {
		t.Fatal("unknown zone accepted with bogus ZONEINFO")
	}
}

// A decoy ZONEINFO directory that time.LoadLocation would honor must change
// nothing: bundled names keep their bundled data.
func TestLoadIgnoresDecoyZoneinfoForBundledZone(t *testing.T) {
	root := t.TempDir()
	decoy := filepath.Join(root, "Asia", "Tbilisi")
	if err := os.MkdirAll(filepath.Dir(decoy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decoy, bundledZoneBytes(t, "Asia/Tokyo"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZONEINFO", root)
	if got := tbilisiOffset(t); got != 4*3600 {
		t.Fatalf("Asia/Tbilisi offset = %d, decoy ZONEINFO was consulted", got)
	}
}

// A zone that exists only in ZONEINFO must stay unknown to the loader.
func TestLoadNeverFallsBackToSystemZones(t *testing.T) {
	root := t.TempDir()
	only := filepath.Join(root, "Decoy", "Only")
	if err := os.MkdirAll(filepath.Dir(only), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(only, bundledZoneBytes(t, "Asia/Tokyo"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZONEINFO", root)
	if _, err := time.LoadLocation("Decoy/Only"); err != nil {
		t.Fatalf("decoy is not reachable through time.LoadLocation, the test proves nothing: %v", err)
	}
	if _, err := Load("Decoy/Only"); err == nil {
		t.Fatal("zone outside the bundle was resolved through ZONEINFO")
	}
}
