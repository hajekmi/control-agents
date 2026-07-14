package server

import (
	"math"
	"sync"
	"testing"
)

func TestOutputActivityStoreIsMonotonicUnderConcurrentOutput(t *testing.T) {
	store := newOutputActivityStore()
	const writers = 32
	const recordsPerWriter = 1000

	var wait sync.WaitGroup
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range recordsPerWriter {
				store.Record(testSessionRef, 1)
			}
		}()
	}
	wait.Wait()

	if got, want := store.Epoch(testSessionRef), int64(writers*recordsPerWriter); got != want {
		t.Fatalf("output epoch = %d, want %d", got, want)
	}
	store.Record(testSessionRef, 5)
	if got, want := store.Epoch(testSessionRef), int64(writers*recordsPerWriter+5); got != want {
		t.Fatalf("advanced output epoch = %d, want %d", got, want)
	}
}

func TestOutputActivityStoreSaturatesWithoutWrapping(t *testing.T) {
	store := newOutputActivityStore()
	store.epochs[testSessionRef] = math.MaxInt64 - 1
	store.Record(testSessionRef, 2)
	if got := store.Epoch(testSessionRef); got != math.MaxInt64 {
		t.Fatalf("saturated output epoch = %d, want %d", got, int64(math.MaxInt64))
	}
	store.Record(testSessionRef, 1)
	if got := store.Epoch(testSessionRef); got != math.MaxInt64 {
		t.Fatalf("wrapped output epoch = %d", got)
	}
}
