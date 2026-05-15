package auth

import (
	"net/http"
	"net/http/httptest"
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
