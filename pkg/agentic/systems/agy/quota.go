package agy

import (
	"encoding/json"
	"fmt"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
	"path/filepath"
	"time"
)

var _ providerquota.Reader = (*System)(nil)

const quotaSource = "agy-print-usage"

// MinimumQuotaVersion is the earliest captured headless usage contract, not
// an inferred introduction version. Consumers probe these flags offline.
const MinimumQuotaVersion = "1.1.27"

func RequiredQuotaFlags() []string {
	return []string{"--output-format", "--mode", "--print-timeout", "--print="}
}
func (s *System) QuotaPlan(req providerquota.Request) (providerquota.QuotaPlan, error) {
	if s.runtime.IsZero() {
		return providerquota.QuotaPlan{}, providerquota.Refuse("runtime_not_preflighted")
	}
	if !quotaVersionSupported(s.runtime.Version) {
		return providerquota.QuotaPlan{}, providerquota.Refuse("version_unsupported")
	}
	if !filepath.IsAbs(trimmedExecutable(s.runtime)) {
		return providerquota.QuotaPlan{}, providerquota.Refuse("binary_evidence_invalid")
	}
	binary, err := providerquota.ResolveBinary(req, providerquota.BinarySpec{Name: trimmedExecutable(s.runtime)}, providerquota.NativeBinaryFS{})
	if err != nil {
		return providerquota.QuotaPlan{}, err
	}
	return providerquota.QuotaPlan{Binary: binary, Argv: []string{"--output-format", "json", "--mode", "plan", "--print-timeout", "2m", "--print=/usage"}, Env: append([]string{}, req.Env...), Timeout: 2 * time.Minute, CwdPolicy: providerquota.ScratchCwd, Ready: true, SideEffectFlags: []string{"--mode plan"}}, nil
}
func (s *System) ParseQuota(stdout []byte, c providerquota.ParseContext) (providerquota.QuotaRecord, error) {
	fail := func(reason string) (providerquota.QuotaRecord, error) {
		return providerquota.Failure(c, quotaSource, reason)
	}
	var e struct {
		Status  string `json:"status"`
		Turns   *int   `json:"num_turns"`
		Command *struct {
			Name string `json:"name"`
			Data struct {
				Groups []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					Buckets     []struct {
						ID        string   `json:"id"`
						Name      string   `json:"name"`
						Window    string   `json:"window"`
						Remaining *float64 `json:"remaining_fraction"`
						Reset     *string  `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if json.Unmarshal(stdout, &e) != nil {
		return fail("payload_invalid")
	}
	if e.Turns == nil {
		return fail("envelope_missing")
	}
	if *e.Turns != 0 {
		return fail("model_turn_spent")
	}
	if e.Status != "SUCCESS" || e.Command == nil || e.Command.Name != "usage" {
		return fail("command_invalid")
	}
	if e.Command.Data.Groups == nil {
		return fail("payload_missing")
	}
	if c.HarnessVersion == "" {
		c.HarnessVersion = s.runtime.Version
	}
	r, err := providerquota.Base(c, quotaSource)
	if err != nil {
		return r, err
	}
	r.State = providerquota.Exact
	r.Confidence = "exact-fraction"
	r.Authenticated = providerquota.AuthYes
	r.Inventory.Installed = true
	r.Inventory.AuthSource = quotaSource
	r.Checked = []string{"print /usage"}
	for _, g := range e.Command.Data.Groups {
		for _, b := range g.Buckets {
			if b.Remaining == nil {
				return fail("fraction_missing")
			}
			reset, err := providerquota.ParseTimestamp(b.Reset)
			if err != nil {
				return fail("payload_invalid")
			}
			w := providerquota.QuotaWindow{ID: b.ID, Label: b.Name, Word: b.Window, Scope: g.Name, Description: g.Description, ResetsAt: reset}
			// The observed command schema does not state a measurement time. Preserve
			// absence instead of assuming that every successful command fetched live.
			switch b.Window {
			case "weekly":
				w.Minutes = 10080
			case "5h":
				w.Minutes = 300
			}
			if err = providerquota.Fraction(&w, *b.Remaining); err != nil {
				return fail(providerquota.Reason(err))
			}
			r.Windows = append(r.Windows, w)
		}
	}
	return providerquota.Finish(r, c)
}

func quotaVersionSupported(version string) bool {
	var major, minor, patch int
	n, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch)
	if err != nil || n != 3 || version != fmt.Sprintf("%d.%d.%d", major, minor, patch) {
		return false
	}
	return major > 1 || major == 1 && (minor > 1 || minor == 1 && patch >= 27)
}
