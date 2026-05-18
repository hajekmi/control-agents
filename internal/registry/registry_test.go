package registry

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestReadValidatesSession(t *testing.T) {
	store := newTestStore(t)
	session := Session{
		ID:        "alpha",
		Name:      "Alpha",
		TmuxName:  "alpha",
		Socket:    filepath.Join(t.TempDir(), "alpha.sock"),
		PID:       123,
		CWD:       "/tmp",
		CreatedAt: "2026-05-15T12:00:00Z",
	}
	writeSession(t, store, session)

	got, err := store.Read("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != session.ID || got.Name != session.Name {
		t.Fatalf("session = %#v, want %#v", got, session)
	}
}

func TestReadRejectsPathTraversal(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Read("../alpha"); err == nil {
		t.Fatal("expected invalid id error")
	}
	if _, err := store.Read(".."); err == nil {
		t.Fatal("expected parent directory id error")
	}
}

func TestListRemovesStaleSessions(t *testing.T) {
	store := newTestStore(t)
	store.liveness.processAlive = func(pid int) error {
		if pid == 100 {
			return nil
		}
		return errors.New("process is not alive")
	}
	store.liveness.tmuxAlive = func(name string) error {
		if name == "live" {
			return nil
		}
		return errors.New("tmux session is not alive")
	}
	store.liveness.socketAlive = func(path string) error {
		if path == "/tmp/live.sock" {
			return nil
		}
		return errors.New("socket is not alive")
	}

	writeSession(t, store, Session{
		ID:        "live",
		Name:      "Live",
		TmuxName:  "live",
		Socket:    "/tmp/live.sock",
		PID:       100,
		CreatedAt: "2026-05-15T12:00:00Z",
	})
	writeSession(t, store, Session{
		ID:        "stale",
		Name:      "Stale",
		TmuxName:  "stale",
		Socket:    "/tmp/stale.sock",
		PID:       200,
		CreatedAt: "2026-05-15T12:01:00Z",
	})

	sessions, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "live" {
		t.Fatalf("sessions = %#v, want only live", sessions)
	}
	if _, err := os.Stat(filepath.Join(store.sessionsDir, "stale.json")); !os.IsNotExist(err) {
		t.Fatalf("stale session file still exists, stat err = %v", err)
	}
}

func TestSocketAlive(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	if err := socketAlive(socketPath); err != nil {
		t.Fatalf("expected unix socket to be alive: %v", err)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store := NewStore(t.TempDir())
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	return store
}

func writeSession(t *testing.T, store *Store, session Session) {
	t.Helper()
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.sessionsDir, session.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
