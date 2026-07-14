package server

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHistoryCaptureCoordinatorCoalescesOnlyIdenticalAuthorizationScope(t *testing.T) {
	coordinator := newHistoryCaptureCoordinator()
	key := testHistoryCaptureKey()
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	capture := func(context.Context) (historyCaptureProduct, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return historyCaptureProduct{ParseBytes: 12, NodeEstimate: 3}, nil
	}

	results := make(chan historyCaptureProduct, 2)
	errorsSeen := make(chan error, 2)
	go func() {
		result, err := coordinator.Do(context.Background(), key, capture)
		results <- result
		errorsSeen <- err
	}()
	<-entered
	go func() {
		result, err := coordinator.Do(context.Background(), key, capture)
		results <- result
		errorsSeen <- err
	}()
	waitForHistoryWaiters(t, coordinator, key, 1)
	close(release)
	for range 2 {
		if err := <-errorsSeen; err != nil {
			t.Fatal(err)
		}
		if result := <-results; result.ParseBytes != 12 || result.NodeEstimate != 3 {
			t.Fatalf("coalesced result = %#v", result)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("capture calls = %d, want one", calls.Load())
	}

}

func TestHistoryCaptureCoordinatorNeverCoalescesAcrossScopeBoundary(t *testing.T) {
	tests := []struct {
		name   string
		change func(*historyCaptureKey)
	}{
		{name: "login", change: func(key *historyCaptureKey) { key.User = "other-login" }},
		{name: "viewer", change: func(key *historyCaptureKey) { key.Viewer = ViewerID("viewer-00000000-0000-0000-0000-000000000001") }},
		{name: "session", change: func(key *historyCaptureKey) { key.SessionRef = SessionRef("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") }},
		{name: "pane", change: func(key *historyCaptureKey) { key.PaneRef = PaneRef("p_zyxwvutsrqponmlkjihgfedc") }},
		{name: "generation", change: func(key *historyCaptureKey) { key.Generation.TmuxServerPID++ }},
		{name: "mode", change: func(key *historyCaptureKey) { key.Mode = "fixed" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			coordinator := newHistoryCaptureCoordinator()
			firstKey := testHistoryCaptureKey()
			secondKey := firstKey
			test.change(&secondKey)
			release := make(chan struct{})
			entered := make(chan struct{}, 2)
			var calls atomic.Int32
			capture := func(context.Context) (historyCaptureProduct, error) {
				calls.Add(1)
				entered <- struct{}{}
				<-release
				return historyCaptureProduct{}, nil
			}
			done := make(chan error, 2)
			go func() { _, err := coordinator.Do(context.Background(), firstKey, capture); done <- err }()
			<-entered
			go func() { _, err := coordinator.Do(context.Background(), secondKey, capture); done <- err }()
			<-entered
			close(release)
			for range 2 {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("cross-%s capture calls = %d, want two", test.name, calls.Load())
			}
		})
	}
}

func TestHistoryCaptureCoordinatorBoundsConcurrentCapturesAcrossRotatedViewers(t *testing.T) {
	coordinator := newHistoryCaptureCoordinator()
	coordinator.maxLoginCaptures = 1
	firstKey := testHistoryCaptureKey()
	secondKey := firstKey
	secondKey.Viewer = ViewerID("viewer-00000000-0000-0000-0000-000000000001")
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := coordinator.Do(context.Background(), firstKey, func(context.Context) (historyCaptureProduct, error) {
			close(entered)
			<-release
			return historyCaptureProduct{}, nil
		})
		done <- err
	}()
	<-entered

	if _, err := coordinator.Do(context.Background(), secondKey, func(context.Context) (historyCaptureProduct, error) {
		t.Fatal("cross-viewer capture reached the capture function above the login limit")
		return historyCaptureProduct{}, nil
	}); !errors.Is(err, errHistoryLoginCaptures) {
		t.Fatalf("cross-viewer concurrency error = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHistoryCaptureCoordinatorBoundsProcessConcurrencyAcrossLogins(t *testing.T) {
	coordinator := newHistoryCaptureCoordinator()
	coordinator.maxProcessCaptures = 2
	coordinator.maxLoginCaptures = 2
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	done := make(chan error, 2)
	for index, user := range []string{"login-a", "login-b"} {
		key := testHistoryCaptureKey()
		key.User = user
		key.Viewer = ViewerID("viewer-00000000-0000-0000-0000-00000000000" + string(rune('1'+index)))
		go func() {
			_, err := coordinator.Do(context.Background(), key, func(context.Context) (historyCaptureProduct, error) {
				entered <- struct{}{}
				<-release
				return historyCaptureProduct{}, nil
			})
			done <- err
		}()
	}
	<-entered
	<-entered
	thirdKey := testHistoryCaptureKey()
	thirdKey.User = "login-c"
	thirdKey.Viewer = ViewerID("viewer-00000000-0000-0000-0000-000000000003")
	if _, err := coordinator.Do(context.Background(), thirdKey, func(context.Context) (historyCaptureProduct, error) {
		t.Fatal("capture reached the capture function above the process limit")
		return historyCaptureProduct{}, nil
	}); !errors.Is(err, errHistoryProcessCaptures) {
		t.Fatalf("process concurrency error = %v", err)
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestHistoryCaptureCoordinatorBoundsCoalescedWaiters(t *testing.T) {
	coordinator := newHistoryCaptureCoordinator()
	coordinator.maxWaiters = 1
	key := testHistoryCaptureKey()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		_, err := coordinator.Do(context.Background(), key, func(context.Context) (historyCaptureProduct, error) {
			close(entered)
			<-release
			return historyCaptureProduct{}, nil
		})
		done <- err
	}()
	<-entered
	var unexpectedCapture atomic.Int32
	go func() {
		_, err := coordinator.Do(context.Background(), key, func(context.Context) (historyCaptureProduct, error) {
			unexpectedCapture.Add(1)
			return historyCaptureProduct{}, nil
		})
		done <- err
	}()
	waitForHistoryWaiters(t, coordinator, key, 1)

	if _, err := coordinator.Do(context.Background(), key, func(context.Context) (historyCaptureProduct, error) {
		t.Fatal("rejected waiter started another capture")
		return historyCaptureProduct{}, nil
	}); !errors.Is(err, errHistoryCaptureWaiters) {
		t.Fatalf("waiter capacity error = %v", err)
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if unexpectedCapture.Load() != 0 {
		t.Fatal("coalesced waiter started another capture")
	}
}

func TestHistoryCaptureCoordinatorReleasesFailedWaitersWithoutCaching(t *testing.T) {
	coordinator := newHistoryCaptureCoordinator()
	key := testHistoryCaptureKey()
	entered := make(chan struct{})
	release := make(chan struct{})
	want := errors.New("capture failed")
	var calls atomic.Int32
	capture := func(context.Context) (historyCaptureProduct, error) {
		calls.Add(1)
		close(entered)
		<-release
		return historyCaptureProduct{}, want
	}
	var wait sync.WaitGroup
	wait.Add(2)
	errorsSeen := make(chan error, 2)
	go func() {
		defer wait.Done()
		_, err := coordinator.Do(context.Background(), key, capture)
		errorsSeen <- err
	}()
	<-entered
	go func() {
		defer wait.Done()
		_, err := coordinator.Do(context.Background(), key, capture)
		errorsSeen <- err
	}()
	waitForHistoryWaiters(t, coordinator, key, 1)
	close(release)
	wait.Wait()
	for range 2 {
		if !errors.Is(<-errorsSeen, want) {
			t.Fatal("coalesced failure did not reach waiter")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("failed leader calls = %d", calls.Load())
	}
	if _, err := coordinator.Do(context.Background(), key, func(context.Context) (historyCaptureProduct, error) {
		calls.Add(1)
		return historyCaptureProduct{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("failed capture was cached")
	}
}

func TestHistoryCaptureCoordinatorEnforcesTimeoutAndRate(t *testing.T) {
	coordinator := newHistoryCaptureCoordinator()
	coordinator.timeout = 10 * time.Millisecond
	coordinator.limit = 2
	key := testHistoryCaptureKey()
	_, err := coordinator.Do(context.Background(), key, func(ctx context.Context) (historyCaptureProduct, error) {
		<-ctx.Done()
		return historyCaptureProduct{}, ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if _, err := coordinator.Do(context.Background(), key, func(context.Context) (historyCaptureProduct, error) {
		return historyCaptureProduct{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Do(context.Background(), key, func(context.Context) (historyCaptureProduct, error) {
		return historyCaptureProduct{}, nil
	}); !errors.Is(err, errHistoryCreateRate) {
		t.Fatalf("rate error = %v", err)
	}
}

func testHistoryCaptureKey() historyCaptureKey {
	return historyCaptureKey{
		User: "login", Viewer: testHistoryViewer, SessionRef: testSessionRef,
		PaneRef:    PaneRef("p_abcdefghijklmnopqrstuvwx"),
		Generation: PaneGeneration{TmuxServerStart: "100", TmuxServerPID: 101, PaneID: "%42"},
		Mode:       "reflow",
	}
}

func waitForHistoryWaiters(t *testing.T, coordinator *historyCaptureCoordinator, key historyCaptureKey, want int) {
	t.Helper()
	for attempts := 0; attempts < 10000; attempts++ {
		coordinator.mu.Lock()
		call := coordinator.inFlight[key]
		got := 0
		if call != nil {
			got = call.waiters
		}
		coordinator.mu.Unlock()
		if got >= want {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("coalesced waiter did not register")
}
