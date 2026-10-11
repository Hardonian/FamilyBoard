package clock

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Clock provides an injectable abstraction over system time.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Until(t time.Time) time.Duration
	Sleep(d time.Duration)
	After(d time.Duration) <-chan time.Time
}

// RealClock delegates directly to the standard library time package.
type RealClock struct{}

func NewRealClock() *RealClock {
	return &RealClock{}
}

func (r *RealClock) Now() time.Time {
	return time.Now()
}

func (r *RealClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

func (r *RealClock) Until(t time.Time) time.Duration {
	return time.Until(t)
}

func (r *RealClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

func (r *RealClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// MockClock provides a thread-safe deterministic clock for tests.
type MockClock struct {
	mu      sync.RWMutex
	current time.Time
}

func NewMockClock(initial time.Time) *MockClock {
	if initial.IsZero() {
		initial = time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	}
	return &MockClock{current: initial}
}

func (m *MockClock) Now() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

func (m *MockClock) Since(t time.Time) time.Duration {
	return m.Now().Sub(t)
}

func (m *MockClock) Until(t time.Time) time.Duration {
	return t.Sub(m.Now())
}

func (m *MockClock) Set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = t
}

func (m *MockClock) Add(d time.Duration) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = m.current.Add(d)
	return m.current
}

func (m *MockClock) Sleep(d time.Duration) {
	m.Add(d)
}

func (m *MockClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	m.mu.RLock()
	t := m.current.Add(d)
	m.mu.RUnlock()
	ch <- t
	return ch
}

// IDGenerator provides injectable unique ID generation.
type IDGenerator interface {
	NewID(prefix string) string
}

// RealIDGenerator generates cryptographically random hex IDs.
type RealIDGenerator struct{}

func NewRealIDGenerator() *RealIDGenerator {
	return &RealIDGenerator{}
}

func (r *RealIDGenerator) NewID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	hexStr := hex.EncodeToString(b)
	if prefix == "" {
		return hexStr
	}
	return fmt.Sprintf("%s-%s", prefix, hexStr)
}

// MockIDGenerator generates sequential deterministic IDs for testing.
type MockIDGenerator struct {
	counter int64
}

func NewMockIDGenerator(start int64) *MockIDGenerator {
	return &MockIDGenerator{counter: start}
}

func (m *MockIDGenerator) NewID(prefix string) string {
	val := atomic.AddInt64(&m.counter, 1)
	if prefix == "" {
		return fmt.Sprintf("%08d", val)
	}
	return fmt.Sprintf("%s-%08d", prefix, val)
}
