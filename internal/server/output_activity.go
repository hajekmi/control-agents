package server

import (
	"math"
	"sync"
)

// outputActivityStore records only byte counts from proxied terminal output.
// It never retains terminal content.
type outputActivityStore struct {
	mu     sync.RWMutex
	epochs map[SessionRef]int64
}

func newOutputActivityStore() *outputActivityStore {
	return &outputActivityStore{epochs: make(map[SessionRef]int64)}
}

func (s *outputActivityStore) Record(ref SessionRef, bytes int) {
	if s == nil || ref == "" || bytes <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.epochs[ref]
	if int64(bytes) > math.MaxInt64-current {
		s.epochs[ref] = math.MaxInt64
		return
	}
	s.epochs[ref] = current + int64(bytes)
}

func (s *outputActivityStore) Epoch(ref SessionRef) int64 {
	if s == nil || ref == "" {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.epochs[ref]
}

func (s *outputActivityStore) Forget(ref SessionRef) {
	if s == nil || ref == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.epochs, ref)
}
