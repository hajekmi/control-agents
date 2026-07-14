package auth

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoginCreatesAuthenticatedCookie(t *testing.T) {
	authenticator, err := New("secret", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	if !authenticator.Login(recorder, "secret") {
		t.Fatal("login failed")
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range recorder.Result().Cookies() {
		request.AddCookie(cookie)
	}

	if !authenticator.Authenticated(request) {
		t.Fatal("expected request to be authenticated")
	}
	if scope, ok := authenticator.LoginScope(request); !ok || scope == "" {
		t.Fatal("expected a concrete authenticated login scope")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want one", len(cookies))
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != int(time.Hour.Seconds()) || cookie.Expires.IsZero() {
		t.Fatalf("cookie flags/expiry = %#v", cookie)
	}
}

func TestLoginCookieIsSecureWhenConfigured(t *testing.T) {
	authenticator, err := New("secret", time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if !authenticator.Login(recorder, "secret") {
		t.Fatal("login failed")
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("secure cookie = %#v", cookies)
	}
}

func TestCSRFTokenIsBoundToConcreteLogin(t *testing.T) {
	authenticator, err := New("secret", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	requests := make([]*http.Request, 0, 2)
	for range 2 {
		recorder := httptest.NewRecorder()
		if !authenticator.Login(recorder, "secret") {
			t.Fatal("login failed")
		}
		request := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
		request.AddCookie(recorder.Result().Cookies()[0])
		requests = append(requests, request)
	}
	token, ok := authenticator.CSRFToken(requests[0])
	if !ok || !authenticator.VerifyCSRF(requests[0], token) {
		t.Fatal("valid CSRF token was rejected")
	}
	if authenticator.VerifyCSRF(requests[1], token) || authenticator.VerifyCSRF(requests[0], token+"tampered") {
		t.Fatal("cross-login or tampered CSRF token was accepted")
	}
}

func TestLoginScopesAreDistinct(t *testing.T) {
	authenticator, err := New("secret", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}

	scopes := make(map[string]bool)
	for range 2 {
		recorder := httptest.NewRecorder()
		if !authenticator.Login(recorder, "secret") {
			t.Fatal("login failed")
		}
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, cookie := range recorder.Result().Cookies() {
			request.AddCookie(cookie)
		}
		scope, ok := authenticator.LoginScope(request)
		if !ok || scope == "" {
			t.Fatal("missing login scope")
		}
		scopes[scope] = true
	}
	if len(scopes) != 2 {
		t.Fatalf("login scopes were reused: %#v", scopes)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	authenticator, err := New("secret", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	if authenticator.Login(recorder, "wrong") {
		t.Fatal("wrong password was accepted")
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Fatal("unexpected auth cookie")
	}
}

func TestCookieExpires(t *testing.T) {
	authenticator, err := New("secret", time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	authenticator.now = func() time.Time { return now }

	recorder := httptest.NewRecorder()
	if !authenticator.Login(recorder, "secret") {
		t.Fatal("login failed")
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range recorder.Result().Cookies() {
		request.AddCookie(cookie)
	}

	authenticator.now = func() time.Time { return now.Add(2 * time.Minute) }
	if authenticator.Authenticated(request) {
		t.Fatal("expired cookie authenticated")
	}
}

func TestCookieSurvivesAuthenticatorWithSameSecret(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, SecretSize)
	first, err := NewWithSecret("secret", time.Hour, false, secret)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewWithSecret("secret", time.Hour, false, secret)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	if !first.Login(recorder, "secret") {
		t.Fatal("login failed")
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range recorder.Result().Cookies() {
		request.AddCookie(cookie)
	}
	if !second.Authenticated(request) {
		t.Fatal("expected cookie to authenticate with the same secret")
	}
}

func TestLoadOrCreateSecretPersistsSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "session.key")

	first, err := LoadOrCreateSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("auth secret was not reused")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("secret file mode = %v, want 0600", got)
	}
}

func TestLoadOrCreateSecretReconcilesPermissiveExistingModes(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "auth")
	path := filepath.Join(directory, "session.key")
	secret := bytes.Repeat([]byte{9}, SecretSize)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(base64.RawURLEncoding.EncodeToString(secret)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadOrCreateSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded, secret) {
		t.Fatal("permissive existing secret changed during reconciliation")
	}
	assertPathMode(t, directory, 0o700)
	assertPathMode(t, path, 0o600)
}

func TestLoadOrCreateSecretReconcilesExclusiveCreateRaceModes(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "auth")
	path := filepath.Join(directory, "session.key")
	secret := bytes.Repeat([]byte{11}, SecretSize)
	opener := func(name string, _ int, _ os.FileMode) (*os.File, error) {
		if err := os.WriteFile(name, []byte(base64.RawURLEncoding.EncodeToString(secret)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(name, 0o666); err != nil {
			t.Fatal(err)
		}
		return nil, os.ErrExist
	}

	loaded, err := loadOrCreateSecret(path, opener)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded, secret) {
		t.Fatal("exclusive-create race did not use the winning secret")
	}
	assertPathMode(t, directory, 0o700)
	assertPathMode(t, path, 0o600)
}

func assertPathMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %v, want %v", path, got, want)
	}
}

func TestRequireAPIReturnsUnauthorized(t *testing.T) {
	authenticator, err := New("secret", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}

	handler := authenticator.RequireAPI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
