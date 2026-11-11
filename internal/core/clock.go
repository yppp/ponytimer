package core

import (
	"sync"
	"time"
)

// Clock provides the current server time. Injecting it lets tests drive the
// timer deterministically instead of sleeping.
type Clock interface {
	Now() time.Time
}

// RealClock is the production clock.
type RealClock struct{}

// Now returns the current wall-clock time.
func (RealClock) Now() time.Time { return time.Now() }

// FakeClock is a manually advanced clock for tests.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock returns a FakeClock positioned at t.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t}
}

// Now returns the fake clock's current time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the fake clock to t.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Advance moves the fake clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
