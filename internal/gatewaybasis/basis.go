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
	return &Basis{id: hex.EncodeToString(id[:]), value: value}, nil
}

// ID is nonsecret registration association; it carries no admission authority.
func (b *Basis) ID() string {
	if b == nil {
		return ""
	}
	return b.id
}

func Inspect(b *Basis, f func(any) error) error {
	if b == nil {
		return ErrInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.id == "" || b.used {
		return ErrInvalid
	}
	return f(b.value)
}

func Use(b *Basis, f func(any) error) error {
	if b == nil {
		return ErrInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.id == "" || b.used {
		return ErrInvalid
	}
	if err := f(b.value); err != nil {
		return err
	}
	b.used = true
	return nil
}
