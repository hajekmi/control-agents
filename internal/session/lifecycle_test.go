package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"control-agents/internal/registry"
	"control-agents/internal/tmux"
)

func TestCreateBuildsManagedSessionInConfiguredHome(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)

	managed, err := manager.Create(context.Background(), "alpha-1")
	if err != nil {
		t.Fatal(err)
	}
	if managed.ID != "alpha-1" || managed.Name != "alpha-1" || managed.TmuxName != "alpha-1" {
		t.Fatalf("managed session identity = %#v", managed)
	}
	if managed.CWD != manager.cfg.HomeDir {
		t.Fatalf("managed cwd = %q, want %q", managed.CWD, manager.cfg.HomeDir)
	}
	if managed.PID <= 0 || bridgeFake.starts != 1 {
		t.Fatalf("bridge pid/starts = %d/%d, want one bridge", managed.PID, bridgeFake.starts)
	}
	created := tmuxFake.created["alpha-1"]
	if created.home != manager.cfg.HomeDir {
		t.Fatalf("tmux home = %q, want %q", created.home, manager.cfg.HomeDir)
	}
	if created.options.WindowSize != "manual" || created.options.Mouse != "off" || created.options.StatusLeft != "[alpha-1] " || created.options.SSHAuthSock != manager.agentSocket {
		t.Fatalf("tmux options = %#v", created.options)
	}
	assertMode(t, filepath.Join(manager.cfg.StateDir, "sessions", "alpha-1.json"), 0o600)
	for _, directory := range []string{"", "sessions", "sockets", "logs", "locks", "resize", "agent"} {
		assertMode(t, filepath.Join(manager.cfg.StateDir, directory), 0o700)
	}
}

func TestExistingManagedSessionGetsStableAgentEnvironmentWithoutRecreation(t *testing.T) {
	manager, tmuxFake, _ := newTestManager(t)
	if _, err := manager.Create(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	tmuxFake.environment["alpha"]["SSH_AUTH_SOCK"] = "/old/forwarded.sock"

	if _, err := manager.Create(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if got := tmuxFake.environment["alpha"]["SSH_AUTH_SOCK"]; got != manager.agentSocket {
		t.Fatalf("managed session SSH_AUTH_SOCK = %q, want %q", got, manager.agentSocket)
	}
	if tmuxFake.createCalls != 1 {
		t.Fatalf("tmux create calls = %d, want existing session unchanged", tmuxFake.createCalls)
	}
	if tmuxFake.historyCalls != 1 {
		t.Fatalf("history configuration calls = %d, want existing session reconciled", tmuxFake.historyCalls)
	}
	if tmuxFake.sizingCalls != 1 {
		t.Fatalf("manual sizing configuration calls = %d, want existing session reconciled", tmuxFake.sizingCalls)
	}
}

func TestConcurrentCreateProducesOneSessionAndBridge(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)
	secondManager := newManager(manager.cfg, registry.NewStore(manager.cfg.StateDir), tmuxFake, bridgeFake)
	results := make(chan registry.Session, 2)
	errorsChannel := make(chan error, 2)
	var wait sync.WaitGroup
	for _, creator := range []*Manager{manager, secondManager} {
		wait.Add(1)
		go func(manager *Manager) {
			defer wait.Done()
			managed, err := manager.Create(context.Background(), "shared")
			results <- managed
			errorsChannel <- err
		}(creator)
	}
	wait.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	var pid int
	for managed := range results {
		if pid == 0 {
			pid = managed.PID
		}
		if managed.PID != pid {
			t.Fatalf("concurrent create pids differ: %d and %d", pid, managed.PID)
		}
	}
	if tmuxFake.createCalls != 1 || bridgeFake.starts != 1 {
		t.Fatalf("tmux creates/bridge starts = %d/%d, want 1/1", tmuxFake.createCalls, bridgeFake.starts)
	}
}

func TestConcurrentCreateAndTerminateAreSerialized(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)
	if _, err := manager.Create(context.Background(), "shared"); err != nil {
		t.Fatal(err)
	}
	secondManager := newManager(manager.cfg, registry.NewStore(manager.cfg.StateDir), tmuxFake, bridgeFake)
	start := make(chan struct{})
	type createResult struct {
		created bool
		err     error
	}
	createdResult := make(chan createResult, 1)
	terminatedResult := make(chan error, 1)
	go func() {
		<-start
		_, created, err := secondManager.CreateOrSelect(context.Background(), "shared")
		createdResult <- createResult{created: created, err: err}
	}()
	go func() {
		<-start
		terminatedResult <- manager.Terminate(context.Background(), "shared")
	}()
	close(start)

	create := <-createdResult
	if create.err != nil {
		t.Fatalf("concurrent create failed: %v", create.err)
	}
	if err := <-terminatedResult; err != nil {
		t.Fatalf("concurrent terminate failed: %v", err)
	}
	managed, readErr := manager.store.Read("shared")
	if create.created {
		if readErr != nil || !tmuxFake.sessions[managed.TmuxName] || bridgeFake.running[managed.ID] == 0 {
			t.Fatalf("terminate-then-create result is not usable: managed=%#v readErr=%v", managed, readErr)
		}
		return
	}
	if !errors.Is(readErr, os.ErrNotExist) || tmuxFake.sessions["shared"] || bridgeFake.running["shared"] != 0 {
		t.Fatalf("create-then-terminate left state: readErr=%v tmux=%v bridge=%d", readErr, tmuxFake.sessions["shared"], bridgeFake.running["shared"])
	}
}

func TestCreateExistingHealthySessionIsIdempotent(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)
	first, created, err := manager.CreateOrSelect(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first create was reported as an existing session")
	}
	second, created, err := manager.CreateOrSelect(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second create was reported as a new session")
	}
	if first != second {
		t.Fatalf("second create = %#v, want %#v", second, first)
	}
	if tmuxFake.createCalls != 1 || bridgeFake.starts != 1 {
		t.Fatalf("tmux creates/bridge starts = %d/%d, want 1/1", tmuxFake.createCalls, bridgeFake.starts)
	}
}

func TestCreateRejectsUnmanagedTmuxNameConflict(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)
	tmuxFake.sessions["taken"] = true

	_, err := manager.Create(context.Background(), "taken")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if !tmuxFake.sessions["taken"] || tmuxFake.createCalls != 0 || bridgeFake.starts != 0 {
		t.Fatal("unmanaged tmux session was modified")
	}
}

func TestCreateRejectsInvalidNamesWithoutSanitizing(t *testing.T) {
	manager, tmuxFake, _ := newTestManager(t)
	for _, name := range []string{"bad name", "quoted\"name", "semi;colon", "line\nbreak", "$(touch-pwned)", "-leading", "x/../y"} {
		if _, err := manager.Create(context.Background(), name); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("Create(%q) error = %v, want ErrInvalidName", name, err)
		}
	}
	if tmuxFake.createCalls != 0 {
		t.Fatalf("invalid names created %d tmux sessions", tmuxFake.createCalls)
	}
}

func TestEnsureBridgeReturnsTypedNotFoundError(t *testing.T) {
	manager, _, _ := newTestManager(t)
	if _, err := manager.EnsureBridge(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestWithSessionSerializesInFlightUseWithTermination(t *testing.T) {
	manager, _, _ := newTestManager(t)
	if _, err := manager.Create(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	useEntered := make(chan struct{})
	releaseUse := make(chan struct{})
	useResult := make(chan error, 1)
	go func() {
		useResult <- manager.WithSession(context.Background(), "alpha", func(managed registry.Session) error {
			if managed.ID != "alpha" {
				return fmt.Errorf("resolved session %q", managed.ID)
			}
			close(useEntered)
			<-releaseUse
			return nil
		})
	}()
	<-useEntered
	terminateResult := make(chan error, 1)
	go func() {
		terminateResult <- manager.Terminate(context.Background(), "alpha")
	}()
	select {
	case err := <-terminateResult:
		t.Fatalf("termination completed while session use was in flight: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseUse)
	if err := <-useResult; err != nil {
		t.Fatal(err)
	}
	if err := <-terminateResult; err != nil {
		t.Fatal(err)
	}
	if err := manager.WithSession(context.Background(), "alpha", func(registry.Session) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("post-termination use error = %v, want ErrNotFound", err)
	}
}

func TestReconcileRecoversLegacyRecordBridgeMetadata(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)
	legacy := registry.Session{
		ID:        "legacy",
		Name:      "Legacy display",
		TmuxName:  "legacy",
		Socket:    filepath.Join(manager.socketsDir, "legacy.sock"),
		PID:       777,
		CWD:       "/old/workdir",
		CreatedAt: "2026-05-15T12:00:00Z",
	}
	writeRawRegistrySession(t, manager.cfg.StateDir, legacy)
	tmuxFake.sessions[legacy.TmuxName] = true

	sessions, err := manager.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != legacy.ID || sessions[0].PID == legacy.PID {
		t.Fatalf("reconciled sessions = %#v", sessions)
	}
	if bridgeFake.starts != 1 {
		t.Fatalf("bridge starts = %d, want 1", bridgeFake.starts)
	}
	stored, err := manager.store.Read(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != legacy.ID || stored.CWD != legacy.CWD || stored.CreatedAt != legacy.CreatedAt || stored.PID != sessions[0].PID {
		t.Fatalf("stored legacy session = %#v", stored)
	}
}

func TestReconcileDoesNotAdoptUnsafeLegacyIdentity(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)
	unsafe := registry.Session{
		ID:        "legacy",
		Name:      "legacy",
		TmuxName:  "other",
		Socket:    filepath.Join(manager.socketsDir, "legacy.sock"),
		PID:       777,
		CreatedAt: "2026-05-15T12:00:00Z",
	}
	writeRawRegistrySession(t, manager.cfg.StateDir, unsafe)
	tmuxFake.sessions[unsafe.TmuxName] = true

	sessions, err := manager.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 || bridgeFake.starts != 0 {
		t.Fatalf("unsafe legacy record was adopted: sessions/starts = %#v/%d", sessions, bridgeFake.starts)
	}
	if !tmuxFake.sessions[unsafe.TmuxName] {
		t.Fatal("unsafe legacy tmux session was modified")
	}
}

func TestReconcileRemovesRecordAndArtifactsWhenTmuxIsGone(t *testing.T) {
	manager, _, bridgeFake := newTestManager(t)
	managed := registry.Session{
		ID:        "stale",
		Name:      "stale",
		PublicRef: "ssssssssssssssssssssssssssssssss",
		TmuxName:  "stale",
		Socket:    filepath.Join(manager.socketsDir, "stale.sock"),
		PID:       1001,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := manager.store.Write(managed); err != nil {
		t.Fatal(err)
	}
	bridgeFake.running[managed.ID] = managed.PID
	for _, path := range []string{
		managed.Socket,
		filepath.Join(manager.logsDir, managed.ID+".log"),
		filepath.Join(manager.resizeDir, managed.ID+".json"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("state"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	sessions, err := manager.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 || bridgeFake.stops != 1 {
		t.Fatalf("sessions/stops = %d/%d, want 0/1", len(sessions), bridgeFake.stops)
	}
	for _, path := range []string{
		filepath.Join(manager.cfg.StateDir, "sessions", managed.ID+".json"),
		managed.Socket,
		filepath.Join(manager.logsDir, managed.ID+".log"),
		filepath.Join(manager.resizeDir, managed.ID+".json"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("artifact %s still exists: %v", path, err)
		}
	}
}

func TestTerminateStopsBridgeKillsTmuxAndRemovesState(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)
	managed, err := manager.Create(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Terminate(context.Background(), managed.ID); err != nil {
		t.Fatal(err)
	}
	if tmuxFake.sessions[managed.TmuxName] || tmuxFake.killCalls != 1 || bridgeFake.stops != 1 {
		t.Fatalf("tmux alive/kills/bridge stops = %v/%d/%d", tmuxFake.sessions[managed.TmuxName], tmuxFake.killCalls, bridgeFake.stops)
	}
	if _, err := manager.store.Read(managed.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed record remains: %v", err)
	}
}

func TestTerminateClassifiesManagedRecordReadErrors(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, *Manager)
		wantError error
	}{
		{
			name:      "missing record",
			prepare:   func(*testing.T, *Manager) {},
			wantError: ErrNotFound,
		},
		{
			name: "invalid record",
			prepare: func(t *testing.T, manager *Manager) {
				path := filepath.Join(manager.cfg.StateDir, "sessions", "alpha.json")
				if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: ErrNotFound,
		},
		{
			name: "operational read failure",
			prepare: func(t *testing.T, manager *Manager) {
				path := filepath.Join(manager.cfg.StateDir, "sessions", "alpha.json")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
			wantError: ErrDependency,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, tmuxFake, bridgeFake := newTestManager(t)
			test.prepare(t, manager)
			err := manager.Terminate(context.Background(), "alpha")
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if tmuxFake.killCalls != 0 || bridgeFake.stops != 0 {
				t.Fatalf("read failure invoked tmux/bridge cleanup: kills/stops = %d/%d", tmuxFake.killCalls, bridgeFake.stops)
			}
		})
	}
}

func TestTerminatePropagatesLegacyMigrationWriteFailure(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)
	legacy := registry.Session{
		ID:        "legacy",
		Name:      "Legacy display",
		TmuxName:  "legacy",
		Socket:    filepath.Join(manager.socketsDir, "legacy.sock"),
		PID:       777,
		CreatedAt: "2026-05-15T12:00:00Z",
	}
	writeRawRegistrySession(t, manager.cfg.StateDir, legacy)
	tmuxFake.sessions[legacy.ID] = true
	manager.store = &writeFailingRegistryStore{
		registryStore: manager.store,
		err:           errors.New("registry write unavailable"),
	}

	err := manager.Terminate(context.Background(), legacy.ID)
	if !errors.Is(err, ErrDependency) || errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want only ErrDependency", err)
	}
	if !tmuxFake.sessions[legacy.ID] || tmuxFake.killCalls != 0 || bridgeFake.stops != 0 {
		t.Fatalf("migration failure modified runtime state: alive/kills/stops = %v/%d/%d", tmuxFake.sessions[legacy.ID], tmuxFake.killCalls, bridgeFake.stops)
	}
}

func TestTerminateCleanupRunsBeforeReplacementCanBeCreated(t *testing.T) {
	manager, _, _ := newTestManager(t)
	if _, err := manager.Create(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	terminateResult := make(chan error, 1)
	go func() {
		terminateResult <- manager.TerminateWithCleanup(context.Background(), "alpha", func() {
			close(cleanupEntered)
			<-releaseCleanup
		})
	}()
	<-cleanupEntered
	type createResult struct {
		created bool
		err     error
	}
	createDone := make(chan createResult, 1)
	go func() {
		_, created, err := manager.CreateOrSelect(context.Background(), "alpha")
		createDone <- createResult{created: created, err: err}
	}()
	select {
	case result := <-createDone:
		t.Fatalf("replacement completed before termination cleanup: %#v", result)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseCleanup)
	if err := <-terminateResult; err != nil {
		t.Fatal(err)
	}
	result := <-createDone
	if result.err != nil || !result.created {
		t.Fatalf("replacement result = %#v, want newly created session", result)
	}
}

func TestTerminatePreservesRecordForRetryAfterFailures(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fakeTmuxLifecycle, *fakeBridgeLifecycle)
		clear     func(*fakeTmuxLifecycle, *fakeBridgeLifecycle)
	}{
		{
			name: "bridge stop",
			configure: func(_ *fakeTmuxLifecycle, bridge *fakeBridgeLifecycle) {
				bridge.stopErr = errors.New("stop failed")
			},
			clear: func(_ *fakeTmuxLifecycle, bridge *fakeBridgeLifecycle) { bridge.stopErr = nil },
		},
		{
			name: "tmux check",
			configure: func(tmux *fakeTmuxLifecycle, _ *fakeBridgeLifecycle) {
				tmux.hasErr = errors.New("check failed")
			},
			clear: func(tmux *fakeTmuxLifecycle, _ *fakeBridgeLifecycle) { tmux.hasErr = nil },
		},
		{
			name: "tmux kill",
			configure: func(tmux *fakeTmuxLifecycle, _ *fakeBridgeLifecycle) {
				tmux.killErr = errors.New("kill failed")
			},
			clear: func(tmux *fakeTmuxLifecycle, _ *fakeBridgeLifecycle) { tmux.killErr = nil },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, tmuxFake, bridgeFake := newTestManager(t)
			managed, err := manager.Create(context.Background(), "retry")
			if err != nil {
				t.Fatal(err)
			}
			artifact := filepath.Join(manager.logsDir, managed.ID+".log")
			if err := os.WriteFile(artifact, []byte("bridge metadata"), 0o600); err != nil {
				t.Fatal(err)
			}
			test.configure(tmuxFake, bridgeFake)
			if err := manager.Terminate(context.Background(), managed.ID); !errors.Is(err, ErrDependency) {
				t.Fatalf("err = %v, want ErrDependency", err)
			}
			if _, err := manager.store.Read(managed.ID); err != nil {
				t.Fatalf("durable record was removed after failure: %v", err)
			}
			if _, err := os.Stat(artifact); err != nil {
				t.Fatalf("session artifact was removed after failure: %v", err)
			}
			test.clear(tmuxFake, bridgeFake)
			if err := manager.Terminate(context.Background(), managed.ID); err != nil {
				t.Fatalf("retry termination failed: %v", err)
			}
			if _, err := manager.store.Read(managed.ID); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("record remains after successful retry: %v", err)
			}
		})
	}
}

func TestReconcilePreservesMissingTmuxRecordWhenBridgeStopFails(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)
	managed, err := manager.Create(context.Background(), "retry")
	if err != nil {
		t.Fatal(err)
	}
	delete(tmuxFake.sessions, managed.TmuxName)
	bridgeFake.stopErr = errors.New("stop failed")
	if _, err := manager.Reconcile(context.Background()); !errors.Is(err, ErrDependency) {
		t.Fatalf("err = %v, want ErrDependency", err)
	}
	if _, err := manager.store.Read(managed.ID); err != nil {
		t.Fatalf("durable record was removed after bridge stop failure: %v", err)
	}
	bridgeFake.stopErr = nil
	if sessions, err := manager.Reconcile(context.Background()); err != nil || len(sessions) != 0 {
		t.Fatalf("retry reconcile sessions/error = %#v/%v", sessions, err)
	}
	if _, err := manager.store.Read(managed.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record remains after successful retry: %v", err)
	}
}

func TestFailedCreationDoesNotWriteUsableRecord(t *testing.T) {
	manager, tmuxFake, bridgeFake := newTestManager(t)
	bridgeFake.ensureErr = lifecycleError(ErrorBridgeIncomplete, "start bridge", "broken", errors.New("test failure"))

	_, err := manager.Create(context.Background(), "broken")
	if !errors.Is(err, ErrBridgeIncomplete) {
		t.Fatalf("err = %v, want ErrBridgeIncomplete", err)
	}
	if tmuxFake.sessions["broken"] || tmuxFake.killCalls != 1 {
		t.Fatalf("failed creation tmux alive/kills = %v/%d", tmuxFake.sessions["broken"], tmuxFake.killCalls)
	}
	if _, err := manager.store.Read("broken"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed creation left registry record: %v", err)
	}
}

func TestStopDoesNotKillUnrelatedProcessFromStalePID(t *testing.T) {
	command := exec.Command("sleep", "30")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})
	bridge := &processBridge{ttydBinary: "ttyd", tmuxBinary: "tmux"}
	managed := registry.Session{ID: "alpha", TmuxName: "alpha", Socket: "/tmp/alpha.sock", PID: command.Process.Pid}

	if err := bridge.Stop(context.Background(), managed); err != nil {
		t.Fatal(err)
	}
	if err := command.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unrelated process was stopped: %v", err)
	}
}

func TestStopDoesNotKillProcessWithSpoofedBridgeArguments(t *testing.T) {
	managed := registry.Session{ID: "alpha", TmuxName: "alpha", Socket: "/tmp/alpha.sock"}
	command := startBridgeHelperProcess(t, "true", managed)
	defer stopAndWaitTestProcess(command)
	bridge := &processBridge{ttydBinary: "/bin/true", tmuxBinary: "tmux"}

	if kind := bridge.pidKind(managed, command.Process.Pid); kind != bridgeCommandUnrelated {
		t.Fatalf("spoofed bridge process kind = %d, want unrelated", kind)
	}
	if err := bridge.Stop(context.Background(), managed); err != nil {
		t.Fatal(err)
	}
	if err := command.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("process with spoofed argv was stopped: %v", err)
	}
}

func TestStopRequiresGoOwnedZombieToBeReaped(t *testing.T) {
	managed := registry.Session{ID: "alpha", TmuxName: "alpha", Socket: "/tmp/alpha.sock"}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := startBridgeHelperProcess(t, filepath.Base(executable), managed)
	waited := false
	t.Cleanup(func() {
		if !waited {
			stopAndWaitTestProcess(command)
		}
	})
	bridge := &processBridge{ttydBinary: executable, tmuxBinary: "tmux"}
	reaped := make(chan struct{})
	bridge.registerOwned(command.Process.Pid, reaped)

	err = bridge.stopVerifiedPID(context.Background(), managed, command.Process.Pid)
	if err == nil || !strings.Contains(err.Error(), "Go child was not reaped") {
		t.Fatalf("stop error = %v, want unreaped zombie failure", err)
	}
	if state, stateErr := processState(command.Process.Pid); stateErr != nil || state != "Z" {
		t.Fatalf("helper process state/error = %q/%v, want zombie", state, stateErr)
	}
	if waitErr := command.Wait(); waitErr == nil {
		t.Fatal("terminated helper unexpectedly exited successfully")
	}
	close(reaped)
	bridge.unregisterOwned(command.Process.Pid, reaped)
	waited = true
	if _, statErr := os.Stat(filepath.Join("/proc", strconv.Itoa(command.Process.Pid))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("helper process was not reaped after Wait: %v", statErr)
	}
}

func TestBridgeCommandVerificationDistinguishesCurrentAndLegacyCommands(t *testing.T) {
	managed := registry.Session{ID: "alpha", TmuxName: "alpha", Socket: "/state/sockets/alpha.sock"}
	valid := append([]string{"/usr/bin/ttyd"}, bridgeArguments(managed, 10000, "tmux")...)
	if !bridgeCommandMatches(valid, "ttyd", "tmux", managed) {
		t.Fatal("valid bridge command did not match")
	}
	if legacyBridgeCommandMatches(valid, "ttyd", "tmux", managed) {
		t.Fatal("current bridge command matched as legacy")
	}
	wrongSocket := append([]string(nil), valid...)
	wrongSocket[3] = managed.Socket + ".other"
	if bridgeCommandMatches(wrongSocket, "ttyd", "tmux", managed) || legacyBridgeCommandMatches(wrongSocket, "ttyd", "tmux", managed) {
		t.Fatal("bridge command with another socket matched")
	}
	wrongSession := append([]string(nil), valid...)
	wrongSession[len(wrongSession)-1] = "alpha-other"
	if bridgeCommandMatches(wrongSession, "ttyd", "tmux", managed) || legacyBridgeCommandMatches(wrongSession, "ttyd", "tmux", managed) {
		t.Fatal("bridge command with another tmux session matched")
	}
	legacy := append([]string(nil), valid...)
	for index, argument := range legacy {
		if argument == "-E" {
			legacy = append(legacy[:index], legacy[index+1:]...)
			break
		}
	}
	if bridgeCommandMatches(legacy, "ttyd", "tmux", managed) {
		t.Fatal("bridge command that can update the managed session environment matched")
	}
	if !legacyBridgeCommandMatches(legacy, "ttyd", "tmux", managed) {
		t.Fatal("verified legacy bridge command was not recognized for migration")
	}
	legacyWithTrailingCommand := append(append([]string(nil), legacy...), "sleep", "60")
	if bridgeCommandMatches(legacyWithTrailingCommand, "ttyd", "tmux", managed) || legacyBridgeCommandMatches(legacyWithTrailingCommand, "ttyd", "tmux", managed) {
		t.Fatal("bridge command with an unrelated trailing command matched")
	}
}

func TestBridgeUsesOnlyPrivateUnixSocketTransport(t *testing.T) {
	managed := registry.Session{ID: "alpha", PublicRef: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TmuxName: "alpha", Socket: "/state/sockets/alpha.sock"}
	arguments := bridgeArguments(managed, 10000, "tmux")
	if !hasOptionValue(arguments, "-i", managed.Socket) {
		t.Fatalf("bridge arguments lack unix socket: %#v", arguments)
	}
	for _, argument := range arguments {
		if argument == "-p" || strings.HasPrefix(argument, "--port") || argument == "0.0.0.0" {
			t.Fatalf("bridge arguments expose a TCP listener: %#v", arguments)
		}
	}
}

func TestSecureSocketRestrictsModeAndRejectsSymlink(t *testing.T) {
	directory, err := os.MkdirTemp(".", ".socket-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "ttyd.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(path, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := secureSocket(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket mode = %o, want 0600", got)
	}
	alias := filepath.Join(directory, "alias.sock")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if err := secureSocket(alias); err == nil {
		t.Fatal("symlink socket endpoint was accepted")
	}
}

func TestBridgeReconcilePlanReplacesLegacyAndStopsOnlyVerifiedProcesses(t *testing.T) {
	tests := []struct {
		name          string
		verified      verifiedBridgeProcesses
		registeredPID int
		ready         bool
		wantKeep      int
		wantStop      []int
	}{
		{
			name:          "legacy bridge is replaced",
			verified:      verifiedBridgeProcesses{legacy: []int{101}},
			registeredPID: 101,
			ready:         true,
			wantStop:      []int{101},
		},
		{
			name:          "current bridge is kept and legacy duplicate is stopped",
			verified:      verifiedBridgeProcesses{current: []int{202}, legacy: []int{101}},
			registeredPID: 101,
			ready:         true,
			wantKeep:      202,
			wantStop:      []int{101},
		},
		{
			name:          "registered current bridge wins",
			verified:      verifiedBridgeProcesses{current: []int{202, 303}, legacy: []int{101}},
			registeredPID: 303,
			ready:         true,
			wantKeep:      303,
			wantStop:      []int{101, 202},
		},
		{
			name:          "unready bridge set is fully replaced",
			verified:      verifiedBridgeProcesses{current: []int{202}, legacy: []int{101}},
			registeredPID: 202,
			wantStop:      []int{101, 202},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			keep, stop := test.verified.reconcilePlan(test.registeredPID, test.ready)
			if keep != test.wantKeep || fmt.Sprint(stop) != fmt.Sprint(test.wantStop) {
				t.Fatalf("plan keep/stop = %d/%v, want %d/%v", keep, stop, test.wantKeep, test.wantStop)
			}
		})
	}
}

func TestBridgeReportsMissingTtydAsDependencyFailure(t *testing.T) {
	dir := t.TempDir()
	bridge := &processBridge{
		ttydBinary: "control-agents-definitely-missing-ttyd",
		tmuxBinary: "tmux",
		logsDir:    dir,
		scrollback: 10000,
		timeout:    time.Second,
		poll:       time.Millisecond,
	}
	managed := registry.Session{
		ID:       "alpha",
		TmuxName: "alpha",
		Socket:   "/tmp/control-agents-missing-ttyd.sock",
	}
	if _, err := bridge.start(context.Background(), managed); !errors.Is(err, ErrDependency) {
		t.Fatalf("err = %v, want ErrDependency", err)
	}
}

func TestBridgeReportsEarlyTtydExitAsIncompleteStartup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "early-exit.pid")
	t.Setenv("CONTROL_AGENTS_TEST_PID_FILE", pidFile)
	ttyd := filepath.Join(dir, "early-exit-ttyd")
	if err := os.WriteFile(ttyd, []byte("#!/bin/sh\nprintf '%s' \"$$\" > \"$CONTROL_AGENTS_TEST_PID_FILE\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	bridge := &processBridge{
		ttydBinary: ttyd,
		tmuxBinary: "tmux",
		logsDir:    dir,
		scrollback: 10000,
		timeout:    200 * time.Millisecond,
		poll:       time.Millisecond,
	}
	managed := registry.Session{
		ID:       "alpha",
		TmuxName: "alpha",
		Socket:   "/tmp/control-agents-failed-ttyd.sock",
	}
	if _, err := bridge.start(context.Background(), managed); !errors.Is(err, ErrBridgeIncomplete) {
		t.Fatalf("err = %v, want ErrBridgeIncomplete", err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); !errors.Is(err, os.ErrNotExist) {
		state, _ := processState(pid)
		t.Fatalf("early ttyd child was not reaped (state %q): %v", state, err)
	}
}

func TestBridgeHelperProcess(t *testing.T) {
	if os.Getenv("CONTROL_AGENTS_BRIDGE_HELPER") != "1" {
		return
	}
	for {
		time.Sleep(time.Second)
	}
}

func startBridgeHelperProcess(t *testing.T, argv0 string, managed registry.Session) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0])
	command.Args = append(
		[]string{argv0, "-test.run=^TestBridgeHelperProcess$", "--"},
		bridgeArguments(managed, 10000, "tmux")...,
	)
	command.Env = append(os.Environ(), "CONTROL_AGENTS_BRIDGE_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		arguments, err := processCommandLine(command.Process.Pid)
		if err == nil && classifyBridgeCommand(arguments, filepath.Base(argv0), "tmux", managed) == bridgeCommandCurrent {
			return command
		}
		if time.Now().After(deadline) {
			stopAndWaitTestProcess(command)
			t.Fatalf("bridge helper did not start with matching arguments: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func stopAndWaitTestProcess(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Kill()
	_ = command.Wait()
}

type createdTmux struct {
	home    string
	options tmux.ManagedSessionOptions
}

type fakeTmuxLifecycle struct {
	mu           sync.Mutex
	sessions     map[string]bool
	created      map[string]createdTmux
	createCalls  int
	historyCalls int
	sizingCalls  int
	killCalls    int
	hasErr       error
	killErr      error
	environment  map[string]map[string]string
}

func (f *fakeTmuxLifecycle) HasSession(ctx context.Context, target string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hasErr != nil {
		return false, f.hasErr
	}
	return f.sessions[target], nil
}

func (f *fakeTmuxLifecycle) CreateManagedSession(ctx context.Context, name, home string, options tmux.ManagedSessionOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.sessions[name] = true
	f.created[name] = createdTmux{home: home, options: options}
	f.environment[name] = map[string]string{"SSH_AUTH_SOCK": options.SSHAuthSock}
	return nil
}

func (f *fakeTmuxLifecycle) ConfigureHistory(ctx context.Context, target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.historyCalls++
	return nil
}

func (f *fakeTmuxLifecycle) ConfigureManualWindowSize(ctx context.Context, target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizingCalls++
	return nil
}

func (f *fakeTmuxLifecycle) SetSessionEnvironment(ctx context.Context, target, name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.environment[target] == nil {
		f.environment[target] = make(map[string]string)
	}
	f.environment[target][name] = value
	return nil
}

func (f *fakeTmuxLifecycle) KillSession(ctx context.Context, target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.killErr != nil {
		return f.killErr
	}
	f.killCalls++
	delete(f.sessions, target)
	return nil
}

type fakeBridgeLifecycle struct {
	mu        sync.Mutex
	nextPID   int
	running   map[string]int
	starts    int
	stops     int
	ensureErr error
	stopErr   error
}

type writeFailingRegistryStore struct {
	registryStore
	err error
}

func (s *writeFailingRegistryStore) Write(registry.Session) error {
	return s.err
}

func (f *fakeBridgeLifecycle) Ensure(ctx context.Context, managed registry.Session) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ensureErr != nil {
		return 0, f.ensureErr
	}
	if pid := f.running[managed.ID]; pid > 0 {
		return pid, nil
	}
	f.nextPID++
	f.starts++
	f.running[managed.ID] = f.nextPID
	return f.nextPID, nil
}

func (f *fakeBridgeLifecycle) Stop(ctx context.Context, managed registry.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopErr != nil {
		return f.stopErr
	}
	if f.running[managed.ID] > 0 {
		f.stops++
	}
	delete(f.running, managed.ID)
	return nil
}

func newTestManager(t *testing.T) (*Manager, *fakeTmuxLifecycle, *fakeBridgeLifecycle) {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "state")
	cfg := withDefaults(Config{
		StateDir: stateDir,
		HomeDir:  filepath.Join(t.TempDir(), "home"),
	})
	if err := os.MkdirAll(cfg.HomeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store := registry.NewStore(stateDir)
	tmuxFake := &fakeTmuxLifecycle{sessions: make(map[string]bool), created: make(map[string]createdTmux), environment: make(map[string]map[string]string)}
	bridgeFake := &fakeBridgeLifecycle{nextPID: 1000, running: make(map[string]int)}
	manager := newManager(cfg, store, tmuxFake, bridgeFake)
	if err := manager.ensurePrivateState(); err != nil {
		t.Fatal(err)
	}
	return manager, tmuxFake, bridgeFake
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
	}
}

func writeRawRegistrySession(t *testing.T, stateDir string, managed registry.Session) {
	t.Helper()
	data, err := json.Marshal(managed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "sessions", managed.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
