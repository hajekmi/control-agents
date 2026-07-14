package registry

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"control-agents/internal/tmux"
)

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var publicRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)

// ErrInvalidSessionRecord marks registry contents that cannot identify a safe
// managed session. Filesystem and persistence failures deliberately do not
// wrap this error so lifecycle callers can distinguish absence/invalid input
// from an operational dependency failure.
var ErrInvalidSessionRecord = errors.New("invalid managed session registry record")

type Session struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PublicRef string `json:"publicRef"`
	TmuxName  string `json:"tmuxName"`
	Socket    string `json:"socket"`
	PID       int    `json:"pid"`
	CWD       string `json:"cwd"`
	CreatedAt string `json:"createdAt"`
}

type Store struct {
	stateDir    string
	sessionsDir string
	tmuxAlive   func(name string) error
	logger      *slog.Logger
}

func NewStore(stateDir string) *Store {
	if absolute, err := filepath.Abs(stateDir); err == nil {
		stateDir = absolute
	}
	return &Store{
		stateDir:    stateDir,
		sessionsDir: filepath.Join(stateDir, "sessions"),
		tmuxAlive: func(name string) error {
			return tmuxAlive("tmux", name)
		},
	}
}

func (s *Store) SetLogger(logger *slog.Logger) {
	s.logger = logger
}

// SetTmuxBinary keeps compatibility checks on the same verified executable as
// the managed lifecycle and terminal API.
func (s *Store) SetTmuxBinary(binary string) {
	if strings.TrimSpace(binary) == "" {
		binary = "tmux"
	}
	s.tmuxAlive = func(name string) error {
		return tmuxAlive(binary, name)
	}
}

func (s *Store) Ensure() error {
	if err := ensurePrivateDir(s.stateDir); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(s.stateDir, "sockets"), 0o700); err != nil {
		return fmt.Errorf("create sockets dir: %w", err)
	}
	if err := os.Chmod(filepath.Join(s.stateDir, "sockets"), 0o700); err != nil {
		return fmt.Errorf("set sockets dir permissions: %w", err)
	}
	if err := os.MkdirAll(s.sessionsDir, 0o700); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}
	if err := os.Chmod(s.sessionsDir, 0o700); err != nil {
		return fmt.Errorf("set sessions dir permissions: %w", err)
	}
	return nil
}

func (s *Store) List() ([]Session, error) {
	return s.list(false)
}

// ListCompatible includes the narrow legacy wrapper format accepted for
// lifecycle migration. Callers must canonicalize records before exposing them.
func (s *Store) ListCompatible() ([]Session, error) {
	return s.list(true)
}

func (s *Store) list(compatible bool) ([]Session, error) {
	if err := s.Ensure(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(s.sessionsDir)
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	sessions := make([]Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		var session Session
		var err error
		if compatible {
			session, err = s.ReadCompatible(id)
		} else {
			session, err = s.Read(id)
		}
		if err != nil {
			s.logWarn("ignore invalid session registry entry", "reason_code", "invalid_record")
			continue
		}
		sessions = append(sessions, session)
	}

	sort.Slice(sessions, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339, sessions[i].CreatedAt)
		right, rightErr := time.Parse(time.RFC3339, sessions[j].CreatedAt)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.Before(right)
		}
		return sessions[i].ID < sessions[j].ID
	})

	return sessions, nil
}

// Write atomically replaces a durable managed-session record.
func (s *Store) Write(session Session) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	if err := s.validateCanonical(session); err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session %q: %w", session.ID, err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(s.sessionsDir, "."+session.ID+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary session %q: %w", session.ID, err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set temporary session permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary session %q: %w", session.ID, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary session %q: %w", session.ID, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary session %q: %w", session.ID, err)
	}
	if err := os.Rename(temporaryPath, s.sessionPath(session.ID)); err != nil {
		return fmt.Errorf("replace session %q: %w", session.ID, err)
	}
	removeTemporary = false
	if err := os.Chmod(s.sessionPath(session.ID), 0o600); err != nil {
		return fmt.Errorf("set session %q permissions: %w", session.ID, err)
	}
	directory, err := os.Open(s.sessionsDir)
	if err != nil {
		return fmt.Errorf("open sessions directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync sessions directory: %w", err)
	}
	return nil
}

func (s *Store) Read(id string) (Session, error) {
	return s.read(id, false)
}

// ReadCompatible accepts only legacy records whose tmux identity and socket
// already match the canonical ID. It permits a historical display-only Name
// so the lifecycle can migrate that field atomically under its session lock.
func (s *Store) ReadCompatible(id string) (Session, error) {
	return s.read(id, true)
}

// ReadByPublicRef resolves only the opaque public reference. Canonical names
// are deliberately not accepted as public authorization identities.
func (s *Store) ReadByPublicRef(ref string) (Session, error) {
	if !ValidPublicRef(ref) {
		return Session{}, fmt.Errorf("%w: invalid public session reference", ErrInvalidSessionRecord)
	}
	sessions, err := s.List()
	if err != nil {
		return Session{}, err
	}
	for _, session := range sessions {
		if session.PublicRef == ref {
			return session, nil
		}
	}
	return Session{}, os.ErrNotExist
}

func (s *Store) read(id string, compatible bool) (Session, error) {
	if !ValidID(id) {
		return Session{}, fmt.Errorf("%w: invalid session id %q", ErrInvalidSessionRecord, id)
	}
	path := s.sessionPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, fmt.Errorf("read session %q: %w", id, err)
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, fmt.Errorf("%w: decode session %q: %v", ErrInvalidSessionRecord, id, err)
	}
	if session.ID != id {
		return Session{}, fmt.Errorf("%w: session file %q contains id %q", ErrInvalidSessionRecord, id, session.ID)
	}
	var validateErr error
	if compatible {
		validateErr = s.validateCompatible(session)
	} else {
		validateErr = s.validateCanonical(session)
	}
	if validateErr != nil {
		return Session{}, fmt.Errorf("%w: session %q: %v", ErrInvalidSessionRecord, id, validateErr)
	}
	return session, nil
}

func (s *Store) Remove(id string) error {
	if !ValidID(id) {
		return fmt.Errorf("invalid session id %q", id)
	}
	if err := os.Remove(s.sessionPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove session %q: %w", id, err)
	}
	return nil
}

func (s *Store) Alive(session Session) bool {
	return session.TmuxName != "" && s.tmuxAlive(session.TmuxName) == nil
}

func (s *Store) logWarn(message string, args ...any) {
	if s.logger != nil {
		s.logger.Warn(message, args...)
	}
}

func (s *Store) sessionPath(id string) string {
	return filepath.Join(s.sessionsDir, id+".json")
}

func (session Session) Validate() error {
	if !ValidID(session.ID) {
		return fmt.Errorf("invalid session id %q", session.ID)
	}
	if session.Name != session.ID {
		return errors.New("session name must equal session id")
	}
	if !ValidPublicRef(session.PublicRef) {
		return errors.New("session public reference must be opaque")
	}
	if session.TmuxName != session.ID {
		return errors.New("tmux name must equal session id")
	}
	if strings.TrimSpace(session.Socket) == "" || !filepath.IsAbs(session.Socket) {
		return errors.New("socket path must be absolute")
	}
	if session.PID < 0 {
		return errors.New("pid cannot be negative")
	}
	return nil
}

func (s *Store) validateCanonical(session Session) error {
	if err := session.Validate(); err != nil {
		return err
	}
	if session.Socket != s.expectedSocket(session.ID) {
		return errors.New("session socket must match canonical state socket path")
	}
	return nil
}

func (s *Store) validateCompatible(session Session) error {
	if !ValidID(session.ID) {
		return fmt.Errorf("invalid session id %q", session.ID)
	}
	if strings.TrimSpace(session.Name) == "" {
		return errors.New("legacy session name cannot be empty")
	}
	if session.PublicRef != "" && !ValidPublicRef(session.PublicRef) {
		return errors.New("legacy session public reference is invalid")
	}
	if session.TmuxName != session.ID {
		return errors.New("legacy tmux name must equal session id")
	}
	if session.PID < 0 {
		return errors.New("pid cannot be negative")
	}
	if session.Socket != s.expectedSocket(session.ID) {
		return errors.New("legacy session socket must match canonical state socket path")
	}
	return nil
}

func (s *Store) expectedSocket(id string) string {
	return filepath.Join(s.stateDir, "sockets", id+".sock")
}

func ValidID(id string) bool {
	return sessionIDPattern.MatchString(id)
}

func ValidPublicRef(ref string) bool {
	return publicRefPattern.MatchString(ref)
}

func NewPublicRef() (string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate public session reference: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func tmuxAlive(binary, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "has-session", "-t", name)
	tmux.ConfigureCommand(cmd)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(path, fs.FileMode(0o700)); err != nil {
		return err
	}
	return nil
}
