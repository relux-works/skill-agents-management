package codex

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/relux-works/skill-agents-management/pkg/providerquota"
)

var _ providerquota.Reader = (*System)(nil)

const quotaSource = "codex-app-server"

func (*System) QuotaPlan(req providerquota.Request) (providerquota.QuotaPlan, error) {
	pkg, triple, _ := platformPackageForHost()
	binary, err := providerquota.ResolveBinary(req, providerquota.BinarySpec{Name: executableName, ManagedRoot: envValue(req.Env, managedPackageRootEnv), PlatformPackage: pkg, TargetTriple: triple}, providerquota.NativeBinaryFS{})
	if err != nil {
		return providerquota.QuotaPlan{}, err
	}
	return providerquota.QuotaPlan{Binary: binary, Argv: []string{"-s", "read-only", "-a", "never", "app-server"}, Env: append([]string{}, req.Env...),
		Stdin:           []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"task-board","version":"0"}}}` + "\n"),
		AfterInitialize: []byte(`{"jsonrpc":"2.0","method":"initialized","params":{}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"account/rateLimits/read","params":{}}` + "\n"),
		HoldStdinOpen:   true, Complete: providerquota.RPCComplete(2), Timeout: 30 * time.Second, CwdPolicy: providerquota.ScratchCwd, Ready: true, SideEffectFlags: []string{"-s read-only", "-a never"}}, nil
}

type quotaWindow struct {
	UsedPercent *float64 `json:"usedPercent"`
	Minutes     *int     `json:"windowDurationMins"`
	ResetsAt    *int64   `json:"resetsAt"`
}
type quotaSnapshot struct {
	LimitID   string       `json:"limitId"`
	LimitName string       `json:"limitName"`
	PlanType  string       `json:"planType"`
	Primary   *quotaWindow `json:"primary"`
	Secondary *quotaWindow `json:"secondary"`
	Credits   *struct {
		Balance    *string `json:"balance"`
		HasCredits *bool   `json:"hasCredits"`
		Unlimited  *bool   `json:"unlimited"`
	} `json:"credits"`
	RateLimitReachedType *string `json:"rateLimitReachedType"`
	SpendControlReached  *bool   `json:"spendControlReached"`
}

var quotaVersion = regexp.MustCompile(`/([0-9]+\.[0-9]+\.[0-9]+)`)

func (*System) ParseQuota(stdout []byte, c providerquota.ParseContext) (providerquota.QuotaRecord, error) {
	fail := func(reason string) (providerquota.QuotaRecord, error) {
		r, err := providerquota.Failure(c, quotaSource, reason)
		if reason == "unauthenticated" {
			r.Authenticated = providerquota.AuthNo
		}
		return r, err
	}
	init, err := providerquota.RPCResult(stdout, 1)
	if err != nil {
		return fail(providerquota.Reason(err))
	}
	var greeting struct {
		Home      string `json:"codexHome"`
		UserAgent string `json:"userAgent"`
	}
	if json.Unmarshal(init, &greeting) != nil {
		return fail("payload_invalid")
	}
	// The returned home is compared lexically to normalized caller evidence.
	// Resolving a symlink here would stat a harness home, violating the boundary.
	if c.Identity == nil || greeting.Home == "" || !filepath.IsAbs(greeting.Home) || filepath.Clean(greeting.Home) != c.Identity.Home {
		return fail("home_mismatch")
	}
	if m := quotaVersion.FindStringSubmatch(greeting.UserAgent); len(m) > 1 {
		c.HarnessVersion = m[1]
	}
	payload, err := providerquota.RPCResult(stdout, 2)
	if err != nil {
		return fail(providerquota.Reason(err))
	}
	var response struct {
		RateLimits *quotaSnapshot           `json:"rateLimits"`
		ByID       map[string]quotaSnapshot `json:"rateLimitsByLimitId"`
	}
	if json.Unmarshal(payload, &response) != nil {
		return fail("payload_invalid")
	}
	if response.RateLimits == nil && response.ByID == nil {
		return fail("payload_missing")
	}
	r, err := providerquota.Base(c, quotaSource)
	if err != nil {
		return r, err
	}
	r.Inventory.Installed = true
	r.Inventory.AuthSource = quotaSource
	r.Authenticated = providerquota.AuthYes
	r.State = providerquota.Exact
	r.Confidence = "exact"
	r.Checked = []string{"account/rateLimits/read"}
	if response.RateLimits != nil {
		r.Plan = response.RateLimits.PlanType
	}
	r.Flags = map[string]string{}
	snapshots := response.ByID
	// A present map is authoritative, independently of optional legacy metadata.
	if snapshots == nil {
		if response.RateLimits == nil || response.RateLimits.LimitID == "" {
			return fail("limit_id_missing")
		}
		snapshots = map[string]quotaSnapshot{response.RateLimits.LimitID: *response.RateLimits}
	}
	ids := make([]string, 0, len(snapshots))
	for id := range snapshots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s := snapshots[id]
		if s.LimitID != "" && s.LimitID != id {
			return fail("limit_id_mismatch")
		}
		if s.PlanType != "" {
			r.Flags[id+"/planType"] = s.PlanType
		}
		if s.RateLimitReachedType != nil {
			r.Flags[id+"/rateLimitReachedType"] = *s.RateLimitReachedType
		}
		if s.SpendControlReached != nil {
			r.Flags[id+"/spendControlReached"] = strconv.FormatBool(*s.SpendControlReached)
		}
		if s.Credits != nil {
			if s.Credits.Balance != nil {
				r.Flags[id+"/credits.balance"] = *s.Credits.Balance
				if response.RateLimits != nil && id == response.RateLimits.LimitID {
					balance, e := strconv.ParseFloat(*s.Credits.Balance, 64)
					if e != nil {
						return fail("payload_invalid")
					}
					unlimited := s.Credits.Unlimited != nil && *s.Credits.Unlimited
					r.Credits = &providerquota.Credits{Balance: balance, Unit: "credits", Unlimited: unlimited}
				}
			}
			if s.Credits.HasCredits != nil {
				r.Flags[id+"/credits.hasCredits"] = strconv.FormatBool(*s.Credits.HasCredits)
			}
			if s.Credits.Unlimited != nil {
				r.Flags[id+"/credits.unlimited"] = strconv.FormatBool(*s.Credits.Unlimited)
			}
		}
		for _, slot := range []struct {
			kind  string
			value *quotaWindow
		}{{"primary", s.Primary}, {"secondary", s.Secondary}} {
			if slot.value == nil {
				continue
			}
			v := slot.value
			if v.UsedPercent == nil {
				return fail("percent_missing")
			}
			w := providerquota.QuotaWindow{ID: id, Kind: slot.kind, Label: s.LimitName, Scope: s.LimitName}
			if v.Minutes != nil {
				w.Minutes = *v.Minutes
			}
			if v.ResetsAt != nil {
				w.ResetsAt = providerquota.Time(time.Unix(*v.ResetsAt, 0))
			}
			if !c.ReadAt.IsZero() {
				w.ObservedAt = providerquota.Time(c.ReadAt)
			}
			if err := providerquota.Percent(&w, *v.UsedPercent); err != nil {
				return fail(providerquota.Reason(err))
			}
			r.Windows = append(r.Windows, w)
		}
	}
	return providerquota.Finish(r, c)
}
