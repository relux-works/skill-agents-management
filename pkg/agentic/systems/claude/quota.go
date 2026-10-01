package claude

import (
	"encoding/json"
	"github.com/relux-works/skill-agents-management/internal/timezones"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var _ providerquota.Reader = (*System)(nil)

const quotaSource = "claude-print-usage"

func (*System) QuotaPlan(req providerquota.Request) (providerquota.QuotaPlan, error) {
	binary, err := providerquota.ResolveBinary(req, providerquota.BinarySpec{Name: executableName}, providerquota.NativeBinaryFS{})
	if err != nil {
		return providerquota.QuotaPlan{}, err
	}
	return providerquota.QuotaPlan{Binary: binary, Argv: []string{"-p", "--output-format", "json", "--no-session-persistence", "--setting-sources", "", "--strict-mcp-config", "/usage"}, Env: append([]string{}, req.Env...), CwdPolicy: providerquota.ScratchCwd, Timeout: 30 * time.Second, Ready: true, SideEffectFlags: []string{"--no-session-persistence", "--setting-sources", "--strict-mcp-config"}}, nil
}

var quotaLane = regexp.MustCompile(`^(Current session|Current week \([^)]+\)): ([^ ]+)% used [·-] resets (.+) \(([^)]+)\)$`)

const subscriptionLine = "You are currently using your subscription to power your Claude Code usage"

// The observed prose has no measurement timestamp. Even without a last-known
// note, cached bars cannot be excluded: never date them with retrieval time.
func (*System) ParseQuota(stdout []byte, c providerquota.ParseContext) (providerquota.QuotaRecord, error) {
	fail := func(reason string) (providerquota.QuotaRecord, error) {
		return providerquota.Failure(c, quotaSource, reason)
	}
	var e struct {
		Result  string          `json:"result"`
		Turns   *int            `json:"num_turns"`
		API     *float64        `json:"duration_api_ms"`
		IsError bool            `json:"is_error"`
		Report  json.RawMessage `json:"usage_report"`
	}
	if json.Unmarshal(stdout, &e) != nil {
		return fail("payload_invalid")
	}
	if e.Turns == nil || e.API == nil {
		return fail("envelope_missing")
	}
	if *e.Turns != 0 || *e.API != 0 {
		return fail("model_turn_spent")
	}
	if strings.HasPrefix(strings.TrimSpace(e.Result), "Unknown command:") {
		return fail("slash_unsupported")
	}
	if e.IsError {
		return fail("read_failure")
	}
	if !strings.HasPrefix(e.Result, subscriptionLine) {
		return fail("subscription_missing")
	}
	r, err := providerquota.Base(c, quotaSource)
	if err != nil {
		return r, err
	}
	r.State = providerquota.PercentOnly
	r.Confidence = "percent_only"
	r.Authenticated = providerquota.AuthYes
	r.Inventory.Installed = true
	r.Inventory.AuthSource = quotaSource
	r.Checked = []string{"print /usage"}
	// TODO(decision): confirm the timestamp member names and placement of the
	// structured twin in print mode; L1 §5 item 3. Until then timestamp fields
	// observed_at are fixture-tested evidence only, not a claim of a live probe.
	if len(e.Report) > 0 && string(e.Report) != "null" {
		var report struct {
			ObservedAt *string `json:"observed_at"`
			Limits     []struct {
				Kind       string          `json:"kind"`
				Group      string          `json:"group"`
				Percent    *float64        `json:"percent"`
				ResetsAt   *string         `json:"resets_at"`
				ObservedAt *string         `json:"observed_at"`
				Scope      json.RawMessage `json:"scope"`
			} `json:"limits"`
		}
		if json.Unmarshal(e.Report, &report) != nil {
			return fail("payload_invalid")
		}
		observed, err := providerquota.ParseTimestamp(report.ObservedAt)
		if err != nil {
			return fail("payload_invalid")
		}
		session, weekly := false, false
		for _, row := range report.Limits {
			if row.Kind == "" || row.Percent == nil {
				return fail("lane_missing")
			}
			reset, err := providerquota.ParseTimestamp(row.ResetsAt)
			if err != nil {
				return fail("payload_invalid")
			}
			measured, err := providerquota.ParseTimestamp(row.ObservedAt)
			if err != nil {
				return fail("payload_invalid")
			}
			w := providerquota.QuotaWindow{ID: row.Kind, Label: row.Group, Scope: row.Group, ResetsAt: reset, ObservedAt: measured}
			if w.ObservedAt == nil {
				w.ObservedAt = observed
			}
			unscoped := len(row.Scope) == 0 || string(row.Scope) == "null" || string(row.Scope) == `""`
			if !unscoped {
				var text string
				if json.Unmarshal(row.Scope, &text) == nil {
					w.Scope = text
				} else {
					var scope struct {
						Model *struct {
							ID          string `json:"id"`
							DisplayName string `json:"display_name"`
						} `json:"model"`
					}
					if json.Unmarshal(row.Scope, &scope) != nil || scope.Model == nil {
						return fail("scope_invalid")
					}
					w.Scope = scope.Model.ID
				}
			}
			switch row.Kind {
			case "five_hour":
				w.Minutes = 300
				session = session || unscoped
			case "seven_day":
				w.Minutes = 10080
				weekly = weekly || unscoped
			}
			if err := providerquota.Percent(&w, *row.Percent); err != nil {
				return fail(providerquota.Reason(err))
			}
			r.Windows = append(r.Windows, w)
		}
		if !session || !weekly {
			return fail("lane_missing")
		}
	} else {
		session, weekly := false, false
		for _, line := range strings.Split(e.Result, "\n") {
			if strings.HasPrefix(line, "What's contributing") {
				break
			}
			m := quotaLane.FindStringSubmatch(strings.TrimSpace(line))
			if m == nil {
				continue
			}
			used, err := strconv.ParseFloat(m[2], 64)
			if err != nil {
				return fail("out_of_range")
			}
			reset, err := quotaReset(m[3], m[4], c.ReadAt)
			if err != nil {
				return fail(providerquota.Reason(err))
			}
			w := providerquota.QuotaWindow{ID: m[1], Label: m[1], ResetsAt: providerquota.Time(reset)}
			if m[1] == "Current session" {
				w.Minutes = 300
				session = true
			} else {
				w.Minutes = 10080
				group := strings.TrimSuffix(strings.TrimPrefix(m[1], "Current week ("), ")")
				if group == "all models" {
					weekly = true
				} else {
					w.Scope = group
				}
			}
			if err = providerquota.Percent(&w, used); err != nil {
				return fail(providerquota.Reason(err))
			}
			r.Windows = append(r.Windows, w)
		}
		if !session || !weekly {
			return fail("lane_missing")
		}
	}
	if text := strings.ToLower(e.Result); strings.Contains(text, "last-known usage") || strings.Contains(text, "last known usage") {
		for i := range r.Windows {
			r.Windows[i].ObservedAt = nil
		}
	}
	unknown := false
	for _, w := range r.Windows {
		if w.ObservedAt == nil {
			unknown = true
		}
	}
	if unknown {
		r.Failures = append(r.Failures, providerquota.ReadFailure{Source: quotaSource, Reason: "measurement_time_unknown", At: c.RetrievedAt})
	}
	return providerquota.Finish(r, c)
}
func quotaReset(text, zone string, observed time.Time) (time.Time, error) {
	location, err := timezones.Load(zone)
	if err != nil {
		return time.Time{}, providerquota.Refuse("zone_unknown")
	}
	if observed.IsZero() {
		return time.Time{}, providerquota.Refuse("read_time_required")
	}
	year := observed.In(location).Year()
	parse := func(y int) (time.Time, error) {
		// Decode wall-clock syntax in UTC, then apply only the explicitly
		// named vendor zone from embedded data. Neither step consults Local.
		wall, err := time.ParseInLocation("2006 Jan 2 at 3:04pm", strconv.Itoa(y)+" "+text, time.UTC)
		if err != nil {
			return time.Time{}, err
		}
		return time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), 0, 0, location), nil
	}
	reset, err := parse(year)
	if err != nil {
		return time.Time{}, providerquota.Refuse("reset_invalid")
	}
	if reset.Before(observed) {
		reset, err = parse(year + 1)
	}
	if err != nil {
		return time.Time{}, providerquota.Refuse("reset_invalid")
	}
	return reset.UTC(), nil
}
