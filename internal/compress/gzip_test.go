package compress

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareCompressesHTTP(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello"))
	}))
	request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	request.Header.Set("Accept-Encoding", "gzip, br")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q, want hello", body)
	}
}

func TestMiddlewareSkipsTerminalProxy(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("terminal"))
	}))
	request := httptest.NewRequest(http.MethodGet, "/terminal/main/", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if recorder.Body.String() != "terminal" {
		t.Fatalf("body = %q, want terminal", recorder.Body.String())
	}
}

func TestMiddlewareSkipsWebSocketUpgrade(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ws"))
	}))
	request := httptest.NewRequest(http.MethodGet, "/ws", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Upgrade", "websocket")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
}

func TestAcceptsGzip(t *testing.T) {
	if !acceptsGzip("br, gzip;q=1.0") {
		t.Fatal("expected gzip to be accepted")
	}
	if acceptsGzip("br, deflate") {
		t.Fatal("gzip was unexpectedly accepted")
	}
	if acceptsGzip(strings.TrimSpace("")) {
		t.Fatal("empty header was unexpectedly accepted")
	}
}

