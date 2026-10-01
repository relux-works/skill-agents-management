package timezones

import (
	"testing"
	"time"
)

func TestZonesAreIndependentOfHomeAndZoneinfo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ZONEINFO", t.TempDir())
	zone, err := Load("Asia/Tbilisi")
	if err != nil {
		t.Fatal(err)
	}
	_, offset := time.Date(2026, 10, 1, 12, 0, 0, 0, zone).Zone()
	if offset != 4*3600 {
		t.Fatal("bundled zone offset drift")
	}
	if _, err = Load("../unopened"); err == nil {
		t.Fatal("path-shaped zone accepted")
	}
	if _, err = Load("Missing/Zone"); err == nil {
		t.Fatal("unknown zone accepted")
	}
}
