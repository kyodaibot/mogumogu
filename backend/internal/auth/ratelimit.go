package auth

import (
	"sync"
	"time"
)

// Limiter is a fixed-window counter kept in process memory. It resets on
// restart, which is acceptable for a single application server.
type Limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]*window
	now     func() time.Time
}

type window struct {
	start time.Time
	count int
}

// maxEntries triggers a sweep of expired windows so memory stays bounded.
const maxEntries = 10_000

func NewLimiter(limit int, period time.Duration) *Limiter {
	return &Limiter{limit: limit, window: period, entries: map[string]*window{}, now: time.Now}
}

// Blocked reports whether key has used up its limit in the current window,
// and if so how long until the window ends.
func (l *Limiter) Blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w := l.current(key, now)
	if w == nil || w.count < l.limit {
		return false, 0
	}
	return true, w.start.Add(l.window).Sub(now)
}

// Add counts one event for key.
func (l *Limiter) Add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w := l.current(key, now)
	if w == nil {
		if len(l.entries) >= maxEntries {
			l.sweep(now)
		}
		w = &window{start: now}
		l.entries[key] = w
	}
	w.count++
}

// Reset forgets key, for example after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// current returns key's window if it has not expired yet.
func (l *Limiter) current(key string, now time.Time) *window {
	w, ok := l.entries[key]
	if !ok {
		return nil
	}
	if now.Sub(w.start) >= l.window {
		delete(l.entries, key)
		return nil
	}
	return w
}

func (l *Limiter) sweep(now time.Time) {
	for k, w := range l.entries {
		if now.Sub(w.start) >= l.window {
			delete(l.entries, k)
		}
	}
}
