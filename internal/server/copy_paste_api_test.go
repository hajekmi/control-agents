package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"control-agents/internal/auth"
	"control-agents/internal/config"
	"control-agents/internal/registry"
	"control-agents/internal/tmux"
)

func TestPasteAPILoadsMetacharactersOnlyThroughStdin(t *testing.T) {
	runner := &terminalAPIRunner{}
	server, managed, _, paneRef := newTerminalAPITestServer(t, runner, tmux.DefaultSnapshotBytes)
	text := " space \"quote\";\n$(touch /tmp/pwned) `id` & | > < * ? "
	token, cookie := issuePasteTokenForTest(t, server, managed, paneRef, text)
	body := `{"text":` + mustJSONQuote(t, text) + `,"paneRef":"` + string(paneRef) + `","token":"` + token + `"}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/"+testSessionRef+"/paste", strings.NewReader(body))
	request.AddCookie(cookie)

	server.handlePasteAPI(recorder, request, testSessionRef, managed)

	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"ok":true}` {
		t.Fatalf("status/body = %d/%q", recorder.Code, recorder.Body.String())
	}
	var loadCall, pasteCall, deleteCall []string
	for _, call := range runner.callsSnapshot() {
		if len(call) > 2 && call[0] == "input" && call[2] == "load-buffer" {
			loadCall = call
		}
		if len(call) > 2 && call[0] == "run" && call[2] == "paste-buffer" {
			pasteCall = call
		}
		if len(call) > 2 && call[0] == "run" && call[2] == "delete-buffer" {
			deleteCall = call
		}
	}
	if len(loadCall) != 6 || loadCall[5] != "-" || runner.inputSnapshot() != text {
		t.Fatalf("load-buffer call/input = %#v/%q", loadCall, runner.inputSnapshot())
	}
	if len(pasteCall) == 0 || pasteCall[len(pasteCall)-1] != "%42" {
		t.Fatalf("paste-buffer call = %#v", pasteCall)
	}
	if len(deleteCall) == 0 || deleteCall[4] != loadCall[4] {
		t.Fatalf("delete-buffer call = %#v", deleteCall)
	}
	for _, call := range runner.callsSnapshot() {
		if strings.Contains(strings.Join(call, "\x00"), text) {
			t.Fatalf("paste content entered command arguments: %#v", call)
		}
	}
}

func TestPasteAPIBoundaryAndCharacterMatrix(t *testing.T) {
	accepted := []struct {
		name string
		text string
	}{
		{name: "one byte", text: "x"},
		{name: "exactly 64 KiB", text: strings.Repeat("a", tmux.MaxPasteBytes)},
		{name: "emoji at byte boundary", text: strings.Repeat("a", tmux.MaxPasteBytes-len("🙂")) + "🙂"},
		{name: "CRLF and trailing newline", text: "first\r\nsecond\r\n"},
		{name: "multiline", text: "first\nsecond"},
		{name: "tab and control characters", text: "tab\tescape\x1bdelete\x7f"},
	}
	for _, test := range accepted {
		t.Run(test.name, func(t *testing.T) {
			runner := &terminalAPIRunner{}
			server, managed, _, paneRef := newTerminalAPITestServer(t, runner, tmux.DefaultSnapshotBytes)
			token, cookie := issuePasteTokenForTest(t, server, managed, paneRef, test.text)
			body := `{"text":` + mustJSONQuote(t, test.text) + `,"paneRef":"` + string(paneRef) + `","token":"` + token + `"}`
			request := httptest.NewRequest(http.MethodPost, "/paste", strings.NewReader(body))
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			server.handlePasteAPI(response, request, testSessionRef, managed)
			if response.Code != http.StatusOK {
				t.Fatalf("status/body = %d/%q", response.Code, response.Body.String())
			}
			if runner.inputSnapshot() != test.text {
				t.Fatalf("stdin bytes = %d, want %d", len(runner.inputSnapshot()), len(test.text))
			}
			var pasteCall []string
			for _, call := range runner.callsSnapshot() {
				if len(call) > 2 && call[2] == "paste-buffer" {
					pasteCall = call
				}
			}
			if len(pasteCall) != 9 || !containsCall([][]string{pasteCall}, []string{"run", "tmux", "paste-buffer", "-p", "-r", "-b", pasteCall[6], "-t", "%42"}) {
				t.Fatalf("bracketed-paste call = %#v", pasteCall)
			}
		})
	}

	rejected := []struct {
		name string
		text string
	}{
		{name: "zero bytes", text: ""},
		{name: "64 KiB plus one", text: strings.Repeat("a", tmux.MaxPasteBytes+1)},
		{name: "NUL", text: "before\x00after"},
	}
	for _, test := range rejected {
		t.Run(test.name, func(t *testing.T) {
			runner := &terminalAPIRunner{}
			server, managed, _, paneRef := newTerminalAPITestServer(t, runner, tmux.DefaultSnapshotBytes)
			login := httptest.NewRecorder()
			if !server.auth.Login(login, "secret") {
				t.Fatal("test login failed")
			}
			body := `{"text":` + mustJSONQuote(t, test.text) + `,"paneRef":"` + string(paneRef) + `","token":"pt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
			request := httptest.NewRequest(http.MethodPost, "/paste", strings.NewReader(body))
			request.AddCookie(login.Result().Cookies()[0])
			response := httptest.NewRecorder()
			server.handlePasteAPI(response, request, testSessionRef, managed)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status/body = %d/%q", response.Code, response.Body.String())
			}
			for _, call := range runner.callsSnapshot() {
				if len(call) > 2 && (call[2] == "load-buffer" || call[2] == "paste-buffer") {
					t.Fatalf("rejected paste reached tmux: %#v", call)
				}
			}
		})
	}
}

func TestPasteAPIRejectsStaleOrForeignPaneReference(t *testing.T) {
	runner := &terminalAPIRunner{}
	server, managed, logs, paneRef := newTerminalAPITestServer(t, runner, tmux.DefaultSnapshotBytes)
	_, cookie := issuePasteTokenForTest(t, server, managed, paneRef, "safe")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/"+testSessionRef+"/paste", strings.NewReader(`{"text":"safe","paneRef":"p_foreign","token":"pt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	request.AddCookie(cookie)

	server.handlePasteAPI(recorder, request, testSessionRef, managed)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want stale identity conflict", recorder.Code)
	}
	if logged := logs.String(); !strings.Contains(logged, `"status":409`) || !strings.Contains(logged, `"reason_code":"stale_identity"`) {
		t.Fatalf("stale Paste audit does not match HTTP status: %s", logged)
	}
	calls := runner.callsSnapshot()
	for _, call := range calls {
		if len(call) > 2 && call[0] == "run" && (call[2] == "set-buffer" || call[2] == "paste-buffer") {
			t.Fatalf("foreign pane reached paste command: %#v", calls)
		}
	}
}

func TestTerminalAuditOmitsPasteCanary(t *testing.T) {
	const pasteCanary = "PASTE-CANARY-93d03aa1"
	runner := &terminalAPIRunner{}
	server, managed, logs, paneRef := newTerminalAPITestServer(t, runner, tmux.DefaultSnapshotBytes)

	token, cookie := issuePasteTokenForTest(t, server, managed, paneRef, pasteCanary)
	pasteBody := `{"text":"` + pasteCanary + `","paneRef":"` + string(paneRef) + `","token":"` + token + `"}`
	pasteRecorder := httptest.NewRecorder()
	pasteRequest := httptest.NewRequest(http.MethodPost, "/paste", strings.NewReader(pasteBody))
	pasteRequest.AddCookie(cookie)
	server.handlePasteAPI(pasteRecorder, pasteRequest, testSessionRef, managed)

	if pasteRecorder.Code != http.StatusOK {
		t.Fatalf("paste status = %d", pasteRecorder.Code)
	}
	logged := logs.String()
	if strings.Contains(logged, pasteCanary) {
		t.Fatalf("terminal content leaked into audit log: %s", logged)
	}
	for _, field := range []string{`"opaque_id"`, `"status":200`, `"bytes"`, `"duration_ms"`, `"reason_code"`} {
		if !strings.Contains(logged, field) {
			t.Fatalf("audit log missing %s: %s", field, logged)
		}
	}
	for _, forbidden := range []string{`"request"`, `"response"`, `"body"`, `"text"`, `"frame"`} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("audit log contains forbidden field %s: %s", forbidden, logged)
		}
	}
}

func TestPasteAPIRequiresAndConsumesBoundTokenExactlyOnce(t *testing.T) {
	lifecycle := newFakeWebLifecycle(managedWebSession("alpha"))
	server := newLifecycleTestServer(t, lifecycle, 32)
	runner := &terminalAPIRunner{}
	server.tmux = tmux.NewClientWithRunner(runner)
	cookies := loginCookies(t, server)
	paneRef := lifecyclePaneRef(t, server, managedWebSession("alpha"))
	text := "line one\n"
	action := pasteTextAction(text)
	tokenBody := `{"paneRef":"` + string(paneRef) + `","digest":"` + action.Digest + `","bytes":9,"lines":2,"controlCharacters":true,"trailingNewline":true}`
	issued := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost,
		"/api/sessions/"+testSessionRef+"/paste/token", tokenBody, "https://control.test")
	if issued.Code != http.StatusCreated {
		t.Fatalf("token status/body = %d/%q", issued.Code, issued.Body.String())
	}
	var tokenPayload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &tokenPayload); err != nil || tokenPayload.Token == "" {
		t.Fatalf("token response = %v/%q", err, issued.Body.String())
	}
	pasteBody := `{"text":"line one\n","paneRef":"` + string(paneRef) + `","token":"` + tokenPayload.Token + `"}`
	first := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost,
		"/api/sessions/"+testSessionRef+"/paste", pasteBody, "https://control.test")
	if first.Code != http.StatusOK {
		t.Fatalf("first paste status/body = %d/%q", first.Code, first.Body.String())
	}
	second := authenticatedLifecycleRequest(t, server, cookies, http.MethodPost,
		"/api/sessions/"+testSessionRef+"/paste", pasteBody, "https://control.test")
	if second.Code != http.StatusConflict {
		t.Fatalf("replayed paste status/body = %d/%q", second.Code, second.Body.String())
	}
	pasteCalls := 0
	for _, call := range runner.callsSnapshot() {
		if len(call) > 2 && call[2] == "paste-buffer" {
			pasteCalls++
		}
	}
	if pasteCalls != 1 {
		t.Fatalf("paste-buffer calls = %d, want one", pasteCalls)
	}
}

func TestPasteAPIRejectsInvalidUTF8BeforeTerminalMutation(t *testing.T) {
	runner := &terminalAPIRunner{}
	server, managed, _, paneRef := newTerminalAPITestServer(t, runner, tmux.DefaultSnapshotBytes)
	_, cookie := issuePasteTokenForTest(t, server, managed, paneRef, "safe")
	body := append([]byte(`{"text":"`), 0xff)
	body = append(body, []byte(`","paneRef":"`+string(paneRef)+`","token":"pt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)...)
	request := httptest.NewRequest(http.MethodPost, "/paste", bytes.NewReader(body))
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	server.handlePasteAPI(recorder, request, testSessionRef, managed)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid UTF-8 status = %d", recorder.Code)
	}
	for _, call := range runner.callsSnapshot() {
		if len(call) > 2 && (call[2] == "load-buffer" || call[2] == "paste-buffer") {
			t.Fatalf("invalid UTF-8 reached tmux: %#v", call)
		}
	}
}

func newTerminalAPITestServer(t *testing.T, runner *terminalAPIRunner, snapshotLimit int64) (*Server, registry.Session, *bytes.Buffer, PaneRef) {
	t.Helper()
	logs := &bytes.Buffer{}
	client := tmux.NewClientWithRunner(runner)
	server := &Server{
		cfg:         config.Config{SnapshotMaxBytes: snapshotLimit},
		tmux:        client,
		identity:    newIdentityStore(),
		snapshots:   newSnapshotManager(snapshotManagerConfig{}),
		activity:    newOutputActivityStore(),
		pasteTokens: newPasteTokenManager(0, 0),
		logger:      slog.New(slog.NewJSONHandler(logs, nil)),
	}
	authenticator, err := auth.New("secret", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	server.auth = authenticator
	managed := registry.Session{ID: "alpha", Name: "alpha", PublicRef: testSessionRef, TmuxName: "alpha", PID: 300}
	topology, err := server.identity.refresh(context.Background(), client, managed)
	if err != nil {
		t.Fatal(err)
	}
	runner.resetCalls()
	return server, managed, logs, topology.ActivePaneRef
}

func issuePasteTokenForTest(t *testing.T, server *Server, managed registry.Session, paneRef PaneRef, text string) (string, *http.Cookie) {
	t.Helper()
	login := httptest.NewRecorder()
	if !server.auth.Login(login, "secret") {
		t.Fatal("test login failed")
	}
	cookie := login.Result().Cookies()[0]
	request := httptest.NewRequest(http.MethodPost, "/paste", nil)
	request.AddCookie(cookie)
	user, ok := server.auth.LoginScope(request)
	if !ok {
		t.Fatal("test login scope unavailable")
	}
	pane, err := server.identity.resolvePane(context.Background(), server.tmux, managed, paneRef, true)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := server.pasteTokens.Create(pasteTokenBinding{
		User: user, SessionRef: testSessionRef, PaneRef: paneRef, Generation: pane.generation, Action: pasteTextAction(text),
	})
	if err != nil {
		t.Fatal(err)
	}
	return token, cookie
}

func containsCall(calls [][]string, want []string) bool {
	for _, call := range calls {
		if reflect.DeepEqual(call, want) {
			return true
		}
	}
	return false
}

func mustJSONQuote(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

type terminalAPIRunner struct {
	mu sync.Mutex

	capture            string
	historyMetadata    []string
	calls              [][]string
	largestLimitedRead int
	topologyErr        error
	captureHook        func()
	input              string
}

func (r *terminalAPIRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	r.recordCall(append([]string{"output", name}, args...))
	if len(args) == 0 {
		return nil, errors.New("missing command")
	}
	switch {
	case args[0] == "list-panes":
		return []byte("@7\x1f%42\x1f1\x1f1\x1f120\x1f40\x1fshell\n"), nil
	case args[0] == "display-message" && strings.Contains(args[len(args)-1], "#{pane_id}"):
		return []byte("100\x1f101\x1f%42\n"), nil
	case args[0] == "display-message" && strings.Contains(args[len(args)-1], "#{pane_width}"):
		return []byte(r.nextHistoryMetadata()), nil
	case args[0] == "display-message":
		if err := r.topologyError(); err != nil {
			return nil, err
		}
		return []byte("100\x1f101\n"), nil
	default:
		return nil, errors.New("unsupported output")
	}
}

func (r *terminalAPIRunner) nextHistoryMetadata() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.historyMetadata) == 0 {
		return "120\x1f40\x1f100\x1f50000\x1f4096\x1f0\n"
	}
	metadata := r.historyMetadata[0]
	r.historyMetadata = r.historyMetadata[1:]
	return metadata
}

func (r *terminalAPIRunner) OutputLimited(_ context.Context, limit int64, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{"output-limited", name}, args...))
	read := len(r.capture)
	if int64(read) > limit+1 {
		read = int(limit + 1)
	}
	r.largestLimitedRead = read
	capture := r.capture
	hook := r.captureHook
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	if int64(len(capture)) > limit {
		return nil, tmux.ErrSnapshotTooLarge
	}
	return []byte(capture), nil
}

func (r *terminalAPIRunner) Run(_ context.Context, name string, args ...string) error {
	r.recordCall(append([]string{"run", name}, args...))
	return nil
}

func (r *terminalAPIRunner) RunWithInput(_ context.Context, input io.Reader, name string, args ...string) error {
	data, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.input = string(data)
	r.calls = append(r.calls, append([]string{"input", name}, args...))
	r.mu.Unlock()
	return nil
}

func (r *terminalAPIRunner) inputSnapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.input
}

func (r *terminalAPIRunner) recordCall(call []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *terminalAPIRunner) callsSnapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	calls := make([][]string, len(r.calls))
	for index, call := range r.calls {
		calls[index] = append([]string(nil), call...)
	}
	return calls
}

func (r *terminalAPIRunner) resetCalls() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

func (r *terminalAPIRunner) maxLimitedRead() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.largestLimitedRead
}

func (r *terminalAPIRunner) setTopologyError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.topologyErr = err
}

func (r *terminalAPIRunner) setCaptureHook(hook func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.captureHook = hook
}

func (r *terminalAPIRunner) topologyError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.topologyErr
}
