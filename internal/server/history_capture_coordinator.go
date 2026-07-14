package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"control-agents/internal/tmux"
)

const (
	defaultHistoryCaptureTimeout  = 5 * time.Second
	defaultHistoryCreateLimit     = 6
	defaultHistoryCreateWindow    = time.Minute
	defaultHistoryRateScopes      = 1024
	defaultHistoryProcessCaptures = 4
	defaultHistoryLoginCaptures   = 2
	defaultHistoryCaptureWaiters  = 8
)

var (
	errHistoryCreateRate      = errors.New("history snapshot create rate exceeded")
	errHistoryRateScopes      = errors.New("history snapshot create scope capacity reached")
	errHistoryProcessCaptures = errors.New("history process capture concurrency exceeded")
	errHistoryLoginCaptures   = errors.New("history login capture concurrency exceeded")
	errHistoryCaptureWaiters  = errors.New("history capture waiter capacity reached")
)

type historyCaptureKey struct {
	User       string
	Viewer     ViewerID
	SessionRef SessionRef
	PaneRef    PaneRef
	Generation PaneGeneration
	Mode       string
}

type historyCaptureProduct struct {
	Capture       tmux.HistoryCapture
	Lines         []historyLine
	ParseBytes    int
	ParseDuration time.Duration
	NodeEstimate  int
}

type historyCaptureCall struct {
	done    chan struct{}
	product historyCaptureProduct
	err     error
	waiters int
}

type historyRateState struct {
	requests []time.Time
	lastSeen time.Time
}

type historyCaptureCoordinator struct {
	mu                 sync.Mutex
	now                func() time.Time
	timeout            time.Duration
	limit              int
	window             time.Duration
	maxScopes          int
	maxProcessCaptures int
	maxLoginCaptures   int
	maxWaiters         int
	activeCaptures     int
	inFlight           map[historyCaptureKey]*historyCaptureCall
	activeByLogin      map[string]int
	rates              map[historyCaptureKey]historyRateState
}

func newHistoryCaptureCoordinator() *historyCaptureCoordinator {
	return &historyCaptureCoordinator{
		now: time.Now, timeout: defaultHistoryCaptureTimeout, limit: defaultHistoryCreateLimit,
		window: defaultHistoryCreateWindow, maxScopes: defaultHistoryRateScopes,
		maxProcessCaptures: defaultHistoryProcessCaptures, maxLoginCaptures: defaultHistoryLoginCaptures,
		maxWaiters: defaultHistoryCaptureWaiters, inFlight: make(map[historyCaptureKey]*historyCaptureCall),
		activeByLogin: make(map[string]int), rates: make(map[historyCaptureKey]historyRateState),
	}
}

func (c *historyCaptureCoordinator) Do(ctx context.Context, key historyCaptureKey, capture func(context.Context) (historyCaptureProduct, error)) (historyCaptureProduct, error) {
	c.mu.Lock()
	if call := c.inFlight[key]; call != nil {
		if call.waiters >= c.maxWaiters {
			c.mu.Unlock()
			return historyCaptureProduct{}, errHistoryCaptureWaiters
		}
		call.waiters++
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			c.removeWaiter(key, call)
			return historyCaptureProduct{}, ctx.Err()
		case <-call.done:
			return call.product, call.err
		}
	}
	if c.activeCaptures >= c.maxProcessCaptures {
		c.mu.Unlock()
		return historyCaptureProduct{}, errHistoryProcessCaptures
	}
	if c.activeByLogin[key.User] >= c.maxLoginCaptures {
		c.mu.Unlock()
		return historyCaptureProduct{}, errHistoryLoginCaptures
	}
	if err := c.allowLocked(key, c.now()); err != nil {
		c.mu.Unlock()
		return historyCaptureProduct{}, err
	}
	call := &historyCaptureCall{done: make(chan struct{})}
	c.inFlight[key] = call
	c.activeCaptures++
	c.activeByLogin[key.User]++
	c.mu.Unlock()

	captureContext, cancel := context.WithTimeout(ctx, c.timeout)
	call.product, call.err = capture(captureContext)
	cancel()

	c.mu.Lock()
	delete(c.inFlight, key)
	c.activeCaptures--
	c.activeByLogin[key.User]--
	if c.activeByLogin[key.User] == 0 {
		delete(c.activeByLogin, key.User)
	}
	close(call.done)
	c.mu.Unlock()
	return call.product, call.err
}

func (c *historyCaptureCoordinator) removeWaiter(key historyCaptureKey, call *historyCaptureCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inFlight[key] == call && call.waiters > 0 {
		call.waiters--
	}
}

func (c *historyCaptureCoordinator) allowLocked(key historyCaptureKey, now time.Time) error {
	cutoff := now.Add(-c.window)
	for candidate, state := range c.rates {
		if state.lastSeen.Before(cutoff) {
			delete(c.rates, candidate)
		}
	}
	state, exists := c.rates[key]
	if !exists && len(c.rates) >= c.maxScopes {
		return errHistoryRateScopes
	}
	kept := state.requests[:0]
	for _, requested := range state.requests {
		if requested.After(cutoff) {
			kept = append(kept, requested)
		}
	}
	if len(kept) >= c.limit {
		state.requests = kept
		state.lastSeen = now
		c.rates[key] = state
		return errHistoryCreateRate
	}
	state.requests = append(kept, now)
	state.lastSeen = now
	c.rates[key] = state
	return nil
}

func historyNodeEstimate(lines []historyLine) int {
	nodes := len(lines)
	for _, line := range lines {
		nodes += len(line.Runs)
	}
	return nodes
}
