package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"log/slog"

	"terminal-mirror/internal/config"
)

func TestUnauthenticatedAPIReturnsUnauthorized(t *testing.T) {
	handler := newTestServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestStaticAssetsArePublic(t *testing.T) {
	handler := newTestServer(t)
	tests := map[string]string{
		"/app.js":     "fetchSessions",
		"/styles.css": ".login-panel",
	}
	for path, want := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %q", path, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("%s body does not contain %q", path, want)
		}
	}
}

func TestLoginAndSessionList(t *testing.T) {
	handler := newTestServer(t)
	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("password=secret"))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(login, loginRequest)

	if login.Code != http.StatusFound {
		t.Fatalf("login status = %d", login.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	for _, cookie := range login.Result().Cookies() {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"sessions"`) {
		t.Fatalf("unexpected body %q", recorder.Body.String())
	}
}

func TestLoginAndVersion(t *testing.T) {
	handler := newTestServer(t)
	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("password=secret"))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(login, loginRequest)

	request := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	for _, cookie := range login.Result().Cookies() {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"version"`) {
		t.Fatalf("unexpected body %q", recorder.Body.String())
	}
}

func TestRootAfterLoginReturnsIndexWithoutRedirect(t *testing.T) {
	handler := newTestServer(t)
	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("password=secret"))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(login, loginRequest)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range login.Result().Cookies() {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if location := recorder.Header().Get("Location"); location != "" {
		t.Fatalf("unexpected redirect location %q", location)
	}
	if !strings.Contains(recorder.Body.String(), "Control Agents") {
		t.Fatalf("unexpected index body %q", recorder.Body.String())
	}
}

func TestParseSessionAPIPath(t *testing.T) {
	id, suffix, ok := parseSessionAPIPath("/api/sessions/main-1/scroll")
	if !ok || id != "main-1" || suffix != "scroll" {
		t.Fatalf("id=%q suffix=%q ok=%v", id, suffix, ok)
	}

	if _, _, ok := parseSessionAPIPath("/api/sessions/../scroll"); ok {
		t.Fatal("path traversal id was accepted")
	}
}

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{
		BindAddr:  "127.0.0.1",
		Port:      8080,
		Password:  "secret",
		StateDir:  filepath.Join(t.TempDir(), "state"),
		CookieTTL: 60,
	}
	handler, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
