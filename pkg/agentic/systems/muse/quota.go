package muse

import (
	"encoding/json"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/agentic"
	"github.com/relux-works/skill-agents-management/pkg/providerquota"
)

var _ providerquota.Reader = (*System)(nil)

const quotaSource = "msp-usage-read"

// QuotaPlan declares the MSP JSONL handshake and usage read. The logged Muse
// 1.4.2 echo-provider probe in the W3 Muse update brief establishes JSONL
// JSON-RPC 2.0 framing in both directions: initialize, initialized, usage/read
// produce newline-delimited id-1 and id-2 replies.
func (*System) QuotaPlan(req providerquota.Request) (providerquota.QuotaPlan, error) {
	binary, err := providerquota.ResolveBinary(req, providerquota.BinarySpec{Name: executableName}, providerquota.NativeBinaryFS{})
	if err != nil {
		return providerquota.QuotaPlan{}, err
	}
	return providerquota.QuotaPlan{Binary: binary, Argv: []string{"serve"}, Env: agentic.SetEnvValue(append([]string{}, req.Env...), "MUSE_NO_AUTO_UPDATE", "1"),
		Stdin:           []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"task_board","version":"0"}}}` + "\n"),
		AfterInitialize: []byte(`{"jsonrpc":"2.0","method":"initialized","params":{}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"usage/read"}` + "\n"),
		HoldStdinOpen:   true, Complete: providerquota.RPCComplete(2), Timeout: 30 * time.Second, CwdPolicy: providerquota.ScratchCwd, SideEffectFlags: []string{"MUSE_NO_AUTO_UPDATE=1"}, Ready: true}, nil
}

// TODO(decision): L1 §5 item 1 asks whether usage/read observes anything before
// a turn or session/start. The logged Muse 1.4.2 fresh echo-host probe returned
// {} (no observation). Whether a subscription host can observe before key mint
// remains unresolved. Never add session/start, key mint or inference fallback.
// SubscriptionUsage is unchanged from the Muse 1.4.1 stable schema.
func (*System) ParseQuota(stdout []byte, c providerquota.ParseContext) (providerquota.QuotaRecord, error) {
	fail := func(reason string) (providerquota.QuotaRecord, error) {
		return providerquota.Failure(c, quotaSource, reason)
	}
	if _, err := providerquota.RPCResult(stdout, 1); err != nil {
		return fail(providerquota.Reason(err))
	}
	payload, err := providerquota.RPCResult(stdout, 2)
	if err != nil {
		return fail(providerquota.Reason(err))
	}
	var e struct {
		Usage *struct {
			Tier       *string `json:"tier"`
			ObservedAt *int64  `json:"observedAtMs"`
			Window     *struct {
				Used    *float64 `json:"usedPercent"`
				Minutes *int     `json:"windowDurationMins"`
				Reset   *int64   `json:"resetsAtMs"`
			} `json:"window"`
			Weekly *struct {
				Used  *float64 `json:"usedPercent"`
				Reset *int64   `json:"resetsAtMs"`
			} `json:"weekly"`
		} `json:"usage"`
	}
	if json.Unmarshal(payload, &e) != nil {
		return fail("payload_invalid")
	}
	if e.Usage == nil {
		return fail("no_observation")
	}
	u := e.Usage
	if u.Tier == nil || u.ObservedAt == nil || u.Window == nil || u.Weekly == nil || u.Window.Used == nil || u.Window.Minutes == nil || *u.Window.Minutes <= 0 || u.Window.Reset == nil || u.Weekly.Used == nil || u.Weekly.Reset == nil {
		return fail("payload_missing")
	}
	r, err := providerquota.Base(c, quotaSource)
	if err != nil {
		return r, err
	}
	r.State = providerquota.LastObserved
	r.Confidence = "last_observed"
	r.Plan = *u.Tier
	r.Inventory.Installed = true
	r.Checked = []string{"usage/read"}
	for _, slot := range []struct {
		id      string
		used    float64
		minutes int
		reset   int64
	}{{"window", *u.Window.Used, *u.Window.Minutes, *u.Window.Reset}, {"weekly", *u.Weekly.Used, 10080, *u.Weekly.Reset}} {
		if slot.used != float64(int64(slot.used)) {
			return fail("out_of_range")
		}
		w := providerquota.QuotaWindow{ID: slot.id, Minutes: slot.minutes, ResetsAt: providerquota.Time(time.UnixMilli(slot.reset)), ObservedAt: providerquota.Time(time.UnixMilli(*u.ObservedAt))}
		if err = providerquota.Percent(&w, slot.used); err != nil {
			return fail(providerquota.Reason(err))
		}
		r.Windows = append(r.Windows, w)
	}
	return providerquota.Finish(r, c)
}
