package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"control-agents/internal/registry"
	"control-agents/internal/tmux"
)

const (
	defaultWindowSize     = "manual"
	defaultMouse          = "off"
	defaultScrollback     = 10000
	defaultStartupTimeout = 5 * time.Second
)

type Lifecycle interface {
	List(ctx context.Context) ([]registry.Session, error)
	Create(ctx context.Context, name string) (registry.Session, error)
	EnsureBridge(ctx context.Context, sessionID string) (registry.Session, error)
	Reconcile(ctx context.Context) ([]registry.Session, error)
	Terminate(ctx context.Context, sessionID string) error
}

type Config struct {
	StateDir                string
	HomeDir                 string
	TmuxBinary              string
	TtydBinary              string
	WindowSize              string
	Mouse                   string
	AppName                 string
	WebScrollbackLines      int
	WebScrollbackConfigured bool
	BridgeStartupTimeout    time.Duration
	Logger                  *slog.Logger
}

type registryStore interface {
	Ensure() error
	ListCompatible() ([]registry.Session, error)
	Read(id string) (registry.Session, error)
	ReadCompatible(id string) (registry.Session, error)
	Write(session registry.Session) error
	Remove(id string) error
}

type tmuxLifecycle interface {
	HasSession(ctx context.Context, target string) (bool, error)
	CreateManagedSession(ctx context.Context, name, home string, options tmux.ManagedSessionOptions) error
	ConfigureHistory(ctx context.Context, target string) error
	ConfigureManualWindowSize(ctx context.Context, target string) error
	SetSessionEnvironment(ctx context.Context, target, name, value string) error
	KillSession(ctx context.Context, target string) error
}

type bridgeLifecycle interface {
	Ensure(ctx context.Context, managed registry.Session) (int, error)
	Stop(ctx context.Context, managed registry.Session) error
}

type Manager struct {
	cfg         Config
	store       registryStore
	tmux        tmuxLifecycle
	bridge      bridgeLifecycle
	logger      *slog.Logger
	locksDir    string
	socketsDir  string
	logsDir     string
	resizeDir   string
	agentDir    string
	agentSocket string
}

func ConfigFromEnvironment(stateDir, homeDir string, logger *slog.Logger) (Config, error) {
	scrollback := defaultScrollback
	if value := os.Getenv("CONTROL_AGENTS_WEB_SCROLLBACK_LINES"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return Config{}, errors.New("CONTROL_AGENTS_WEB_SCROLLBACK_LINES must be a non-negative integer")
		}
		scrollback = parsed
	}
	tmuxBinary, err := tmux.ResolveBinary(homeDir)
	if err != nil {
		return Config{}, err
	}
	return Config{
		StateDir:                stateDir,
		HomeDir:                 homeDir,
		TmuxBinary:              tmuxBinary,
		WindowSize:              environmentOrDefault("CONTROL_AGENTS_TMUX_WINDOW_SIZE", defaultWindowSize),
		Mouse:                   environmentOrDefault("CONTROL_AGENTS_TMUX_MOUSE", defaultMouse),
		AppName:                 os.Getenv("CONTROL_AGENTS_APP_NAME"),
		WebScrollbackLines:      scrollback,
		WebScrollbackConfigured: true,
		Logger:                  logger,
	}, nil
}

func New(cfg Config) (*Manager, error) {
	if strings.TrimSpace(cfg.TmuxBinary) == "" {
		binary, err := tmux.ResolveBinary(cfg.HomeDir)
		if err != nil {
			return nil, err
		}
		cfg.TmuxBinary = binary
	}
	cfg = withDefaults(cfg)
	if strings.TrimSpace(cfg.StateDir) == "" {
		return nil, errors.New("managed session state directory cannot be empty")
	}
	stateDir, err := filepath.Abs(cfg.StateDir)
	if err != nil {
		return nil, fmt.Errorf("resolve managed session state directory: %w", err)
	}
	cfg.StateDir = stateDir
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	store := registry.NewStore(cfg.StateDir)
	store.SetTmuxBinary(cfg.TmuxBinary)
	store.SetLogger(cfg.Logger.With("component", "registry"))
	manager := newManager(cfg, store, tmux.NewClientWithBinary(cfg.TmuxBinary), nil)
	manager.bridge = newProcessBridge(cfg, manager.logsDir)
	if err := manager.ensurePrivateState(); err != nil {
		return nil, err
	}
	return manager, nil
}

func newManager(cfg Config, store registryStore, tmuxClient tmuxLifecycle, bridge bridgeLifecycle) *Manager {
	agentDir := filepath.Join(cfg.StateDir, forwardedAgentDirectory)
	return &Manager{
		cfg:         cfg,
		store:       store,
		tmux:        tmuxClient,
		bridge:      bridge,
		logger:      cfg.Logger,
		locksDir:    filepath.Join(cfg.StateDir, "locks"),
		socketsDir:  filepath.Join(cfg.StateDir, "sockets"),
		logsDir:     filepath.Join(cfg.StateDir, "logs"),
		resizeDir:   filepath.Join(cfg.StateDir, "resize"),
		agentDir:    agentDir,
		agentSocket: filepath.Join(agentDir, forwardedAgentSocket),
	}
}

func (m *Manager) List(ctx context.Context) ([]registry.Session, error) {
	return m.Reconcile(ctx)
}

func (m *Manager) Create(ctx context.Context, name string) (registry.Session, error) {
	managed, _, err := m.CreateOrSelect(ctx, name)
	return managed, err
}

// CreateOrSelect creates a managed session or returns the healthy managed
// session that already owns name. The boolean is true only when this call
// created the durable session and terminal bridge while holding the lifecycle
// lock.
func (m *Manager) CreateOrSelect(ctx context.Context, name string) (registry.Session, bool, error) {
	if !registry.ValidID(name) {
		return registry.Session{}, false, lifecycleError(ErrorInvalidName, "create", name, errors.New("name must match [A-Za-z0-9][A-Za-z0-9._-]{0,63}"))
	}
	lock, err := m.lock(ctx, name)
	if err != nil {
		return registry.Session{}, false, lifecycleError(ErrorDependency, "create", name, err)
	}
	defer lock.Close()

	existing, readErr := m.readManaged(name)
	if readErr == nil {
		alive, err := m.tmux.HasSession(ctx, existing.TmuxName)
		if err != nil {
			return registry.Session{}, false, lifecycleError(ErrorDependency, "create", name, fmt.Errorf("check tmux session: %w", err))
		}
		if alive {
			managed, err := m.ensureBridgeLocked(ctx, existing)
			return managed, false, err
		}
		if err := m.cleanupLocked(ctx, existing); err != nil {
			return registry.Session{}, false, lifecycleError(ErrorDependency, "create", name, err)
		}
	} else if errors.Is(readErr, registry.ErrInvalidSessionRecord) {
		m.logger.Warn("replace invalid managed session record", "reason_code", "invalid_record")
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return registry.Session{}, false, lifecycleError(ErrorDependency, "create", name, readErr)
	}

	exists, err := m.tmux.HasSession(ctx, name)
	if err != nil {
		return registry.Session{}, false, lifecycleError(ErrorDependency, "create", name, fmt.Errorf("check tmux session: %w", err))
	}
	if exists {
		return registry.Session{}, false, lifecycleError(ErrorConflict, "create", name, errors.New("an unmanaged tmux session already uses this name"))
	}

	statusLabel := name
	if strings.TrimSpace(m.cfg.AppName) != "" {
		statusLabel = m.cfg.AppName
	}
	options := tmux.ManagedSessionOptions{
		WindowSize:  m.cfg.WindowSize,
		Mouse:       m.cfg.Mouse,
		StatusLeft:  "[" + statusLabel + "] ",
		SSHAuthSock: m.agentSocket,
	}
	if err := m.tmux.CreateManagedSession(ctx, name, m.cfg.HomeDir, options); err != nil {
		_ = m.killTmuxIfPresent(context.Background(), name)
		return registry.Session{}, false, lifecycleError(ErrorDependency, "create", name, fmt.Errorf("create tmux session: %w", err))
	}

	managed := registry.Session{
		ID:        name,
		Name:      name,
		TmuxName:  name,
		Socket:    filepath.Join(m.socketsDir, name+".sock"),
		CWD:       m.cfg.HomeDir,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	managed.PublicRef, err = registry.NewPublicRef()
	if err != nil {
		_ = m.cleanupNewSession(context.Background(), managed)
		return registry.Session{}, false, lifecycleError(ErrorDependency, "create", name, err)
	}
	pid, err := m.bridge.Ensure(ctx, managed)
	if err != nil {
		_ = m.cleanupNewSession(context.Background(), managed)
		return registry.Session{}, false, classifyBridgeError("create", managed.ID, err)
	}
	managed.PID = pid
	if err := m.store.Write(managed); err != nil {
		_ = m.cleanupNewSession(context.Background(), managed)
		return registry.Session{}, false, lifecycleError(ErrorDependency, "create", name, fmt.Errorf("write managed session record: %w", err))
	}
	return managed, true, nil
}

func (m *Manager) EnsureBridge(ctx context.Context, sessionID string) (registry.Session, error) {
	if !registry.ValidID(sessionID) {
		return registry.Session{}, lifecycleError(ErrorInvalidName, "ensure bridge", sessionID, nil)
	}
	lock, err := m.lock(ctx, sessionID)
	if err != nil {
		return registry.Session{}, lifecycleError(ErrorDependency, "ensure bridge", sessionID, err)
	}
	defer lock.Close()

	managed, err := m.readManaged(sessionID)
	if err != nil {
		return registry.Session{}, classifyManagedReadError("ensure bridge", sessionID, err)
	}
	alive, err := m.tmux.HasSession(ctx, managed.TmuxName)
	if err != nil {
		return registry.Session{}, lifecycleError(ErrorDependency, "ensure bridge", sessionID, fmt.Errorf("check tmux session: %w", err))
	}
	if !alive {
		cleanupErr := m.cleanupLocked(ctx, managed)
		return registry.Session{}, lifecycleError(ErrorNotFound, "ensure bridge", sessionID, cleanupErr)
	}
	return m.ensureBridgeLocked(ctx, managed)
}

// WithSession resolves one live managed session and holds its cross-process
// lifecycle lock while use runs. Server-side session operations use this to
// avoid acting on a replacement session with the same canonical name while a
// create or terminate operation is racing with the request.
func (m *Manager) WithSession(ctx context.Context, sessionID string, use func(registry.Session) error) error {
	if !registry.ValidID(sessionID) {
		return lifecycleError(ErrorInvalidName, "use", sessionID, nil)
	}
	lock, err := m.lock(ctx, sessionID)
	if err != nil {
		return lifecycleError(ErrorDependency, "use", sessionID, err)
	}
	defer lock.Close()

	managed, err := m.readManaged(sessionID)
	if err != nil {
		return classifyManagedReadError("use", sessionID, err)
	}
	alive, err := m.tmux.HasSession(ctx, managed.TmuxName)
	if err != nil {
		return lifecycleError(ErrorDependency, "use", sessionID, fmt.Errorf("check tmux session: %w", err))
	}
	if !alive {
		cleanupErr := m.cleanupLocked(ctx, managed)
		return lifecycleError(ErrorNotFound, "use", sessionID, cleanupErr)
	}
	return use(managed)
}

func (m *Manager) Reconcile(ctx context.Context) ([]registry.Session, error) {
	records, err := m.store.ListCompatible()
	if err != nil {
		return nil, lifecycleError(ErrorDependency, "reconcile", "", err)
	}
	reconciled := make([]registry.Session, 0, len(records))
	var reconcileErrors []error
	for _, record := range records {
		managed, keep, err := m.reconcileOne(ctx, record.ID)
		if err != nil {
			reconcileErrors = append(reconcileErrors, err)
			m.logger.Warn("managed session reconciliation failed", "opaque_id", record.PublicRef, "reason_code", "reconciliation_failure")
			continue
		}
		if keep {
			reconciled = append(reconciled, managed)
		}
	}
	if len(reconcileErrors) > 0 {
		return nil, errors.Join(reconcileErrors...)
	}
	return reconciled, nil
}

func (m *Manager) Terminate(ctx context.Context, sessionID string) error {
	return m.TerminateWithCleanup(ctx, sessionID, nil)
}

// TerminateWithCleanup runs cleanup after the managed tmux session has stopped
// but before the lifecycle lock is released. The server uses this hook for
// session-owned in-memory state that must not be cleared from a replacement
// session created concurrently under the same canonical name.
func (m *Manager) TerminateWithCleanup(ctx context.Context, sessionID string, cleanup func()) error {
	return m.TerminateChecked(ctx, sessionID, "", nil, cleanup)
}

// TerminateChecked keeps public-reference and pane-generation verification
// inside the lifecycle lock so a replacement record cannot be terminated by a
// request authorized for an older managed session.
func (m *Manager) TerminateChecked(ctx context.Context, sessionID, expectedPublicRef string, verify func(registry.Session) error, cleanup func()) error {
	if !registry.ValidID(sessionID) {
		return lifecycleError(ErrorInvalidName, "terminate", sessionID, nil)
	}
	lock, err := m.lock(ctx, sessionID)
	if err != nil {
		return lifecycleError(ErrorDependency, "terminate", sessionID, err)
	}
	defer lock.Close()

	managed, err := m.readManaged(sessionID)
	if err != nil {
		return classifyManagedReadError("terminate", sessionID, err)
	}
	if expectedPublicRef != "" && managed.PublicRef != expectedPublicRef {
		return lifecycleError(ErrorNotFound, "terminate", sessionID, errors.New("public session reference changed"))
	}
	if verify != nil {
		if err := verify(managed); err != nil {
			return err
		}
	}
	if err := m.bridge.Stop(ctx, managed); err != nil {
		return lifecycleError(ErrorDependency, "terminate", sessionID, fmt.Errorf("stop terminal bridge: %w", err))
	}
	alive, err := m.tmux.HasSession(ctx, managed.TmuxName)
	if err != nil {
		return lifecycleError(ErrorDependency, "terminate", sessionID, fmt.Errorf("check tmux session: %w", err))
	} else if alive {
		if err := m.tmux.KillSession(ctx, managed.TmuxName); err != nil {
			return lifecycleError(ErrorDependency, "terminate", sessionID, fmt.Errorf("kill tmux session: %w", err))
		}
	}
	if cleanup != nil {
		cleanup()
	}
	if err := m.removeArtifacts(managed); err != nil {
		return lifecycleError(ErrorDependency, "terminate", sessionID, err)
	}
	return nil
}

func (m *Manager) reconcileOne(ctx context.Context, sessionID string) (registry.Session, bool, error) {
	lock, err := m.lock(ctx, sessionID)
	if err != nil {
		return registry.Session{}, false, lifecycleError(ErrorDependency, "reconcile", sessionID, err)
	}
	defer lock.Close()
	managed, err := m.readManaged(sessionID)
	if errors.Is(err, os.ErrNotExist) {
		return registry.Session{}, false, nil
	}
	if err != nil {
		return registry.Session{}, false, lifecycleError(ErrorDependency, "reconcile", sessionID, err)
	}
	alive, err := m.tmux.HasSession(ctx, managed.TmuxName)
	if err != nil {
		return registry.Session{}, false, lifecycleError(ErrorDependency, "reconcile", sessionID, fmt.Errorf("check tmux session: %w", err))
	}
	if !alive {
		if err := m.cleanupLocked(ctx, managed); err != nil {
			return registry.Session{}, false, lifecycleError(ErrorDependency, "reconcile", sessionID, err)
		}
		m.logger.Info("removed stale managed session", "opaque_id", managed.PublicRef, "reason_code", "stale_record")
		return registry.Session{}, false, nil
	}
	managed, err = m.ensureBridgeLocked(ctx, managed)
	if err != nil {
		return registry.Session{}, false, err
	}
	return managed, true, nil
}

func (m *Manager) ensureBridgeLocked(ctx context.Context, managed registry.Session) (registry.Session, error) {
	if err := m.tmux.ConfigureHistory(ctx, managed.TmuxName); err != nil {
		return registry.Session{}, lifecycleError(ErrorDependency, "ensure bridge", managed.ID, fmt.Errorf("configure tmux history: %w", err))
	}
	if err := m.tmux.ConfigureManualWindowSize(ctx, managed.TmuxName); err != nil {
		return registry.Session{}, lifecycleError(ErrorDependency, "ensure bridge", managed.ID, fmt.Errorf("configure fixed tmux sizing: %w", err))
	}
	pid, err := m.bridge.Ensure(ctx, managed)
	if err != nil {
		return registry.Session{}, classifyBridgeError("ensure bridge", managed.ID, err)
	}
	// A verified legacy bridge may attach once during its readiness check before
	// migration and apply tmux update-environment. Restore the managed value only
	// after bridge reconciliation has retired every unsafe attach command.
	if err := m.tmux.SetSessionEnvironment(ctx, managed.TmuxName, "SSH_AUTH_SOCK", m.agentSocket); err != nil {
		return registry.Session{}, lifecycleError(ErrorDependency, "ensure bridge", managed.ID, fmt.Errorf("configure forwarded agent socket: %w", err))
	}
	if managed.PID == pid {
		return managed, nil
	}
	managed.PID = pid
	if err := m.store.Write(managed); err != nil {
		return registry.Session{}, lifecycleError(ErrorDependency, "ensure bridge", managed.ID, fmt.Errorf("update bridge metadata: %w", err))
	}
	return managed, nil
}

func (m *Manager) readManaged(sessionID string) (registry.Session, error) {
	managed, err := m.store.ReadCompatible(sessionID)
	if err != nil {
		return registry.Session{}, err
	}
	if managed.Name == managed.ID && registry.ValidPublicRef(managed.PublicRef) {
		return managed, nil
	}
	managed.Name = managed.ID
	if !registry.ValidPublicRef(managed.PublicRef) {
		managed.PublicRef, err = registry.NewPublicRef()
		if err != nil {
			return registry.Session{}, err
		}
	}
	if err := m.store.Write(managed); err != nil {
		return registry.Session{}, fmt.Errorf("migrate legacy managed session %q: %w", sessionID, err)
	}
	m.logger.Info("migrated legacy managed session record", "opaque_id", managed.PublicRef, "reason_code", "legacy_migration")
	return managed, nil
}

func (m *Manager) cleanupLocked(ctx context.Context, managed registry.Session) error {
	if err := m.bridge.Stop(ctx, managed); err != nil {
		return fmt.Errorf("stop terminal bridge: %w", err)
	}
	return m.removeArtifacts(managed)
}

func (m *Manager) cleanupNewSession(ctx context.Context, managed registry.Session) error {
	return errors.Join(m.bridge.Stop(ctx, managed), m.killTmuxIfPresent(ctx, managed.TmuxName), m.removeArtifacts(managed))
}

func (m *Manager) killTmuxIfPresent(ctx context.Context, name string) error {
	alive, err := m.tmux.HasSession(ctx, name)
	if err != nil || !alive {
		return err
	}
	return m.tmux.KillSession(ctx, name)
}

func (m *Manager) removeArtifacts(managed registry.Session) error {
	var cleanupErrors []error
	for _, path := range []string{
		filepath.Join(m.socketsDir, managed.ID+".sock"),
		filepath.Join(m.logsDir, managed.ID+".log"),
		filepath.Join(m.resizeDir, managed.ID+".json"),
		filepath.Join(m.resizeDir, managed.PublicRef+".json"),
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove session artifact: %w", err))
		}
	}
	if len(cleanupErrors) > 0 {
		return errors.Join(cleanupErrors...)
	}
	if err := m.store.Remove(managed.ID); err != nil {
		return err
	}
	return nil
}

func (m *Manager) lock(ctx context.Context, sessionID string) (*sessionLock, error) {
	if err := m.ensurePrivateState(); err != nil {
		return nil, err
	}
	return acquireSessionLock(ctx, m.locksDir, sessionID)
}

func (m *Manager) ensurePrivateState() error {
	if err := m.store.Ensure(); err != nil {
		return err
	}
	for _, path := range []string{m.cfg.StateDir, m.socketsDir, m.logsDir, m.locksDir, m.resizeDir, m.agentDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create private state directory: %w", err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("set private state directory permissions: %w", err)
		}
	}
	return nil
}

func withDefaults(cfg Config) Config {
	if cfg.TmuxBinary == "" {
		cfg.TmuxBinary = "tmux"
	}
	if cfg.TtydBinary == "" {
		cfg.TtydBinary = "ttyd"
	}
	if cfg.WindowSize == "" {
		cfg.WindowSize = defaultWindowSize
	}
	if cfg.Mouse == "" {
		cfg.Mouse = defaultMouse
	}
	if !cfg.WebScrollbackConfigured && cfg.WebScrollbackLines == 0 {
		cfg.WebScrollbackLines = defaultScrollback
	}
	if cfg.BridgeStartupTimeout <= 0 {
		cfg.BridgeStartupTimeout = defaultStartupTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return cfg
}

func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.StateDir) == "" {
		return errors.New("managed session state directory cannot be empty")
	}
	if strings.TrimSpace(cfg.HomeDir) == "" || !filepath.IsAbs(cfg.HomeDir) {
		return errors.New("managed session home directory must be absolute")
	}
	if cfg.Mouse != "on" && cfg.Mouse != "off" {
		return errors.New("CONTROL_AGENTS_TMUX_MOUSE must be either on or off")
	}
	if cfg.WebScrollbackLines < 0 {
		return errors.New("managed session web scrollback must be non-negative")
	}
	return nil
}

func environmentOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func classifyBridgeError(operation, sessionID string, err error) error {
	if errors.Is(err, ErrDependency) || errors.Is(err, ErrBridgeIncomplete) {
		return err
	}
	return lifecycleError(ErrorDependency, operation, sessionID, err)
}

func classifyManagedReadError(operation, sessionID string, err error) error {
	kind := ErrorDependency
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, registry.ErrInvalidSessionRecord) {
		kind = ErrorNotFound
	}
	return lifecycleError(kind, operation, sessionID, err)
}
