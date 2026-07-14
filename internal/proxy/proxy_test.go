package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"control-agents/internal/registry"
)

func TestProxyForwardsHTTPToUnixSocket(t *testing.T) {
	const sessionRef = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	socketPath, closeServer := startUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/terminal/"+sessionRef+"/health" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer closeServer()

	handler := New(fakeStore{session: registry.Session{
		ID:        "alpha",
		PublicRef: sessionRef,
		Name:      "Alpha",
		Socket:    socketPath,
		PID:       1,
	}})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/terminal/"+sessionRef+"/health", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "ok" {
		t.Fatalf("body = %q, want ok", recorder.Body.String())
	}
}

func TestProxyInjectsWebSocketTransportObserverIntoTerminalHTML(t *testing.T) {
	const sessionRef = "dddddddddddddddddddddddddddddddd"
	const upstreamScript = `<script>window.ttydStarted=true</script>`
	socketPath, closeServer := startUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if encoding := r.Header.Get("Accept-Encoding"); encoding != "identity" {
			t.Errorf("Accept-Encoding = %q, want identity", encoding)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", `"upstream"`)
		_, _ = io.WriteString(w, "<!doctype html><html><head>"+upstreamScript+"</head><body></body></html>")
	}))
	defer closeServer()

	handler := New(fakeStore{session: registry.Session{
		ID:        "alpha",
		PublicRef: sessionRef,
		Name:      "Alpha",
		Socket:    socketPath,
		PID:       1,
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/terminal/"+sessionRef+"/", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	observer := strings.Index(body, `<script src="/terminal-observer.js"></script>`)
	application := strings.Index(body, upstreamScript)
	if observer < 0 || application < 0 || observer >= application {
		t.Fatalf("transport observer was not injected before ttyd application: %q", body)
	}
	if etag := recorder.Header().Get("ETag"); etag != "" {
		t.Fatalf("ETag = %q, want empty after HTML modification", etag)
	}
	if strings.Contains(body, "postMessage") || strings.Contains(body, "ObservedWebSocket") {
		t.Fatalf("terminal HTML contains an inline injected observer: %q", body)
	}
	if got := recorder.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'self'" {
		t.Fatalf("terminal CSP = %q", got)
	}
	if got, want := recorder.Header().Get("Content-Length"), fmt.Sprintf("%d", recorder.Body.Len()); got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
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
	const sessionRef = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
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
		ID:        "alpha",
		PublicRef: sessionRef,
		Name:      "Alpha",
		Socket:    socketPath,
		PID:       1,
	}})
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_, _ = fmt.Fprintf(conn, "GET /terminal/%s/ws HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", sessionRef)
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

func TestProxyObservesOnlyWebSocketDataPayloadBytes(t *testing.T) {
	const sessionRef = "cccccccccccccccccccccccccccccccc"
	socketPath, closeServer := startUnixHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		_, _ = rw.Write([]byte{0x82, 0x04, '0', 'a'})
		_ = rw.Flush()
		_, _ = rw.Write([]byte{'b', 'c', 0x89, 0x01, 'x'})
		_ = rw.Flush()
		extended := append([]byte{0x82, 126, 0, 130}, bytes.Repeat([]byte{'y'}, 130)...)
		_, _ = rw.Write(extended)
		_ = rw.Flush()
	}))
	defer closeServer()

	observed := make(chan int, 32)
	handler := NewWithOutputObserver(fakeStore{session: registry.Session{
		ID:        "alpha",
		PublicRef: sessionRef,
		Name:      "Alpha",
		Socket:    socketPath,
		PID:       1,
	}}, func(ref string, bytes int) {
		if ref != sessionRef {
			t.Errorf("observed ref = %q", ref)
		}
		observed <- bytes
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = fmt.Fprintf(conn, "GET /terminal/%s/ws HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", sessionRef)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d", response.StatusCode)
	}
	frame := make([]byte, 143)
	if _, err := io.ReadFull(reader, frame); err != nil {
		t.Fatal(err)
	}

	total := 0
	for total < 134 {
		select {
		case bytes := <-observed:
			total += bytes
		case <-time.After(time.Second):
			t.Fatalf("observed bytes = %d, want 134", total)
		}
	}
	select {
	case bytes := <-observed:
		t.Fatalf("control frame reported as output: %d bytes", bytes)
	case <-time.After(20 * time.Millisecond):
	}
}

type fakeStore struct {
	session registry.Session
}

func (f fakeStore) ReadByPublicRef(ref string) (registry.Session, error) {
	if f.session.PublicRef != ref {
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
