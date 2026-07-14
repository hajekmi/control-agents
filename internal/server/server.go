package server

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"control-agents/internal/auth"
	"control-agents/internal/compress"
	"control-agents/internal/config"
	"control-agents/internal/proxy"
	"control-agents/internal/registry"
	managedsession "control-agents/internal/session"
	"control-agents/internal/tmux"
	"control-agents/internal/version"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	cfg             config.Config
	auth            *auth.Authenticator
	registry        *registry.Store
	sessions        webSessionLifecycle
	tmux            *tmux.Client
	identity        *identityStore
	resize          *resizeStore
	snapshots       *snapshotManager
	pasteTokens     *pasteTokenManager
	historyCaptures *historyCaptureCoordinator
	activity        *outputActivityStore
	logger          *slog.Logger
	mux             *http.ServeMux
	limiter         *loginLimiter

	webCreateMu sync.Mutex
}

type sessionResponse struct {
	ID              SessionRef `json:"id"`
	Name            string     `json:"name"`
	CWD             string     `json:"cwd"`
	CreatedAt       string     `json:"createdAt"`
	ActiveWindowRef WindowRef  `json:"activeWindowRef"`
	ActivePaneRef   PaneRef    `json:"activePaneRef"`
	WindowWidth     int        `json:"windowWidth"`
	WindowHeight    int        `json:"windowHeight"`
	TmuxWindowCount int        `json:"tmuxWindowCount,omitempty"`
}

type webSessionLifecycle interface {
	List(ctx context.Context) ([]registry.Session, error)
	CreateOrSelect(ctx context.Context, name string) (registry.Session, bool, error)
	TerminateWithCleanup(ctx context.Context, sessionID string, cleanup func()) error
	TerminateChecked(ctx context.Context, sessionID, expectedPublicRef string, verify func(registry.Session) error, cleanup func()) error
	WithSession(ctx context.Context, sessionID string, use func(registry.Session) error) error
}

type createSessionRequest struct {
	Name *string `json:"name"`
}

type terminateSessionRequest struct {
	ConfirmName *string `json:"confirmName"`
	PaneRef     *string `json:"paneRef"`
}

func (r *createSessionRequest) UnmarshalJSON(data []byte) error {
	name, err := decodeSingleStringField(data, "name")
	if err != nil {
		return err
	}
	r.Name = name
	return nil
}

func (r *terminateSessionRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeStringFields(data, "confirmName", "paneRef")
	if err != nil {
		return err
	}
	r.ConfirmName = fields["confirmName"]
	r.PaneRef = fields["paneRef"]
	return nil
}

type resizeRequest struct {
	Mode     string   `json:"mode"`
	ViewerID ViewerID `json:"viewerId,omitempty"`
	PaneRef  PaneRef  `json:"paneRef"`
}

type resizeViewerRequest struct {
	ViewerID  ViewerID `json:"viewerId"`
	PaneRef   PaneRef  `json:"paneRef"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Transient bool     `json:"transient,omitempty"`
}

type resizeResponse struct {
	Mode             string                 `json:"mode"`
	SelectedViewerID ViewerID               `json:"selectedViewerId,omitempty"`
	Viewers          []resizeViewerResponse `json:"viewers"`
	Window           resizeWindowResponse   `json:"window"`
	Capabilities     []resizeCapability     `json:"capabilities"`
	Applied          *tmux.ResizeState      `json:"applied,omitempty"`
}

type resizeWindowResponse struct {
	Ref    WindowRef `json:"ref"`
	Width  int       `json:"width"`
	Height int       `json:"height"`
}

type resizeCapability struct {
	Mode      string `json:"mode"`
	Supported bool   `json:"supported"`
}

type resizeViewerResponse struct {
	ID        ViewerID  `json:"id"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"userAgent"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	LastSeen  time.Time `json:"lastSeen"`
	Active    bool      `json:"active"`
}

type pasteAPIRequest struct {
	Text    string  `json:"text"`
	PaneRef PaneRef `json:"paneRef"`
	Token   string  `json:"token"`
}

type pasteTokenAPIRequest struct {
	PaneRef           PaneRef `json:"paneRef"`
	Digest            string  `json:"digest"`
	Bytes             int     `json:"bytes"`
	Lines             int     `json:"lines"`
	ControlCharacters bool    `json:"controlCharacters"`
	TrailingNewline   bool    `json:"trailingNewline"`
}

type keyAPIRequest struct {
	Key     string  `json:"key"`
	Text    string  `json:"text"`
	PaneRef PaneRef `json:"paneRef"`
}

type controlAPIRequest struct {
	Action    string    `json:"action"`
	WindowRef WindowRef `json:"windowRef,omitempty"`
	PaneRef   PaneRef   `json:"paneRef"`
	Name      string    `json:"name,omitempty"`
}

var errInvalidResizeRequest = errors.New("invalid resize request")
var errPublicSessionNotFound = errors.New("public session not found")
var ErrStartupReconciliation = errors.New("managed session startup reconciliation failed")

const lifecycleRequestMaxBytes = 4 * 1024

const (
	maxKeyNameBytes           = 32
	maxResizeModeBytes        = 32
	maxControlActionBytes     = 32
	maxControlWindowNameBytes = 128
	maxViewerUserAgentBytes   = 512
	maxViewerIPBytes          = 128
	maxOpaqueReferenceBytes   = 128
)

func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	store := registry.NewStore(cfg.StateDir)
	store.SetLogger(logger.With("component", "registry"))
	if err := store.Ensure(); err != nil {
		return nil, err
	}
	homeDir := cfg.HomeDir
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine service user home directory: %w", err)
		}
	}
	lifecycleConfig, err := managedsession.ConfigFromEnvironment(cfg.StateDir, homeDir, logger.With("component", "session"))
	if err != nil {
		return nil, err
	}
	store.SetTmuxBinary(lifecycleConfig.TmuxBinary)
	lifecycle, err := managedsession.New(lifecycleConfig)
	if err != nil {
		return nil, err
	}
	if _, err := lifecycle.Reconcile(context.Background()); err != nil {
		return nil, ErrStartupReconciliation
	}

	application, err := newServerWithLifecycle(cfg, logger, store, lifecycle)
	if err != nil {
		return nil, err
	}
	application.tmux = tmux.NewClientWithBinary(lifecycleConfig.TmuxBinary)
	return application, nil
}

func StartupFailureReason(err error) string {
	if errors.Is(err, ErrStartupReconciliation) {
		return "reconciliation_failure"
	}
	return "startup_failure"
}

func newServerWithLifecycle(cfg config.Config, logger *slog.Logger, store *registry.Store, lifecycle webSessionLifecycle) (*Server, error) {
	authSecretFile := cfg.AuthSecretFile
	if authSecretFile == "" {
		authSecretFile = filepath.Join(cfg.StateDir, "auth", "session.key")
	}
	authSecret, err := auth.LoadOrCreateSecret(authSecretFile)
	if err != nil {
		return nil, err
	}
	authenticator, err := auth.NewWithSecret(cfg.Password, time.Duration(cfg.CookieTTL)*time.Second, cfg.CookieSecure, authSecret)
	if err != nil {
		return nil, err
	}

	server := &Server{
		cfg:      cfg,
		auth:     authenticator,
		registry: store,
		sessions: lifecycle,
		tmux:     tmux.NewClient(),
		identity: newIdentityStore(),
		resize:   newResizeStore(filepath.Join(cfg.StateDir, "resize"), 60*time.Second),
		snapshots: newSnapshotManager(snapshotManagerConfig{
			ProcessMaxBytes: max(defaultSnapshotProcessMaxBytes, cfg.SnapshotMaxBytes),
		}),
		activity:        newOutputActivityStore(),
		pasteTokens:     newPasteTokenManager(0, 0),
		historyCaptures: newHistoryCaptureCoordinator(),
		logger:          logger,
		mux:             http.NewServeMux(),
		limiter:         newLoginLimiter(loginAttemptLimit, loginAttemptWindow),
	}
	server.routes()
	return server, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recover() != nil {
			s.logger.Error("request panic recovered", "reason_code", "panic")
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	}()
	applySecurityHeaders(w, r)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	if requiresSameOrigin(r) && !sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if requiresCSRF(r) && s.auth.Authenticated(r) && !s.auth.VerifyCSRF(r, r.Header.Get(csrfHeader)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	compress.Middleware(s.mux).ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/login", s.handleLogin)
	s.mux.HandleFunc("/logout", s.handleLogout)
	s.mux.HandleFunc("/app.js", s.handlePublicStaticAsset)
	s.mux.HandleFunc("/login.js", s.handlePublicStaticAsset)
	s.mux.HandleFunc("/terminal-observer.js", s.handlePublicStaticAsset)
	s.mux.HandleFunc("/styles.css", s.handlePublicStaticAsset)
	s.mux.Handle("/api/version", s.auth.RequireAPI(http.HandlerFunc(s.handleVersion)))
	s.mux.Handle("/api/csrf", s.auth.RequireAPI(http.HandlerFunc(s.handleCSRF)))
	s.mux.Handle("/api/v1/", s.auth.RequireAPI(http.HandlerFunc(s.handleHistoryAPI)))
	s.mux.Handle("/api/sessions", s.auth.RequireAPI(http.HandlerFunc(s.handleSessions)))
	s.mux.Handle("/api/sessions/", s.auth.RequireAPI(http.HandlerFunc(s.handleSessionAPI)))
	s.mux.Handle("/terminal/", s.auth.RequireAPI(proxy.NewWithOutputObserver(s.registry, func(ref string, bytes int) {
		s.activity.Record(SessionRef(ref), bytes)
	})))
	s.mux.Handle("/", s.auth.Require(http.HandlerFunc(s.handleStatic)))
}

func (s *Server) handleCSRF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token, ok := s.auth.CSRFToken(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]string{"token": token})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.auth.Authenticated(r) {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		http.ServeFileFS(w, r, staticFS, "static/login.html")
	case http.MethodPost:
		client := clientIP(r)
		if ok, retryAfter := s.limiter.Allow(client); !ok {
			seconds := int(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			http.Error(w, "too many login attempts", http.StatusTooManyRequests)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if !s.auth.Login(w, r.FormValue("password")) {
			s.limiter.RecordFailure(client)
			http.Redirect(w, r, "/login?error=1", http.StatusFound)
			return
		}
		s.limiter.Reset(client)
		http.Redirect(w, r, "/", http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePublicStaticAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/app.js":
		http.ServeFileFS(w, r, staticFS, "static/app.js")
	case "/login.js":
		http.ServeFileFS(w, r, staticFS, "static/login.js")
	case "/terminal-observer.js":
		http.ServeFileFS(w, r, staticFS, "static/terminal-observer.js")
	case "/styles.css":
		http.ServeFileFS(w, r, staticFS, "static/styles.css")
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := requireEmptyMutationBody(w, r); err != nil {
		http.Error(w, "invalid logout request", http.StatusBadRequest)
		return
	}
	if scope, ok := s.auth.LoginScope(r); ok {
		s.snapshots.DeleteUser(scope)
		s.pasteTokens.DeleteUser(scope)
	}
	s.auth.Logout(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, version.Current())
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSessions(w, r)
	case http.MethodPost:
		s.handleCreateSession(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.sessions.List(r.Context())
	if err != nil {
		s.logger.Error("list sessions failed", "reason_code", "dependency_failure")
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}
	payload := make([]sessionResponse, 0, len(sessions))
	for _, managed := range sessions {
		response, err := s.publicSession(r.Context(), managed)
		if err != nil {
			s.logger.Warn("session topology unavailable", "opaque_id", managed.PublicRef, "reason_code", "dependency_failure")
			continue
		}
		payload = append(payload, response)
	}
	writeJSON(w, map[string]any{"sessions": payload})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var request createSessionRequest
	if err := decodeLifecycleRequest(w, r, &request); err != nil || request.Name == nil || !registry.ValidID(*request.Name) {
		http.Error(w, "invalid session create request", http.StatusBadRequest)
		return
	}
	name := *request.Name

	// The web limit is intentionally a server policy rather than a lifecycle
	// restriction on the local CLI. Serialize the count and create decision so
	// concurrent web requests cannot oversubscribe it.
	s.webCreateMu.Lock()
	defer s.webCreateMu.Unlock()

	sessions, err := s.sessions.List(r.Context())
	if err != nil {
		s.logger.Error("web session create preflight failed", "reason_code", "dependency_failure")
		http.Error(w, "session lifecycle unavailable", http.StatusServiceUnavailable)
		return
	}
	existing := false
	for _, managed := range sessions {
		if managed.ID == name {
			existing = true
			break
		}
	}
	if !existing && len(sessions) >= s.cfg.MaxSessions {
		s.logger.Info("web session create rejected", "reason_code", "session_limit")
		http.Error(w, "managed session limit reached", http.StatusConflict)
		return
	}

	managed, created, err := s.sessions.CreateOrSelect(r.Context(), name)
	if err != nil {
		s.logger.Warn("web session create failed", "reason_code", "lifecycle_failure")
		http.Error(w, lifecycleErrorMessage(err), lifecycleHTTPStatus(err))
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	createReason := "selected"
	if created {
		createReason = "created"
	}
	s.logger.Info("web session create completed", "opaque_id", managed.PublicRef, "reason_code", createReason)
	response, err := s.publicSession(r.Context(), managed)
	if err != nil {
		s.logger.Warn("created session topology unavailable", "opaque_id", managed.PublicRef, "reason_code", "dependency_failure")
		http.Error(w, "session topology unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSONStatus(w, status, map[string]any{
		"created": created,
		"session": response,
	})
}

func (s *Server) handleSessionAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		ref, ok := parseSessionDeletePath(r.URL.Path)
		if !ok {
			http.Error(w, "invalid managed session id", http.StatusBadRequest)
			return
		}
		s.handleTerminateSession(w, r, ref)
		return
	}
	ref, suffix, ok := parseSessionAPIPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	err := s.withPublicSession(r.Context(), ref, func(session registry.Session) error {
		switch suffix {
		case "keys":
			s.handleKeysAPI(w, r, ref, session)
		case "paste":
			s.handlePasteAPI(w, r, ref, session)
		case "paste/token":
			s.handlePasteTokenAPI(w, r, ref, session)
		case "resize":
			s.handleResizeAPI(w, r, ref, session)
		case "resize/viewer":
			s.handleResizeViewerAPI(w, r, ref, session)
		case "tmux-control":
			s.handleTmuxControlAPI(w, r, ref, session)
		default:
			http.NotFound(w, r)
		}
		return nil
	})
	if err != nil {
		s.logger.Warn("managed session request resolution failed", "opaque_id", ref, "reason_code", "resolution_failure")
		if errors.Is(err, errPublicSessionNotFound) {
			http.Error(w, "managed session not found", http.StatusNotFound)
			return
		}
		http.Error(w, lifecycleErrorMessage(err), lifecycleHTTPStatus(err))
	}
}

func (s *Server) handleTerminateSession(w http.ResponseWriter, r *http.Request, ref SessionRef) {
	var request terminateSessionRequest
	if err := decodeLifecycleRequest(w, r, &request); err != nil || request.ConfirmName == nil || request.PaneRef == nil ||
		len(*request.ConfirmName) > 64 || len(*request.PaneRef) > maxOpaqueReferenceBytes {
		http.Error(w, "invalid session termination confirmation", http.StatusBadRequest)
		return
	}
	managed, err := s.findPublicSession(r.Context(), ref)
	if err != nil {
		http.Error(w, "managed session not found", http.StatusNotFound)
		return
	}
	if *request.ConfirmName != managed.Name {
		http.Error(w, "invalid session termination confirmation", http.StatusBadRequest)
		return
	}
	verify := func(current registry.Session) error {
		_, err := s.identity.resolvePane(r.Context(), s.tmux, current, PaneRef(*request.PaneRef), true)
		return err
	}
	err = s.sessions.TerminateChecked(r.Context(), managed.ID, string(ref), verify, func() {
		s.resize.ForgetSession(string(ref))
		s.snapshots.DeleteSession(ref)
		s.pasteTokens.DeleteSession(ref)
		s.activity.Forget(ref)
		s.identity.forget(ref)
	})
	if err != nil {
		if errors.Is(err, errStaleTerminalIdentity) {
			http.Error(w, "stale terminal identity", http.StatusConflict)
			return
		}
		s.logger.Warn("web session termination failed", "opaque_id", ref, "reason_code", "lifecycle_failure")
		http.Error(w, lifecycleErrorMessage(err), lifecycleHTTPStatus(err))
		return
	}
	s.logger.Info("web session termination completed", "opaque_id", ref, "reason_code", "terminated")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePasteAPI(w http.ResponseWriter, r *http.Request, ref SessionRef, session registry.Session) {
	started := time.Now()
	auditStatus := http.StatusOK
	auditBytes := 0
	defer func() { s.auditTerminalAction(started, ref, auditStatus, auditBytes, auditStatusReason(auditStatus)) }()
	if r.Method != http.MethodPost {
		auditStatus = http.StatusMethodNotAllowed
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, authenticated := s.auth.LoginScope(r)
	if !authenticated {
		auditStatus = http.StatusUnauthorized
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var request pasteAPIRequest
	r.Body = http.MaxBytesReader(w, r.Body, int64(tmux.MaxPasteBytes*8+1024))
	encoded, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(encoded) {
		auditStatus = http.StatusBadRequest
		http.Error(w, "invalid paste request", http.StatusBadRequest)
		return
	}
	if err := validateObjectFields(encoded, &request); err != nil {
		auditStatus = http.StatusBadRequest
		http.Error(w, "invalid paste request", http.StatusBadRequest)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		!utf8.ValidString(request.Text) || request.Text == "" || len(request.Text) > tmux.MaxPasteBytes || strings.ContainsRune(request.Text, '\x00') ||
		len(request.PaneRef) > maxOpaqueReferenceBytes || !pasteTokenIDPattern.MatchString(request.Token) {
		auditStatus = http.StatusBadRequest
		http.Error(w, "invalid paste request", http.StatusBadRequest)
		return
	}
	pane, ok := s.mutationPane(w, r, session, request.PaneRef)
	if !ok {
		auditStatus = http.StatusConflict
		return
	}
	if err := s.pasteTokens.Consume(request.Token, pasteTokenBinding{
		User: user, SessionRef: ref, PaneRef: request.PaneRef, Generation: pane.generation, Action: pasteTextAction(request.Text),
	}); err != nil {
		auditStatus = http.StatusConflict
		http.Error(w, "invalid or expired paste token", http.StatusConflict)
		return
	}
	if err := s.tmux.Paste(r.Context(), pane.rawID, tmux.PasteRequest{Text: request.Text}); err != nil {
		if errors.Is(err, tmux.ErrInvalidPaste) {
			auditStatus = http.StatusBadRequest
			http.Error(w, "invalid paste", http.StatusBadRequest)
			return
		}
		auditStatus = http.StatusBadGateway
		http.Error(w, "failed to paste terminal text", http.StatusBadGateway)
		return
	}
	auditBytes = len([]byte(request.Text))
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handlePasteTokenAPI(w http.ResponseWriter, r *http.Request, ref SessionRef, session registry.Session) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, ok := s.auth.LoginScope(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var request pasteTokenAPIRequest
	if err := decodeLifecycleRequest(w, r, &request); err != nil || len(request.PaneRef) > maxOpaqueReferenceBytes ||
		len(request.Digest) > 43 {
		http.Error(w, "invalid paste token request", http.StatusBadRequest)
		return
	}
	pane, ok := s.mutationPane(w, r, session, request.PaneRef)
	if !ok {
		return
	}
	token, expires, err := s.pasteTokens.Create(pasteTokenBinding{
		User:       user,
		SessionRef: ref,
		PaneRef:    request.PaneRef,
		Generation: pane.generation,
		Action: pasteAction{
			Digest: request.Digest, Bytes: request.Bytes, Lines: request.Lines,
			ControlCharacters: request.ControlCharacters, TrailingNewline: request.TrailingNewline,
		},
	})
	if err != nil {
		http.Error(w, "invalid paste token request", http.StatusBadRequest)
		return
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{"token": token, "expiresAt": expires.UTC().Format(time.RFC3339Nano)})
}

func (s *Server) handleKeysAPI(w http.ResponseWriter, r *http.Request, ref SessionRef, session registry.Session) {
	started := time.Now()
	auditStatus := http.StatusOK
	defer func() { s.auditTerminalAction(started, ref, auditStatus, 0, auditStatusReason(auditStatus)) }()
	if r.Method != http.MethodPost {
		auditStatus = http.StatusMethodNotAllowed
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request keyAPIRequest
	if err := decodeLifecycleRequest(w, r, &request); err != nil ||
		len(request.Key) > maxKeyNameBytes || len(request.Text) > utf8.UTFMax || len(request.PaneRef) > maxOpaqueReferenceBytes {
		auditStatus = http.StatusBadRequest
		http.Error(w, "invalid key request", http.StatusBadRequest)
		return
	}
	pane, ok := s.mutationPane(w, r, session, request.PaneRef)
	if !ok {
		auditStatus = http.StatusConflict
		return
	}
	if (request.Key == "") == (request.Text == "") {
		auditStatus = http.StatusBadRequest
		http.Error(w, "invalid key request", http.StatusBadRequest)
		return
	}
	var err error
	if request.Text != "" {
		err = s.tmux.SendText(r.Context(), pane.rawID, request.Text)
	} else {
		err = s.tmux.SendKey(r.Context(), pane.rawID, tmux.KeyRequest{Key: request.Key})
	}
	if err != nil {
		if errors.Is(err, tmux.ErrUnsupportedKey) || errors.Is(err, tmux.ErrInvalidInput) {
			auditStatus = http.StatusBadRequest
			http.Error(w, "unsupported key", http.StatusBadRequest)
			return
		}
		auditStatus = http.StatusBadGateway
		http.Error(w, "failed to send key", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleResizeAPI(w http.ResponseWriter, r *http.Request, ref SessionRef, session registry.Session) {
	started := time.Now()
	auditStatus := http.StatusOK
	defer func() { s.auditTerminalAction(started, ref, auditStatus, 0, auditStatusReason(auditStatus)) }()
	switch r.Method {
	case http.MethodGet:
		response, err := s.resizeResponse(r, ref, session, nil)
		if err != nil {
			auditStatus = http.StatusInternalServerError
			http.Error(w, "failed to read resize state", http.StatusInternalServerError)
			return
		}
		writeJSON(w, response)
	case http.MethodPost:
		var request resizeRequest
		if err := decodeLifecycleRequest(w, r, &request); err != nil || len(request.Mode) > maxResizeModeBytes ||
			len(request.PaneRef) > maxOpaqueReferenceBytes ||
			(request.ViewerID != "" && !viewerIDPattern.MatchString(string(request.ViewerID))) {
			auditStatus = http.StatusBadRequest
			http.Error(w, "invalid resize request", http.StatusBadRequest)
			return
		}
		pane, ok := s.mutationPane(w, r, session, request.PaneRef)
		if !ok {
			auditStatus = http.StatusConflict
			return
		}
		applied, err := s.applyResizeRequest(r, ref, session, pane, request)
		if err != nil {
			if errors.Is(err, errInvalidResizeRequest) {
				auditStatus = http.StatusBadRequest
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			auditStatus = http.StatusBadGateway
			http.Error(w, "failed to resize terminal", http.StatusBadGateway)
			return
		}
		response, err := s.resizeResponse(r, ref, session, applied)
		if err != nil {
			auditStatus = http.StatusInternalServerError
			http.Error(w, "failed to read resize state", http.StatusInternalServerError)
			return
		}
		writeJSON(w, response)
	default:
		auditStatus = http.StatusMethodNotAllowed
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleResizeViewerAPI(w http.ResponseWriter, r *http.Request, ref SessionRef, session registry.Session) {
	started := time.Now()
	auditStatus := http.StatusOK
	defer func() { s.auditTerminalAction(started, ref, auditStatus, 0, auditStatusReason(auditStatus)) }()
	if r.Method != http.MethodPost {
		auditStatus = http.StatusMethodNotAllowed
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request resizeViewerRequest
	if err := decodeLifecycleRequest(w, r, &request); err != nil || len(request.PaneRef) > maxOpaqueReferenceBytes ||
		!viewerIDPattern.MatchString(string(request.ViewerID)) {
		auditStatus = http.StatusBadRequest
		http.Error(w, "invalid resize viewer request", http.StatusBadRequest)
		return
	}
	if _, ok := s.mutationPane(w, r, session, request.PaneRef); !ok {
		auditStatus = http.StatusConflict
		return
	}
	_, err := s.resize.RecordViewer(string(ref), resizeViewer{
		ID:        request.ViewerID,
		IP:        boundedUTF8(clientIP(r), maxViewerIPBytes),
		UserAgent: boundedUTF8(r.UserAgent(), maxViewerUserAgentBytes),
		Width:     request.Width,
		Height:    request.Height,
	})
	if err != nil {
		if errors.Is(err, errResizeViewerCapacity) {
			auditStatus = http.StatusTooManyRequests
			w.Header().Set("Retry-After", "10")
			http.Error(w, "resize viewer capacity reached", auditStatus)
			return
		}
		auditStatus = http.StatusBadRequest
		http.Error(w, "invalid resize viewer request", http.StatusBadRequest)
		return
	}

	response, err := s.resizeResponse(r, ref, session, nil)
	if err != nil {
		auditStatus = http.StatusInternalServerError
		http.Error(w, "failed to read resize state", http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func (s *Server) applyResizeRequest(r *http.Request, ref SessionRef, session registry.Session, pane paneBinding, request resizeRequest) (*tmux.ResizeState, error) {
	mode := normalizeResizeMode(request.Mode)
	if err := validateResizeMode(mode); err != nil {
		return nil, fmtInvalidResizeRequest(err)
	}

	settings := resizeSettings{Mode: resizeModeFixed}
	switch mode {
	case resizeModeFixed:
		state, err := s.tmux.ResizeFixed(r.Context(), pane.windowID)
		if err != nil {
			return nil, err
		}
		state.Mode = resizeModeFixed
		if err := s.resize.Save(string(ref), settings); err != nil {
			return nil, err
		}
		return &state, nil
	case resizeModeFitOnce:
		viewerID := ViewerID(strings.TrimSpace(string(request.ViewerID)))
		viewer, ok := s.resize.Viewer(string(ref), viewerID)
		if viewerID == "" || !ok {
			return nil, fmtInvalidResizeRequest(errors.New("selected web viewer is not active"))
		}
		state, err := s.tmux.ResizeManual(r.Context(), pane.windowID, viewer.Width, viewer.Height)
		if err != nil {
			return nil, err
		}
		state.Mode = resizeModeFitOnce
		settings.SelectedViewerID = viewerID
		if err := s.resize.Save(string(ref), settings); err != nil {
			return nil, err
		}
		return &state, nil
	case resizeModeFollowDevice:
		return nil, fmtInvalidResizeRequest(errors.New("follow this device is not available yet"))
	}
	return nil, fmtInvalidResizeRequest(errors.New("unsupported resize mode"))
}

func (s *Server) resizeResponse(r *http.Request, ref SessionRef, session registry.Session, applied *tmux.ResizeState) (resizeResponse, error) {
	settings, err := s.resize.Load(string(ref))
	if err != nil {
		return resizeResponse{}, err
	}
	viewers := s.resize.Viewers(string(ref))
	sort.Slice(viewers, func(i, j int) bool {
		if !viewers[i].LastSeen.Equal(viewers[j].LastSeen) {
			return viewers[i].LastSeen.After(viewers[j].LastSeen)
		}
		return viewers[i].ID < viewers[j].ID
	})

	response := resizeResponse{
		Mode:             settings.Mode,
		SelectedViewerID: settings.SelectedViewerID,
		Viewers:          make([]resizeViewerResponse, 0, len(viewers)),
		Applied:          applied,
		Capabilities: []resizeCapability{
			{Mode: resizeModeFixed, Supported: true},
			{Mode: resizeModeFitOnce, Supported: true},
			{Mode: resizeModeFollowDevice, Supported: false},
		},
	}
	for _, viewer := range viewers {
		response.Viewers = append(response.Viewers, resizeViewerResponse{
			ID:        viewer.ID,
			IP:        viewer.IP,
			UserAgent: viewer.UserAgent,
			Width:     viewer.Width,
			Height:    viewer.Height,
			LastSeen:  viewer.LastSeen,
			Active:    true,
		})
	}
	topology, err := s.identity.refresh(r.Context(), s.tmux, session)
	if err != nil {
		return resizeResponse{}, err
	}
	response.Window = resizeWindowResponse{
		Ref: topology.ActiveWindowRef, Width: topology.WindowWidth, Height: topology.WindowHeight,
	}
	return response, nil
}

func fmtInvalidResizeRequest(err error) error {
	return fmt.Errorf("%w: %v", errInvalidResizeRequest, err)
}

func (s *Server) handleTmuxControlAPI(w http.ResponseWriter, r *http.Request, ref SessionRef, session registry.Session) {
	started := time.Now()
	auditStatus := http.StatusOK
	defer func() { s.auditTerminalAction(started, ref, auditStatus, 0, auditStatusReason(auditStatus)) }()
	switch r.Method {
	case http.MethodGet:
		topology, err := s.identity.refresh(r.Context(), s.tmux, session)
		if err != nil {
			auditStatus = http.StatusBadGateway
			http.Error(w, "failed to list tmux windows", http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"windows": topology.Windows, "activePaneRef": topology.ActivePaneRef})
	case http.MethodPost:
		var request controlAPIRequest
		if err := decodeLifecycleRequest(w, r, &request); err != nil ||
			len(request.Action) > maxControlActionBytes || len(request.Name) > maxControlWindowNameBytes ||
			len(request.PaneRef) > maxOpaqueReferenceBytes || len(request.WindowRef) > maxOpaqueReferenceBytes {
			auditStatus = http.StatusBadRequest
			http.Error(w, "invalid tmux control request", http.StatusBadRequest)
			return
		}
		pane, ok := s.mutationPane(w, r, session, request.PaneRef)
		if !ok {
			auditStatus = http.StatusConflict
			return
		}
		windowTarget := pane.windowID
		if request.Action == "select-window" {
			window, err := s.identity.resolveWindow(session, request.WindowRef)
			if err != nil {
				auditStatus = http.StatusConflict
				http.Error(w, "stale terminal identity", http.StatusConflict)
				return
			}
			windowTarget = window.rawID
		}
		err := s.tmux.Control(r.Context(), pane.rawID, windowTarget, tmux.ControlRequest{Action: request.Action, Name: request.Name})
		if err != nil {
			if errors.Is(err, tmux.ErrUnsupportedControlAction) || errors.Is(err, tmux.ErrInvalidControlRequest) {
				auditStatus = http.StatusBadRequest
				http.Error(w, "unsupported tmux control action", http.StatusBadRequest)
				return
			}
			auditStatus = http.StatusBadGateway
			http.Error(w, "failed to run tmux control action", http.StatusBadGateway)
			return
		}
		topology, err := s.identity.refresh(r.Context(), s.tmux, session)
		if err != nil {
			auditStatus = http.StatusBadGateway
			http.Error(w, "failed to list tmux windows", http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"windows": topology.Windows, "activePaneRef": topology.ActivePaneRef})
	default:
		auditStatus = http.StatusMethodNotAllowed
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/terminal/") {
		http.NotFound(w, r)
		return
	}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		http.Error(w, "static assets unavailable", http.StatusInternalServerError)
		return
	}

	if r.URL.Path == "/" {
		http.ServeFileFS(w, r, staticFS, "static/index.html")
		return
	}
	http.FileServer(http.FS(sub)).ServeHTTP(w, r)
}

func parseSessionAPIPath(path string) (SessionRef, string, bool) {
	trimmed := strings.TrimPrefix(path, "/api/sessions/")
	if trimmed == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || !registry.ValidPublicRef(parts[0]) {
		return "", "", false
	}
	return SessionRef(parts[0]), strings.Join(parts[1:], "/"), true
}

func parseSessionDeletePath(path string) (SessionRef, bool) {
	trimmed := strings.TrimPrefix(path, "/api/sessions/")
	if trimmed == path || strings.Contains(trimmed, "/") || !registry.ValidPublicRef(trimmed) {
		return "", false
	}
	return SessionRef(trimmed), true
}

func decodeLifecycleRequest(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, lifecycleRequestMaxBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if !utf8.Valid(data) {
		return errors.New("request body must be valid UTF-8")
	}
	if err := validateObjectFields(data, target); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func validateObjectFields(data []byte, target any) error {
	typeOfTarget := reflect.TypeOf(target)
	if typeOfTarget == nil || typeOfTarget.Kind() != reflect.Pointer || typeOfTarget.Elem().Kind() != reflect.Struct {
		return errors.New("request target must be a struct pointer")
	}
	allowed := make(map[string]struct{})
	structType := typeOfTarget.Elem()
	for index := 0; index < structType.NumField(); index++ {
		field := structType.Field(index)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		allowed[name] = struct{}{}
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return errors.New("request body must be a JSON object")
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		field, ok := token.(string)
		if !ok {
			return errors.New("request object field must be a string")
		}
		if _, exists := allowed[field]; !exists {
			return fmt.Errorf("unknown field %q", field)
		}
		if _, exists := seen[field]; exists {
			return fmt.Errorf("duplicate field %q", field)
		}
		seen[field] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func requireEmptyMutationBody(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(data) != 0 {
		return errors.New("request body must be empty")
	}
	return nil
}

func boundedUTF8(value string, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	var bounded strings.Builder
	bounded.Grow(min(len(value), maximum))
	for len(value) > 0 {
		runeValue, size := utf8.DecodeRuneInString(value)
		if runeValue == utf8.RuneError && size == 1 {
			runeValue = '\uFFFD'
		}
		runeBytes := utf8.RuneLen(runeValue)
		if bounded.Len()+runeBytes > maximum {
			break
		}
		bounded.WriteRune(runeValue)
		value = value[size:]
	}
	return bounded.String()
}

func decodeSingleStringField(data []byte, field string) (*string, error) {
	fields, err := decodeStringFields(data, field)
	if err != nil {
		return nil, err
	}
	return fields[field], nil
}

func decodeStringFields(data []byte, expected ...string) (map[string]*string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, errors.New("request body must be a JSON object")
	}
	allowed := make(map[string]bool, len(expected))
	values := make(map[string]*string, len(expected))
	for _, field := range expected {
		allowed[field] = true
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok || !allowed[key] {
			return nil, fmt.Errorf("unknown field %q", key)
		}
		if _, seen := values[key]; seen {
			return nil, fmt.Errorf("duplicate field %q", key)
		}
		var value *string
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	for _, field := range expected {
		if values[field] == nil {
			return nil, fmt.Errorf("field %q must be one string", field)
		}
	}
	return values, nil
}

func (s *Server) publicSession(ctx context.Context, managed registry.Session) (sessionResponse, error) {
	topology, err := s.identity.refresh(ctx, s.tmux, managed)
	if err != nil {
		return sessionResponse{}, err
	}
	response := sessionResponse{
		ID:              SessionRef(managed.PublicRef),
		Name:            managed.Name,
		CWD:             managed.CWD,
		CreatedAt:       managed.CreatedAt,
		ActiveWindowRef: topology.ActiveWindowRef,
		ActivePaneRef:   topology.ActivePaneRef,
		WindowWidth:     topology.WindowWidth,
		WindowHeight:    topology.WindowHeight,
	}
	if len(topology.Windows) > 1 {
		response.TmuxWindowCount = len(topology.Windows)
	}
	return response, nil
}

func (s *Server) findPublicSession(ctx context.Context, ref SessionRef) (registry.Session, error) {
	if !registry.ValidPublicRef(string(ref)) {
		return registry.Session{}, errPublicSessionNotFound
	}
	sessions, err := s.sessions.List(ctx)
	if err != nil {
		return registry.Session{}, err
	}
	for _, managed := range sessions {
		if managed.PublicRef == string(ref) {
			return managed, nil
		}
	}
	return registry.Session{}, errPublicSessionNotFound
}

func (s *Server) withPublicSession(ctx context.Context, ref SessionRef, use func(registry.Session) error) error {
	managed, err := s.findPublicSession(ctx, ref)
	if err != nil {
		return err
	}
	return s.sessions.WithSession(ctx, managed.ID, func(current registry.Session) error {
		if current.PublicRef != string(ref) {
			return errPublicSessionNotFound
		}
		return use(current)
	})
}

func (s *Server) currentPane(w http.ResponseWriter, r *http.Request, managed registry.Session) (paneBinding, int, bool) {
	topology, err := s.identity.refresh(r.Context(), s.tmux, managed)
	if err != nil {
		http.Error(w, "failed to resolve terminal identity", http.StatusBadGateway)
		return paneBinding{}, http.StatusBadGateway, false
	}
	pane, ok := s.mutationPane(w, r, managed, topology.ActivePaneRef)
	if !ok {
		return paneBinding{}, http.StatusConflict, false
	}
	return pane, 0, true
}

func (s *Server) mutationPane(w http.ResponseWriter, r *http.Request, managed registry.Session, ref PaneRef) (paneBinding, bool) {
	pane, err := s.identity.resolvePane(r.Context(), s.tmux, managed, ref, true)
	if err != nil {
		http.Error(w, "stale terminal identity", http.StatusConflict)
		return paneBinding{}, false
	}
	return pane, true
}

func lifecycleHTTPStatus(err error) int {
	switch {
	case errors.Is(err, managedsession.ErrInvalidName):
		return http.StatusBadRequest
	case errors.Is(err, managedsession.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, managedsession.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, managedsession.ErrDependency), errors.Is(err, managedsession.ErrBridgeIncomplete):
		return http.StatusServiceUnavailable
	default:
		return http.StatusServiceUnavailable
	}
}

func lifecycleErrorMessage(err error) string {
	switch lifecycleHTTPStatus(err) {
	case http.StatusBadRequest:
		return "invalid managed session name"
	case http.StatusConflict:
		return "managed session conflict"
	case http.StatusNotFound:
		return "managed session not found"
	default:
		return "session lifecycle unavailable"
	}
}

func writeJSON(w http.ResponseWriter, payload any) {
	writeJSONStatus(w, http.StatusOK, payload)
}

func writeJSONStatus(w http.ResponseWriter, status int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
