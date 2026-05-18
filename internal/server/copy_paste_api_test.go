package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"control-agents/internal/registry"
	"control-agents/internal/tmux"
)

func TestCaptureAPIReturnsPaneText(t *testing.T) {
	runner := &copyPasteAPIRunner{
		capture: "first line\nselected word\n",
	}
	server := &Server{
		tmux: tmux.NewClientWithRunner(runner),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/sessions/alpha/capture", nil)
	session := registry.Session{ID: "alpha", TmuxName: "main", PID: 300}

	server.handleCaptureAPI(recorder, request, "alpha", session)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Body.String(); got != `{"text":"first line\nselected word"}` {
		t.Fatalf("body = %q", got)
	}
	wantCalls := [][]string{
		{"output", "tmux", "capture-pane", "-p", "-S", "-2000", "-E", "-1", "-t", "main"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestPasteAPIPastesTextThroughTmuxBuffer(t *testing.T) {
	runner := &copyPasteAPIRunner{}
	server := &Server{
		tmux: tmux.NewClientWithRunner(runner),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/alpha/paste", strings.NewReader(`{"text":"hello\nworld"}`))
	session := registry.Session{ID: "alpha", TmuxName: "main", PID: 300}

	server.handlePasteAPI(recorder, request, "alpha", session)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if recorder.Body.String() != `{"ok":true}` {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %#v, want 2 calls", runner.calls)
	}
	setCall := runner.calls[0]
	pasteCall := runner.calls[1]
	if len(setCall) != 7 || !reflect.DeepEqual(setCall[:4], []string{"run", "tmux", "set-buffer", "-b"}) || setCall[5] != "--" || setCall[6] != "hello\nworld" {
		t.Fatalf("set-buffer call = %#v", setCall)
	}
	buffer := setCall[4]
	wantPaste := []string{"run", "tmux", "paste-buffer", "-d", "-b", buffer, "-t", "main"}
	if !reflect.DeepEqual(pasteCall, wantPaste) {
		t.Fatalf("paste call = %#v, want %#v", pasteCall, wantPaste)
	}
}

func TestPasteAPIRejectsInvalidPaste(t *testing.T) {
	runner := &copyPasteAPIRunner{}
	server := &Server{
		tmux: tmux.NewClientWithRunner(runner),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/alpha/paste", strings.NewReader(`{"text":"bad\u0000text"}`))
	session := registry.Session{ID: "alpha", TmuxName: "main", PID: 300}

	server.handlePasteAPI(recorder, request, "alpha", session)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %#v, want none", runner.calls)
	}
}

type copyPasteAPIRunner struct {
	capture string
	calls   [][]string
}

func (r *copyPasteAPIRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{"output", name}, args...))
	return []byte(r.capture), nil
}

func (r *copyPasteAPIRunner) Run(ctx context.Context, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{"run", name}, args...))
	return nil
}
