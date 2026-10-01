package providerquota

import (
	"encoding/json"
	"time"
)

// ParseTimestamp decodes optional wire strings with an explicit UTC location.
// time.Time's JSON decoder consults ambient Local for numeric offsets, which
// can read a TZ-selected file. Quota parsing must never initialize that zone.
func ParseTimestamp(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	v, err := time.ParseInLocation(time.RFC3339Nano, *s, time.UTC)
	if err != nil {
		return nil, Refuse("timestamp_invalid")
	}
	return Time(v), nil
}

// Explicit string fields shadow every time.Time field of the embedded alias.
// Nested windows and failures use their own string decoders. Aliases preserve
// the public record types and JSON output without invoking time's decoder.
func (w *QuotaWindow) UnmarshalJSON(b []byte) error {
	type plain QuotaWindow
	var out plain
	wire := struct {
		*plain
		ResetsAt   *string `json:"resets_at"`
		ObservedAt *string `json:"observed_at"`
	}{plain: &out}
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	var err error
	if out.ResetsAt, err = ParseTimestamp(wire.ResetsAt); err != nil {
		return err
	}
	if out.ObservedAt, err = ParseTimestamp(wire.ObservedAt); err != nil {
		return err
	}
	*w = QuotaWindow(out)
	return nil
}

func (f *ReadFailure) UnmarshalJSON(b []byte) error {
	type plain ReadFailure
	var out plain
	wire := struct {
		*plain
		At *string `json:"at"`
	}{plain: &out}
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	at, err := ParseTimestamp(wire.At)
	if err != nil {
		return err
	}
	if at != nil {
		out.At = *at
	}
	*f = ReadFailure(out)
	return nil
}

func (r *QuotaRecord) UnmarshalJSON(b []byte) error {
	type plain QuotaRecord
	var out plain
	wire := struct {
		*plain
		ObservedAt  *string `json:"observed_at"`
		RetrievedAt *string `json:"retrieved_at"`
	}{plain: &out}
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	var err error
	if out.ObservedAt, err = ParseTimestamp(wire.ObservedAt); err != nil {
		return err
	}
	retrieved, err := ParseTimestamp(wire.RetrievedAt)
	if err != nil {
		return err
	}
	if retrieved != nil {
		out.RetrievedAt = *retrieved
	}
	*r = QuotaRecord(out)
	return nil
}

func (p *Projection) UnmarshalJSON(b []byte) error {
	type plain Projection
	var out plain
	wire := struct {
		*plain
		ObservedAt  *string `json:"observed_at"`
		RetrievedAt *string `json:"retrieved_at"`
	}{plain: &out}
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	var err error
	if out.ObservedAt, err = ParseTimestamp(wire.ObservedAt); err != nil {
		return err
	}
	retrieved, err := ParseTimestamp(wire.RetrievedAt)
	if err != nil {
		return err
	}
	if retrieved != nil {
		out.RetrievedAt = *retrieved
	}
	*p = Projection(out)
	return nil
}
