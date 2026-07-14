package registry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadValidatesSession(t *testing.T) {
	store := newTestStore(t)
	session := Session{
		ID:        "alpha",
		Name:      "alpha",
		PublicRef: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TmuxName:  "alpha",
		Socket:    filepath.Join(store.stateDir, "sockets", "alpha.sock"),
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

func TestReadRejectsMismatchedFileAndRecordIDs(t *testing.T) {
	store := newTestStore(t)
	writeSession(t, store, Session{
		ID:        "beta",
		Name:      "beta",
		PublicRef: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		TmuxName:  "beta",
		Socket:    filepath.Join(store.stateDir, "sockets", "beta.sock"),
	})
	if err := os.Rename(filepath.Join(store.sessionsDir, "beta.json"), filepath.Join(store.sessionsDir, "alpha.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read("alpha"); !errors.Is(err, ErrInvalidSessionRecord) {
		t.Fatalf("error = %v, want ErrInvalidSessionRecord", err)
	}
}

func TestReadDistinguishesInvalidRecordsFromOperationalFailures(t *testing.T) {
	store := newTestStore(t)
	if err := os.WriteFile(filepath.Join(store.sessionsDir, "invalid.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadCompatible("invalid"); !errors.Is(err, ErrInvalidSessionRecord) {
		t.Fatalf("invalid record error = %v, want ErrInvalidSessionRecord", err)
	}
	if err := os.Mkdir(filepath.Join(store.sessionsDir, "unreadable.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadCompatible("unreadable"); err == nil || errors.Is(err, ErrInvalidSessionRecord) {
		t.Fatalf("operational read error = %v, want non-record failure", err)
	}
}

func TestListKeepsDurableSessionsWithoutLiveRuntimeMetadata(t *testing.T) {
	store := newTestStore(t)
	writeSession(t, store, Session{
		ID:        "live",
		Name:      "live",
		PublicRef: "llllllllllllllllllllllllllllllll",
		TmuxName:  "live",
		Socket:    filepath.Join(store.stateDir, "sockets", "live.sock"),
		PID:       0,
		CreatedAt: "2026-05-15T12:00:00Z",
	})

	sessions, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "live" {
		t.Fatalf("sessions = %#v, want only live", sessions)
	}
	if _, err := os.Stat(filepath.Join(store.sessionsDir, "live.json")); err != nil {
		t.Fatalf("durable session file was removed: %v", err)
	}
}

func TestWriteAtomicallyStoresPrivateRegistryFile(t *testing.T) {
	store := newTestStore(t)
	session := Session{
		ID:        "alpha",
		Name:      "alpha",
		PublicRef: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TmuxName:  "alpha",
		Socket:    filepath.Join(store.stateDir, "sockets", "alpha.sock"),
		PID:       321,
		CWD:       "/home/service",
		CreatedAt: "2026-07-13T12:00:00Z",
	}
	if err := store.Write(session); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(store.sessionsDir, "alpha.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("registry mode = %o, want 600", info.Mode().Perm())
	}
	got, err := store.Read("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got != session {
		t.Fatalf("session = %#v, want %#v", got, session)
	}
	entries, err := os.ReadDir(store.sessionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("session directory contains %d entries, want 1", len(entries))
	}
}

func TestReadCompatibleAcceptsOnlySafeDisplayNameMigration(t *testing.T) {
	store := newTestStore(t)
	legacy := Session{
		ID:        "legacy",
		Name:      "Legacy display",
		TmuxName:  "legacy",
		Socket:    filepath.Join(store.stateDir, "sockets", "legacy.sock"),
		PID:       123,
		CWD:       "/old/workdir",
		CreatedAt: "2026-05-15T12:00:00Z",
	}
	writeSession(t, store, legacy)
	if _, err := store.Read("legacy"); err == nil {
		t.Fatal("canonical read accepted legacy display name")
	}
	if got, err := store.ReadCompatible("legacy"); err != nil || got != legacy {
		t.Fatalf("compatible read = %#v, %v", got, err)
	}

	legacy.TmuxName = "other"
	writeSession(t, store, legacy)
	if _, err := store.ReadCompatible("legacy"); err == nil {
		t.Fatal("compatible read accepted non-canonical tmux name")
	}
	legacy.TmuxName = legacy.ID
	legacy.Socket = filepath.Join(store.stateDir, "sockets", "other.sock")
	writeSession(t, store, legacy)
	if _, err := store.ReadCompatible("legacy"); err == nil {
		t.Fatal("compatible read accepted non-canonical socket")
	}
}

func TestReadRejectsCleanEquivalentSocketPathAcrossSymlinkAndDotDot(t *testing.T) {
	store := newTestStore(t)
	socketsDir := filepath.Join(store.stateDir, "sockets")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(socketsDir, "link")); err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(socketsDir, "alpha.sock")
	malicious := filepath.Join(socketsDir, "link") + string(filepath.Separator) + ".." + string(filepath.Separator) + "alpha.sock"
	if filepath.Clean(malicious) != expected {
		t.Fatalf("test path %q does not clean to %q", malicious, expected)
	}
	writeSession(t, store, Session{
		ID:        "alpha",
		Name:      "alpha",
		TmuxName:  "alpha",
		Socket:    malicious,
		PID:       123,
		CWD:       "/home/service",
		CreatedAt: "2026-07-13T12:00:00Z",
	})
	if _, err := store.ReadCompatible("alpha"); err == nil {
		t.Fatal("compatible read accepted an unclean socket path crossing a symlink")
	}
}

func TestValidIDUsesCanonicalManagedNamePattern(t *testing.T) {
	for _, valid := range []string{"a", "Alpha-1", "a.b_c", "x" + strings.Repeat("y", 63)} {
		if !ValidID(valid) {
			t.Errorf("ValidID(%q) = false, want true", valid)
		}
	}
	for _, invalid := range []string{"", ".alpha", "-alpha", "alpha beta", "x" + strings.Repeat("y", 64)} {
		if ValidID(invalid) {
			t.Errorf("ValidID(%q) = true, want false", invalid)
		}
	}
}

func TestPublicReferencesAreOpaqueAndResolveWithoutCanonicalNames(t *testing.T) {
	store := newTestStore(t)
	ref, err := NewPublicRef()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidPublicRef(ref) || ref == "alpha" {
		t.Fatalf("public reference = %q, want opaque reference", ref)
	}
	session := Session{
		ID:        "alpha",
		Name:      "alpha",
		PublicRef: ref,
		TmuxName:  "alpha",
		Socket:    filepath.Join(store.stateDir, "sockets", "alpha.sock"),
	}
	if err := store.Write(session); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ReadByPublicRef(ref); err != nil || got.ID != "alpha" {
		t.Fatalf("ReadByPublicRef() = %#v, %v", got, err)
	}
	if _, err := store.ReadByPublicRef("alpha"); err == nil {
		t.Fatal("canonical session name resolved as a public reference")
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
