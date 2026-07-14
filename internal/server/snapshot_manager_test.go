package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"control-agents/internal/tmux"
)

const testHistoryViewer = ViewerID("viewer-550e8400-e29b-41d4-a716-446655440000")

func TestSnapshotManagerScopesPagesAndCursors(t *testing.T) {
	manager := newSnapshotManager(snapshotManagerConfig{PageMaxLines: 2, PageMaxBytes: 4096})
	page, err := manager.Create(testSnapshotCreate("login-a", testHistoryViewer, 5))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Lines) != 2 || page.Lines[0].Runs[0].Text != "line-3" || !page.HasMore || page.Before == "" {
		t.Fatalf("initial page = %#v", page)
	}
	older, err := manager.Page(page.SnapshotID, page.Before, "login-a", testHistoryViewer)
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Lines) != 2 || older.Lines[0].Runs[0].Text != "line-1" {
		t.Fatalf("older page = %#v", older)
	}
	if _, err := manager.Page(page.SnapshotID, page.Before+"tampered", "login-a", testHistoryViewer); !errors.Is(err, errSnapshotCursor) {
		t.Fatalf("tampered cursor error = %v", err)
	}
	second, err := manager.Create(testSnapshotCreate("login-a", ViewerID("viewer-00000000-0000-0000-0000-000000000001"), 5))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Page(second.SnapshotID, page.Before, "login-a", ViewerID("viewer-00000000-0000-0000-0000-000000000001")); !errors.Is(err, errSnapshotCursor) {
		t.Fatalf("cross-snapshot cursor error = %v", err)
	}
	if _, err := manager.Page(page.SnapshotID, "", "login-b", testHistoryViewer); !errors.Is(err, errSnapshotNotFound) {
		t.Fatalf("cross-login error = %v", err)
	}
	if _, err := manager.Page(page.SnapshotID, "", "login-a", ViewerID("viewer-00000000-0000-0000-0000-000000000000")); !errors.Is(err, errSnapshotNotFound) {
		t.Fatalf("cross-viewer error = %v", err)
	}
}

func TestSnapshotManagerReturnsDeeplyImmutablePages(t *testing.T) {
	manager := newSnapshotManager(snapshotManagerConfig{PageMaxLines: 2, PageMaxBytes: 4096})
	request := testSnapshotCreate("login-a", testHistoryViewer, 3)
	request.Lines[2].Runs[0].Style = &historyStyle{Bold: true, Foreground: "#cd0000"}
	page, err := manager.Create(request)
	if err != nil {
		t.Fatal(err)
	}
	page.Lines[1].Runs[0].Text = "mutated"
	page.Lines[1].Runs[0].Style.Bold = false
	page.Lines[1].Runs[0].Style.Foreground = "#ffffff"

	again, err := manager.Page(page.SnapshotID, "", "login-a", testHistoryViewer)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Lines[1].Runs[0].Text; got != "line-2" {
		t.Fatalf("stored text changed through returned page alias: %q", got)
	}
	style := again.Lines[1].Runs[0].Style
	if style == nil || !style.Bold || style.Foreground != "#cd0000" {
		t.Fatalf("stored style changed through returned page alias: %#v", style)
	}
}

func TestSnapshotManagerPagesTwoTenAndFiftyThousandLines(t *testing.T) {
	for _, lineCount := range []int{2000, 10000, 50000} {
		t.Run(fmt.Sprintf("%d-lines", lineCount), func(t *testing.T) {
			manager := newSnapshotManager(snapshotManagerConfig{})
			page, err := manager.Create(testSnapshotCreate("login-a", testHistoryViewer, lineCount))
			if err != nil {
				t.Fatal(err)
			}
			seen := len(page.Lines)
			newest := page.Lines[len(page.Lines)-1].Runs[0].Text
			for page.HasMore {
				if !historySnapshotIDExpr.MatchString(page.SnapshotID) || !strings.HasPrefix(page.Before, "hc_") {
					t.Fatalf("non-opaque page identity or cursor: %#v", page)
				}
				page, err = manager.Page(page.SnapshotID, page.Before, "login-a", testHistoryViewer)
				if err != nil {
					t.Fatal(err)
				}
				seen += len(page.Lines)
			}
			if seen != lineCount || newest != fmt.Sprintf("line-%d", lineCount-1) || page.Lines[0].Runs[0].Text != "line-0" {
				t.Fatalf("paged %d lines: seen=%d oldest=%q newest=%q", lineCount, seen, page.Lines[0].Runs[0].Text, newest)
			}
		})
	}
}

func TestSnapshotManagerRejectsLineBeyondPageByteEnvelope(t *testing.T) {
	manager := newSnapshotManager(snapshotManagerConfig{PageMaxBytes: 1024})
	request := testSnapshotCreate("login-a", testHistoryViewer, 1)
	request.Lines[0].Runs[0].Text = strings.Repeat("x", 1024)
	if _, err := manager.Create(request); !errors.Is(err, errHistoryANSIResourceLimit) {
		t.Fatalf("extreme line error = %v, want rendering resource limit", err)
	}
}

func TestSnapshotManagerPaginatesByEncodedByteLimit(t *testing.T) {
	const pageMaxBytes = 640
	manager := newSnapshotManager(snapshotManagerConfig{PageMaxLines: 100, PageMaxBytes: pageMaxBytes})
	request := testSnapshotCreate("login-a", testHistoryViewer, 3)
	for index := range request.Lines {
		request.Lines[index].Runs[0].Text = fmt.Sprintf("line-%d-%s", index, strings.Repeat("x", 55))
	}

	page, err := manager.Create(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Lines) != 1 || !page.HasMore || page.Before == "" {
		t.Fatalf("byte-bounded initial page = %#v", page)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > pageMaxBytes {
		t.Fatalf("encoded initial page bytes = %d, want at most %d", len(encoded), pageMaxBytes)
	}
	older, err := manager.Page(page.SnapshotID, page.Before, "login-a", testHistoryViewer)
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Lines) != 1 || !older.HasMore || older.Before == "" {
		t.Fatalf("byte-bounded older page = %#v", older)
	}
}

func TestHistoryPageEnvelopeFitsReservedByteBudget(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	page := historyPageResponse{
		SnapshotID:       "hs_" + strings.Repeat("A", 32),
		Mode:             "reflow",
		Columns:          maxInt,
		Rows:             maxInt,
		HistorySize:      maxInt,
		HistoryLimit:     maxInt,
		AlternateScreen:  true,
		OutputEpoch:      int64(^uint64(0) >> 1),
		FollowedByOutput: true,
		Lines:            []historyLine{},
		Before:           "hc_" + strings.Repeat("A", 32),
		HasMore:          true,
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > historyPageEnvelopeMaxBytes {
		t.Fatalf("history page envelope bytes = %d, reserved budget = %d", len(encoded), historyPageEnvelopeMaxBytes)
	}
}

func TestSnapshotManagerTTLDeleteAndRestartSemantics(t *testing.T) {
	manager := newSnapshotManager(snapshotManagerConfig{IdleTTL: time.Minute})
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	page, err := manager.Create(testSnapshotCreate("login-a", testHistoryViewer, 1))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := manager.Page(page.SnapshotID, "", "login-a", testHistoryViewer); !errors.Is(err, errSnapshotGone) {
		t.Fatalf("expired snapshot error = %v", err)
	}

	page, err = manager.Create(testSnapshotCreate("login-a", testHistoryViewer, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Delete(page.SnapshotID, "login-a", testHistoryViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Page(page.SnapshotID, "", "login-a", testHistoryViewer); !errors.Is(err, errSnapshotGone) {
		t.Fatalf("deleted snapshot error = %v", err)
	}

	restarted := newSnapshotManager(snapshotManagerConfig{})
	if _, err := restarted.Page(page.SnapshotID, "", "login-a", testHistoryViewer); !errors.Is(err, errSnapshotGone) {
		t.Fatalf("post-restart error = %v", err)
	}
}

func TestSnapshotManagerRefusesCapacityWithoutEviction(t *testing.T) {
	manager := newSnapshotManager(snapshotManagerConfig{PerViewer: 1, PerUser: 4, PerProcess: 4, ProcessMaxBytes: 4096, PageMaxBytes: 4096})
	first, err := manager.Create(testSnapshotCreate("login-a", testHistoryViewer, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(testSnapshotCreate("login-a", testHistoryViewer, 1)); !errors.Is(err, errSnapshotCapacity) {
		t.Fatalf("capacity error = %v", err)
	}
	if _, err := manager.Page(first.SnapshotID, "", "login-a", testHistoryViewer); err != nil {
		t.Fatalf("active snapshot was evicted: %v", err)
	}
}

func TestSnapshotManagerEnforcesEveryCountAndMemoryLimit(t *testing.T) {
	tests := []struct {
		name    string
		config  snapshotManagerConfig
		first   snapshotCreate
		second  snapshotCreate
		firstOK bool
	}{
		{
			name: "per user",
			config: snapshotManagerConfig{PerViewer: 4, PerUser: 1, PerProcess: 4,
				ProcessMaxBytes: 4096, PageMaxBytes: 4096},
			first:   testSnapshotCreate("login-a", testHistoryViewer, 1),
			second:  testSnapshotCreate("login-a", ViewerID("viewer-00000000-0000-0000-0000-000000000001"), 1),
			firstOK: true,
		},
		{
			name: "per process",
			config: snapshotManagerConfig{PerViewer: 4, PerUser: 4, PerProcess: 1,
				ProcessMaxBytes: 4096, PageMaxBytes: 4096},
			first:   testSnapshotCreate("login-a", testHistoryViewer, 1),
			second:  testSnapshotCreate("login-b", ViewerID("viewer-00000000-0000-0000-0000-000000000001"), 1),
			firstOK: true,
		},
		{
			name: "process memory",
			config: snapshotManagerConfig{PerViewer: 4, PerUser: 4, PerProcess: 4,
				ProcessMaxBytes: 1, PageMaxBytes: 4096},
			first:  testSnapshotCreate("login-a", testHistoryViewer, 1),
			second: testSnapshotCreate("login-b", ViewerID("viewer-00000000-0000-0000-0000-000000000001"), 1),
		},
		{
			name: "process node estimate",
			config: snapshotManagerConfig{PerViewer: 4, PerUser: 4, PerProcess: 4,
				ProcessMaxBytes: 4096, ProcessMaxNodes: 3, PageMaxBytes: 4096},
			first:   testSnapshotCreate("login-a", testHistoryViewer, 1),
			second:  testSnapshotCreate("login-b", ViewerID("viewer-00000000-0000-0000-0000-000000000001"), 1),
			firstOK: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := newSnapshotManager(test.config)
			first, err := manager.Create(test.first)
			if test.firstOK {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := manager.Create(test.second); !errors.Is(err, errSnapshotCapacity) {
					t.Fatalf("capacity error = %v", err)
				}
				if _, err := manager.Page(first.SnapshotID, "", test.first.User, test.first.Viewer); err != nil {
					t.Fatalf("first snapshot was evicted: %v", err)
				}
				return
			}
			if !errors.Is(err, errSnapshotCapacity) {
				t.Fatalf("initial capacity error = %v", err)
			}
		})
	}
}

func TestSnapshotManagerConsumesMeasuredNodeEstimate(t *testing.T) {
	manager := newSnapshotManager(snapshotManagerConfig{ProcessMaxNodes: 10, PageMaxBytes: 4096})
	request := testSnapshotCreate("login-a", testHistoryViewer, 1)
	request.NodeEstimate = historyNodeEstimate(request.Lines) + 1
	if _, err := manager.Create(request); !errors.Is(err, errHistoryANSIResourceLimit) {
		t.Fatalf("mismatched node estimate error = %v", err)
	}

	request.NodeEstimate = historyNodeEstimate(request.Lines)
	if _, err := manager.Create(request); err != nil {
		t.Fatalf("measured node estimate was not accepted: %v", err)
	}

	limited := newSnapshotManager(snapshotManagerConfig{ProcessMaxNodes: request.NodeEstimate - 1, PageMaxBytes: 4096})
	if _, err := limited.Create(request); !errors.Is(err, errSnapshotCapacity) {
		t.Fatalf("measured node estimate did not consume the process budget: %v", err)
	}
}

func testSnapshotCreate(user string, viewer ViewerID, lineCount int) snapshotCreate {
	lines := make([]historyLine, 0, lineCount)
	for index := 0; index < lineCount; index++ {
		lines = append(lines, historyLine{Runs: []historyRun{{Text: fmt.Sprintf("line-%d", index)}}})
	}
	return snapshotCreate{
		User:       user,
		Viewer:     viewer,
		SessionRef: testSessionRef,
		PaneRef:    PaneRef("p_abcdefghijklmnopqrstuvwx"),
		Generation: PaneGeneration{TmuxServerStart: "100", TmuxServerPID: 101, PaneID: "%42"},
		Mode:       "reflow",
		Capture: tmux.HistoryCapture{
			Before: tmux.HistoryMetadata{Columns: 120, Rows: 40, HistorySize: lineCount, HistoryLimit: 50000, HistoryBytes: int64(lineCount), OutputEpoch: 1000},
			After:  tmux.HistoryMetadata{Columns: 120, Rows: 40, HistorySize: lineCount, HistoryLimit: 50000, HistoryBytes: int64(lineCount), OutputEpoch: 1000},
		},
		Lines: lines,
	}
}
