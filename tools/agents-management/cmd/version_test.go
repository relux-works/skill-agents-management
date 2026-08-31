package cmd

import "testing"

func withVersionMetadata(t *testing.T, version, commit, buildDate string) {
	t.Helper()
	pv, pc, pd := Version, Commit, BuildDate
	Version, Commit, BuildDate = version, commit, buildDate
	t.Cleanup(func() { Version, Commit, BuildDate = pv, pc, pd })
}

func TestVersionCommandReportsInjectedMetadata(t *testing.T) {
	withVersionMetadata(t, "1.2.3", "abc1234", "2026-01-02T03:04:05Z")

	stdout, stderr, err := runRoot(t, "version")
	if err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	want := "agents-management version 1.2.3 (commit abc1234, built 2026-01-02T03:04:05Z)\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty: version metadata belongs on stdout", stderr)
	}
}

// The build date is absent from a plain `go build`; an empty one must be
// omitted rather than printed as "built ".
func TestFormatVersionOmitsAbsentMetadata(t *testing.T) {
	cases := []struct {
		name                      string
		version, commit, buildDte string
		want                      string
	}{
		{"no commit, no date", "dev", "", "", "agents-management version dev"},
		{"commit without date", "dev", "abc1234", "", "agents-management version dev (commit abc1234)"},
		{"date without commit", "dev", "", "2026-01-02T03:04:05Z", "agents-management version dev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withVersionMetadata(t, tc.version, tc.commit, tc.buildDte)
			if got := formatVersion(); got != tc.want {
				t.Errorf("formatVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}
