package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"log/slog"

	"control-agents/internal/config"
)

func TestServerUsesVerifiedUserLocalTmuxWithConflictingPath(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	managedTmux := filepath.Join(homeDir, ".local", "bin", "tmux")
	operatorBin := filepath.Join(root, "operator-bin")
	probeLog := filepath.Join(root, "tmux.log")
	for _, directory := range []string{filepath.Dir(managedTmux), operatorBin} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	managedScript := `#!/bin/sh
if [ "${1:-}" = "-V" ]; then
  printf '%s\n' 'tmux 3.7b'
  exit 0
fi
printf '%s\n' "$0" >> "$TMUX_PROBE_LOG"
exit 1
`
	operatorScript := "#!/bin/sh\nprintf '%s\\n' 'tmux 3.4'\nexit 99\n"
	if err := os.WriteFile(managedTmux, []byte(managedScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(operatorBin, "tmux"), []byte(operatorScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", operatorBin+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "C")
	t.Setenv("TMUX_PROBE_LOG", probeLog)

	cfg := config.Config{
		BindAddr:         "127.0.0.1",
		Port:             8080,
		Password:         "secret",
		StateDir:         filepath.Join(root, "state"),
		HomeDir:          homeDir,
		CookieTTL:        60,
		MaxSessions:      32,
		SnapshotMaxBytes: 32 * 1024 * 1024,
	}
	application, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.tmux.HasSession(context.Background(), "missing"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(probeLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != managedTmux {
		t.Fatalf("server tmux executable = %q, want %q", strings.TrimSpace(string(data)), managedTmux)
	}
}

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
		"/app.js":               {"fetchSessions"},
		"/login.js":             {"URLSearchParams"},
		"/terminal-observer.js": {"ObservedWebSocket"},
		"/styles.css":           {".login-panel", ".terminal-frame[hidden]"},
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
	csp := recorder.Header().Get("Content-Security-Policy")
	if csp != appContentSecurityPolicy {
		t.Fatalf("application CSP = %q, want exact self-only policy %q", csp, appContentSecurityPolicy)
	}
	for _, forbidden := range []string{"'unsafe-inline'", "http:", "https:", "ws:", "wss:", "data:"} {
		if strings.Contains(csp, forbidden) {
			t.Fatalf("application CSP permits forbidden source %q: %q", forbidden, csp)
		}
	}
}

func TestBrowserCodeHasNoRawHTMLTerminalRenderingOrInlineScripts(t *testing.T) {
	application, err := fs.ReadFile(staticFS, "static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "createContextualFragment"} {
		if strings.Contains(string(application), forbidden) {
			t.Fatalf("browser application uses raw HTML sink %q", forbidden)
		}
	}
	for _, path := range []string{"static/index.html", "static/login.html"} {
		data, err := fs.ReadFile(staticFS, path)
		if err != nil {
			t.Fatal(err)
		}
		if regexp.MustCompile(`<script(?:\s[^>]*)?>\s*[^<\s]`).Match(data) {
			t.Fatalf("%s contains an inline script", path)
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
		{name: "origin userinfo", origin: "https://user@control.test", want: http.StatusForbidden},
		{name: "origin trailing slash", origin: "https://control.test/", want: http.StatusForbidden},
		{name: "origin path", origin: "https://control.test/path", want: http.StatusForbidden},
		{name: "origin query", origin: "https://control.test?x=1", want: http.StatusForbidden},
		{name: "origin empty query", origin: "https://control.test?", want: http.StatusForbidden},
		{name: "origin fragment", origin: "https://control.test#fragment", want: http.StatusForbidden},
		{name: "origin empty fragment", origin: "https://control.test#", want: http.StatusForbidden},
		{name: "origin surrounding whitespace", origin: " https://control.test", want: http.StatusForbidden},
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
			request.Header.Set(csrfHeader, csrfTokenForCookies(t, handler, cookies))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.want)
			}
		})
	}
}

func TestAuthenticatedMutationRequiresCSRF(t *testing.T) {
	handler := newTestServer(t)
	cookies := loginCookies(t, handler)
	request := httptest.NewRequest(http.MethodPost, "https://control.test/logout", nil)
	request.Header.Set("Origin", "https://control.test")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want forbidden", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "https://control.test/logout", nil)
	request.Header.Set("Origin", "https://control.test")
	request.Header.Set(csrfHeader, csrfTokenForCookies(t, handler, cookies)+"tampered")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("tampered CSRF status = %d, want forbidden", recorder.Code)
	}
}

func TestEveryAuthenticatedHTTPMutationSurfaceRequiresCSRF(t *testing.T) {
	handler := newTestServer(t)
	cookies := loginCookies(t, handler)
	paths := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/logout"},
		{http.MethodPost, "/api/sessions"},
		{http.MethodDelete, "/api/sessions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{http.MethodPost, "/api/sessions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/keys"},
		{http.MethodPost, "/api/sessions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/paste/token"},
		{http.MethodPost, "/api/sessions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/paste"},
		{http.MethodPost, "/api/sessions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/resize"},
		{http.MethodPost, "/api/sessions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/resize/viewer"},
		{http.MethodPost, "/api/sessions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/tmux-control"},
		{http.MethodPost, "/api/v1/panes/p_abcdefghijklmnopqrstuvwx/history-snapshots"},
		{http.MethodDelete, "/api/v1/history-snapshots/hs_abcdefghijklmnopqrstuvwx"},
	}
	for _, test := range paths {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "https://control.test"+test.path, strings.NewReader(`{}`))
			request.Header.Set("Origin", "https://control.test")
			for _, cookie := range cookies {
				request.AddCookie(cookie)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want CSRF rejection", recorder.Code)
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

func TestDuplicateOriginHeadersAreRejected(t *testing.T) {
	handler := newTestServer(t)
	cookies := loginCookies(t, handler)

	mutation := httptest.NewRequest(http.MethodPost, "https://control.test/logout", nil)
	mutation.Header.Add("Origin", "https://control.test")
	mutation.Header.Add("Origin", "https://control.test")
	mutation.Header.Set(csrfHeader, csrfTokenForCookies(t, handler, cookies))
	for _, cookie := range cookies {
		mutation.AddCookie(cookie)
	}
	mutationResponse := httptest.NewRecorder()
	handler.ServeHTTP(mutationResponse, mutation)
	if mutationResponse.Code != http.StatusForbidden {
		t.Fatalf("duplicate mutation origins status = %d", mutationResponse.Code)
	}

	websocket := httptest.NewRequest(http.MethodGet, "https://control.test/terminal/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/ws", nil)
	websocket.Header.Set("Connection", "Upgrade")
	websocket.Header.Set("Upgrade", "websocket")
	websocket.Header.Add("Origin", "https://control.test")
	websocket.Header.Add("Origin", "https://control.test")
	websocketResponse := httptest.NewRecorder()
	handler.ServeHTTP(websocketResponse, websocket)
	if websocketResponse.Code != http.StatusForbidden {
		t.Fatalf("duplicate WebSocket origins status = %d", websocketResponse.Code)
	}
}

func TestTerminalWebSocketOriginMustMatchExactly(t *testing.T) {
	handler := newTestServer(t)
	for _, test := range []struct {
		name    string
		origin  string
		referer string
		want    int
	}{
		{name: "missing", want: http.StatusForbidden},
		{name: "referer is not origin", referer: "https://control.test/", want: http.StatusForbidden},
		{name: "host suffix", origin: "https://control.test.evil.example", want: http.StatusForbidden},
		{name: "scheme mismatch", origin: "http://control.test", want: http.StatusForbidden},
		{name: "userinfo", origin: "https://user@control.test", want: http.StatusForbidden},
		{name: "trailing slash", origin: "https://control.test/", want: http.StatusForbidden},
		{name: "path", origin: "https://control.test/ws", want: http.StatusForbidden},
		{name: "query", origin: "https://control.test?ws=1", want: http.StatusForbidden},
		{name: "fragment", origin: "https://control.test#ws", want: http.StatusForbidden},
		{name: "empty fragment", origin: "https://control.test#", want: http.StatusForbidden},
		{name: "exact", origin: "https://control.test", want: http.StatusUnauthorized},
		{name: "exact default port", origin: "https://control.test:443", want: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "https://control.test/terminal/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/ws", nil)
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.referer != "" {
				request.Header.Set("Referer", test.referer)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
		})
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

func TestSensitiveDiagnosticAndKeyUploadSurfacesAreAbsent(t *testing.T) {
	handler := newTestServer(t)
	cookies := loginCookies(t, handler)
	for _, path := range []string{
		"/debug/pprof/", "/debug/pprof/heap", "/api/diagnostics", "/api/heap-dump",
		"/api/ssh-keys", "/api/sessions/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/ssh-key",
	} {
		request := httptest.NewRequest(http.MethodGet, "https://control.test"+path, nil)
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want absent", path, recorder.Code)
		}
	}
}

func TestPanicRecoveryLogsNoPanicValueOrRequestData(t *testing.T) {
	const canary = "PANIC-CANARY-817c7d"
	logs := &bytes.Buffer{}
	server := newTestServerInstance(t, slog.New(slog.NewJSONHandler(logs, nil)))
	server.mux.HandleFunc("/panic-test", func(http.ResponseWriter, *http.Request) { panic(canary) })
	cookies := loginCookies(t, server)
	request := httptest.NewRequest(http.MethodGet, "https://control.test/panic-test?secret="+canary, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d", recorder.Code)
	}
	if strings.Contains(logs.String(), canary) || !strings.Contains(logs.String(), `"reason_code":"panic"`) {
		t.Fatalf("panic log is not content-free: %s", logs.String())
	}
}

func TestParseSessionAPIPath(t *testing.T) {
	id, suffix, ok := parseSessionAPIPath("/api/sessions/" + testSessionRef + "/scroll")
	if !ok || id != testSessionRef || suffix != "scroll" {
		t.Fatalf("id=%q suffix=%q ok=%v", id, suffix, ok)
	}

	id, suffix, ok = parseSessionAPIPath("/api/sessions/" + testSessionRef + "/resize/viewer")
	if !ok || id != testSessionRef || suffix != "resize/viewer" {
		t.Fatalf("id=%q suffix=%q ok=%v", id, suffix, ok)
	}

	if _, _, ok := parseSessionAPIPath("/api/sessions/../scroll"); ok {
		t.Fatal("path traversal id was accepted")
	}
	if _, _, ok := parseSessionAPIPath("/api/sessions/alpha/scroll"); ok {
		t.Fatal("canonical session name was accepted as public API identity")
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

func csrfTokenForCookies(t *testing.T, handler http.Handler, cookies []*http.Cookie) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "https://control.test/api/csrf", nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("CSRF token status/body = %d/%q", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.Token == "" {
		t.Fatalf("invalid CSRF token response: %v %q", err, recorder.Body.String())
	}
	return payload.Token
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
	return newTestServerInstance(t, slog.Default())
}

func newTestServerInstance(t *testing.T, logger *slog.Logger) *Server {
	t.Helper()
	cfg := config.Config{
		BindAddr:         "127.0.0.1",
		Port:             8080,
		Password:         "secret",
		StateDir:         filepath.Join(t.TempDir(), "state"),
		CookieTTL:        60,
		MaxSessions:      32,
		SnapshotMaxBytes: 32 * 1024 * 1024,
	}
	handler, err := New(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
