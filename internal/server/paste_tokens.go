package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"regexp"
	"sync"
	"time"
)

const (
	defaultPasteTokenTTL      = 30 * time.Second
	defaultPasteTokensPerUser = 32
)

var (
	errPasteTokenInvalid = errors.New("invalid or expired paste token")
	pasteTokenIDPattern  = regexp.MustCompile(`^pt_[A-Za-z0-9_-]{24,96}$`)
	pasteDigestPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

type pasteAction struct {
	Digest            string
	Bytes             int
	Lines             int
	ControlCharacters bool
	TrailingNewline   bool
}

type pasteTokenBinding struct {
	User       string
	SessionRef SessionRef
	PaneRef    PaneRef
	Generation PaneGeneration
	Action     pasteAction
}

type storedPasteToken struct {
	pasteTokenBinding
	Expires time.Time
}

type pasteTokenManager struct {
	mu      sync.Mutex
	ttl     time.Duration
	perUser int
	now     func() time.Time
	tokens  map[string]storedPasteToken
}

func newPasteTokenManager(ttl time.Duration, perUser int) *pasteTokenManager {
	if ttl <= 0 {
		ttl = defaultPasteTokenTTL
	}
	if perUser <= 0 {
		perUser = defaultPasteTokensPerUser
	}
	return &pasteTokenManager{ttl: ttl, perUser: perUser, now: time.Now, tokens: make(map[string]storedPasteToken)}
}

func (m *pasteTokenManager) Create(binding pasteTokenBinding) (string, time.Time, error) {
	if !validPasteTokenBinding(binding) {
		return "", time.Time{}, errPasteTokenInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneLocked(now)
	userCount := 0
	for _, token := range m.tokens {
		if token.User == binding.User {
			userCount++
		}
	}
	if userCount >= m.perUser {
		return "", time.Time{}, errPasteTokenInvalid
	}
	id := newPasteTokenID()
	expires := now.Add(m.ttl)
	m.tokens[id] = storedPasteToken{pasteTokenBinding: binding, Expires: expires}
	return id, expires, nil
}

// Consume removes the token before checking its binding, so every presented
// token is single-use even when the request is stale or malformed.
func (m *pasteTokenManager) Consume(id string, expected pasteTokenBinding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.pruneLocked(now)
	if !pasteTokenIDPattern.MatchString(id) {
		return errPasteTokenInvalid
	}
	stored, ok := m.tokens[id]
	delete(m.tokens, id)
	if !ok || stored.Expires.Before(now) || stored.pasteTokenBinding != expected {
		return errPasteTokenInvalid
	}
	return nil
}

func (m *pasteTokenManager) DeleteUser(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, token := range m.tokens {
		if token.User == user {
			delete(m.tokens, id)
		}
	}
}

func (m *pasteTokenManager) DeleteSession(ref SessionRef) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, token := range m.tokens {
		if token.SessionRef == ref {
			delete(m.tokens, id)
		}
	}
}

func (m *pasteTokenManager) pruneLocked(now time.Time) {
	for id, token := range m.tokens {
		if !token.Expires.After(now) {
			delete(m.tokens, id)
		}
	}
}

func validPasteTokenBinding(binding pasteTokenBinding) bool {
	digest, err := base64.RawURLEncoding.DecodeString(binding.Action.Digest)
	if err != nil || len(digest) != sha256.Size || binding.Action.Lines > binding.Action.Bytes+1 {
		return false
	}
	if binding.Action.Lines > 1 && !binding.Action.ControlCharacters {
		return false
	}
	if binding.Action.TrailingNewline && (!binding.Action.ControlCharacters || binding.Action.Lines < 2) {
		return false
	}
	return binding.User != "" && binding.SessionRef != "" && binding.PaneRef != "" &&
		binding.Generation != (PaneGeneration{}) && pasteDigestPattern.MatchString(binding.Action.Digest) &&
		binding.Action.Bytes > 0 && binding.Action.Bytes <= 64*1024 && binding.Action.Lines > 0
}

func pasteTextAction(text string) pasteAction {
	digest := sha256.Sum256([]byte(text))
	action := pasteAction{
		Digest:          base64.RawURLEncoding.EncodeToString(digest[:]),
		Bytes:           len([]byte(text)),
		Lines:           1,
		TrailingNewline: len(text) > 0 && (text[len(text)-1] == '\n' || text[len(text)-1] == '\r'),
	}
	previousCR := false
	for _, value := range text {
		if value == '\r' {
			action.Lines++
			previousCR = true
		} else if value == '\n' {
			if !previousCR {
				action.Lines++
			}
			previousCR = false
		} else {
			previousCR = false
		}
		if value <= 0x1f || (value >= 0x7f && value <= 0x9f) {
			action.ControlCharacters = true
		}
	}
	return action
}

func newPasteTokenID() string {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		panic("generate paste token identity")
	}
	return "pt_" + base64.RawURLEncoding.EncodeToString(random)
}
