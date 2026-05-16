package auth

import (
	"bytes"
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
