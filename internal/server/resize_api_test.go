package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"terminal-mirror/internal/registry"
	"terminal-mirror/internal/tmux"
)

func TestApplyResizeRequestRejectsMissingWebViewer(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), 60*time.Second)
	server := &Server{
		resize: store,
		tmux:   tmux.NewClientWithRunner(&resizeAPIRunner{}),
	}
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/alpha/resize", nil)
	session := registry.Session{ID: "alpha", TmuxName: "main", PID: 300}

	_, err := server.applyResizeRequest(request, "alpha", session, resizeRequest{Mode: resizeModeWeb, ViewerID: "missing"})
	if !errors.Is(err, errInvalidResizeRequest) {
		t.Fatalf("err = %v, want errInvalidResizeRequest", err)
	}
	settings, err := store.Load("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != resizeModeOff {
		t.Fatalf("settings = %#v, want default off", settings)
	}
}

func TestApplyResizeRequestPrimaryUsesManualPrimaryClientSize(t *testing.T) {
	runner := &resizeAPIRunner{
		resizeClients: "/dev/pts/web|300|80|24|100|off|80|24\n/dev/pts/ssh|501|200|60|300|on|200|59\n",
	}
	server := &Server{
		resize: newResizeStore(filepath.Join(t.TempDir(), "resize"), 60*time.Second),
		tmux:   tmux.NewClientWithRunner(runner),
	}
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/alpha/resize", nil)
	session := registry.Session{ID: "alpha", TmuxName: "main", PID: 300}

	applied, err := server.applyResizeRequest(request, "alpha", session, resizeRequest{Mode: resizeModePrimary})
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil || applied.Mode != resizeModePrimary || applied.ClientName != "/dev/pts/ssh" || applied.Width != 200 || applied.Height != 59 {
		t.Fatalf("applied = %#v, want primary ssh 200x59", applied)
	}
	wantCalls := [][]string{
		{"output", "tmux", "list-clients", "-t", "main", "-F", "#{client_name}|#{client_pid}|#{client_width}|#{client_height}|#{client_activity}|#{status}|#{window_width}|#{window_height}"},
		{"run", "tmux", "set-option", "-w", "-t", "main:", "window-size", "manual"},
		{"run", "tmux", "resize-window", "-t", "main:", "-x", "200", "-y", "59"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestResizeResponseMarksRecentViewersActive(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), 60*time.Second)
	_, err := store.RecordViewer("alpha", resizeViewer{ID: "viewer-1", Width: 120, Height: 40})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		resize: store,
		tmux:   tmux.NewClientWithRunner(&resizeAPIRunner{}),
	}
	request := httptest.NewRequest(http.MethodGet, "/api/sessions/alpha/resize", nil)
	session := registry.Session{ID: "alpha", TmuxName: "main", PID: 300}

	response, err := server.resizeResponse(request, "alpha", session, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Viewers) != 1 || !response.Viewers[0].Active {
		t.Fatalf("viewers = %#v, want recent viewer active", response.Viewers)
	}
}

func TestTransientResizeViewerHeartbeatDoesNotAutoApplyWebMode(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), 60*time.Second)
	if err := store.Save("alpha", resizeSettings{Mode: resizeModeWeb, SelectedViewerID: "viewer-1"}); err != nil {
		t.Fatal(err)
	}
	runner := &resizeAPIRunner{}
	server := &Server{
		resize: store,
		tmux:   tmux.NewClientWithRunner(runner),
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/sessions/alpha/resize/viewer",
		strings.NewReader(`{"viewerId":"viewer-1","width":80,"height":18,"transient":true}`),
	)
	recorder := httptest.NewRecorder()
	session := registry.Session{ID: "alpha", TmuxName: "main", PID: 300}

	server.handleResizeViewerAPI(recorder, request, "alpha", session)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	for _, call := range runner.calls {
		if len(call) > 0 && call[0] == "run" {
			t.Fatalf("transient heartbeat applied tmux command: %#v", runner.calls)
		}
	}
}

type resizeAPIRunner struct {
	resizeClients string
	calls         [][]string
}

func (r *resizeAPIRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "list-clients" {
		r.calls = append(r.calls, append([]string{"output", name}, args...))
		if r.resizeClients == "" {
			return nil, errors.New("no resize clients")
		}
		return []byte(r.resizeClients), nil
	}
	return nil, errors.New("unsupported output")
}

func (r *resizeAPIRunner) Run(ctx context.Context, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{"run", name}, args...))
	return nil
}
