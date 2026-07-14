package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"control-agents/internal/registry"
)

type processBridge struct {
	ttydBinary string
	tmuxBinary string
	logsDir    string
	scrollback int
	timeout    time.Duration
	poll       time.Duration

	ownedMu sync.Mutex
	owned   map[int]<-chan struct{}
}

type bridgeCommandKind uint8

const (
	bridgeCommandUnrelated bridgeCommandKind = iota
	bridgeCommandLegacy
	bridgeCommandCurrent
)

type verifiedBridgeProcesses struct {
	current []int
	legacy  []int
}

func newProcessBridge(cfg Config, logsDir string) *processBridge {
	return &processBridge{
		ttydBinary: cfg.TtydBinary,
		tmuxBinary: cfg.TmuxBinary,
		logsDir:    logsDir,
		scrollback: cfg.WebScrollbackLines,
		timeout:    cfg.BridgeStartupTimeout,
		poll:       50 * time.Millisecond,
	}
}

func (b *processBridge) Ensure(ctx context.Context, managed registry.Session) (int, error) {
	verified := b.verifiedProcesses(managed)
	keep, stop := verified.reconcilePlan(managed.PID, socketReady(managed.Socket))
	stopContext := ctx
	if keep != 0 {
		stopContext = context.Background()
	}
	for _, pid := range stop {
		if err := b.stopVerifiedPID(stopContext, managed, pid); err != nil {
			return 0, lifecycleError(ErrorDependency, "replace bridge", managed.ID, err)
		}
	}
	if keep != 0 {
		if err := secureSocket(managed.Socket); err != nil {
			return 0, lifecycleError(ErrorBridgeIncomplete, "secure bridge socket", managed.ID, err)
		}
		return keep, nil
	}
	if err := os.Remove(managed.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, lifecycleError(ErrorDependency, "replace bridge", managed.ID, fmt.Errorf("remove stale socket: %w", err))
	}
	return b.start(ctx, managed)
}

func (b *processBridge) Stop(ctx context.Context, managed registry.Session) error {
	var stopErrors []error
	for _, pid := range b.verifiedProcesses(managed).all() {
		if err := b.stopVerifiedPID(ctx, managed, pid); err != nil {
			stopErrors = append(stopErrors, err)
		}
	}
	return errors.Join(stopErrors...)
}

func (b *processBridge) start(ctx context.Context, managed registry.Session) (int, error) {
	if len(managed.Socket) > 100 {
		return 0, lifecycleError(ErrorBridgeIncomplete, "start bridge", managed.ID, fmt.Errorf("unix socket path is too long: %s", managed.Socket))
	}
	if err := os.MkdirAll(b.logsDir, 0o700); err != nil {
		return 0, lifecycleError(ErrorDependency, "start bridge", managed.ID, fmt.Errorf("create bridge log directory: %w", err))
	}
	if err := os.Chmod(b.logsDir, 0o700); err != nil {
		return 0, lifecycleError(ErrorDependency, "start bridge", managed.ID, fmt.Errorf("set bridge log directory permissions: %w", err))
	}
	logPath := filepath.Join(b.logsDir, managed.ID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, lifecycleError(ErrorDependency, "start bridge", managed.ID, fmt.Errorf("open bridge log: %w", err))
	}
	if err := logFile.Chmod(0o600); err != nil {
		_ = logFile.Close()
		return 0, lifecycleError(ErrorDependency, "start bridge", managed.ID, fmt.Errorf("set bridge log permissions: %w", err))
	}

	arguments := bridgeArguments(managed, b.scrollback, b.tmuxBinary)
	// The bridge intentionally outlives the request that reconciled it. Use a
	// non-canceling context while still executing ttyd directly without a shell.
	command := exec.CommandContext(context.Background(), b.ttydBinary, arguments...)
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		var pathError *os.PathError
		var execError *exec.Error
		if errors.As(err, &pathError) || errors.As(err, &execError) {
			return 0, lifecycleError(ErrorDependency, "start bridge", managed.ID, fmt.Errorf("execute ttyd: %w", err))
		}
		return 0, lifecycleError(ErrorBridgeIncomplete, "start bridge", managed.ID, fmt.Errorf("ttyd failed to start; see %s: %w", logPath, err))
	}
	pid := command.Process.Pid
	reaped := make(chan struct{})
	b.registerOwned(pid, reaped)
	go func() {
		_ = command.Wait()
		close(reaped)
		b.unregisterOwned(pid, reaped)
	}()
	_ = logFile.Close()

	deadline := time.Now().Add(b.timeout)
	for {
		if socketReady(managed.Socket) && b.pidVerified(managed, pid) {
			if err := secureSocket(managed.Socket); err != nil {
				_ = b.stopVerifiedPID(context.Background(), managed, pid)
				return 0, lifecycleError(ErrorBridgeIncomplete, "secure bridge socket", managed.ID, err)
			}
			return pid, nil
		}
		select {
		case <-reaped:
			return 0, lifecycleError(ErrorBridgeIncomplete, "start bridge", managed.ID, fmt.Errorf("ttyd exited before its socket was ready; see %s", logPath))
		default:
		}
		if time.Now().After(deadline) {
			_ = b.stopVerifiedPID(context.Background(), managed, pid)
			return 0, lifecycleError(ErrorBridgeIncomplete, "start bridge", managed.ID, fmt.Errorf("timed out waiting for ttyd unix socket; see %s", logPath))
		}
		select {
		case <-ctx.Done():
			_ = b.stopVerifiedPID(context.Background(), managed, pid)
			return 0, lifecycleError(ErrorBridgeIncomplete, "start bridge", managed.ID, ctx.Err())
		case <-time.After(b.poll):
		}
	}
}

func (b *processBridge) stopVerifiedPID(ctx context.Context, managed registry.Session, pid int) error {
	reaped, owned := b.ownedProcess(pid)
	handle, verified, err := b.openVerifiedHandle(managed, pid)
	if err != nil {
		return err
	}
	if !verified {
		if owned {
			return waitForOwnedReap(ctx, pid, reaped, time.Second)
		}
		return nil
	}
	defer handle.Close()
	if err := handle.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("stop verified ttyd process %d: %w", pid, err)
	}
	deadline := time.Now().Add(time.Second)
	killSent := false
	for {
		if owned {
			select {
			case <-reaped:
				return nil
			default:
			}
		}
		if state, err := processState(pid); err == nil && state == "Z" {
			if !owned {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("verified ttyd process %d exited but its Go child was not reaped", pid)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
				continue
			}
		}
		alive, err := handle.Alive()
		if err != nil {
			return fmt.Errorf("check verified ttyd process %d: %w", pid, err)
		}
		if !alive {
			if owned {
				if err := waitForOwnedReap(ctx, pid, reaped, time.Until(deadline)); err != nil {
					return err
				}
			}
			return nil
		}
		if time.Now().After(deadline) {
			if killSent {
				return fmt.Errorf("verified ttyd process %d did not stop after SIGKILL", pid)
			}
			if err := handle.Signal(syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("kill verified ttyd process %d: %w", pid, err)
			}
			killSent = true
			deadline = time.Now().Add(time.Second)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (b *processBridge) registerOwned(pid int, reaped <-chan struct{}) {
	if pid <= 1 || reaped == nil {
		return
	}
	b.ownedMu.Lock()
	defer b.ownedMu.Unlock()
	if b.owned == nil {
		b.owned = make(map[int]<-chan struct{})
	}
	b.owned[pid] = reaped
}

func (b *processBridge) unregisterOwned(pid int, reaped <-chan struct{}) {
	b.ownedMu.Lock()
	defer b.ownedMu.Unlock()
	if b.owned[pid] == reaped {
		delete(b.owned, pid)
	}
}

func (b *processBridge) ownedProcess(pid int) (<-chan struct{}, bool) {
	b.ownedMu.Lock()
	defer b.ownedMu.Unlock()
	reaped, ok := b.owned[pid]
	return reaped, ok
}

func waitForOwnedReap(ctx context.Context, pid int, reaped <-chan struct{}, timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("verified ttyd process %d exited but its Go child was not reaped", pid)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-reaped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("verified ttyd process %d exited but its Go child was not reaped", pid)
	}
}

func (b *processBridge) openVerifiedHandle(managed registry.Session, pid int) (*linuxProcessHandle, bool, error) {
	if pid <= 1 {
		return nil, false, nil
	}
	handle, err := openLinuxProcessHandle(pid)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil, false, nil
		}
		return nil, false, err
	}
	executable, err := expectedExecutable(b.ttydBinary)
	if err != nil || !processExecutableMatches(pid, executable) {
		_ = handle.Close()
		return nil, false, nil
	}
	arguments, err := processCommandLine(pid)
	if err != nil || classifyBridgeCommand(arguments, filepath.Base(b.ttydBinary), b.tmuxBinary, managed) == bridgeCommandUnrelated {
		_ = handle.Close()
		return nil, false, nil
	}
	alive, err := handle.Alive()
	if err != nil {
		_ = handle.Close()
		return nil, false, err
	}
	if !alive {
		_ = handle.Close()
		return nil, false, nil
	}
	return handle, true, nil
}

func (b *processBridge) verifiedProcesses(managed registry.Session) verifiedBridgeProcesses {
	executable, err := expectedExecutable(b.ttydBinary)
	if err != nil {
		return verifiedBridgeProcesses{}
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		var verified verifiedBridgeProcesses
		verified.add(managed.PID, b.pidKindForExecutable(managed, managed.PID, executable))
		return verified
	}
	var verified verifiedBridgeProcesses
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		verified.add(pid, b.pidKindForExecutable(managed, pid, executable))
	}
	sort.Ints(verified.current)
	sort.Ints(verified.legacy)
	return verified
}

func (b *processBridge) pidVerified(managed registry.Session, pid int) bool {
	return b.pidKind(managed, pid) == bridgeCommandCurrent
}

func (b *processBridge) pidKind(managed registry.Session, pid int) bridgeCommandKind {
	executable, err := expectedExecutable(b.ttydBinary)
	if err != nil {
		return bridgeCommandUnrelated
	}
	return b.pidKindForExecutable(managed, pid, executable)
}

func (b *processBridge) pidKindForExecutable(managed registry.Session, pid int, executable os.FileInfo) bridgeCommandKind {
	if pid <= 1 {
		return bridgeCommandUnrelated
	}
	if !processExecutableMatches(pid, executable) {
		return bridgeCommandUnrelated
	}
	arguments, err := processCommandLine(pid)
	if err != nil {
		return bridgeCommandUnrelated
	}
	return classifyBridgeCommand(arguments, filepath.Base(b.ttydBinary), b.tmuxBinary, managed)
}

func expectedExecutable(binary string) (os.FileInfo, error) {
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, err
	}
	return os.Stat(path)
}

func processExecutableMatches(pid int, expected os.FileInfo) bool {
	if pid <= 1 || expected == nil {
		return false
	}
	actual, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	return err == nil && os.SameFile(expected, actual)
}

func bridgeArguments(managed registry.Session, scrollback int, tmuxBinary string) []string {
	return []string{
		"-W",
		"-i", managed.Socket,
		"-b", "/terminal/" + managed.PublicRef,
		"-t", "scrollback=" + strconv.Itoa(scrollback),
		tmuxBinary, "attach-session", "-E", "-t", managed.TmuxName,
	}
}

func bridgeCommandMatches(arguments []string, ttydBase, tmuxBinary string, managed registry.Session) bool {
	return classifyBridgeCommand(arguments, ttydBase, tmuxBinary, managed) == bridgeCommandCurrent
}

func legacyBridgeCommandMatches(arguments []string, ttydBase, tmuxBinary string, managed registry.Session) bool {
	return classifyBridgeCommand(arguments, ttydBase, tmuxBinary, managed) == bridgeCommandLegacy
}

func classifyBridgeCommand(arguments []string, ttydBase, tmuxBinary string, managed registry.Session) bridgeCommandKind {
	if len(arguments) == 0 || filepath.Base(arguments[0]) != ttydBase {
		return bridgeCommandUnrelated
	}
	if !hasOptionValue(arguments[1:], "-i", managed.Socket) {
		return bridgeCommandUnrelated
	}
	base := optionValue(arguments[1:], "-b")
	currentBase := "/terminal/" + managed.PublicRef
	legacyBase := "/terminal/" + managed.ID
	if base != currentBase && base != legacyBase {
		return bridgeCommandUnrelated
	}
	if hasArgumentSuffix(arguments[1:], []string{tmuxBinary, "attach-session", "-E", "-t", managed.TmuxName}) {
		if base == legacyBase && base != currentBase {
			return bridgeCommandLegacy
		}
		return bridgeCommandCurrent
	}
	if hasArgumentSuffix(arguments[1:], []string{tmuxBinary, "attach-session", "-t", managed.TmuxName}) {
		return bridgeCommandLegacy
	}
	return bridgeCommandUnrelated
}

func hasOptionValue(arguments []string, option, value string) bool {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == option && arguments[index+1] == value {
			return true
		}
	}
	return false
}

func optionValue(arguments []string, option string) string {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == option {
			return arguments[index+1]
		}
	}
	return ""
}

func hasArgumentSuffix(arguments, suffix []string) bool {
	if len(suffix) == 0 || len(suffix) > len(arguments) {
		return false
	}
	return strings.Join(arguments[len(arguments)-len(suffix):], "\x00") == strings.Join(suffix, "\x00")
}

func (p *verifiedBridgeProcesses) add(pid int, kind bridgeCommandKind) {
	switch kind {
	case bridgeCommandCurrent:
		p.current = append(p.current, pid)
	case bridgeCommandLegacy:
		p.legacy = append(p.legacy, pid)
	}
}

func (p verifiedBridgeProcesses) all() []int {
	all := make([]int, 0, len(p.current)+len(p.legacy))
	all = append(all, p.current...)
	all = append(all, p.legacy...)
	sort.Ints(all)
	return all
}

func (p verifiedBridgeProcesses) reconcilePlan(registeredPID int, ready bool) (int, []int) {
	keep := 0
	if ready && len(p.current) > 0 {
		keep = p.current[0]
		if containsPID(p.current, registeredPID) {
			keep = registeredPID
		}
	}
	stop := make([]int, 0, len(p.current)+len(p.legacy))
	for _, pid := range p.all() {
		if pid != keep {
			stop = append(stop, pid)
		}
	}
	return keep, stop
}

func processCommandLine(pid int) ([]string, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil, err
	}
	text := strings.TrimRight(string(data), "\x00")
	if text == "" {
		return nil, errors.New("empty process command line")
	}
	return strings.Split(text, "\x00"), nil
}

func processAlive(pid int) bool {
	if pid <= 1 {
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
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", err
	}
	text := string(data)
	end := strings.LastIndex(text, ")")
	if end < 0 || end+2 >= len(text) {
		return "", errors.New("unexpected process stat format")
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) == 0 {
		return "", errors.New("missing process state")
	}
	return fields[0], nil
}

func socketReady(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return false
	}
	connection, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func secureSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect ttyd unix socket: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("ttyd endpoint is not a direct unix socket")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set ttyd unix socket permissions: %w", err)
	}
	return nil
}

func containsPID(pids []int, target int) bool {
	for _, pid := range pids {
		if pid == target {
			return true
		}
	}
	return false
}
