package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"control-agents/internal/config"
	"control-agents/internal/registry"
	managedsession "control-agents/internal/session"
	"control-agents/internal/tmux"
)

func TestSessionListAndCreateResponsesHideLifecycleInternals(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	cookies := loginCookies(t, server)

	list := authenticatedLifecycleRequest(t, server, cookies, http.MethodGet, "/api/sessions", "", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %q", list.Code, list.Body.String())
	}
	if !strings.Contains(list.Body.String(), `"id":"`+testSessionRef+`"`) || !strings.Contains(list.Body.String(), `"name":"alpha"`) {
		t.Fatalf("opaque session response missing healthy session: %q", list.Body.String())
	}
	assertNoPrivateSessionFields(t, list.Body.Bytes())

	create := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, "/api/sessions", `{"name":"beta"}`, "https://control.test")
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %q", create.Code, create.Body.String())
	}
	assertNoPrivateSessionFields(t, create.Body.Bytes())
	var response struct {
		Created bool            `json:"created"`
		Session sessionResponse `json:"session"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Created || !registry.ValidPublicRef(string(response.Session.ID)) || string(response.Session.ID) == "beta" || response.Session.Name != "beta" || response.Session.CWD != "/service/home" || response.Session.ActivePaneRef == "" {
		t.Fatalf("create response = %#v", response)
	}

	duplicate := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, "/api/sessions", `{"name":"beta"}`, "https://control.test")
	if duplicate.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d, body = %q", duplicate.Code, duplicate.Body.String())
	}
	if strings.Contains(duplicate.Body.String(), `"created":true`) {
		t.Fatalf("duplicate response reported creation: %q", duplicate.Body.String())
	}
	if lifecycle.StartCount() != 1 {
		t.Fatalf("session starts = %d, want 1", lifecycle.StartCount())
	}
}

func TestCreateSessionRejectsInvalidBodiesWithoutLifecycleSideEffects(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"name":`},
		{name: "empty", body: `{"name":""}`},
		{name: "missing", body: `{}`},
		{name: "null", body: `{"name":null}`},
		{name: "wrong type", body: `{"name":42}`},
		{name: "duplicate name", body: `{"name":"alpha","name":"beta"}`},
		{name: "unknown field", body: `{"name":"alpha","cwd":"/tmp"}`},
		{name: "invalid name", body: `{"name":"bad name"}`},
		{name: "multiple values", body: `{"name":"alpha"} {"name":"beta"}`},
		{name: "oversized", body: `{"name":"alpha","padding":"` + strings.Repeat("x", lifecycleRequestMaxBytes) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := newFakeWebLifecycle()
			server := newLifecycleTestServer(t, lifecycle, 32)
			cookies := loginCookies(t, server)
			response := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, "/api/sessions", test.body, "https://control.test")
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
			}
			if lifecycle.CreateCalls() != 0 || lifecycle.StartCount() != 0 {
				t.Fatalf("create calls/starts = %d/%d", lifecycle.CreateCalls(), lifecycle.StartCount())
			}
		})
	}
}

func TestTerminalMutationBodiesAreBoundedAndStrictlyDecoded(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	runner := &terminalAPIRunner{}
	server.tmux = tmux.NewClientWithRunner(runner)
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))
	runner.resetCalls()

	base := "/api/sessions/" + testSessionRef
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "keys unknown field", path: base + "/keys", body: `{"key":"enter","paneRef":"` + string(paneRef) + `","extra":true}`},
		{name: "keys wrong field case", path: base + "/keys", body: `{"Key":"enter","paneRef":"` + string(paneRef) + `"}`},
		{name: "keys duplicate field", path: base + "/keys", body: `{"key":"enter","key":"escape","paneRef":"` + string(paneRef) + `"}`},
		{name: "keys trailing value", path: base + "/keys", body: `{"key":"enter","paneRef":"` + string(paneRef) + `"} {}`},
		{name: "keys oversized string", path: base + "/keys", body: `{"key":"` + strings.Repeat("x", maxKeyNameBytes+1) + `","paneRef":"` + string(paneRef) + `"}`},
		{name: "keys oversized body", path: base + "/keys", body: `{"text":"` + strings.Repeat("x", lifecycleRequestMaxBytes) + `","paneRef":"` + string(paneRef) + `"}`},
		{name: "resize unknown field", path: base + "/resize", body: `{"mode":"fixed","paneRef":"` + string(paneRef) + `","automatic":true}`},
		{name: "resize oversized string", path: base + "/resize", body: `{"mode":"` + strings.Repeat("x", maxResizeModeBytes+1) + `","paneRef":"` + string(paneRef) + `"}`},
		{name: "viewer unknown field", path: base + "/resize/viewer", body: `{"viewerId":"` + testViewerID + `","paneRef":"` + string(paneRef) + `","width":80,"height":24,"follow":true}`},
		{name: "viewer oversized id", path: base + "/resize/viewer", body: `{"viewerId":"` + strings.Repeat("v", 129) + `","paneRef":"` + string(paneRef) + `","width":80,"height":24}`},
		{name: "viewer oversized dimensions", path: base + "/resize/viewer", body: `{"viewerId":"` + testViewerID + `","paneRef":"` + string(paneRef) + `","width":1001,"height":24}`},
		{name: "control unknown field", path: base + "/tmux-control", body: `{"action":"new-window","paneRef":"` + string(paneRef) + `","command":"sh"}`},
		{name: "control duplicate action", path: base + "/tmux-control", body: `{"action":"new-window","action":"split-horizontal","paneRef":"` + string(paneRef) + `"}`},
		{name: "control oversized string", path: base + "/tmux-control", body: `{"action":"rename-window","name":"` + strings.Repeat("n", maxControlWindowNameBytes+1) + `","paneRef":"` + string(paneRef) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, test.path, test.body, "https://control.test")
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status/body = %d/%q", response.Code, response.Body.String())
			}
		})
	}
	for _, call := range runner.callsSnapshot() {
		if len(call) > 2 && (call[0] == "run" || call[0] == "input") {
			t.Fatalf("invalid mutation body reached tmux: %#v", call)
		}
	}
}

func TestBodylessAuthenticatedMutationsRejectBodies(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	cookies := loginCookies(t, server)

	logout := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, "/logout", `{}`, "https://control.test")
	if logout.Code != http.StatusBadRequest {
		t.Fatalf("logout body status/body = %d/%q", logout.Code, logout.Body.String())
	}
	deleted := historyAPIRequest(t, server, cookies, http.MethodDelete,
		"/api/v1/history-snapshots/hs_missing", `{}`, testHistoryViewer)
	if deleted.Code != http.StatusBadRequest {
		t.Fatalf("history delete body status/body = %d/%q", deleted.Code, deleted.Body.String())
	}
}

func TestCreateSessionMapsConflictLimitAndDependencyFailures(t *testing.T) {
	tests := []struct {
		name        string
		lifecycle   *fakeWebLifecycle
		maximum     int
		requestName string
		want        int
	}{
		{
			name:        "unmanaged conflict",
			lifecycle:   fakeWebLifecycleWithCreateError("taken", managedsession.ErrConflict),
			maximum:     32,
			requestName: "taken",
			want:        http.StatusConflict,
		},
		{
			name:        "dependency failure",
			lifecycle:   fakeWebLifecycleWithCreateError("broken", managedsession.ErrBridgeIncomplete),
			maximum:     32,
			requestName: "broken",
			want:        http.StatusServiceUnavailable,
		},
		{
			name:        "creation limit",
			lifecycle:   newFakeWebLifecycle(managedWebSession("existing")),
			maximum:     1,
			requestName: "other",
			want:        http.StatusConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newLifecycleTestServer(t, test.lifecycle, test.maximum)
			cookies := loginCookies(t, server)
			body := `{"name":"` + test.requestName + `"}`
			response := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, "/api/sessions", body, "https://control.test")
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body = %q", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestLifecycleLogsOmitCanonicalSessionNameAndErrorText(t *testing.T) {
	const canary = "SESSION-CANARY-4f927d"
	lifecycle := fakeWebLifecycleWithCreateError(canary, errors.Join(managedsession.ErrDependency, errors.New(canary)))
	server := newLifecycleTestServer(t, lifecycle, 32)
	logs := &bytes.Buffer{}
	server.logger = slog.New(slog.NewJSONHandler(logs, nil))
	cookies := loginCookies(t, server)

	response := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, "/api/sessions", `{"name":"`+canary+`"}`, "https://control.test")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	if strings.Contains(logs.String(), canary) || strings.Contains(logs.String(), `"error"`) || !strings.Contains(logs.String(), `"reason_code"`) {
		t.Fatalf("lifecycle log is not content-free: %s", logs.String())
	}
}

func TestCreateAndTerminateRequireAuthenticationAndSameOrigin(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	cookies := loginCookies(t, server)
	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		cookies []*http.Cookie
		origin  string
		want    int
	}{
		{name: "unauthenticated create", method: http.MethodPost, path: "/api/sessions", body: `{"name":"beta"}`, origin: "https://control.test", want: http.StatusUnauthorized},
		{name: "cross-origin create", method: http.MethodPost, path: "/api/sessions", body: `{"name":"beta"}`, cookies: cookies, origin: "https://evil.test", want: http.StatusForbidden},
		{name: "unauthenticated terminate", method: http.MethodDelete, path: "/api/sessions/" + testSessionRef, body: `{"confirmName":"alpha"}`, origin: "https://control.test", want: http.StatusUnauthorized},
		{name: "cross-origin terminate", method: http.MethodDelete, path: "/api/sessions/" + testSessionRef, body: `{"confirmName":"alpha"}`, cookies: cookies, origin: "https://evil.test", want: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := authenticatedLifecycleRequest(t, server, test.cookies, test.method, test.path, test.body, test.origin)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body = %q", response.Code, test.want, response.Body.String())
			}
		})
	}
	if lifecycle.CreateCalls() != 0 || lifecycle.TerminateCalls() != 0 {
		t.Fatalf("create/terminate calls = %d/%d", lifecycle.CreateCalls(), lifecycle.TerminateCalls())
	}
}

func TestTerminateSessionRequiresExactConfirmationAndClearsResizeState(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	lifecycle.terminateArtifactCleanup = func(string) {
		_ = os.Remove(filepath.Join(server.resize.dir, testSessionRef+".json"))
	}
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))
	if err := server.resize.Save(testSessionRef, resizeSettings{Mode: resizeModeFixed}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.resize.RecordViewer(testSessionRef, resizeViewer{ID: testViewerID, Width: 100, Height: 30}); err != nil {
		t.Fatal(err)
	}

	wrongBody := `{"confirmName":"other","paneRef":"` + string(paneRef) + `"}`
	wrong := authenticatedLifecycleRequest(t, server, cookies, http.MethodDelete, "/api/sessions/"+testSessionRef, wrongBody, "https://control.test")
	if wrong.Code != http.StatusBadRequest || lifecycle.TerminateCalls() != 0 {
		t.Fatalf("wrong confirmation status/calls = %d/%d", wrong.Code, lifecycle.TerminateCalls())
	}
	if len(server.resize.Viewers(testSessionRef)) != 1 {
		t.Fatal("wrong confirmation removed active viewer state")
	}

	deleteBody := `{"confirmName":"alpha","paneRef":"` + string(paneRef) + `"}`
	deleted := authenticatedLifecycleRequest(t, server, cookies, http.MethodDelete, "/api/sessions/"+testSessionRef, deleteBody, "https://control.test")
	if deleted.Code != http.StatusNoContent || deleted.Body.Len() != 0 {
		t.Fatalf("delete status/body = %d/%q", deleted.Code, deleted.Body.String())
	}
	if lifecycle.TerminateCalls() != 1 || len(server.resize.Viewers(testSessionRef)) != 0 {
		t.Fatalf("terminate calls/viewers = %d/%d", lifecycle.TerminateCalls(), len(server.resize.Viewers(testSessionRef)))
	}
	if _, err := os.Stat(filepath.Join(server.resize.dir, testSessionRef+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resize settings remain: %v", err)
	}

	repeated := authenticatedLifecycleRequest(t, server, cookies, http.MethodDelete, "/api/sessions/"+testSessionRef, deleteBody, "https://control.test")
	if repeated.Code != http.StatusNotFound {
		t.Fatalf("repeated delete status = %d, body = %q", repeated.Code, repeated.Body.String())
	}
}

func TestTerminateSessionMapsOperationalLifecycleFailureToServiceUnavailable(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	lifecycle.terminateError = managedsession.ErrDependency
	server := newLifecycleTestServer(t, lifecycle, 32)
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))
	if _, err := server.resize.RecordViewer(testSessionRef, resizeViewer{ID: testViewerID, Width: 100, Height: 30}); err != nil {
		t.Fatal(err)
	}

	body := `{"confirmName":"alpha","paneRef":"` + string(paneRef) + `"}`
	response := authenticatedLifecycleRequest(t, server, cookies, http.MethodDelete, "/api/sessions/"+testSessionRef, body, "https://control.test")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body = %q", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	if !lifecycle.Has("alpha") || len(server.resize.Viewers(testSessionRef)) != 1 {
		t.Fatal("operational termination failure modified session or resize state")
	}
}

func TestConcurrentWebCreatesReturnOneCreatedAndOneSelected(t *testing.T) {
	lifecycle := newFakeWebLifecycle()
	server := newLifecycleTestServer(t, lifecycle, 32)
	cookies := loginCookies(t, server)
	responses := make(chan int, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, "/api/sessions", `{"name":"shared"}`, "https://control.test")
			responses <- response.Code
		}()
	}
	wait.Wait()
	close(responses)
	statuses := make([]int, 0, 2)
	for status := range responses {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)
	if statuses[0] != http.StatusOK || statuses[1] != http.StatusCreated {
		t.Fatalf("statuses = %v, want [%d %d]", statuses, http.StatusOK, http.StatusCreated)
	}
	if lifecycle.StartCount() != 1 {
		t.Fatalf("session starts = %d, want 1", lifecycle.StartCount())
	}
}

func TestConcurrentWebCreatesCannotOversubscribeLimit(t *testing.T) {
	lifecycle := newFakeWebLifecycle()
	server := newLifecycleTestServer(t, lifecycle, 1)
	cookies := loginCookies(t, server)
	responses := make(chan int, 2)
	var wait sync.WaitGroup
	for _, name := range []string{"alpha", "beta"} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, "/api/sessions", `{"name":"`+name+`"}`, "https://control.test")
			responses <- response.Code
		}()
	}
	wait.Wait()
	close(responses)
	statuses := make([]int, 0, 2)
	for status := range responses {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)
	if statuses[0] != http.StatusCreated || statuses[1] != http.StatusConflict {
		t.Fatalf("statuses = %v, want [%d %d]", statuses, http.StatusCreated, http.StatusConflict)
	}
	if lifecycle.StartCount() != 1 {
		t.Fatalf("session starts = %d, want 1", lifecycle.StartCount())
	}
}

func TestExistingSessionRemainsSelectableAboveWebLimit(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"), managedWebSession("beta"))
	server := newLifecycleTestServer(t, lifecycle, 1)
	cookies := loginCookies(t, server)
	response := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, "/api/sessions", `{"name":"alpha"}`, "https://control.test")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"created":true`) {
		t.Fatalf("select status/body = %d/%q", response.Code, response.Body.String())
	}
}

func TestCreateRacingWithTerminateIsSerializedPerSession(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	blockCreate := make(chan struct{})
	createEntered := make(chan struct{})
	lifecycle.blockCreate = blockCreate
	lifecycle.createEntered = createEntered
	server := newLifecycleTestServer(t, lifecycle, 32)
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))

	createResult := make(chan int, 1)
	go func() {
		response := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost, "/api/sessions", `{"name":"alpha"}`, "https://control.test")
		createResult <- response.Code
	}()
	<-createEntered
	deleteResult := make(chan int, 1)
	go func() {
		body := `{"confirmName":"alpha","paneRef":"` + string(paneRef) + `"}`
		response := authenticatedLifecycleRequest(t, server, cookies, http.MethodDelete, "/api/sessions/"+testSessionRef, body, "https://control.test")
		deleteResult <- response.Code
	}()
	close(blockCreate)

	if status := <-createResult; status != http.StatusOK {
		t.Fatalf("create status = %d, want %d", status, http.StatusOK)
	}
	if status := <-deleteResult; status != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d", status, http.StatusNoContent)
	}
	if lifecycle.Has("alpha") {
		t.Fatal("create followed by serialized terminate left the session live")
	}
}

func TestTerminateSessionRejectsInvalidPathAndBody(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))
	tests := []struct {
		path string
		body string
	}{
		{path: "/api/sessions/-invalid", body: `{"confirmName":"-invalid"}`},
		{path: "/api/sessions/" + testSessionRef + "/scroll", body: `{"confirmName":"alpha"}`},
		{path: "/api/sessions/" + testSessionRef, body: `{}`},
		{path: "/api/sessions/" + testSessionRef, body: `{"confirmName":"alpha","confirmName":"alpha","paneRef":"` + string(paneRef) + `"}`},
		{path: "/api/sessions/" + testSessionRef, body: `{"confirmName":"alpha","paneRef":"` + string(paneRef) + `","force":true}`},
		{path: "/api/sessions/" + testSessionRef, body: `{"confirmName":"alpha","paneRef":"` + string(paneRef) + `"} true`},
	}
	for _, test := range tests {
		response := authenticatedLifecycleRequest(t, server, cookies, http.MethodDelete, test.path, test.body, "https://control.test")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("DELETE %s status = %d, body = %q", test.path, response.Code, response.Body.String())
		}
	}
	if lifecycle.TerminateCalls() != 0 {
		t.Fatalf("terminate calls = %d, want 0", lifecycle.TerminateCalls())
	}
}

func assertNoPrivateSessionFields(t *testing.T, body []byte) {
	t.Helper()
	for _, field := range []string{`"pid"`, `"socket"`, `"tmuxName"`, `"paneId"`, `"windowId"`, `%42`, `@7`} {
		if strings.Contains(string(body), field) {
			t.Fatalf("response exposes private field %s: %s", field, body)
		}
	}
}

func newLifecycleTestServer(t *testing.T, lifecycle *fakeWebLifecycle, maximum int) *Server {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "state")
	store := registry.NewStore(stateDir)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{
		BindAddr:         "127.0.0.1",
		Port:             8080,
		Password:         "secret",
		StateDir:         stateDir,
		CookieTTL:        60,
		MaxSessions:      maximum,
		SnapshotMaxBytes: tmux.DefaultSnapshotBytes,
	}
	server, err := newServerWithLifecycle(cfg, logger, store, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	server.tmux = tmux.NewClientWithRunner(&terminalAPIRunner{})
	return server
}

func authenticatedLifecycleRequest(t *testing.T, handler http.Handler, cookies []*http.Cookie, method, path, body, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "https://control.test"+path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	if len(cookies) > 0 && isUnsafeMethod(method) {
		request.Header.Set(csrfHeader, csrfTokenForCookies(t, handler, cookies))
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type fakeWebLifecycle struct {
	mu sync.Mutex

	sessions                 map[string]registry.Session
	createErrors             map[string]error
	createCalls              int
	terminateCalls           int
	startCount               int
	blockCreate              chan struct{}
	createEntered            chan struct{}
	terminateError           error
	terminateArtifactCleanup func(string)
}

func newFakeWebLifecycle(sessions ...registry.Session) *fakeWebLifecycle {
	fake := &fakeWebLifecycle{
		sessions:     make(map[string]registry.Session),
		createErrors: make(map[string]error),
	}
	for _, session := range sessions {
		fake.sessions[session.ID] = session
	}
	return fake
}

func fakeWebLifecycleWithCreateError(name string, err error) *fakeWebLifecycle {
	fake := newFakeWebLifecycle()
	fake.createErrors[name] = err
	return fake
}

func (f *fakeWebLifecycle) List(context.Context) ([]registry.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sessions := make([]registry.Session, 0, len(f.sessions))
	for _, session := range f.sessions {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })
	return sessions, nil
}

func (f *fakeWebLifecycle) CreateOrSelect(_ context.Context, name string) (registry.Session, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	if f.createEntered != nil {
		close(f.createEntered)
		f.createEntered = nil
	}
	if f.blockCreate != nil {
		<-f.blockCreate
	}
	if err := f.createErrors[name]; err != nil {
		return registry.Session{}, false, err
	}
	if existing, ok := f.sessions[name]; ok {
		return existing, false, nil
	}
	managed := managedWebSession(name)
	f.sessions[name] = managed
	f.startCount++
	return managed, true, nil
}

func (f *fakeWebLifecycle) TerminateWithCleanup(_ context.Context, sessionID string, cleanup func()) error {
	return f.TerminateChecked(context.Background(), sessionID, "", nil, cleanup)
}

func (f *fakeWebLifecycle) TerminateChecked(_ context.Context, sessionID, expectedPublicRef string, verify func(registry.Session) error, cleanup func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminateCalls++
	if f.terminateError != nil {
		return f.terminateError
	}
	managed, ok := f.sessions[sessionID]
	if !ok {
		return managedsession.ErrNotFound
	}
	if expectedPublicRef != "" && managed.PublicRef != expectedPublicRef {
		return managedsession.ErrNotFound
	}
	if verify != nil {
		if err := verify(managed); err != nil {
			return err
		}
	}
	delete(f.sessions, sessionID)
	if cleanup != nil {
		cleanup()
	}
	if f.terminateArtifactCleanup != nil {
		f.terminateArtifactCleanup(sessionID)
	}
	return nil
}

func (f *fakeWebLifecycle) WithSession(_ context.Context, sessionID string, use func(registry.Session) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	managed, ok := f.sessions[sessionID]
	if !ok {
		return managedsession.ErrNotFound
	}
	return use(managed)
}

func (f *fakeWebLifecycle) CreateCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCalls
}

func (f *fakeWebLifecycle) TerminateCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.terminateCalls
}

func (f *fakeWebLifecycle) StartCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCount
}

func (f *fakeWebLifecycle) Has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.sessions[id]
	return ok
}

func managedWebSession(id string) registry.Session {
	digest := sha256.Sum256([]byte(id))
	publicRef := hex.EncodeToString(digest[:16])
	if id == "alpha" {
		publicRef = testSessionRef
	}
	return registry.Session{
		ID:        id,
		Name:      id,
		PublicRef: publicRef,
		TmuxName:  id,
		Socket:    "/private/state/sockets/" + id + ".sock",
		PID:       4242,
		CWD:       "/service/home",
		CreatedAt: "2026-07-13T12:00:00Z",
	}
}

func lifecyclePaneRef(t *testing.T, server *Server, managed registry.Session) PaneRef {
	t.Helper()
	response, err := server.publicSession(context.Background(), managed)
	if err != nil {
		t.Fatal(err)
	}
	return response.ActivePaneRef
}
