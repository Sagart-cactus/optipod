package harness

import (
	"sync"
	"time"
)

// MockClock provides controllable time for testing
type MockClock struct {
	mu   sync.RWMutex
	time time.Time
}

// NewMockClock creates a new mock clock starting at the given time
func NewMockClock(startTime time.Time) *MockClock {
	return &MockClock{
		time: startTime,
	}
}

// Now returns the current mock time
func (c *MockClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.time
}

// Advance moves the clock forward by the given duration
func (c *MockClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.time = c.time.Add(duration)
}

// Set sets the clock to a specific time
func (c *MockClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.time = t
}

// Since returns the duration since the given time
func (c *MockClock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
}

// Until returns the duration until the given time
func (c *MockClock) Until(t time.Time) time.Duration {
	return t.Sub(c.Now())
}
