package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const CookieName = "terminal_mirror_session"

type Authenticator struct {
	passwordHash []byte
	secret       []byte
	ttl          time.Duration
	secureCookie bool
	now          func() time.Time
}

func New(password string, ttl time.Duration, secureCookie bool) (*Authenticator, error) {
	if password == "" {
		return nil, errors.New("password cannot be empty")
	}
	if ttl <= 0 {
		return nil, errors.New("ttl must be positive")
	}

	passwordHash := sha256.Sum256([]byte(password))
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate auth secret: %w", err)
	}

	return &Authenticator{
		passwordHash: passwordHash[:],
		secret:       secret,
		ttl:          ttl,
		secureCookie: secureCookie,
		now:          time.Now,
	}, nil
}

func (a *Authenticator) CheckPassword(password string) bool {
	hash := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare(a.passwordHash, hash[:]) == 1
}

func (a *Authenticator) Login(w http.ResponseWriter, password string) bool {
	if !a.CheckPassword(password) {
		return false
	}

	value := a.sign(a.now().Unix())
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.secureCookie,
		MaxAge:   int(a.ttl.Seconds()),
	})
	return true
}

func (a *Authenticator) Logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.secureCookie,
		MaxAge:   -1,
	})
}

func (a *Authenticator) Authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	return a.verify(cookie.Value)
}

func (a *Authenticator) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Authenticated(r) {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

func (a *Authenticator) RequireAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Authenticated(r) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func (a *Authenticator) sign(issuedAt int64) string {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}

	payload := fmt.Sprintf("%d:%s", issuedAt, base64.RawURLEncoding.EncodeToString(nonce))
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	signature := mac.Sum(nil)

	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (a *Authenticator) verify(value string) bool {
	payloadPart, signaturePart, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, a.secret)
	mac.Write(payloadBytes)
	expected := mac.Sum(nil)
	if !hmac.Equal(signature, expected) {
		return false
	}

	issuedAtText, _, ok := strings.Cut(string(payloadBytes), ":")
	if !ok {
		return false
	}
	issuedAt, err := strconv.ParseInt(issuedAtText, 10, 64)
	if err != nil {
		return false
	}

	issued := time.Unix(issuedAt, 0)
	now := a.now()
	return !issued.After(now.Add(30*time.Second)) && now.Sub(issued) <= a.ttl
}
