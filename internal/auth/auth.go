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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const CookieName = "control_agents_session"
const SecretSize = 32
const csrfDomain = "control-agents-csrf-v1\x00"

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
	secret, err := randomSecret()
	if err != nil {
		return nil, err
	}

	return &Authenticator{
		passwordHash: passwordHash[:],
		secret:       secret,
		ttl:          ttl,
		secureCookie: secureCookie,
		now:          time.Now,
	}, nil
}

func NewWithSecret(password string, ttl time.Duration, secureCookie bool, secret []byte) (*Authenticator, error) {
	if password == "" {
		return nil, errors.New("password cannot be empty")
	}
	if ttl <= 0 {
		return nil, errors.New("ttl must be positive")
	}
	if len(secret) != SecretSize {
		return nil, fmt.Errorf("auth secret must be %d bytes", SecretSize)
	}

	passwordHash := sha256.Sum256([]byte(password))
	secretCopy := append([]byte(nil), secret...)

	return &Authenticator{
		passwordHash: passwordHash[:],
		secret:       secretCopy,
		ttl:          ttl,
		secureCookie: secureCookie,
		now:          time.Now,
	}, nil
}

func LoadOrCreateSecret(path string) ([]byte, error) {
	return loadOrCreateSecret(path, os.OpenFile)
}

type secretFileOpener func(string, int, os.FileMode) (*os.File, error)

func loadOrCreateSecret(path string, openFile secretFileOpener) ([]byte, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create auth secret dir: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, fmt.Errorf("set auth secret dir permissions: %w", err)
	}

	data, err := readSecretFile(path)
	if err == nil {
		return parseSecret(data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	secret, err := randomSecret()
	if err != nil {
		return nil, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(secret) + "\n"
	file, err := openFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		data, err := readSecretFile(path)
		if err != nil {
			return nil, err
		}
		return parseSecret(data)
	}
	if err != nil {
		return nil, fmt.Errorf("create auth secret: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("set auth secret permissions: %w", err)
	}

	if _, err := file.WriteString(encoded); err != nil {
		return nil, fmt.Errorf("write auth secret: %w", err)
	}
	return secret, nil
}

func readSecretFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("stat auth secret: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("auth secret must be a regular file")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("set auth secret permissions: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read auth secret: %w", err)
	}
	return data, nil
}

func randomSecret() ([]byte, error) {
	secret := make([]byte, SecretSize)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate auth secret: %w", err)
	}
	return secret, nil
}

func parseSecret(data []byte) ([]byte, error) {
	secret, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decode auth secret: %w", err)
	}
	if len(secret) != SecretSize {
		return nil, fmt.Errorf("auth secret must be %d bytes", SecretSize)
	}
	return secret, nil
}

func (a *Authenticator) CheckPassword(password string) bool {
	hash := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare(a.passwordHash, hash[:]) == 1
}

func (a *Authenticator) Login(w http.ResponseWriter, password string) bool {
	if !a.CheckPassword(password) {
		return false
	}

	now := a.now()
	value := a.sign(now.Unix())
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   a.secureCookie,
		MaxAge:   int(a.ttl.Seconds()),
		Expires:  now.Add(a.ttl),
	})
	return true
}

func (a *Authenticator) Logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   a.secureCookie,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0).UTC(),
	})
}

func (a *Authenticator) Authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	return a.verify(cookie.Value)
}

// LoginScope returns an opaque representation of the concrete authenticated
// login. Each successful login receives a fresh cookie nonce, so two browsers
// using the same configured password still have distinct scopes. The raw
// cookie is never retained outside the authenticator.
func (a *Authenticator) LoginScope(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || !a.verify(cookie.Value) {
		return "", false
	}
	digest := sha256.Sum256([]byte(cookie.Value))
	return base64.RawURLEncoding.EncodeToString(digest[:]), true
}

// CSRFToken returns a session-bound token without exposing the authentication
// cookie or persistent signing secret to JavaScript.
func (a *Authenticator) CSRFToken(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || !a.verify(cookie.Value) {
		return "", false
	}
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(csrfDomain))
	_, _ = mac.Write([]byte(cookie.Value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), true
}

// VerifyCSRF validates a token against the concrete authenticated login.
func (a *Authenticator) VerifyCSRF(r *http.Request, token string) bool {
	expected, ok := a.CSRFToken(r)
	if !ok {
		return false
	}
	provided, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return false
	}
	expectedBytes, err := base64.RawURLEncoding.DecodeString(expected)
	return err == nil && hmac.Equal(provided, expectedBytes)
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
