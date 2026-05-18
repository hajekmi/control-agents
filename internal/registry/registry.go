package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Session struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TmuxName  string `json:"tmuxName"`
	Socket    string `json:"socket"`
	PID       int    `json:"pid"`
	CWD       string `json:"cwd"`
	CreatedAt string `json:"createdAt"`
}

type Store struct {
	stateDir    string
	sessionsDir string
	liveness    livenessChecks
}

type livenessChecks struct {
	tmuxAlive   func(name string) bool
	socketAlive func(path string) bool
}

func NewStore(stateDir string) *Store {
	return &Store{
		stateDir:    stateDir,
		sessionsDir: filepath.Join(stateDir, "sessions"),
		liveness: livenessChecks{
			tmuxAlive:   tmuxAlive,
			socketAlive: socketAlive,
		},
	}
}

func (s *Store) Ensure() error {
	if err := os.MkdirAll(filepath.Join(s.stateDir, "sockets"), 0o700); err != nil {
		return fmt.Errorf("create sockets dir: %w", err)
	}
	if err := os.MkdirAll(s.sessionsDir, 0o700); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}
	return nil
}

func (s *Store) List() ([]Session, error) {
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
		session, err := s.Read(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			continue
		}
		if !s.Alive(session) {
			_ = s.Remove(session.ID)
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

func (s *Store) Read(id string) (Session, error) {
	if !ValidID(id) {
		return Session{}, fmt.Errorf("invalid session id %q", id)
	}
	path := s.sessionPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return Session{}, fmt.Errorf("read session %q: %w", id, err)
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, fmt.Errorf("decode session %q: %w", id, err)
	}
	if err := session.Validate(); err != nil {
		return Session{}, err
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
	if !s.liveness.socketAlive(session.Socket) {
		return false
	}
	if session.TmuxName != "" && !s.liveness.tmuxAlive(session.TmuxName) {
		return false
	}
	return true
}

func (s *Store) sessionPath(id string) string {
	return filepath.Join(s.sessionsDir, id+".json")
}

func (session Session) Validate() error {
	if !ValidID(session.ID) {
		return fmt.Errorf("invalid session id %q", session.ID)
	}
	if strings.TrimSpace(session.Name) == "" {
		return errors.New("session name cannot be empty")
	}
	if strings.TrimSpace(session.TmuxName) == "" {
		return errors.New("tmux name cannot be empty")
	}
	if strings.TrimSpace(session.Socket) == "" || !filepath.IsAbs(session.Socket) {
		return errors.New("socket path must be absolute")
	}
	if session.PID <= 0 {
		return errors.New("pid must be positive")
	}
	return nil
}

func ValidID(id string) bool {
	return id != "." && id != ".." && sessionIDPattern.MatchString(id)
}

func tmuxAlive(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

func socketAlive(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return false
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
