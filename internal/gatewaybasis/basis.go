// Package gatewaybasis is the module-private authority issuer. Consumers cannot
// import it, and the public basis has no constructor or writable fields.
package gatewaybasis

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
)

var ErrInvalid = errors.New("gateway admission basis is missing or consumed")

type Basis struct {
	state *basisState
}

// Copies of the public handle retain the same issuance and consumption state.
// A zero handle has no authority; only Issue constructs the shared state.
type basisState struct {
	mu    sync.Mutex
	id    string
	value any
	used  bool
}

func Issue(value any) (*Basis, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	return &Basis{state: &basisState{id: hex.EncodeToString(id[:]), value: value}}, nil
}

// ID is nonsecret registration association; it carries no admission authority.
func (b *Basis) ID() string {
	if b == nil || b.state == nil {
		return ""
	}
	return b.state.id
}

func Inspect(b *Basis, f func(any) error) error {
	if b == nil || b.state == nil {
		return ErrInvalid
	}
	s := b.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.id == "" || s.used {
		return ErrInvalid
	}
	return f(s.value)
}

func Use(b *Basis, f func(any) error) error {
	if b == nil || b.state == nil {
		return ErrInvalid
	}
	s := b.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.id == "" || s.used {
		return ErrInvalid
	}
	if err := f(s.value); err != nil {
		return err
	}
	s.used = true
	return nil
}
