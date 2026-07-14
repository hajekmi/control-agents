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

	"control-agents/internal/registry"
	"control-agents/internal/tmux"
)

func TestApplyResizeRequestRejectsMissingFitViewer(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), 60*time.Second)
	server := &Server{resize: store, tmux: tmux.NewClientWithRunner(&resizeAPIRunner{})}
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/"+testSessionRef+"/resize", nil)
	pane := paneBinding{rawID: "%42", windowID: "@7"}

	_, err := server.applyResizeRequest(request, testSessionRef, registry.Session{}, pane, resizeRequest{Mode: resizeModeFitOnce, ViewerID: "missing_viewer_123"})
	if !errors.Is(err, errInvalidResizeRequest) {
		t.Fatalf("err = %v, want errInvalidResizeRequest", err)
	}
	settings, err := store.Load(testSessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != resizeModeFixed {
		t.Fatalf("settings = %#v, want default fixed", settings)
	}
}

func TestApplyResizeFixedUsesManualModeWithoutResizeWindow(t *testing.T) {
	runner := &resizeAPIRunner{}
	server := &Server{resize: newResizeStore(filepath.Join(t.TempDir(), "resize"), time.Minute), tmux: tmux.NewClientWithRunner(runner)}
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/"+testSessionRef+"/resize", nil)

	applied, err := server.applyResizeRequest(request, testSessionRef, registry.Session{}, paneBinding{windowID: "@7"}, resizeRequest{Mode: resizeModeFixed})
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil || applied.Mode != resizeModeFixed {
		t.Fatalf("applied = %#v, want fixed", applied)
	}
	want := [][]string{{"run", "tmux", "set-option", "-w", "-t", "@7", "window-size", "manual"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want no resize-window", runner.calls)
	}
}

func TestApplyResizeFitOnceResizesOnceThenPersistsFixed(t *testing.T) {
	store := newResizeStore(filepath.Join(t.TempDir(), "resize"), time.Minute)
	if _, err := store.RecordViewer(testSessionRef, resizeViewer{ID: testViewerID, Width: 140, Height: 47}); err != nil {
		t.Fatal(err)
	}
	runner := &resizeAPIRunner{}
	server := &Server{resize: store, tmux: tmux.NewClientWithRunner(runner)}
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/"+testSessionRef+"/resize", nil)

	applied, err := server.applyResizeRequest(request, testSessionRef, registry.Session{}, paneBinding{windowID: "@7"}, resizeRequest{Mode: resizeModeFitOnce, ViewerID: testViewerID})
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil || applied.Mode != resizeModeFitOnce || applied.Width != 140 || applied.Height != 47 {
		t.Fatalf("applied = %#v, want one 140x47 fit", applied)
	}
	want := [][]string{
		{"run", "tmux", "set-option", "-w", "-t", "@7", "window-size", "manual"},
		{"run", "tmux", "resize-window", "-t", "@7", "-x", "140", "-y", "47"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
	settings, err := store.Load(testSessionRef)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != resizeModeFixed || settings.SelectedViewerID != testViewerID {
		t.Fatalf("settings = %#v, want fixed after one fit", settings)
	}
}

func TestTransientResizeViewerHeartbeatNeverResizesTmux(t *testing.T) {
	runner := &resizeAPIRunner{}
	server := &Server{
		identity: newIdentityStore(),
		resize:   newResizeStore(filepath.Join(t.TempDir(), "resize"), time.Minute),
		tmux:     tmux.NewClientWithRunner(runner),
	}
	managed := registry.Session{ID: "alpha", PublicRef: testSessionRef, TmuxName: "alpha", PID: 300}
	topology, err := server.identity.refresh(context.Background(), server.tmux, managed)
	if err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	body := `{"viewerId":"` + testViewerID + `","paneRef":"` + string(topology.ActivePaneRef) + `","width":80,"height":18,"transient":true}`
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/"+testSessionRef+"/resize/viewer", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	server.handleResizeViewerAPI(recorder, request, testSessionRef, managed)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	for _, call := range runner.calls {
		if len(call) > 1 && call[0] == "run" && call[2] == "resize-window" {
			t.Fatalf("keyboard/transient heartbeat resized tmux: %#v", runner.calls)
		}
	}
}

func TestResizeViewerHeartbeatBoundsRetainedClientMetadata(t *testing.T) {
	runner := &resizeAPIRunner{}
	server := &Server{
		identity: newIdentityStore(),
		resize:   newResizeStore(filepath.Join(t.TempDir(), "resize"), time.Minute),
		tmux:     tmux.NewClientWithRunner(runner),
	}
	managed := registry.Session{ID: "alpha", PublicRef: testSessionRef, TmuxName: "alpha", PID: 300}
	topology, err := server.identity.refresh(context.Background(), server.tmux, managed)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"viewerId":"` + testViewerID + `","paneRef":"` + string(topology.ActivePaneRef) + `","width":80,"height":24}`
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/"+testSessionRef+"/resize/viewer", strings.NewReader(body))
	request.Header.Set("User-Agent", strings.Repeat("agent", maxViewerUserAgentBytes))
	request.RemoteAddr = strings.Repeat("1", maxViewerIPBytes+100)
	recorder := httptest.NewRecorder()

	server.handleResizeViewerAPI(recorder, request, testSessionRef, managed)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status/body = %d/%q", recorder.Code, recorder.Body.String())
	}
	viewer, ok := server.resize.Viewer(testSessionRef, testViewerID)
	if !ok {
		t.Fatal("bounded viewer metadata was not recorded")
	}
	if len(viewer.UserAgent) > maxViewerUserAgentBytes || len(viewer.IP) > maxViewerIPBytes {
		t.Fatalf("retained viewer metadata is unbounded: user-agent=%d ip=%d", len(viewer.UserAgent), len(viewer.IP))
	}
}

type resizeAPIRunner struct {
	calls [][]string
}

func (r *resizeAPIRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{"output", name}, args...))
	if len(args) == 0 {
		return nil, errors.New("missing command")
	}
	switch {
	case args[0] == "list-panes":
		return []byte("@7\x1f%42\x1f1\x1f1\x1f120\x1f40\x1fshell\n"), nil
	case args[0] == "display-message" && strings.Contains(args[len(args)-1], "#{pane_id}"):
		return []byte("100\x1f101\x1f%42\n"), nil
	case args[0] == "display-message":
		return []byte("100\x1f101\n"), nil
	default:
		return nil, errors.New("unsupported output")
	}
}

func (r *resizeAPIRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{"run", name}, args...))
	return nil
}
