package e2e

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	managedsession "control-agents/internal/session"
	managedtmux "control-agents/internal/tmux"
)

func TestRealTmuxAndTtydSessionAppears(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sessionName := fmt.Sprintf("e2e-%d", os.Getpid())
	stateDir := compactRealProcessStateDir(t, root, "rs", sessionName)
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer killRegisteredTtyd(stateDir, sessionName)

	port := freePort(t)
	app := exec.CommandContext(ctx, "go", "run", "../../cmd/server")
	app.Env = environmentWith(map[string]string{
		"LANG":                     "C",
		"LC_ALL":                   "C",
		"CONTROL_AGENTS_PASSWORD":  "secret",
		"CONTROL_AGENTS_BIND_ADDR": "127.0.0.1",
		"CONTROL_AGENTS_PORT":      strconv.Itoa(port),
		"CONTROL_AGENTS_STATE_DIR": stateDir,
	})
	var appLog bytes.Buffer
	app.Stdout = &appLog
	app.Stderr = &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer app.Process.Kill()
	client := insecureHTTPSClient()
	waitForHTTP(t, ctx, client, fmt.Sprintf("https://127.0.0.1:%d/login", port))

	clientCommand := exec.CommandContext(ctx, "../../bin/control-agents", sessionName)
	clientCommand.Env = environmentWith(map[string]string{
		"LANG":                                "C",
		"LC_ALL":                              "C",
		"CONTROL_AGENTS_STATE_DIR":            stateDir,
		"CONTROL_AGENTS_NO_ATTACH":            "1",
		"CONTROL_AGENTS_WEB_SCROLLBACK_LINES": "2345",
	})
	if output, err := clientCommand.CombinedOutput(); err != nil {
		t.Fatalf("client failed: %v\n%s", err, output)
	}

	waitForSession(t, ctx, client, port, sessionName)
	tmuxCreated := tmuxSessionCreated(t, sessionName)
	assertTmuxWindowSize(t, sessionName, "manual")
	assertTmuxPaneHistoryLimit(t, sessionName, 50000)
	assertTmuxMouse(t, sessionName, "off")
	assertTmuxOption(t, sessionName, "status-left-length", "80")
	assertTmuxOption(t, sessionName, "status-left", "["+sessionName+"] ")
	assertTmuxOption(t, sessionName, "status-right", "#{pane_current_path}")
	assertTtydCommandLineContains(t, stateDir, sessionName, "scrollback", "2345")

	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/terminal/%s/", port, sessionName))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated terminal status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	oldPID := readRegisteredTtydPID(stateDir, sessionName)
	stopProcess(t, oldPID)
	waitForProcessExit(t, ctx, oldPID)
	waitForSession(t, ctx, client, port, sessionName)
	newPID := readRegisteredTtydPID(stateDir, sessionName)
	if newPID <= 0 || newPID == oldPID {
		t.Fatalf("reconciled bridge pid = %d, want replacement for %d", newPID, oldPID)
	}
	if got := tmuxSessionCreated(t, sessionName); got != tmuxCreated {
		t.Fatalf("tmux session was recreated during bridge recovery: created %q, want %q", got, tmuxCreated)
	}

	if output, err := exec.Command("tmux", "kill-session", "-t", sessionName).CombinedOutput(); err != nil {
		t.Fatalf("kill tmux session: %v\n%s", err, output)
	}
	waitForSessionRemoval(t, ctx, client, port, sessionName)
	for _, path := range []string{
		filepath.Join(stateDir, "sessions", sessionName+".json"),
		filepath.Join(stateDir, "sockets", sessionName+".sock"),
		filepath.Join(stateDir, "logs", sessionName+".log"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale artifact %s remains: %v", path, err)
		}
	}
	waitForProcessExit(t, ctx, newPID)
}

func TestLifecycleCreatesAndTerminatesRealManagedSession(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sessionName := fmt.Sprintf("lifecycle-e2e-%d", os.Getpid())
	stateDir := compactRealProcessStateDir(t, root, "lc", sessionName)
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	homeDir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(homeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer killRegisteredTtyd(stateDir, sessionName)

	lifecycle, err := managedsession.New(managedsession.Config{
		StateDir:                stateDir,
		HomeDir:                 homeDir,
		WebScrollbackLines:      4321,
		WebScrollbackConfigured: true,
		BridgeStartupTimeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	managed, err := lifecycle.Create(ctx, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	if got := tmuxPanePath(t, sessionName); got != homeDir {
		t.Fatalf("initial tmux pane path = %q, want configured home %q", got, homeDir)
	}
	assertTtydCommandLineContains(t, stateDir, sessionName, "scrollback", "4321")

	if err := lifecycle.Terminate(ctx, sessionName); err != nil {
		t.Fatal(err)
	}
	if exec.Command("tmux", "has-session", "-t", sessionName).Run() == nil {
		t.Fatal("tmux session still exists after termination")
	}
	waitForProcessExit(t, ctx, managed.PID)
	if _, err := os.Stat(filepath.Join(stateDir, "sessions", sessionName+".json")); !os.IsNotExist(err) {
		t.Fatalf("registry record remains after termination: %v", err)
	}
}

func TestManagedCreationScopesHistoryAndMakesEveryWindowManual(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	managedName := fmt.Sprintf("policy-managed-%d", os.Getpid())
	unmanagedName := fmt.Sprintf("policy-unmanaged-%d", os.Getpid())
	stateDir := compactRealProcessStateDir(t, root, "pm", managedName)
	homeDir := t.TempDir()
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	for _, name := range []string{managedName, unmanagedName} {
		name := name
		defer exec.Command("tmux", "kill-session", "-t", name).Run()
	}
	defer killRegisteredTtyd(stateDir, managedName)

	if output, err := exec.Command("tmux", "new-session", "-d", "-s", unmanagedName, "-c", homeDir).CombinedOutput(); err != nil {
		t.Fatalf("create unmanaged tmux session: %v\n%s", err, output)
	}
	originalHistory := tmuxGlobalOption(t, "history-limit")
	originalWindowSize := tmuxGlobalWindowOption(t, "window-size")
	defer func() {
		_ = exec.Command("tmux", "set-option", "-g", "history-limit", originalHistory).Run()
		_ = exec.Command("tmux", "set-option", "-gw", "window-size", originalWindowSize).Run()
	}()
	if output, err := exec.Command("tmux", "set-option", "-g", "history-limit", "3456").CombinedOutput(); err != nil {
		t.Fatalf("set isolated global history baseline: %v\n%s", err, output)
	}
	if output, err := exec.Command("tmux", "set-option", "-gw", "window-size", "latest").CombinedOutput(); err != nil {
		t.Fatalf("set isolated global sizing baseline: %v\n%s", err, output)
	}

	lifecycle, err := managedsession.New(managedsession.Config{
		StateDir: stateDir, HomeDir: homeDir, BridgeStartupTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Create(ctx, managedName); err != nil {
		t.Fatal(err)
	}

	if got := tmuxGlobalOption(t, "history-limit"); got != "3456" {
		t.Fatalf("managed creation changed global history-limit to %q, want isolated baseline 3456", got)
	}
	if got := tmuxGlobalWindowOption(t, "window-size"); got != "latest" {
		t.Fatalf("managed creation changed global window-size to %q, want isolated baseline latest", got)
	}
	assertTmuxPaneHistoryLimit(t, unmanagedName, 3456)
	assertTmuxPaneHistoryLimit(t, managedName, 50000)
	assertTmuxWindowSize(t, managedName, "manual")

	initialWindows, err := exec.Command("tmux", "list-windows", "-t", managedName, "-F", "#{window_id}").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Fields(string(initialWindows))); got != 1 {
		t.Fatalf("managed session has %d initial windows, want one durable user window", got)
	}

	managedWindow, managedPane := createDetachedTmuxWindow(t, managedName)
	assertTmuxWindowSizeTarget(t, managedWindow, "manual")
	assertTmuxPaneHistoryLimit(t, managedPane, 50000)
	for _, windowID := range strings.Fields(string(mustTmuxOutput(t, "list-windows", "-t", managedName, "-F", "#{window_id}"))) {
		assertTmuxWindowSizeTarget(t, windowID, "manual")
	}

	unmanagedWindow, unmanagedPane := createDetachedTmuxWindow(t, unmanagedName)
	assertTmuxWindowSizeTarget(t, unmanagedWindow, "latest")
	assertTmuxPaneHistoryLimit(t, unmanagedPane, 3456)
	if got := tmuxGlobalOption(t, "history-limit"); got != "3456" {
		t.Fatalf("subsequent managed window changed global history-limit to %q", got)
	}

	if err := lifecycle.Terminate(ctx, managedName); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileHistoryLimitKeepsExistingPaneAllocationAndCoversNewPanes(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sessionName := fmt.Sprintf("history-existing-%d", os.Getpid())
	stateDir := compactRealProcessStateDir(t, root, "h", sessionName)
	homeDir := t.TempDir()
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer killRegisteredTtyd(stateDir, sessionName)

	if output, err := exec.Command(
		"tmux", "start-server", ";", "set-option", "-g", "history-limit", "2000", ";",
		"new-session", "-d", "-s", sessionName, "-c", homeDir,
	).CombinedOutput(); err != nil {
		t.Fatalf("create existing tmux session: %v\n%s", err, output)
	}
	assertTmuxPaneHistoryLimit(t, sessionName, 2000)
	if output, err := exec.Command("tmux", "send-keys", "-t", sessionName, "seq 1 2500", "C-m").CombinedOutput(); err != nil {
		t.Fatalf("fill existing pane history: %v\n%s", err, output)
	}
	beforeHistory := waitForTmuxHistoryAtLeast(t, ctx, sessionName, 1700)
	if beforeHistory > 2000 {
		t.Fatalf("legacy pane history = %d, want old 2000-line cap", beforeHistory)
	}

	store := registry.NewStore(stateDir)
	publicRef, err := registry.NewPublicRef()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(registry.Session{
		ID: sessionName, Name: sessionName, PublicRef: publicRef, TmuxName: sessionName,
		Socket: filepath.Join(stateDir, "sockets", sessionName+".sock"), CWD: homeDir,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := managedsession.New(managedsession.Config{
		StateDir: stateDir, HomeDir: homeDir, BridgeStartupTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	// Raising the limit cannot reconstruct lines already discarded under the
	// old cap. Tmux 3.7b updates the live pane's future limit in place.
	assertTmuxPaneHistoryLimit(t, sessionName, 50000)
	afterHistory := tmuxFormatInt(t, sessionName, "#{history_size}")
	if afterHistory > beforeHistory+5 || afterHistory >= 2500 {
		t.Fatalf("existing pane history was unexpectedly backfilled: before=%d after=%d", beforeHistory, afterHistory)
	}
	output, err := exec.Command("tmux", "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", sessionName).Output()
	if err != nil {
		t.Fatal(err)
	}
	newPane := strings.TrimSpace(string(output))
	assertTmuxPaneHistoryLimit(t, newPane, 50000)

	if err := lifecycle.Terminate(ctx, sessionName); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileMigratesLegacyAutomaticSizingToManualWithoutResizing(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sessionName := fmt.Sprintf("sizing-existing-%d", os.Getpid())
	stateDir := compactRealProcessStateDir(t, root, "sz", sessionName)
	homeDir := t.TempDir()
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer killRegisteredTtyd(stateDir, sessionName)

	if output, err := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", homeDir).CombinedOutput(); err != nil {
		t.Fatalf("create existing tmux session: %v\n%s", err, output)
	}
	if output, err := exec.Command("tmux", "set-option", "-w", "-t", sessionName+":", "window-size", "manual").CombinedOutput(); err != nil {
		t.Fatalf("prepare manual tmux dimensions: %v\n%s", err, output)
	}
	if output, err := exec.Command("tmux", "resize-window", "-t", sessionName+":", "-x", "137", "-y", "41").CombinedOutput(); err != nil {
		t.Fatalf("set legacy tmux dimensions: %v\n%s", err, output)
	}
	if output, err := exec.Command("tmux", "set-option", "-w", "-t", sessionName+":", "window-size", "smallest").CombinedOutput(); err != nil {
		t.Fatalf("set legacy automatic sizing: %v\n%s", err, output)
	}
	assertTmuxWindowSize(t, sessionName, "smallest")
	widthBefore := tmuxFormatInt(t, sessionName, "#{window_width}")
	heightBefore := tmuxFormatInt(t, sessionName, "#{window_height}")

	store := registry.NewStore(stateDir)
	publicRef, err := registry.NewPublicRef()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(registry.Session{
		ID: sessionName, Name: sessionName, PublicRef: publicRef, TmuxName: sessionName,
		Socket: filepath.Join(stateDir, "sockets", sessionName+".sock"), CWD: homeDir,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := managedsession.New(managedsession.Config{
		StateDir: stateDir, HomeDir: homeDir, BridgeStartupTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	assertTmuxWindowSize(t, sessionName, "manual")
	widthAfter := tmuxFormatInt(t, sessionName, "#{window_width}")
	heightAfter := tmuxFormatInt(t, sessionName, "#{window_height}")
	if widthAfter != widthBefore || heightAfter != heightBefore {
		t.Fatalf("reconciliation resized legacy window from %dx%d to %dx%d", widthBefore, heightBefore, widthAfter, heightAfter)
	}

	if err := lifecycle.Terminate(ctx, sessionName); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileMigratesRegisteredRelativeTmuxBridgeWithoutOrphan(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sessionName := fmt.Sprintf("bridge-migrate-%d", os.Getpid())
	stateDir := compactRealProcessStateDir(t, root, "bm", sessionName)
	homeDir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(homeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer killRegisteredTtyd(stateDir, sessionName)
	resolvedTmux, err := managedtmux.ResolveBinary(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(resolvedTmux) {
		t.Fatalf("resolved tmux path = %q, want absolute", resolvedTmux)
	}
	publicRef, err := registry.NewPublicRef()
	if err != nil {
		t.Fatal(err)
	}

	lifecycle, err := managedsession.New(managedsession.Config{
		StateDir:             stateDir,
		HomeDir:              homeDir,
		BridgeStartupTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", homeDir).CombinedOutput(); err != nil {
		t.Fatalf("create tmux session: %v\n%s", err, output)
	}

	socketPath := filepath.Join(stateDir, "sockets", sessionName+".sock")
	ttydPath, err := exec.LookPath("ttyd")
	if err != nil {
		t.Fatal(err)
	}
	legacyPIDPath := filepath.Join(stateDir, "legacy-ttyd.pid")
	legacyParent := exec.Command(
		"sh", "-c", `
"$1" -W -i "$2" -b "$3" -t scrollback=10000 tmux attach-session -E -t "$4" &
printf '%s\n' "$!" > "$5"
exec sleep 30
`,
		"previous-wrapper", ttydPath, socketPath, "/terminal/"+publicRef, sessionName, legacyPIDPath,
	)
	var legacyLog bytes.Buffer
	legacyParent.Stdout = &legacyLog
	legacyParent.Stderr = &legacyLog
	if err := legacyParent.Start(); err != nil {
		t.Fatal(err)
	}
	legacyPID := 0
	legacyParentWaited := false
	t.Cleanup(func() {
		if legacyPID > 0 && processAlive(legacyPID) {
			if process, findErr := os.FindProcess(legacyPID); findErr == nil {
				_ = process.Kill()
			}
		}
		if !legacyParentWaited {
			if processAlive(legacyParent.Process.Pid) {
				_ = legacyParent.Process.Kill()
			}
			_ = legacyParent.Wait()
		}
	})
	legacyPID = waitForPIDFile(t, ctx, legacyPIDPath)
	if parentPID, parentErr := processParentPID(legacyPID); parentErr != nil || parentPID != legacyParent.Process.Pid {
		t.Fatalf("legacy ttyd parent PID/error = %d/%v, want wrapper PID %d", parentPID, parentErr, legacyParent.Process.Pid)
	}
	previousArguments, err := readProcessArguments(legacyPID)
	if err != nil {
		t.Fatal(err)
	}
	wantPreviousArguments := []string{
		ttydPath,
		"-W",
		"-i", socketPath,
		"-b", "/terminal/" + publicRef,
		"-t", "scrollback=10000",
		"tmux", "attach-session", "-E", "-t", sessionName,
	}
	wantNormalizedArguments := append([]string(nil), wantPreviousArguments...)
	wantNormalizedArguments = append(wantNormalizedArguments[:7], append([]string{"scrollback", "10000"}, wantNormalizedArguments[8:]...)...)
	if strings.Join(previousArguments, "\x00") != strings.Join(wantPreviousArguments, "\x00") &&
		strings.Join(previousArguments, "\x00") != strings.Join(wantNormalizedArguments, "\x00") {
		t.Fatalf("previous bridge argv = %#v, want exact former argv %#v", previousArguments, wantPreviousArguments)
	}
	waitForUnixSocket(t, ctx, socketPath)
	previousSocketInode := unixSocketInode(t, socketPath)

	store := registry.NewStore(stateDir)
	if err := store.Write(registry.Session{
		ID:        sessionName,
		Name:      sessionName,
		PublicRef: publicRef,
		TmuxName:  sessionName,
		Socket:    socketPath,
		PID:       legacyPID,
		CWD:       homeDir,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}

	sessions, err := lifecycle.Reconcile(ctx)
	if err != nil {
		t.Fatalf("reconcile previous bridge: %v\nprevious log:\n%s", err, legacyLog.String())
	}
	assertFileMode(t, socketPath, 0o600)
	replacementSocketInode := unixSocketInode(t, socketPath)
	if replacementSocketInode == previousSocketInode {
		t.Fatal("reconciliation retained the previous bridge socket instead of replacing it")
	}
	if len(sessions) != 1 || sessions[0].ID != sessionName || sessions[0].PID == legacyPID {
		t.Fatalf("reconciled sessions = %#v, previous PID = %d", sessions, legacyPID)
	}
	waitForProcessState(t, ctx, legacyPID, "Z")
	if processAlive(legacyPID) {
		t.Fatalf("verified previous bridge PID %d remains alive", legacyPID)
	}
	bridgePIDs := ttydPIDsForSocket(t, socketPath)
	if len(bridgePIDs) != 1 || bridgePIDs[0] != sessions[0].PID {
		t.Fatalf("bridge PIDs for managed socket = %v, want only %d", bridgePIDs, sessions[0].PID)
	}
	assertTtydAttachSuffix(t, sessions[0].PID, resolvedTmux, sessionName)
	stablePath, err := managedsession.ForwardedAgentSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	assertTmuxEnvironment(t, sessionName, "SSH_AUTH_SOCK", stablePath)
	migratedPaneEnvironment := filepath.Join(stateDir, "migrated-pane-agent-path")
	createTmuxPane(t, migratedPaneEnvironment, "split-window", "-d", "-t", sessionName)
	assertRecordedEnvironment(t, migratedPaneEnvironment, stablePath)

	if err := lifecycle.Terminate(ctx, sessionName); err != nil {
		t.Fatal(err)
	}
	waitForProcessExit(t, ctx, sessions[0].PID)
	if processAlive(legacyParent.Process.Pid) {
		if err := legacyParent.Process.Kill(); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacyParent.Wait(); err == nil {
		t.Fatal("legacy wrapper unexpectedly exited successfully")
	}
	legacyParentWaited = true
	waitForProcessExit(t, ctx, legacyPID)
}

func TestServerRestartMigratesRegistryRecoversBridgeAndLeavesUnmanagedTmuxUntouched(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	managedName := fmt.Sprintf("restart-e2e-%d", os.Getpid())
	unmanagedName := fmt.Sprintf("unmanaged-restart-e2e-%d", os.Getpid())
	stateDir := compactRealProcessStateDir(t, root, "rr", managedName)
	homeDir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(homeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	for _, name := range []string{managedName, unmanagedName} {
		name := name
		defer exec.Command("tmux", "kill-session", "-t", name).Run()
	}
	defer killRegisteredTtyd(stateDir, managedName)

	if output, err := exec.Command("tmux", "new-session", "-d", "-s", managedName, "-c", homeDir).CombinedOutput(); err != nil {
		t.Fatalf("create legacy managed tmux session: %v\n%s", err, output)
	}
	if output, err := exec.Command("tmux", "new-session", "-d", "-s", unmanagedName, "-c", homeDir).CombinedOutput(); err != nil {
		t.Fatalf("create unmanaged tmux session: %v\n%s", err, output)
	}
	tmuxCreated := tmuxSessionCreated(t, managedName)

	socketPath := filepath.Join(stateDir, "sockets", managedName+".sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacyBridge := exec.Command(
		"ttyd",
		"-W",
		"-i", socketPath,
		"-b", "/terminal/"+managedName,
		"-t", "scrollback=10000",
		"tmux", "attach-session", "-t", managedName,
	)
	var legacyLog bytes.Buffer
	legacyBridge.Stdout = &legacyLog
	legacyBridge.Stderr = &legacyLog
	if err := legacyBridge.Start(); err != nil {
		t.Fatal(err)
	}
	legacyPID := legacyBridge.Process.Pid
	legacyExit := make(chan error, 1)
	go func() { legacyExit <- legacyBridge.Wait() }()
	t.Cleanup(func() {
		if processAlive(legacyPID) {
			_ = legacyBridge.Process.Kill()
		}
	})
	waitForUnixSocket(t, ctx, socketPath)

	sessionsDir := filepath.Join(stateDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyRecord := registry.Session{
		ID:        managedName,
		Name:      "Legacy display name",
		TmuxName:  managedName,
		Socket:    socketPath,
		PID:       legacyPID,
		CWD:       homeDir,
		CreatedAt: "2026-05-18T12:00:00Z",
	}
	legacyJSON, err := json.MarshalIndent(legacyRecord, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, managedName+".json"), append(legacyJSON, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	serverBinary := filepath.Join(root, "bin", "control-agents-server")
	var activeServer *exec.Cmd
	var serverLog bytes.Buffer
	startServer := func() {
		t.Helper()
		serverLog.Reset()
		activeServer = exec.Command(serverBinary)
		activeServer.Env = environmentWith(map[string]string{
			"HOME":                         homeDir,
			"CONTROL_AGENTS_PASSWORD":      "secret",
			"CONTROL_AGENTS_BIND_ADDR":     "127.0.0.1",
			"CONTROL_AGENTS_PORT":          strconv.Itoa(port),
			"CONTROL_AGENTS_STATE_DIR":     stateDir,
			"CONTROL_AGENTS_MAX_SESSIONS":  "32",
			"CONTROL_AGENTS_COOKIE_SECURE": "true",
		})
		activeServer.Stdout = &serverLog
		activeServer.Stderr = &serverLog
		if err := activeServer.Start(); err != nil {
			t.Fatal(err)
		}
		waitForHTTP(t, ctx, insecureHTTPSClient(), fmt.Sprintf("https://127.0.0.1:%d/login", port))
	}
	stopServer := func() {
		t.Helper()
		if activeServer == nil || activeServer.Process == nil {
			return
		}
		command := activeServer
		activeServer = nil
		_ = command.Process.Signal(syscall.SIGTERM)
		wait := make(chan error, 1)
		go func() { wait <- command.Wait() }()
		select {
		case err := <-wait:
			if err != nil {
				t.Fatalf("server did not stop cleanly: %v\n%s", err, serverLog.String())
			}
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			t.Fatalf("server did not stop within the timeout\n%s", serverLog.String())
		}
	}
	t.Cleanup(func() {
		if activeServer != nil && activeServer.Process != nil {
			_ = activeServer.Process.Kill()
		}
	})

	client := insecureHTTPSClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", port)
	startServer()
	cookie := login(t, client, port)

	select {
	case <-legacyExit:
	case <-ctx.Done():
		t.Fatalf("legacy bridge was not replaced: %v\n%s", ctx.Err(), legacyLog.String())
	}
	firstPID := readRegisteredTtydPID(stateDir, managedName)
	if firstPID <= 0 || firstPID == legacyPID {
		t.Fatalf("migrated bridge PID = %d, want replacement for %d", firstPID, legacyPID)
	}
	migrated, err := registry.NewStore(stateDir).Read(managedName)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Name != managedName || migrated.TmuxName != managedName || migrated.Socket != socketPath || migrated.CreatedAt != legacyRecord.CreatedAt {
		t.Fatalf("migrated registry record = %#v", migrated)
	}
	assertManagedSessionList(t, ctx, client, cookie, baseURL, managedName, unmanagedName)
	assertTerminalProxyUsable(t, ctx, client, cookie, baseURL, migrated.PublicRef)

	marker := "control-agents-restart-preserves-tmux"
	if output, err := exec.Command("tmux", "send-keys", "-t", managedName, "printf '"+marker+"\\n'", "C-m").CombinedOutput(); err != nil {
		t.Fatalf("write tmux marker: %v\n%s", err, output)
	}
	stopServer()
	if exec.Command("tmux", "has-session", "-t", managedName).Run() != nil {
		t.Fatal("stopping the web server destroyed the managed tmux session")
	}
	stopProcess(t, firstPID)
	waitForProcessExit(t, ctx, firstPID)

	startServer()
	assertManagedSessionList(t, ctx, client, cookie, baseURL, managedName, unmanagedName)
	secondPID := readRegisteredTtydPID(stateDir, managedName)
	if secondPID <= 0 || secondPID == firstPID {
		t.Fatalf("restart bridge PID = %d, want replacement for %d", secondPID, firstPID)
	}
	if got := tmuxSessionCreated(t, managedName); got != tmuxCreated {
		t.Fatalf("server restart recreated tmux session: created %q, want %q", got, tmuxCreated)
	}
	capture, err := exec.Command("tmux", "capture-pane", "-p", "-S", "-100", "-t", managedName).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(capture), marker) {
		t.Fatalf("tmux contents were not preserved across restart:\n%s", capture)
	}
	assertTerminalProxyUsable(t, ctx, client, cookie, baseURL, migrated.PublicRef)

	if output, err := exec.Command("tmux", "kill-session", "-t", managedName).CombinedOutput(); err != nil {
		t.Fatalf("kill managed tmux session externally: %v\n%s", err, output)
	}
	assertManagedSessionList(t, ctx, client, cookie, baseURL, "", unmanagedName)
	waitForProcessExit(t, ctx, secondPID)
	for _, path := range []string{
		filepath.Join(stateDir, "sessions", managedName+".json"),
		filepath.Join(stateDir, "sockets", managedName+".sock"),
		filepath.Join(stateDir, "logs", managedName+".log"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale managed artifact %s remains: %v", path, err)
		}
	}
	if exec.Command("tmux", "has-session", "-t", unmanagedName).Run() != nil {
		t.Fatal("reconciliation modified the unmanaged tmux session")
	}
	stopServer()
}

func TestWebLifecycleAPICreatesSelectsLimitsAndTerminatesRealSession(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")
	requireCommand(t, "script")

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sessionName := fmt.Sprintf("web-api-e2e-%d", os.Getpid())
	limitedName := fmt.Sprintf("web-limit-e2e-%d", os.Getpid())
	stateDir := compactRealProcessStateDir(t, root, "wa", sessionName, limitedName)
	homeDir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(homeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer killRegisteredTtyd(stateDir, sessionName)
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	tmuxArgumentLog := filepath.Join(stateDir, "tmux-argument-audit.log")
	tmuxShimDir := installTmuxArgumentAuditShim(t, stateDir)

	port := freePort(t)
	app := exec.CommandContext(ctx, "go", "run", "../../cmd/server")
	app.Env = append(os.Environ(),
		"HOME="+homeDir,
		"CONTROL_AGENTS_PASSWORD=secret",
		"CONTROL_AGENTS_BIND_ADDR=127.0.0.1",
		fmt.Sprintf("CONTROL_AGENTS_PORT=%d", port),
		"CONTROL_AGENTS_STATE_DIR="+stateDir,
		"CONTROL_AGENTS_MAX_SESSIONS=1",
		"CONTROL_AGENTS_TEST_REAL_TMUX="+realTmux,
		"CONTROL_AGENTS_TEST_TMUX_ARGUMENT_LOG="+tmuxArgumentLog,
		"PATH="+tmuxShimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	var appLog bytes.Buffer
	app.Stdout = &appLog
	app.Stderr = &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer app.Process.Kill()
	client := insecureHTTPSClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	baseURL := fmt.Sprintf("https://127.0.0.1:%d", port)
	waitForHTTP(t, ctx, client, baseURL+"/login")
	cookie := login(t, client, port)

	created := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions", `{"name":"`+sessionName+`"}`)
	if created.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(created.Body)
		created.Body.Close()
		t.Fatalf("create status = %d, body = %q, server log = %s", created.StatusCode, body, appLog.String())
	}
	var createPayload struct {
		Created bool           `json:"created"`
		Session map[string]any `json:"session"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createPayload); err != nil {
		created.Body.Close()
		t.Fatal(err)
	}
	created.Body.Close()
	sessionRef, _ := createPayload.Session["id"].(string)
	paneRef, _ := createPayload.Session["activePaneRef"].(string)
	if !createPayload.Created || !registry.ValidPublicRef(sessionRef) || sessionRef == sessionName || paneRef == "" || createPayload.Session["name"] != sessionName || createPayload.Session["cwd"] != homeDir {
		t.Fatalf("create payload = %#v", createPayload)
	}
	for _, privateField := range []string{"pid", "socket", "tmuxName"} {
		if _, ok := createPayload.Session[privateField]; ok {
			t.Fatalf("create response exposes %q: %#v", privateField, createPayload.Session)
		}
	}
	canonicalPath := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions/"+sessionName+"/keys", `{"key":"ctrl-l","paneRef":"`+paneRef+`"}`)
	canonicalPath.Body.Close()
	if canonicalPath.StatusCode != http.StatusNotFound {
		t.Fatalf("canonical session path status = %d, want opaque-only 404", canonicalPath.StatusCode)
	}
	foreignPane := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions/"+sessionRef+"/keys", `{"key":"ctrl-l","paneRef":"p_foreign"}`)
	foreignPane.Body.Close()
	if foreignPane.StatusCode != http.StatusConflict {
		t.Fatalf("foreign pane status = %d, want stale identity conflict", foreignPane.StatusCode)
	}

	const auditCanary = "CONTROL-AGENTS-AUDIT-CANARY-18e7a934"
	digest := sha256.Sum256([]byte(auditCanary))
	tokenResponse := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions/"+sessionRef+"/paste/token",
		`{"paneRef":"`+paneRef+`","digest":"`+base64.RawURLEncoding.EncodeToString(digest[:])+`","bytes":36,"lines":1,"controlCharacters":false,"trailingNewline":false}`)
	var pasteToken struct {
		Token string `json:"token"`
	}
	if tokenResponse.StatusCode != http.StatusCreated || json.NewDecoder(tokenResponse.Body).Decode(&pasteToken) != nil || pasteToken.Token == "" {
		t.Fatalf("paste token status = %d", tokenResponse.StatusCode)
	}
	tokenResponse.Body.Close()
	paste := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions/"+sessionRef+"/paste", `{"text":"`+auditCanary+`","paneRef":"`+paneRef+`","token":"`+pasteToken.Token+`"}`)
	paste.Body.Close()
	if paste.StatusCode != http.StatusOK {
		t.Fatalf("canary paste status = %d", paste.StatusCode)
	}
	if strings.Contains(appLog.String(), auditCanary) {
		t.Fatalf("paste canary leaked into server/journal output: %s", appLog.String())
	}
	bridgeLog, err := os.ReadFile(filepath.Join(stateDir, "logs", sessionName+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bridgeLog), auditCanary) {
		t.Fatalf("paste canary leaked into ttyd log: %s", bridgeLog)
	}

	const bracketedPasteCanary = "CONTROL-AGENTS-BRACKETED-PASTE-7f36c2a1\nsecond-line"
	bracketedCapture := filepath.Join(t.TempDir(), "bracketed-paste.bin")
	bracketedReady := fmt.Sprintf("control-agents-bracketed-ready-%d", os.Getpid())
	bracketedDone := fmt.Sprintf("control-agents-bracketed-done-%d", os.Getpid())
	expectedBracketed := []byte("\x1b[200~" + bracketedPasteCanary + "\x1b[201~")
	mustTmuxRun(t, "send-keys", "-t", sessionName, "C-c")
	captureCommand := fmt.Sprintf(
		"umask 077; stty raw -echo; printf '\\033[?2004h'; sleep 0.2; tmux wait-for -S %s; dd if=/dev/stdin of=%q bs=1 count=%d status=none; printf '\\033[?2004l'; stty sane; tmux wait-for -S %s",
		bracketedReady, bracketedCapture, len(expectedBracketed), bracketedDone,
	)
	mustTmuxRun(t, "send-keys", "-t", sessionName, captureCommand, "C-m")
	mustTmuxRunContext(t, ctx, "wait-for", bracketedReady)
	bracketedDigest := sha256.Sum256([]byte(bracketedPasteCanary))
	bracketedTokenBody, err := json.Marshal(map[string]any{
		"paneRef":           paneRef,
		"digest":            base64.RawURLEncoding.EncodeToString(bracketedDigest[:]),
		"bytes":             len([]byte(bracketedPasteCanary)),
		"lines":             2,
		"controlCharacters": true,
		"trailingNewline":   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	bracketedTokenResponse := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions/"+sessionRef+"/paste/token", string(bracketedTokenBody))
	var bracketedToken struct {
		Token string `json:"token"`
	}
	if bracketedTokenResponse.StatusCode != http.StatusCreated || json.NewDecoder(bracketedTokenResponse.Body).Decode(&bracketedToken) != nil || bracketedToken.Token == "" {
		bracketedTokenResponse.Body.Close()
		t.Fatalf("bracketed paste token status = %d", bracketedTokenResponse.StatusCode)
	}
	bracketedTokenResponse.Body.Close()
	bracketedPasteBody, err := json.Marshal(map[string]string{
		"text": bracketedPasteCanary, "paneRef": paneRef, "token": bracketedToken.Token,
	})
	if err != nil {
		t.Fatal(err)
	}
	bracketedPaste := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions/"+sessionRef+"/paste", string(bracketedPasteBody))
	bracketedPaste.Body.Close()
	if bracketedPaste.StatusCode != http.StatusOK {
		t.Fatalf("bracketed paste status = %d", bracketedPaste.StatusCode)
	}
	mustTmuxRunContext(t, ctx, "wait-for", bracketedDone)
	receivedBracketed, err := os.ReadFile(bracketedCapture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(receivedBracketed, expectedBracketed) {
		startMarker := []byte("\x1b[200~")
		endMarker := []byte("\x1b[201~")
		hasStart := bytes.HasPrefix(receivedBracketed, startMarker)
		hasEnd := bytes.HasSuffix(receivedBracketed, endMarker)
		bodyMatches := hasStart && hasEnd && bytes.Equal(receivedBracketed[len(startMarker):len(receivedBracketed)-len(endMarker)], []byte(bracketedPasteCanary))
		t.Fatalf("bracketed paste framing mismatch: bytes=%d/%d start=%t body=%t end=%t", len(receivedBracketed), len(expectedBracketed), hasStart, bodyMatches, hasEnd)
	}
	argumentAudit, err := os.ReadFile(tmuxArgumentLog)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(argumentAudit, []byte(bracketedPasteCanary)) {
		t.Fatal("bracketed Paste content entered a tmux command argument")
	}
	if strings.Contains(appLog.String(), bracketedPasteCanary) {
		t.Fatal("bracketed Paste content entered server output")
	}
	bridgeLog, err = os.ReadFile(filepath.Join(stateDir, "logs", sessionName+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bridgeLog, []byte(bracketedPasteCanary)) {
		t.Fatal("bracketed Paste content entered ttyd output")
	}

	originalPane, err := exec.Command("tmux", "display-message", "-p", "-t", sessionName, "#{pane_id}").Output()
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("tmux", "split-window", "-d", "-t", sessionName, "-c", "#{pane_current_path}").CombinedOutput(); err != nil {
		t.Fatalf("split pane for stale-generation test: %v\n%s", err, output)
	}
	if output, err := exec.Command("tmux", "kill-pane", "-t", strings.TrimSpace(string(originalPane))).CombinedOutput(); err != nil {
		t.Fatalf("replace active pane for stale-generation test: %v\n%s", err, output)
	}
	stalePane := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions/"+sessionRef+"/keys", `{"key":"ctrl-l","paneRef":"`+paneRef+`"}`)
	stalePane.Body.Close()
	if stalePane.StatusCode != http.StatusConflict {
		t.Fatalf("replaced pane generation status = %d, want conflict", stalePane.StatusCode)
	}
	refreshed := fetchPublicSessionByName(t, ctx, client, cookie, baseURL, sessionName)
	paneRef, _ = refreshed["activePaneRef"].(string)
	if paneRef == "" {
		t.Fatalf("refreshed session has no active pane: %#v", refreshed)
	}
	if got := tmuxPanePath(t, sessionName); got != homeDir {
		t.Fatalf("web-created pane path = %q, want HOME %q", got, homeDir)
	}
	stableAgentPath, err := managedsession.ForwardedAgentSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	assertTmuxEnvironment(t, sessionName, "SSH_AUTH_SOCK", stableAgentPath)
	if _, err := os.Lstat(stableAgentPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("web-created session unexpectedly required a forwarded agent link: %v", err)
	}
	bridgePID := readRegisteredTtydPID(stateDir, sessionName)

	selected := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions", `{"name":"`+sessionName+`"}`)
	var selectedPayload struct {
		Created bool `json:"created"`
	}
	if selected.StatusCode != http.StatusOK || json.NewDecoder(selected.Body).Decode(&selectedPayload) != nil || selectedPayload.Created {
		body, _ := io.ReadAll(selected.Body)
		selected.Body.Close()
		t.Fatalf("select status/payload = %d/%#v, remainder = %q", selected.StatusCode, selectedPayload, body)
	}
	selected.Body.Close()
	if got := readRegisteredTtydPID(stateDir, sessionName); got != bridgePID {
		t.Fatalf("duplicate create replaced bridge %d with %d", bridgePID, got)
	}

	limited := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodPost, baseURL+"/api/sessions", `{"name":"`+limitedName+`"}`)
	limited.Body.Close()
	if limited.StatusCode != http.StatusConflict || exec.Command("tmux", "has-session", "-t", limitedName).Run() == nil {
		t.Fatalf("limited create status = %d or created an extra tmux session", limited.StatusCode)
	}

	directClient := exec.CommandContext(ctx, "script", "-qefc", fmt.Sprintf("%q %q", filepath.Join(root, "bin", "control-agents"), sessionName), "/dev/null")
	directClient.Env = environmentWith(map[string]string{
		"CONTROL_AGENTS_STATE_DIR": stateDir,
		"HOME":                     homeDir,
		"SSH_CONNECTION":           "",
		"SSH_CLIENT":               "",
		"SSH_TTY":                  "",
		"SSH_AUTH_SOCK":            "",
	})
	var directTranscript lockedBuffer
	directClient.Stdout = &directTranscript
	directClient.Stderr = &directTranscript
	directInput, err := directClient.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	clientCountBeforeAttach := tmuxClientCount(t, sessionName)
	if err := directClient.Start(); err != nil {
		t.Fatal(err)
	}
	directExit := make(chan error, 1)
	go func() { directExit <- directClient.Wait() }()
	t.Cleanup(func() {
		_ = directInput.Close()
		if directClient.Process != nil && processAlive(directClient.Process.Pid) {
			_ = directClient.Process.Kill()
		}
	})
	waitForTranscriptCount(t, ctx, &directTranscript, "Detach with Ctrl-b d.", 1)
	waitForTmuxClientOrExit(t, ctx, sessionName, clientCountBeforeAttach+1, directExit, &directTranscript)

	wrong := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodDelete, baseURL+"/api/sessions/"+sessionRef, `{"confirmName":"wrong","paneRef":"`+paneRef+`"}`)
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusBadRequest || exec.Command("tmux", "has-session", "-t", sessionName).Run() != nil {
		t.Fatalf("wrong confirmation status = %d or terminated the session", wrong.StatusCode)
	}
	select {
	case err := <-directExit:
		t.Fatalf("incorrect confirmation disconnected the direct client: %v\n%s", err, directTranscript.String())
	default:
	}

	resizePath := filepath.Join(stateDir, "resize", sessionRef+".json")
	if err := os.WriteFile(resizePath, []byte(`{"mode":"fixed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	terminated := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodDelete, baseURL+"/api/sessions/"+sessionRef, `{"confirmName":"`+sessionName+`","paneRef":"`+paneRef+`"}`)
	terminated.Body.Close()
	if terminated.StatusCode != http.StatusNoContent {
		t.Fatalf("terminate status = %d", terminated.StatusCode)
	}
	if exec.Command("tmux", "has-session", "-t", sessionName).Run() == nil {
		t.Fatal("tmux session remains after web termination")
	}
	select {
	case <-directExit:
	case <-ctx.Done():
		t.Fatalf("web termination did not disconnect the direct client: %v\n%s", ctx.Err(), directTranscript.String())
	}
	waitForProcessExit(t, ctx, bridgePID)
	for _, path := range []string{
		filepath.Join(stateDir, "sessions", sessionName+".json"),
		filepath.Join(stateDir, "sockets", sessionName+".sock"),
		filepath.Join(stateDir, "logs", sessionName+".log"),
		resizePath,
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("terminated session artifact %s remains: %v", path, err)
		}
	}

	repeated := doLifecycleAPIRequest(t, ctx, client, cookie, http.MethodDelete, baseURL+"/api/sessions/"+sessionRef, `{"confirmName":"`+sessionName+`","paneRef":"`+paneRef+`"}`)
	repeated.Body.Close()
	if repeated.StatusCode != http.StatusNotFound {
		t.Fatalf("repeated terminate status = %d, want %d", repeated.StatusCode, http.StatusNotFound)
	}
}

func TestClientDirectNoAttachCreatesInHomeAndRecoversBridge(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	workDir := filepath.Join(temporary, "caller-directory")
	if err := os.Mkdir(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	homeDir := filepath.Join(temporary, "home")
	if err := os.Mkdir(homeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sessionName, stateDir := compactRealProcessFixturePaths(t, root, "dc")
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer killRegisteredTtyd(stateDir, sessionName)

	client := exec.CommandContext(ctx, filepath.Join(root, "bin", "control-agents"), "--no-attach", sessionName)
	client.Dir = workDir
	client.Env = environmentWith(map[string]string{
		"CONTROL_AGENTS_STATE_DIR": stateDir,
		"HOME":                     homeDir,
		"SSH_CONNECTION":           "",
		"SSH_CLIENT":               "",
		"SSH_TTY":                  "",
		"SSH_AUTH_SOCK":            "",
	})
	var stdout, stderr bytes.Buffer
	client.Stdout = &stdout
	client.Stderr = &stderr
	if err := client.Run(); err != nil {
		t.Fatalf("client failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if got := stdout.String(); got != sessionName+"\n" {
		t.Fatalf("client output = %q, want canonical ID only", got)
	}
	if !strings.Contains(stderr.String(), "forwarded SSH agent: unavailable") {
		t.Fatalf("missing unavailable agent status: %q", stderr.String())
	}
	if got := tmuxPanePath(t, sessionName); got != homeDir {
		t.Fatalf("initial tmux pane path = %q, want HOME %q", got, homeDir)
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "sessions", sessionName+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		TmuxName  string `json:"tmuxName"`
		PID       int    `json:"pid"`
		CWD       string `json:"cwd"`
		CreatedAt string `json:"createdAt"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.ID != sessionName || session.Name != sessionName || session.TmuxName != sessionName || session.CWD != homeDir {
		t.Fatalf("session = %+v, want canonical name and HOME", session)
	}
	second := exec.CommandContext(ctx, filepath.Join(root, "bin", "control-agents"), sessionName)
	second.Dir = workDir
	second.Env = append(os.Environ(),
		"CONTROL_AGENTS_STATE_DIR="+stateDir,
		"CONTROL_AGENTS_NO_ATTACH=1",
		"HOME="+homeDir,
	)
	if output, err := second.CombinedOutput(); err != nil {
		t.Fatalf("idempotent client failed: %v\n%s", err, output)
	}
	unchanged, err := os.ReadFile(filepath.Join(stateDir, "sessions", sessionName+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, data) {
		t.Fatal("healthy idempotent client reuse rewrote durable registry metadata")
	}

	stopProcess(t, session.PID)
	waitForProcessExit(t, ctx, session.PID)
	recovery := exec.CommandContext(ctx, filepath.Join(root, "bin", "control-agents"), sessionName)
	recovery.Dir = workDir
	recovery.Env = append(os.Environ(),
		"CONTROL_AGENTS_STATE_DIR="+stateDir,
		"CONTROL_AGENTS_ATTACH=0",
		"HOME="+homeDir,
	)
	if output, err := recovery.CombinedOutput(); err != nil {
		t.Fatalf("client bridge recovery failed: %v\n%s", err, output)
	}
	recoveredData, err := os.ReadFile(filepath.Join(stateDir, "sessions", sessionName+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var recovered struct {
		PID       int    `json:"pid"`
		CWD       string `json:"cwd"`
		CreatedAt string `json:"createdAt"`
	}
	if err := json.Unmarshal(recoveredData, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.PID <= 0 || recovered.PID == session.PID || recovered.CWD != session.CWD || recovered.CreatedAt != session.CreatedAt {
		t.Fatalf("recovered metadata = %+v, original = %+v", recovered, session)
	}
}

func compactRealProcessFixturePaths(t *testing.T, root, prefix string) (string, string) {
	t.Helper()
	sessionName, stateDir := compactRealProcessFixturePathsForPID(root, prefix, os.Getpid())
	assertRealProcessFixtureSocketPath(t, stateDir, sessionName)
	return sessionName, stateDir
}

func compactRealProcessStateDir(t *testing.T, root, prefix string, sessionNames ...string) string {
	t.Helper()
	stateDir := compactRealProcessStateDirForPID(root, prefix, os.Getpid())
	for _, sessionName := range sessionNames {
		assertRealProcessFixtureSocketPath(t, stateDir, sessionName)
	}
	return stateDir
}

func compactRealProcessFixturePathsForPID(root, prefix string, pid int) (string, string) {
	suffix := strconv.FormatInt(int64(pid), 36)
	return prefix + "-" + suffix, compactRealProcessStateDirForPID(root, prefix, pid)
}

func compactRealProcessStateDirForPID(root, prefix string, pid int) string {
	suffix := strconv.FormatInt(int64(pid), 36)
	return filepath.Join(root, ".cache", prefix+"-"+suffix)
}

func assertRealProcessFixtureSocketPath(t *testing.T, stateDir, sessionName string) {
	t.Helper()
	socketPath := filepath.Join(stateDir, "sockets", sessionName+".sock")
	const bridgeSocketPathLimit = 100
	if len(socketPath) > bridgeSocketPathLimit {
		t.Fatalf("real-process fixture socket path is %d bytes, want at most %d: %s", len(socketPath), bridgeSocketPathLimit, socketPath)
	}
}

func TestRealProcessFixtureStatePathsFitHostedCheckoutAtMaximumPID(t *testing.T) {
	const hostedCheckout = "/home/runner/work/control-agents/control-agents"
	const maximumLinuxPID = 4_194_304
	fixturePID := strconv.Itoa(maximumLinuxPID)
	fixtures := []struct {
		prefix       string
		sessionNames []string
	}{
		{prefix: "rs", sessionNames: []string{"e2e-" + fixturePID}},
		{prefix: "lc", sessionNames: []string{"lifecycle-e2e-" + fixturePID}},
		{prefix: "pm", sessionNames: []string{"policy-managed-" + fixturePID}},
		{prefix: "h", sessionNames: []string{"history-existing-" + fixturePID}},
		{prefix: "sz", sessionNames: []string{"sizing-existing-" + fixturePID}},
		{prefix: "bm", sessionNames: []string{"bridge-migrate-" + fixturePID}},
		{prefix: "rr", sessionNames: []string{"restart-e2e-" + fixturePID}},
		{prefix: "wa", sessionNames: []string{"web-api-e2e-" + fixturePID, "web-limit-e2e-" + fixturePID}},
		{prefix: "ag", sessionNames: []string{"agent-e2e-" + fixturePID}},
		{prefix: "sl", sessionNames: []string{"selector-e2e-" + fixturePID + "-a", "selector-e2e-" + fixturePID + "-b"}},
	}
	for _, fixture := range fixtures {
		stateDir := compactRealProcessStateDirForPID(hostedCheckout, fixture.prefix, maximumLinuxPID)
		for _, sessionName := range fixture.sessionNames {
			assertRealProcessFixtureSocketPath(t, stateDir, sessionName)
		}
	}
	for _, prefix := range []string{"dc", "hf"} {
		sessionName, stateDir := compactRealProcessFixturePathsForPID(hostedCheckout, prefix, maximumLinuxPID)
		assertRealProcessFixtureSocketPath(t, stateDir, sessionName)
	}
}

func TestForwardedAgentStableLinkInheritsAndRetargetsAcrossReconnect(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")
	requireCommand(t, "script")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sessionName := fmt.Sprintf("agent-e2e-%d", os.Getpid())
	stateDir := compactRealProcessStateDir(t, root, "ag", sessionName)
	homeDir := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(stateDir)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	defer killRegisteredTtyd(stateDir, sessionName)

	firstPath := filepath.Join(stateDir, "first.sock")
	first, err := net.Listen("unix", firstPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	clientPath := filepath.Join(root, "bin", "control-agents")
	runForwardedClient(t, ctx, clientPath, stateDir, homeDir, sessionName, firstPath)

	stablePath, err := managedsession.ForwardedAgentSocketPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	assertStableLinkTarget(t, stablePath, firstPath)
	assertTmuxEnvironment(t, sessionName, "SSH_AUTH_SOCK", stablePath)
	assertUnixSocketAccepts(t, first, stablePath)
	assertTtydCommandLineContains(t, stateDir, sessionName, "attach-session -E -t "+sessionName)
	newWindowEnvironment := filepath.Join(stateDir, "new-window-agent-path")
	newPaneEnvironment := filepath.Join(stateDir, "new-pane-agent-path")
	var newWindowPane, newPane string
	runForwardedAttachedClient(t, ctx, clientPath, stateDir, homeDir, sessionName, firstPath, func() {
		assertTmuxEnvironment(t, sessionName, "SSH_AUTH_SOCK", stablePath)
		newWindowPane = createTmuxPane(t, newWindowEnvironment, "new-window", "-d", "-t", sessionName+":")
		assertRecordedEnvironment(t, newWindowEnvironment, stablePath)
		newPane = createTmuxPane(t, newPaneEnvironment, "split-window", "-d", "-t", sessionName)
		assertRecordedEnvironment(t, newPaneEnvironment, stablePath)
	})
	assertTmuxEnvironment(t, sessionName, "SSH_AUTH_SOCK", stablePath)
	assertTmuxPaneAlive(t, newWindowPane)
	assertTmuxPaneAlive(t, newPane)

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if connection, err := net.DialTimeout("unix", stablePath, 100*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("stable agent socket remained reachable after forwarding transport disappeared")
	}
	secondPath := filepath.Join(stateDir, "second.sock")
	second, err := net.Listen("unix", secondPath)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	runForwardedClient(t, ctx, clientPath, stateDir, homeDir, sessionName, secondPath)

	assertStableLinkTarget(t, stablePath, secondPath)
	assertTmuxEnvironment(t, sessionName, "SSH_AUTH_SOCK", stablePath)
	assertTmuxPaneAlive(t, newWindowPane)
	assertRecordedEnvironment(t, newWindowEnvironment, stablePath)
	assertUnixSocketAccepts(t, second, stablePath)
}

func runForwardedClient(t *testing.T, ctx context.Context, clientPath, stateDir, homeDir, sessionName, socketPath string) {
	t.Helper()
	client := exec.CommandContext(ctx, clientPath, "--no-attach", sessionName)
	client.Env = environmentWith(map[string]string{
		"CONTROL_AGENTS_STATE_DIR": stateDir,
		"HOME":                     homeDir,
		"SSH_CONNECTION":           "192.0.2.10 54321 192.0.2.20 22",
		"SSH_AUTH_SOCK":            socketPath,
	})
	var stdout, stderr bytes.Buffer
	client.Stdout = &stdout
	client.Stderr = &stderr
	if err := client.Run(); err != nil {
		t.Fatalf("forwarded client failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if stdout.String() != sessionName+"\n" {
		t.Fatalf("forwarded client stdout = %q, want canonical ID", stdout.String())
	}
	if !strings.Contains(stderr.String(), "forwarded SSH agent: available") {
		t.Fatalf("forwarded client status = %q, want available", stderr.String())
	}
	if strings.Contains(stderr.String(), socketPath) {
		t.Fatalf("forwarded client exposed transient socket path: %q", stderr.String())
	}
}

func runForwardedAttachedClient(t *testing.T, ctx context.Context, clientPath, stateDir, homeDir, sessionName, socketPath string, whileAttached func()) {
	t.Helper()
	commandLine := fmt.Sprintf("%q %q", clientPath, sessionName)
	client := exec.CommandContext(ctx, "script", "-qefc", commandLine, "/dev/null")
	client.Env = environmentWith(map[string]string{
		"CONTROL_AGENTS_STATE_DIR": stateDir,
		"HOME":                     homeDir,
		"SSH_CONNECTION":           "192.0.2.10 54321 192.0.2.20 22",
		"SSH_AUTH_SOCK":            socketPath,
	})
	input, err := client.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var transcript lockedBuffer
	client.Stdout = &transcript
	client.Stderr = &transcript
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	waitForTranscriptCount(t, ctx, &transcript, "Detach with Ctrl-b d.", 1)
	waitForTmuxClientCount(t, ctx, sessionName, 1)
	whileAttached()
	if _, err := input.Write([]byte{0x02, 'd'}); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- client.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatalf("forwarded attached client failed: %v\n%s", err, transcript.String())
		}
	case <-ctx.Done():
		t.Fatalf("forwarded attached client did not detach: %v\n%s", ctx.Err(), transcript.String())
	}
}

func assertStableLinkTarget(t *testing.T, stablePath, want string) {
	t.Helper()
	target, err := os.Readlink(stablePath)
	if err != nil {
		t.Fatal(err)
	}
	if target != want {
		t.Fatalf("stable agent link target = %q, want %q", target, want)
	}
	assertFileMode(t, filepath.Dir(stablePath), 0o700)
}

func assertTmuxEnvironment(t *testing.T, sessionName, name, want string) {
	t.Helper()
	output, err := exec.Command("tmux", "show-environment", "-t", sessionName, name).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(output)); got != name+"="+want {
		t.Fatalf("tmux environment = %q, want %q", got, name+"="+want)
	}
}

func createTmuxPane(t *testing.T, environmentFile, action string, args ...string) string {
	t.Helper()
	arguments := []string{action, "-P", "-F", "#{pane_id}"}
	arguments = append(arguments, args...)
	arguments = append(arguments,
		"sh", "-c",
		"printf '%s' \"$SSH_AUTH_SOCK\" > \"$1\"; while :; do sleep 60; done",
		"control-agents-agent-e2e", environmentFile,
	)
	output, err := exec.Command("tmux", arguments...).Output()
	if err != nil {
		t.Fatal(err)
	}
	if pane := strings.TrimSpace(string(output)); pane != "" {
		return pane
	}
	t.Fatal("tmux did not return a pane ID")
	return ""
}

func assertRecordedEnvironment(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && string(data) == want {
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("recorded SSH_AUTH_SOCK in %s does not equal %q: %v", path, want, lastErr)
}

func assertTmuxPaneAlive(t *testing.T, pane string) {
	t.Helper()
	output, err := exec.Command("tmux", "display-message", "-p", "-t", pane, "#{pane_dead}").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(output)); got != "0" {
		t.Fatalf("tmux pane %s dead state = %q, want alive", pane, got)
	}
}

func assertUnixSocketAccepts(t *testing.T, listener net.Listener, path string) {
	t.Helper()
	accepted := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			err = connection.Close()
		}
		accepted <- err
	}()
	connection, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-accepted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("fake forwarded agent did not accept through the stable link")
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

func environmentWith(overrides map[string]string) []string {
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[name]; !replaced {
			environment = append(environment, entry)
		}
	}
	for name, value := range overrides {
		environment = append(environment, name+"="+value)
	}
	return environment
}

func installTmuxArgumentAuditShim(t *testing.T, stateDir string) string {
	t.Helper()
	directory := filepath.Join(stateDir, "tmux-audit-bin")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(directory, "tmux")
	script := `#!/bin/sh
set -eu
umask 077
if [ -n "${CONTROL_AGENTS_TEST_TMUX_ARGUMENT_LOG:-}" ]; then
  for argument in "$@"; do
    printf '%s\n' "$argument"
  done >> "$CONTROL_AGENTS_TEST_TMUX_ARGUMENT_LOG"
  printf '%s\n' -- >> "$CONTROL_AGENTS_TEST_TMUX_ARGUMENT_LOG"
fi
exec "$CONTROL_AGENTS_TEST_REAL_TMUX" "$@"
`
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func doLifecycleAPIRequest(t *testing.T, ctx context.Context, client *http.Client, cookie *http.Cookie, method, url, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", request.URL.Scheme+"://"+request.URL.Host)
	request.Header.Set("X-Control-Agents-CSRF-Token", fetchCSRFToken(t, ctx, client, cookie, request.URL.Scheme+"://"+request.URL.Host))
	request.AddCookie(cookie)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func fetchCSRFToken(t *testing.T, ctx context.Context, client *http.Client, cookie *http.Cookie, baseURL string) string {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/csrf", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload struct {
		Token string `json:"token"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&payload) != nil || payload.Token == "" {
		t.Fatalf("CSRF token status = %d", response.StatusCode)
	}
	return payload.Token
}

func fetchPublicSessionByName(t *testing.T, ctx context.Context, client *http.Client, cookie *http.Cookie, baseURL, name string) map[string]any {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("session list status = %d", response.StatusCode)
	}
	var payload struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	for _, session := range payload.Sessions {
		if session["name"] == name {
			return session
		}
	}
	t.Fatalf("managed session %q not found in %#v", name, payload.Sessions)
	return nil
}

func assertManagedSessionList(t *testing.T, ctx context.Context, client *http.Client, cookie *http.Cookie, baseURL, wantManaged, unmanaged string) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("session list status = %d, body = %q", response.StatusCode, body)
	}
	var payload struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if wantManaged == "" {
		if len(payload.Sessions) != 0 {
			t.Fatalf("session list after cleanup = %#v, want empty", payload.Sessions)
		}
		return
	}
	if len(payload.Sessions) != 1 {
		t.Fatalf("session list = %#v, want only %q", payload.Sessions, wantManaged)
	}
	publicRef, _ := payload.Sessions[0]["id"].(string)
	if !registry.ValidPublicRef(publicRef) || publicRef == wantManaged || payload.Sessions[0]["name"] != wantManaged {
		t.Fatalf("session list = %#v, want only %q", payload.Sessions, wantManaged)
	}
	for _, session := range payload.Sessions {
		if session["name"] == unmanaged {
			t.Fatalf("unmanaged tmux session %q appeared in the public list", unmanaged)
		}
		for _, field := range []string{"pid", "socket", "tmuxName"} {
			if _, exposed := session[field]; exposed {
				t.Fatalf("public list exposed %q: %#v", field, session)
			}
		}
	}
}

func assertTerminalProxyUsable(t *testing.T, ctx context.Context, client *http.Client, cookie *http.Cookie, baseURL, sessionRef string) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/terminal/"+sessionRef+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		t.Fatalf("authenticated terminal proxy status = %d, want success", response.StatusCode)
	}
}

func TestClientNoArgumentWithoutTTYFailsPromptly(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run client e2e tests")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := exec.CommandContext(ctx, filepath.Join(root, "bin", "control-agents"))
	client.Stdin = strings.NewReader("")
	output, err := client.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("no-argument non-TTY client blocked: %v", ctx.Err())
	}
	if code := commandExitCode(err); code != 2 {
		t.Fatalf("exit = %d, want 2: %v\n%s", code, err, output)
	}
	if !strings.Contains(string(output), "selector requires an interactive terminal") {
		t.Fatalf("missing non-TTY usage message: %q", output)
	}
}

func TestClientSelectorCreatesAttachesDetachesAndReturnsToPrompt(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")
	requireCommand(t, "script")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	homeDir := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(homeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	firstSessionName := fmt.Sprintf("selector-e2e-%d-a", os.Getpid())
	secondSessionName := fmt.Sprintf("selector-e2e-%d-b", os.Getpid())
	stateDir := compactRealProcessStateDir(t, root, "sl", firstSessionName, secondSessionName)
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	for _, sessionName := range []string{firstSessionName, secondSessionName} {
		sessionName := sessionName
		defer exec.Command("tmux", "kill-session", "-t", sessionName).Run()
		defer killRegisteredTtyd(stateDir, sessionName)
	}

	selector := exec.CommandContext(ctx, "script", "-qefc", filepath.Join(root, "bin", "control-agents"), "/dev/null")
	selector.Env = append(os.Environ(),
		"CONTROL_AGENTS_STATE_DIR="+stateDir,
		"HOME="+homeDir,
	)
	input, err := selector.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var transcript lockedBuffer
	selector.Stdout = &transcript
	selector.Stderr = &transcript
	if err := selector.Start(); err != nil {
		t.Fatal(err)
	}
	waitForTranscriptCount(t, ctx, &transcript, "Select: ", 1)
	if initial := transcript.String(); !strings.Contains(initial, "n) New session") || !strings.Contains(initial, "q) Quit") {
		t.Fatalf("empty selector does not offer New session and Quit:\n%s", initial)
	}
	if _, err := io.WriteString(input, "n\n"); err != nil {
		t.Fatal(err)
	}
	waitForTranscriptCount(t, ctx, &transcript, "Session name (empty to cancel): ", 1)
	if _, err := io.WriteString(input, firstSessionName+"\n"); err != nil {
		t.Fatal(err)
	}
	waitForTranscriptCount(t, ctx, &transcript, "Detach with Ctrl-b d.", 1)
	waitForTmuxClientCount(t, ctx, firstSessionName, 1)
	if _, err := input.Write([]byte{0x02, 'd'}); err != nil {
		t.Fatal(err)
	}
	waitForTranscriptCount(t, ctx, &transcript, "Control Agents sessions", 2)
	waitForTranscriptCount(t, ctx, &transcript, "Select: ", 2)
	if exec.Command("tmux", "has-session", "-t", firstSessionName).Run() != nil {
		t.Fatal("detaching the selector client terminated the tmux session")
	}
	if got := tmuxPanePath(t, firstSessionName); got != homeDir {
		t.Fatalf("selector-created pane path = %q, want HOME %q", got, homeDir)
	}

	if _, err := io.WriteString(input, "n\n"); err != nil {
		t.Fatal(err)
	}
	waitForTranscriptCount(t, ctx, &transcript, "Session name (empty to cancel): ", 2)
	if _, err := io.WriteString(input, secondSessionName+"\n"); err != nil {
		t.Fatal(err)
	}
	waitForTranscriptCount(t, ctx, &transcript, "Detach with Ctrl-b d.", 2)
	waitForTmuxClientCount(t, ctx, secondSessionName, 1)
	if _, err := input.Write([]byte{0x02, 'd'}); err != nil {
		t.Fatal(err)
	}
	waitForTranscriptCount(t, ctx, &transcript, "Select: ", 3)
	refreshed := transcript.String()
	if !strings.Contains(refreshed, ") "+firstSessionName) || !strings.Contains(refreshed, ") "+secondSessionName) {
		t.Fatalf("refreshed selector does not list both managed sessions:\n%s", refreshed)
	}
	if got := tmuxPanePath(t, secondSessionName); got != homeDir {
		t.Fatalf("second selector-created pane path = %q, want HOME %q", got, homeDir)
	}

	if _, err := io.WriteString(input, "1\n"); err != nil {
		t.Fatal(err)
	}
	waitForTranscriptCount(t, ctx, &transcript, "Detach with Ctrl-b d.", 3)
	waitForTmuxClientCount(t, ctx, firstSessionName, 1)
	if _, err := input.Write([]byte{0x02, 'd'}); err != nil {
		t.Fatal(err)
	}
	waitForTranscriptCount(t, ctx, &transcript, "Select: ", 4)
	if _, err := io.WriteString(input, "q\n"); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- selector.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatalf("selector failed: %v\n%s", err, transcript.String())
		}
	case <-ctx.Done():
		t.Fatalf("selector did not quit: %v\n%s", ctx.Err(), transcript.String())
	}
	for _, sessionName := range []string{firstSessionName, secondSessionName} {
		if exec.Command("tmux", "has-session", "-t", sessionName).Run() != nil {
			t.Fatalf("quitting the selector terminated tmux session %q", sessionName)
		}
	}
}

func TestClientRejectsInvalidAndUnmanagedSessionNames(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run real tmux/ttyd e2e tests")
	}
	requireCommand(t, "tmux")
	requireCommand(t, "ttyd")

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, ".cache", fmt.Sprintf("e2e-conflict-%d", os.Getpid()))
	_ = os.RemoveAll(stateDir)
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	unmanagedName := fmt.Sprintf("unmanaged-e2e-%d", os.Getpid())
	if output, err := exec.Command("tmux", "new-session", "-d", "-s", unmanagedName).CombinedOutput(); err != nil {
		t.Fatalf("create unmanaged tmux session: %v\n%s", err, output)
	}
	defer exec.Command("tmux", "kill-session", "-t", unmanagedName).Run()

	client := exec.Command(filepath.Join(root, "bin", "control-agents"), "--no-attach", unmanagedName)
	client.Env = append(os.Environ(), "CONTROL_AGENTS_STATE_DIR="+stateDir)
	if output, err := client.CombinedOutput(); commandExitCode(err) != 3 {
		t.Fatalf("unmanaged conflict exit = %d, want 3: %v\n%s", commandExitCode(err), err, output)
	}
	if exec.Command("tmux", "has-session", "-t", unmanagedName).Run() != nil {
		t.Fatal("unmanaged tmux session was modified")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sessions", unmanagedName+".json")); !os.IsNotExist(err) {
		t.Fatalf("unmanaged session was registered: %v", err)
	}

	malformedName := fmt.Sprintf("malformed-e2e-%d", os.Getpid())
	if output, err := exec.Command("tmux", "new-session", "-d", "-s", malformedName).CombinedOutput(); err != nil {
		t.Fatalf("create malformed-record tmux session: %v\n%s", err, output)
	}
	defer exec.Command("tmux", "kill-session", "-t", malformedName).Run()
	sessionsDir := filepath.Join(stateDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	malformedPath := filepath.Join(sessionsDir, malformedName+".json")
	malformed := fmt.Sprintf(`{"id":%q,"name":%q,"tmuxName":%q,"socket":%q,"pid":123,"cwd":"/tmp","createdAt":"2026-07-13T10:00:00Z"} trailing`, malformedName, malformedName, malformedName, filepath.Join(stateDir, "sockets", malformedName+".sock"))
	if err := os.WriteFile(malformedPath, []byte(malformed), 0o600); err != nil {
		t.Fatal(err)
	}
	client = exec.Command(filepath.Join(root, "bin", "control-agents"), "--no-attach", malformedName)
	client.Env = append(os.Environ(), "CONTROL_AGENTS_STATE_DIR="+stateDir)
	if output, err := client.CombinedOutput(); commandExitCode(err) != 3 {
		t.Fatalf("malformed record exit = %d, want 3: %v\n%s", commandExitCode(err), err, output)
	}
	if exec.Command("tmux", "has-session", "-t", malformedName).Run() != nil {
		t.Fatal("malformed registry file caused existing tmux session adoption")
	}

	client = exec.Command(filepath.Join(root, "bin", "control-agents"), "--no-attach", "bad name")
	client.Env = append(os.Environ(), "CONTROL_AGENTS_STATE_DIR="+stateDir)
	if output, err := client.CombinedOutput(); commandExitCode(err) != 2 {
		t.Fatalf("invalid name exit = %d, want 2: %v\n%s", commandExitCode(err), err, output)
	}
	if exec.Command("tmux", "has-session", "-t", "bad-name").Run() == nil {
		t.Fatal("invalid name was silently sanitized into a tmux session")
	}
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func waitForTranscriptCount(t *testing.T, ctx context.Context, transcript *lockedBuffer, text string, want int) {
	t.Helper()
	for {
		if strings.Count(transcript.String(), text) >= want {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("transcript did not contain %q %d times: %v\n%s", text, want, ctx.Err(), transcript.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForTmuxClientCount(t *testing.T, ctx context.Context, sessionName string, want int) {
	t.Helper()
	for {
		output, err := exec.Command("tmux", "list-clients", "-t", sessionName, "-F", "#{client_name}").Output()
		if err == nil && len(strings.Fields(string(output))) >= want {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("tmux session %q did not reach %d clients: %v", sessionName, want, ctx.Err())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForTmuxClientOrExit(t *testing.T, ctx context.Context, sessionName string, want int, processExit <-chan error, transcript *lockedBuffer) {
	t.Helper()
	for {
		output, err := exec.Command("tmux", "list-clients", "-t", sessionName, "-F", "#{client_name}").Output()
		if err == nil && len(strings.Fields(string(output))) >= want {
			return
		}
		select {
		case err := <-processExit:
			t.Fatalf("direct client exited before attaching: %v\n%s", err, transcript.String())
		default:
		}
		if ctx.Err() != nil {
			t.Fatalf("tmux session %q did not reach %d clients: %v\n%s", sessionName, want, ctx.Err(), transcript.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func tmuxClientCount(t *testing.T, sessionName string) int {
	t.Helper()
	output, err := exec.Command("tmux", "list-clients", "-t", sessionName, "-F", "#{client_name}").Output()
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(output)))
}

func assertTmuxWindowSize(t *testing.T, sessionName, want string) {
	t.Helper()
	cmd := exec.Command("tmux", "show-options", "-w", "-t", sessionName+":", "window-size")
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(output))
	if got != "window-size "+want {
		t.Fatalf("tmux window-size = %q, want %q", got, "window-size "+want)
	}
}

func assertTmuxWindowSizeTarget(t *testing.T, target, want string) {
	t.Helper()
	output, err := exec.Command("tmux", "show-options", "-Awv", "-t", target, "window-size").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(output)); got != want {
		t.Fatalf("tmux window-size for %s = %q, want %q", target, got, want)
	}
}

func tmuxGlobalOption(t *testing.T, option string) string {
	t.Helper()
	return strings.TrimSpace(string(mustTmuxOutput(t, "show-options", "-gv", option)))
}

func tmuxGlobalWindowOption(t *testing.T, option string) string {
	t.Helper()
	return strings.TrimSpace(string(mustTmuxOutput(t, "show-options", "-gwv", option)))
}

func createDetachedTmuxWindow(t *testing.T, sessionName string) (string, string) {
	t.Helper()
	output := mustTmuxOutput(t, "new-window", "-d", "-P", "-F", "#{window_id}\x1f#{pane_id}", "-t", sessionName+":")
	parts := strings.Split(strings.TrimSpace(string(output)), "\x1f")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "@") || !strings.HasPrefix(parts[1], "%") {
		t.Fatalf("new tmux window identity = %q", output)
	}
	return parts[0], parts[1]
}

func mustTmuxOutput(t *testing.T, args ...string) []byte {
	t.Helper()
	output, err := exec.Command("tmux", args...).Output()
	if err != nil {
		t.Fatalf("tmux %s: %v", strings.Join(args, " "), err)
	}
	return output
}

func assertTmuxPaneHistoryLimit(t *testing.T, target string, want int) {
	t.Helper()
	got := tmuxFormatInt(t, target, "#{history_limit}")
	if got != want {
		t.Fatalf("tmux pane history limit = %d, want %d", got, want)
	}
}

func tmuxFormatInt(t *testing.T, target, format string) int {
	t.Helper()
	output, err := exec.Command("tmux", "display-message", "-p", "-t", target, format).Output()
	if err != nil {
		t.Fatal(err)
	}
	got, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func waitForTmuxHistoryAtLeast(t *testing.T, ctx context.Context, target string, minimum int) int {
	t.Helper()
	for {
		value := tmuxFormatInt(t, target, "#{history_size}")
		if value >= minimum {
			return value
		}
		if ctx.Err() != nil {
			t.Fatalf("tmux history stayed at %d, want at least %d: %v", value, minimum, ctx.Err())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertTmuxMouse(t *testing.T, sessionName, want string) {
	t.Helper()
	assertTmuxOption(t, sessionName, "mouse", want)
}

func assertTmuxOption(t *testing.T, sessionName, option, want string) {
	t.Helper()
	cmd := exec.Command("tmux", "show-options", "-v", "-t", sessionName, option)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(string(output), "\r\n")
	if got != want {
		t.Fatalf("tmux %s = %q, want %q", option, got, want)
	}
}

func tmuxSessionCreated(t *testing.T, sessionName string) string {
	t.Helper()
	output, err := exec.Command("tmux", "display-message", "-p", "-t", sessionName, "#{session_created}").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func tmuxPanePath(t *testing.T, sessionName string) string {
	t.Helper()
	output, err := exec.Command("tmux", "display-message", "-p", "-t", sessionName, "#{pane_current_path}").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func killRegisteredTtyd(stateDir, sessionName string) {
	pid := readRegisteredTtydPID(stateDir, sessionName)
	if pid <= 0 {
		return
	}
	process, err := os.FindProcess(pid)
	if err == nil {
		_ = process.Kill()
	}
}

func stopProcess(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		t.Fatal("cannot stop missing process")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
}

func waitForProcessExit(t *testing.T, ctx context.Context, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	for {
		if processReaped(pid) {
			return
		}
		if ctx.Err() != nil {
			state, _ := processState(pid)
			t.Fatalf("process %d was not reaped (state %q): %v", pid, state, ctx.Err())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitForProcessState(t *testing.T, ctx context.Context, pid int, want string) {
	t.Helper()
	for {
		state, err := processState(pid)
		if err == nil && state == want {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("process %d state/error = %q/%v, want %q: %v", pid, state, err, want, ctx.Err())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForPIDFile(t *testing.T, ctx context.Context, path string) int {
	t.Helper()
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil && pid > 1 {
				return pid
			}
			err = parseErr
		}
		if ctx.Err() != nil {
			t.Fatalf("read PID file %s: %v", path, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func processReaped(pid int) bool {
	_, err := os.Stat(filepath.Join("/proc", fmt.Sprint(pid)))
	return errors.Is(err, os.ErrNotExist)
}

func processAlive(pid int) bool {
	if pid <= 1 || processReaped(pid) {
		return false
	}
	if state, err := processState(pid); err == nil && state == "Z" {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func processState(pid int) (string, error) {
	data, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(pid), "stat"))
	if err != nil {
		return "", err
	}
	end := strings.LastIndex(string(data), ")")
	if end < 0 || end+2 >= len(data) {
		return "", errors.New("unexpected process stat format")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) == 0 {
		return "", errors.New("missing process state")
	}
	return fields[0], nil
}

func processParentPID(pid int) (int, error) {
	data, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(pid), "stat"))
	if err != nil {
		return 0, err
	}
	end := strings.LastIndex(string(data), ")")
	if end < 0 || end+2 >= len(data) {
		return 0, errors.New("unexpected process stat format")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 2 {
		return 0, errors.New("missing process parent PID")
	}
	return strconv.Atoi(fields[1])
}

func waitForUnixSocket(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	for {
		connection, err := net.DialTimeout("unix", path, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("unix socket %s did not become ready: %v", path, ctx.Err())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func unixSocketInode(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 8 && fields[len(fields)-1] == path {
			return fields[6]
		}
	}
	t.Fatalf("unix socket %s is absent from /proc/net/unix", path)
	return ""
}

func ttydPIDsForSocket(t *testing.T, socketPath string) []int {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		arguments := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		if len(arguments) == 0 || filepath.Base(arguments[0]) != "ttyd" {
			continue
		}
		for index := 1; index+1 < len(arguments); index++ {
			if arguments[index] == "-i" && arguments[index+1] == socketPath {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}

func assertTtydCommandLineContains(t *testing.T, stateDir, sessionName string, wantParts ...string) {
	t.Helper()
	pid := readRegisteredTtydPID(stateDir, sessionName)
	if pid <= 0 {
		t.Fatalf("missing registered ttyd pid for %s", sessionName)
	}
	data, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(pid), "cmdline"))
	if err != nil {
		t.Fatal(err)
	}
	cmdline := strings.ReplaceAll(string(data), "\x00", " ")
	for _, want := range wantParts {
		if !strings.Contains(cmdline, want) {
			t.Fatalf("ttyd cmdline %q does not contain %q", cmdline, want)
		}
	}
}

func assertTtydAttachSuffix(t *testing.T, pid int, tmuxBinary, sessionName string) {
	t.Helper()
	arguments, err := readProcessArguments(pid)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{tmuxBinary, "attach-session", "-E", "-t", sessionName}
	if len(arguments) < len(want) {
		t.Fatalf("ttyd argv has %d arguments, want exact attach suffix", len(arguments))
	}
	got := arguments[len(arguments)-len(want):]
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("ttyd attach suffix = %#v, want %#v", got, want)
	}
	if !filepath.IsAbs(got[0]) {
		t.Fatalf("new ttyd bridge selected relative tmux path %q", got[0])
	}
}

func readProcessArguments(pid int) ([]string, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(data), "\x00"), "\x00"), nil
}

func readRegisteredTtydPID(stateDir, sessionName string) int {
	data, err := os.ReadFile(filepath.Join(stateDir, "sessions", sessionName+".json"))
	if err != nil {
		return 0
	}
	var session struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(data, &session) != nil || session.PID <= 0 {
		return 0
	}
	return session.PID
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not installed", name)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForHTTP(t *testing.T, ctx context.Context, client *http.Client, url string) {
	t.Helper()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			return
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitForSession(t *testing.T, ctx context.Context, baseClient *http.Client, port int, sessionName string) {
	t.Helper()
	client := *baseClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	for {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		cookie := login(t, &client, port)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://127.0.0.1:%d/api/sessions", port), nil)
		req.AddCookie(cookie)
		resp, err := client.Do(req)
		if err == nil {
			var payload struct {
				Sessions []struct {
					Name string `json:"name"`
				} `json:"sessions"`
			}
			if decodeErr := json.NewDecoder(resp.Body).Decode(&payload); decodeErr == nil {
				for _, session := range payload.Sessions {
					if session.Name == sessionName {
						resp.Body.Close()
						return
					}
				}
			}
			resp.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func waitForSessionRemoval(t *testing.T, ctx context.Context, baseClient *http.Client, port int, sessionName string) {
	t.Helper()
	client := *baseClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	cookie := login(t, &client, port)
	for {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://127.0.0.1:%d/api/sessions", port), nil)
		req.AddCookie(cookie)
		resp, err := client.Do(req)
		if err == nil {
			var payload struct {
				Sessions []struct {
					Name string `json:"name"`
				} `json:"sessions"`
			}
			found := false
			if decodeErr := json.NewDecoder(resp.Body).Decode(&payload); decodeErr == nil {
				for _, managed := range payload.Sessions {
					found = found || managed.Name == sessionName
				}
			}
			resp.Body.Close()
			if !found {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func login(t *testing.T, client *http.Client, port int) *http.Cookie {
	t.Helper()
	body := strings.NewReader("password=secret")
	resp, err := client.Post(fmt.Sprintf("https://127.0.0.1:%d/login", port), "application/x-www-form-urlencoded", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "control_agents_session" {
			return cookie
		}
	}
	scanner := bufio.NewScanner(resp.Body)
	if scanner.Scan() {
		t.Fatalf("missing auth cookie, response starts with %q", scanner.Text())
	}
	t.Fatal("missing auth cookie")
	return nil
}

func insecureHTTPSClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}
