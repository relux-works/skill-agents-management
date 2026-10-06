package timezones

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// zoneinfoZipSHA256 is the sha256 of the original binary data/zoneinfo.zip
// (go1.26.0 lib/time/zoneinfo.zip) that the base64 text replaced.
const zoneinfoZipSHA256 = "8f55634d05f8bca1f7bc7c69c5933428c69357e0bdf565e5ba224e3f88ff12e8"

func TestBundledZipDecodesToTheOriginalBytes(t *testing.T) {
	data, err := bundledZip()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != zoneinfoZipSHA256 {
		t.Fatalf("decoded zoneinfo sha256 = %s, want %s", got, zoneinfoZipSHA256)
	}
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		t.Fatal("decoded zoneinfo is not a zip")
	}
}

func TestBundledZipIsDecodedOnce(t *testing.T) {
	first, err := bundledZip()
	if err != nil {
		t.Fatal(err)
	}
	second, err := bundledZip()
	if err != nil {
		t.Fatal(err)
	}
	if &first[0] != &second[0] {
		t.Fatal("bundled zip was decoded more than once")
	}
}

// nulIndex is the NUL scan the guard uses; the mutants build swaps it.
var nulIndex = indexNUL

func indexNUL(data []byte) int { return bytes.IndexByte(data, 0) }

// findNULFiles returns every regular file under root containing a NUL byte.
// A walk or read failure is returned as an error, never as "no NUL found".
// Walking the directory covers a superset of the tracked files.
func findNULFiles(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if nulIndex(data) >= 0 {
			found = append(found, path)
		}
		return nil
	})
	return found, err
}

func TestNoNULBytesUnderTimezones(t *testing.T) {
	found, err := findNULFiles(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("files with NUL bytes: %v", found)
	}
}

func TestNULGuardFindsNULAnywhereInAFile(t *testing.T) {
	root := t.TempDir()
	clean := bytes.Repeat([]byte("a"), 1<<16)
	late := append(bytes.Repeat([]byte("a"), 1<<15), 0)
	late = append(late, clean...)
	files := map[string][]byte{
		"clean.txt":      clean,
		"sub/late.bin":   late,
		"sub/first.bin":  {0, 'x'},
		"sub/also-clean": []byte("x"),
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	found, err := findNULFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "sub", "first.bin"), filepath.Join(root, "sub", "late.bin")}
	if len(found) != len(want) || found[0] != want[0] || found[1] != want[1] {
		t.Fatalf("found %v, want %v", found, want)
	}
}

func TestNULGuardReportsAReadFailureNotAbsence(t *testing.T) {
	if _, err := findNULFiles(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("unreadable root reported as no NUL files")
	}
}
