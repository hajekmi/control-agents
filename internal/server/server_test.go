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
	tests := map[string][]string{
		"/app.js":     {"fetchSessions"},
		"/login.js":   {"URLSearchParams"},
		"/styles.css": {".login-panel", ".terminal-frame[hidden]"},
	}
	for path, wants := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %q", path, recorder.Code, recorder.Body.String())
		}
		for _, want := range wants {
			if !strings.Contains(recorder.Body.String(), want) {
				t.Fatalf("%s body does not contain %q", path, want)
			}
		}
	}
}

func TestSecurityHeadersAreSet(t *testing.T) {
	handler := newTestServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/login", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	wants := map[string]string{
		"Content-Security-Policy": "frame-ancestors 'self'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "SAMEORIGIN",
		"Referrer-Policy":         "same-origin",
		"Permissions-Policy":      "camera=()",
	}
	for header, want := range wants {
		if got := recorder.Header().Get(header); !strings.Contains(got, want) {
			t.Fatalf("%s = %q, want to contain %q", header, got, want)
		}
	}
}

func TestLogoutRequiresSameOrigin(t *testing.T) {
	handler := newTestServer(t)
	cookies := loginCookies(t, handler)

	tests := []struct {
		name   string
		origin string
		want   int
	}{
		{name: "missing origin", want: http.StatusForbidden},
		{name: "cross origin", origin: "https://evil.test", want: http.StatusForbidden},
		{name: "same origin", origin: "https://control.test", want: http.StatusFound},
		{name: "same origin referer fallback", origin: "referer:https://control.test/", want: http.StatusFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://control.test/logout", nil)
			if strings.HasPrefix(tt.origin, "referer:") {
				request.Header.Set("Referer", strings.TrimPrefix(tt.origin, "referer:"))
			} else if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}
			for _, cookie := range cookies {
				request.AddCookie(cookie)
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.want)
			}
		})
	}
}

func TestTerminalWebSocketRequiresSameOrigin(t *testing.T) {
	handler := newTestServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://control.test/terminal/alpha/ws", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Origin", "https://evil.test")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
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

func TestLoginRateLimiterBlocksRepeatedFailures(t *testing.T) {
	handler := newTestServer(t)

	for range loginAttemptLimit {
		recorder := postLogin(t, handler, "203.0.113.10:1234", "wrong")
		if recorder.Code != http.StatusFound {
			t.Fatalf("login status = %d, want %d", recorder.Code, http.StatusFound)
		}
	}

	blocked := postLogin(t, handler, "203.0.113.10:1234", "wrong")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked status = %d, want %d", blocked.Code, http.StatusTooManyRequests)
	}
	if blocked.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}

	otherIP := postLogin(t, handler, "203.0.113.11:1234", "secret")
	if otherIP.Code != http.StatusFound {
		t.Fatalf("other IP login status = %d, want %d", otherIP.Code, http.StatusFound)
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

	id, suffix, ok = parseSessionAPIPath("/api/sessions/main-1/resize/viewer")
	if !ok || id != "main-1" || suffix != "resize/viewer" {
		t.Fatalf("id=%q suffix=%q ok=%v", id, suffix, ok)
	}

	if _, _, ok := parseSessionAPIPath("/api/sessions/../scroll"); ok {
		t.Fatal("path traversal id was accepted")
	}
}

func loginCookies(t *testing.T, handler http.Handler) []*http.Cookie {
	t.Helper()
	recorder := postLogin(t, handler, "192.0.2.10:1234", "secret")
	if recorder.Code != http.StatusFound {
		t.Fatalf("login status = %d", recorder.Code)
	}
	return recorder.Result().Cookies()
}

func postLogin(t *testing.T, handler http.Handler, remoteAddr, password string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("password="+password))
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(recorder, request)
	return recorder
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
