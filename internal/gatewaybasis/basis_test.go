package gatewaybasis

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// Issued value copies share one successful use, whichever copy wins. A failed
// callback leaves the capability available; invalid handles never run callbacks.
func TestBasisCopiesShareConsumption(t *testing.T) {
	for _, copyFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "original-first", true: "copy-first"}[copyFirst], func(t *testing.T) {
			original, err := Issue("candidate")
			if err != nil {
				t.Fatal(err)
			}
			copied := *original
			first, second := original, &copied
			if copyFirst {
				first, second = second, first
			}
			if first.ID() == "" || first.ID() != second.ID() {
				t.Fatal("copy lost association")
			}
			failed := errors.New("callback refused")
			if err := Use(first, func(any) error { return failed }); !errors.Is(err, failed) {
				t.Fatal(err)
			}
			if err := Use(second, func(value any) error {
				if value != "candidate" {
					t.Fatalf("wrong value: %v", value)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			for _, handle := range []*Basis{first, second} {
				for _, action := range []func(*Basis, func(any) error) error{Inspect, Use} {
					if err := action(handle, func(any) error { t.Fatal("consumed callback ran"); return nil }); !errors.Is(err, ErrInvalid) {
						t.Fatalf("consumed handle: %v", err)
					}
				}
			}
		})
	}
}

// Concurrent value copies permit exactly one callback and success. This tests
// the issuer's consumption boundary, not downstream registration or transport.
func TestBasisConcurrentCopiesHaveOneSuccess(t *testing.T) {
	original, err := Issue("candidate")
	if err != nil {
		t.Fatal(err)
	}
	const count = 16
	handles := make([]Basis, count)
	for i := range handles {
		handles[i] = *original
	}
	var successes, callbacks atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range handles {
		wg.Add(1)
		go func(handle *Basis) {
			defer wg.Done()
			<-start
			err := Use(handle, func(any) error { callbacks.Add(1); return nil })
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrInvalid) {
				t.Errorf("wrong refusal: %v", err)
			}
		}(&handles[i])
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 || callbacks.Load() != 1 {
		t.Fatalf("successes=%d callbacks=%d; want exactly one", successes.Load(), callbacks.Load())
	}
}

// Missing and zero handles reject without invoking either callback; separately
// issued handles remain independent positive controls.
func TestBasisInvalidAndIndependent(t *testing.T) {
	for _, handle := range []*Basis{nil, new(Basis)} {
		if handle.ID() != "" {
			t.Fatal("invalid handle has an identity")
		}
		for _, action := range []func(*Basis, func(any) error) error{Inspect, Use} {
			if err := action(handle, func(any) error { t.Fatal("invalid callback ran"); return nil }); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid handle: %v", err)
			}
		}
	}
	for i := 0; i < 2; i++ {
		handle, err := Issue(i)
		if err != nil {
			t.Fatal(err)
		}
		if err := Inspect(handle, func(value any) error {
			if value != i {
				t.Fatal("wrong identity")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := Use(handle, func(any) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
}
