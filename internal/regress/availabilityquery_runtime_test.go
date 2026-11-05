package regress

import (
	"testing"

	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
)

// CLASS 8 — AvailabilityQuery.Runtime is backward compatible by
// construction (architecture decision TASK-260829-3jlxed, §5.1; adversarial
// plan case 33).
//
// Adding a field to a struct changes no existing vendor's behavior as long
// as every existing vendor takes the whole struct by type and ignores the
// new field's contents — which is exactly how Go structs are consumed here.
// This file proves it rather than trusting the reasoning: drive
// CheckAvailability for each of the four pre-existing vendors with several
// different Runtime values, INCLUDING both of google's two runtimes
// (gemini and agy, which already share one VendorID), and require the
// identical Unchecked() answer regardless of which Runtime was supplied.
func TestAvailabilityQueryRuntimeIsInertForEveryPreExistingVendor(t *testing.T) {
	cases := []struct {
		vendor  vendorplugin.VendorID
		model   vendorplugin.ModelID
		runtime vendorplugin.RuntimeID
	}{
		{"anthropic", "claude-opus-5", "claude"},
		{"anthropic", "claude-opus-5", ""},
		{"openai", "gpt-5.6-terra", "codex"},
		{"openai", "gpt-5.6-terra", ""},
		{"alibaba", "qwen3.7-max", "qwen"},
		{"alibaba", "qwen3.7-max", ""},
		{"google", "gemini-3.5-flash", "gemini"},
		{"google", "gemini-3.6-flash-high", "agy"},
		{"google", "gemini-3.5-flash", ""},
		// A RuntimeID that does not even resolve to this vendor is still
		// inert: the vendor never validates it, since it never reads it.
		{"google", "gemini-3.5-flash", "some-unrelated-runtime"},
	}
	for _, tc := range cases {
		t.Run(string(tc.vendor)+"/"+string(tc.runtime), func(t *testing.T) {
			vendor, ok := vendorplugin.Default.Lookup(tc.vendor)
			if !ok {
				t.Fatalf("vendor %q is not registered in this test binary", tc.vendor)
			}
			modelKnown := false
			for _, m := range vendor.Models() {
				if m.ID == tc.model {
					modelKnown = true
				}
			}
			if !modelKnown {
				t.Fatalf("test setup: %q does not declare model %q", tc.vendor, tc.model)
			}
			verdict, err := vendorplugin.CheckAvailability(vendorplugin.Default, tc.vendor, vendorplugin.AvailabilityQuery{
				Model: tc.model, Runtime: tc.runtime,
			})
			if err != nil {
				t.Fatalf("CheckAvailability: %v", err)
			}
			if verdict.State != vendorplugin.AvailabilityUnknown || len(verdict.Checked) != 0 {
				t.Fatalf("verdict = %+v, want exactly Unchecked() regardless of Runtime=%q", verdict, tc.runtime)
			}
		})
	}
}
