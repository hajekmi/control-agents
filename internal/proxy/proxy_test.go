package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"control-agents/internal/registry"
)

func TestProxyForwardsHTTPToUnixSocket(t *testing.T) {
	socketPath, closeServer := startUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/terminal/alpha/health" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer closeServer()

	handler := New(fakeStore{session: registry.Session{
		ID:     "alpha",
		Name:   "Alpha",
		Socket: socketPath,
		PID:    1,
	}})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/terminal/alpha/health", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "ok" {
		t.Fatalf("body = %q, want ok", recorder.Body.String())
	}
}

func TestProxyRejectsUnknownSession(t *testing.T) {
	handler := New(fakeStore{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/terminal/missing/", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestProxyForwardsWebSocketUpgrade(t *testing.T) {
	socketPath, closeServer := startUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.ToLower(r.Header.Get("Upgrade")) != "websocket" {
			t.Errorf("upgrade header = %q", r.Header.Get("Upgrade"))
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("response writer cannot hijack")
		}
		conn, rw, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		line, _ := rw.ReadString('\n')
		_, _ = rw.WriteString("echo:" + line)
		_ = rw.Flush()
	}))
	defer closeServer()

	handler := New(fakeStore{session: registry.Session{
		ID:     "alpha",
		Name:   "Alpha",
		Socket: socketPath,
		PID:    1,
	}})
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_, _ = fmt.Fprintf(conn, "GET /terminal/alpha/ws HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusSwitchingProtocols)
	}
	_, _ = io.WriteString(conn, "hello\n")
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "echo:hello\n" {
		t.Fatalf("line = %q", line)
	}
}

type fakeStore struct {
	session registry.Session
}

func (f fakeStore) Read(id string) (registry.Session, error) {
	if f.session.ID != id {
		return registry.Session{}, os.ErrNotExist
	}
	return f.session, nil
}

func (f fakeStore) Alive(session registry.Session) bool {
	return f.session.ID == session.ID
}

func startUnixHTTPServer(t *testing.T, handler http.Handler) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ca-proxy-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	socketPath := filepath.Join(dir, "u.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		_ = server.Serve(listener)
		close(done)
	}()
	return socketPath, func() {
		_ = server.Close()
		<-done
	}
}
