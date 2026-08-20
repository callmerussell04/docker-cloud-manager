package service

import "sync"

type SessionLimiter struct {
	mu       sync.Mutex
	counts   map[string]int
	maxTotal int
	total    int
}

func NewSessionLimiter(maxTotal int) *SessionLimiter {
	if maxTotal <= 0 {
		maxTotal = 1
	}
	return &SessionLimiter{
		counts:   make(map[string]int),
		maxTotal: maxTotal,
	}
}

func (l *SessionLimiter) Acquire(key string, maxPerKey int) bool {
	if maxPerKey <= 0 {
		maxPerKey = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= l.maxTotal || l.counts[key] >= maxPerKey {
		return false
	}
	l.counts[key]++
	l.total++
	return true
}

func (l *SessionLimiter) Release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts[key] <= 0 {
		return
	}
	l.counts[key]--
	l.total--
	if l.counts[key] == 0 {
		delete(l.counts, key)
	}
}
