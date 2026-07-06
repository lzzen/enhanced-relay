// Package idgen provides injectable ID generation (trace/task/request IDs) so
// business logic never calls uuid/rand directly. The deterministic Sequence
// generator makes IDs predictable under test.
package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
)

// Generator produces opaque unique identifiers.
type Generator interface {
	NewID() string
}

// Random is the production generator backed by crypto/rand (no external deps).
type Random struct{}

// New returns the production random generator.
func New() Generator { return Random{} }

func (Random) NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is fatal for a security-sensitive gateway.
		panic(fmt.Errorf("idgen: crypto/rand failed: %w", err))
	}
	return hex.EncodeToString(b[:])
}

// Sequence is a deterministic generator for tests: prefix + monotonic counter.
type Sequence struct {
	prefix  string
	counter atomic.Uint64
}

// NewSequence returns a deterministic generator emitting "<prefix>-000001", etc.
func NewSequence(prefix string) *Sequence { return &Sequence{prefix: prefix} }

func (s *Sequence) NewID() string {
	n := s.counter.Add(1)
	return fmt.Sprintf("%s-%06d", s.prefix, n)
}
