package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"control-agents/internal/tmux"
)

func TestHistorySnapshotAPIIsImmutableScopedAndExplicitlyDeleted(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	runner := &terminalAPIRunner{
		capture: "safe <script>window.pwned=1</script>\n\x1b[31mred\x1b[0m\x1b]8;;https://evil.example\x07link\x1b]8;;\x1b\\\nlast\n",
		historyMetadata: []string{
			"120\x1f40\x1f100\x1f50000\x1f4096\x1f0\n",
			"120\x1f40\x1f101\x1f50000\x1f4102\x1f0\n",
		},
	}
	server.tmux = tmux.NewClientWithRunner(runner)
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))
	runner.resetCalls()

	created := historyAPIRequest(t, server, cookies, http.MethodPost,
		"/api/v1/panes/"+string(paneRef)+"/history-snapshots", `{"mode":"reflow"}`, testHistoryViewer)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%q", created.Code, created.Body.String())
	}
	if created.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", created.Header().Get("Cache-Control"))
	}
	var page historyPageResponse
	if err := json.Unmarshal(created.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.SnapshotID == "" || page.Mode != "reflow" || page.Columns != 120 || page.Rows != 40 || !page.FollowedByOutput {
		t.Fatalf("create page = %#v", page)
	}
	encoded := created.Body.String()
	if !strings.Contains(encoded, `safe \u003cscript\u003ewindow.pwned=1\u003c/script\u003e`) || strings.Contains(encoded, "evil.example") || !strings.Contains(encoded, `"foreground":"#cd0000"`) {
		t.Fatalf("unsafe or incomplete structured history response: %s", encoded)
	}
	captureCalls := 0
	for _, call := range runner.callsSnapshot() {
		if len(call) > 2 && call[2] == "capture-pane" {
			captureCalls++
			want := []string{"output-limited", "tmux", "capture-pane", "-p", "-e", "-J", "-S", "-", "-E", "-", "-t", "%42"}
			if !containsCall([][]string{call}, want) {
				t.Fatalf("capture call = %#v", call)
			}
		}
	}
	if captureCalls != 1 {
		t.Fatalf("capture-pane calls = %d, want exactly one", captureCalls)
	}

	read := historyAPIRequest(t, server, cookies, http.MethodGet,
		"/api/v1/history-snapshots/"+page.SnapshotID+"/pages", "", testHistoryViewer)
	if read.Code != http.StatusOK || read.Body.String() != created.Body.String() {
		t.Fatalf("immutable page status/body = %d/%q, want %q", read.Code, read.Body.String(), created.Body.String())
	}
	if got := countCaptureCalls(runner.callsSnapshot()); got != 1 {
		t.Fatalf("page recaptured pane: capture calls = %d", got)
	}
	activity := historyAPIRequest(t, server, cookies, http.MethodGet,
		"/api/v1/history-snapshots/"+page.SnapshotID, "", testHistoryViewer)
	if activity.Code != http.StatusOK || !strings.Contains(activity.Body.String(), `"newOutput":true`) {
		t.Fatalf("activity status/body = %d/%q", activity.Code, activity.Body.String())
	}
	if got := countCaptureCalls(runner.callsSnapshot()); got != 1 {
		t.Fatalf("activity recaptured pane: capture calls = %d", got)
	}

	otherViewer := historyAPIRequest(t, server, cookies, http.MethodGet,
		"/api/v1/history-snapshots/"+page.SnapshotID+"/pages", "", ViewerID("viewer-00000000-0000-0000-0000-000000000000"))
	if otherViewer.Code != http.StatusNotFound {
		t.Fatalf("cross-viewer status = %d", otherViewer.Code)
	}
	otherLogin := loginCookies(t, server)
	crossLogin := historyAPIRequest(t, server, otherLogin, http.MethodGet,
		"/api/v1/history-snapshots/"+page.SnapshotID+"/pages", "", testHistoryViewer)
	if crossLogin.Code != http.StatusNotFound {
		t.Fatalf("cross-login status = %d", crossLogin.Code)
	}

	deleted := historyAPIRequest(t, server, cookies, http.MethodDelete,
		"/api/v1/history-snapshots/"+page.SnapshotID, "", testHistoryViewer)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status/body = %d/%q", deleted.Code, deleted.Body.String())
	}
	gone := historyAPIRequest(t, server, cookies, http.MethodGet,
		"/api/v1/history-snapshots/"+page.SnapshotID+"/pages", "", testHistoryViewer)
	if gone.Code != http.StatusGone {
		t.Fatalf("deleted snapshot status = %d, want gone", gone.Code)
	}
}

func TestHistorySnapshotAPIRestartAndLegacyRoutes(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	runner := &terminalAPIRunner{capture: "snapshot\n"}
	server.tmux = tmux.NewClientWithRunner(runner)
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))
	created := historyAPIRequest(t, server, cookies, http.MethodPost,
		"/api/v1/panes/"+string(paneRef)+"/history-snapshots", `{"mode":"fixed"}`, testHistoryViewer)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%q", created.Code, created.Body.String())
	}
	var page historyPageResponse
	if err := json.Unmarshal(created.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}

	server.snapshots = newSnapshotManager(snapshotManagerConfig{})
	afterRestart := historyAPIRequest(t, server, cookies, http.MethodGet,
		"/api/v1/history-snapshots/"+page.SnapshotID+"/pages", "", testHistoryViewer)
	if afterRestart.Code != http.StatusGone {
		t.Fatalf("post-restart status = %d, want gone", afterRestart.Code)
	}

	for _, suffix := range []string{"scroll", "capture"} {
		request := httptest.NewRequest(http.MethodGet, "https://control.test/api/sessions/"+testSessionRef+"/"+suffix, nil)
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("legacy %s status = %d, want unavailable", suffix, response.Code)
		}
	}
}

func TestHistorySnapshotAPIRejectsOversizedCapture(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	server.cfg.SnapshotMaxBytes = 16
	runner := &terminalAPIRunner{capture: strings.Repeat("x", 17)}
	server.tmux = tmux.NewClientWithRunner(runner)
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))
	runner.resetCalls()

	created := historyAPIRequest(t, server, cookies, http.MethodPost,
		"/api/v1/panes/"+string(paneRef)+"/history-snapshots", `{"mode":"reflow"}`, testHistoryViewer)
	if created.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status/body = %d/%q", created.Code, created.Body.String())
	}
	if runner.maxLimitedRead() != 17 {
		t.Fatalf("bounded capture read = %d, want limit plus one", runner.maxLimitedRead())
	}
}

func TestHistorySnapshotAPIForcesAlternateScreenToFixedGrid(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	runner := &terminalAPIRunner{
		capture: "alternate\n",
		historyMetadata: []string{
			"120\x1f40\x1f100\x1f50000\x1f4096\x1f0\n",
			"120\x1f40\x1f100\x1f50000\x1f4096\x1f1\n",
		},
	}
	server.tmux = tmux.NewClientWithRunner(runner)
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))

	created := historyAPIRequest(t, server, cookies, http.MethodPost,
		"/api/v1/panes/"+string(paneRef)+"/history-snapshots", `{"mode":"reflow"}`, testHistoryViewer)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%q", created.Code, created.Body.String())
	}
	var page historyPageResponse
	if err := json.Unmarshal(created.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Mode != "fixed" || !page.AlternateScreen {
		t.Fatalf("alternate-screen page = %#v", page)
	}
}

func TestHistorySnapshotAPIMonotonicEpochDetectsSameSecondRedrawDuringCapture(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	runner := &terminalAPIRunner{
		capture: "immutable redraw\n",
		historyMetadata: []string{
			"120\x1f40\x1f0\x1f50000\x1f4096\x1f1\n",
			"120\x1f40\x1f0\x1f50000\x1f4096\x1f1\n",
		},
	}
	server.tmux = tmux.NewClientWithRunner(runner)
	runner.setCaptureHook(func() {
		recorded := make(chan struct{})
		go func() {
			server.activity.Record(testSessionRef, 7)
			close(recorded)
		}()
		<-recorded
	})
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))

	created := historyAPIRequest(t, server, cookies, http.MethodPost,
		"/api/v1/panes/"+string(paneRef)+"/history-snapshots", `{"mode":"fixed"}`, testHistoryViewer)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%q", created.Code, created.Body.String())
	}
	var page historyPageResponse
	if err := json.Unmarshal(created.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if !page.FollowedByOutput || page.OutputEpoch != 7 {
		t.Fatalf("same-second capture page = %#v", page)
	}
	if got := countCaptureCalls(runner.callsSnapshot()); got != 1 {
		t.Fatalf("same-second redraw capture calls = %d, want exactly one", got)
	}

	unchanged := historyAPIRequest(t, server, cookies, http.MethodGet,
		"/api/v1/history-snapshots/"+page.SnapshotID, "", testHistoryViewer)
	if unchanged.Code != http.StatusOK || !strings.Contains(unchanged.Body.String(), `"newOutput":false`) {
		t.Fatalf("unchanged activity status/body = %d/%q", unchanged.Code, unchanged.Body.String())
	}
	server.activity.Record(testSessionRef, 1)
	redrawn := historyAPIRequest(t, server, cookies, http.MethodGet,
		"/api/v1/history-snapshots/"+page.SnapshotID, "", testHistoryViewer)
	if redrawn.Code != http.StatusOK || !strings.Contains(redrawn.Body.String(), `"newOutput":true`) {
		t.Fatalf("redraw activity status/body = %d/%q", redrawn.Code, redrawn.Body.String())
	}
	if got := countCaptureCalls(runner.callsSnapshot()); got != 1 {
		t.Fatalf("activity check recaptured pane: capture calls = %d", got)
	}
}

func TestHistorySnapshotAPIDeletesSnapshotWhenPaneBecomesStale(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	runner := &terminalAPIRunner{capture: "snapshot\n"}
	server.tmux = tmux.NewClientWithRunner(runner)
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))

	created := historyAPIRequest(t, server, cookies, http.MethodPost,
		"/api/v1/panes/"+string(paneRef)+"/history-snapshots", `{"mode":"reflow"}`, testHistoryViewer)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d/%q", created.Code, created.Body.String())
	}
	var page historyPageResponse
	if err := json.Unmarshal(created.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	runner.setTopologyError(errors.New("tmux generation changed"))
	stale := historyAPIRequest(t, server, cookies, http.MethodGet,
		"/api/v1/history-snapshots/"+page.SnapshotID+"/pages", "", testHistoryViewer)
	if stale.Code != http.StatusGone {
		t.Fatalf("stale status/body = %d/%q", stale.Code, stale.Body.String())
	}
	runner.setTopologyError(nil)
	gone := historyAPIRequest(t, server, cookies, http.MethodGet,
		"/api/v1/history-snapshots/"+page.SnapshotID+"/pages", "", testHistoryViewer)
	if gone.Code != http.StatusGone {
		t.Fatalf("deleted stale snapshot status = %d, want gone", gone.Code)
	}
}

func historyAPIRequest(t *testing.T, server http.Handler, cookies []*http.Cookie, method, path, body string, viewer ViewerID) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "https://control.test"+path, strings.NewReader(body))
	request.Header.Set(historyViewerHeader, string(viewer))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost || method == http.MethodDelete {
		request.Header.Set("Origin", "https://control.test")
		request.Header.Set(csrfHeader, csrfTokenForCookies(t, server, cookies))
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func countCaptureCalls(calls [][]string) int {
	count := 0
	for _, call := range calls {
		if len(call) > 2 && call[2] == "capture-pane" {
			count++
		}
	}
	return count
}
