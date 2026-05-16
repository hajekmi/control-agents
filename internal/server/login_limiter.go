package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginAttemptLimit  = 10
	loginAttemptWindow = 5 * time.Minute
)

type loginLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	attempts  map[string][]time.Time
	now       func() time.Time
	lastSweep time.Time
}

func newLoginLimiter(limit int, window time.Duration) *loginLimiter {
	return &loginLimiter{
		limit:    limit,
		window:   window,
		attempts: make(map[string][]time.Time),
		now:      time.Now,
	}
}

func (l *loginLimiter) Allow(key string) (bool, time.Duration) {
	key = limiterKey(key)
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)
	attempts := l.pruneLocked(key, now)
	if len(attempts) < l.limit {
		return true, 0
	}
	return false, attempts[0].Add(l.window).Sub(now)
}

func (l *loginLimiter) RecordFailure(key string) {
	key = limiterKey(key)
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)
	attempts := l.pruneLocked(key, now)
	l.attempts[key] = append(attempts, now)
}

func (l *loginLimiter) Reset(key string) {
	key = limiterKey(key)

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.attempts, key)
}

func (l *loginLimiter) pruneLocked(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	attempts := l.attempts[key]
	keepFrom := 0
	for keepFrom < len(attempts) && attempts[keepFrom].Before(cutoff) {
		keepFrom++
	}
	if keepFrom > 0 {
		attempts = append([]time.Time(nil), attempts[keepFrom:]...)
	}
	if len(attempts) == 0 {
		delete(l.attempts, key)
		return nil
	}
	l.attempts[key] = attempts
	return attempts
}

func (l *loginLimiter) sweepLocked(now time.Time) {
	if !l.lastSweep.IsZero() && now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now
	for key := range l.attempts {
		l.pruneLocked(key, now)
	}
}

func limiterKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "unknown"
	}
	return key
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	if strings.TrimSpace(r.RemoteAddr) != "" {
		return r.RemoteAddr
	}
	return "unknown"
}
