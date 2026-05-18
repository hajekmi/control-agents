package server

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"control-agents/internal/auth"
	"control-agents/internal/compress"
	"control-agents/internal/config"
	"control-agents/internal/proxy"
	"control-agents/internal/registry"
	"control-agents/internal/tmux"
	"control-agents/internal/version"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	cfg      config.Config
	auth     *auth.Authenticator
	registry *registry.Store
	tmux     *tmux.Client
	resize   *resizeStore
	logger   *slog.Logger
	mux      *http.ServeMux
	limiter  *loginLimiter
}

type sessionResponse struct {
	registry.Session
	TmuxWindowCount int `json:"tmuxWindowCount,omitempty"`
}

type resizeRequest struct {
	Mode     string `json:"mode"`
	ViewerID string `json:"viewerId,omitempty"`
}

type resizeViewerRequest struct {
	ViewerID  string `json:"viewerId"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Transient bool   `json:"transient,omitempty"`
}

type resizeResponse struct {
	Mode             string                 `json:"mode"`
	SelectedViewerID string                 `json:"selectedViewerId,omitempty"`
	Viewers          []resizeViewerResponse `json:"viewers"`
	PrimaryClient    *tmux.ResizeClient     `json:"primaryClient,omitempty"`
	Applied          *tmux.ResizeState      `json:"applied,omitempty"`
}

type resizeViewerResponse struct {
	ID        string    `json:"id"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"userAgent"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	LastSeen  time.Time `json:"lastSeen"`
	Active    bool      `json:"active"`
}

var errInvalidResizeRequest = errors.New("invalid resize request")

func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	store := registry.NewStore(cfg.StateDir)
	store.SetLogger(logger.With("component", "registry"))
	if err := store.Ensure(); err != nil {
		return nil, err
	}

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
		tmux:     tmux.NewClient(),
		resize:   newResizeStore(filepath.Join(cfg.StateDir, "resize"), 60*time.Second),
		logger:   logger,
		mux:      http.NewServeMux(),
		limiter:  newLoginLimiter(loginAttemptLimit, loginAttemptWindow),
	}
	server.routes()
	return server, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	applySecurityHeaders(w, r)
	if requiresSameOrigin(r) && !sameOrigin(r) {
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
	s.mux.HandleFunc("/styles.css", s.handlePublicStaticAsset)
	s.mux.Handle("/api/version", s.auth.RequireAPI(http.HandlerFunc(s.handleVersion)))
	s.mux.Handle("/api/sessions", s.auth.RequireAPI(http.HandlerFunc(s.handleSessions)))
	s.mux.Handle("/api/sessions/", s.auth.RequireAPI(http.HandlerFunc(s.handleSessionAPI)))
	s.mux.Handle("/terminal/", s.auth.RequireAPI(proxy.New(s.registry)))
	s.mux.Handle("/", s.auth.Require(http.HandlerFunc(s.handleStatic)))
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
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessions, err := s.registry.List()
	if err != nil {
		s.logger.Error("list sessions failed", "error", err)
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}
	payload := make([]sessionResponse, 0, len(sessions))
	for _, session := range sessions {
		next := sessionResponse{Session: session}
		windows, err := s.tmux.Windows(r.Context(), session.TmuxName)
		if err == nil && len(windows) > 1 {
			next.TmuxWindowCount = len(windows)
		}
		payload = append(payload, next)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"sessions": payload})
}

func (s *Server) handleSessionAPI(w http.ResponseWriter, r *http.Request) {
	id, suffix, ok := parseSessionAPIPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	session, err := s.registry.Read(id)
	if err != nil || !s.registry.Alive(session) {
		http.NotFound(w, r)
		return
	}

	switch suffix {
	case "scroll":
		s.handleScrollAPI(w, r, id, session)
	case "keys":
		s.handleKeysAPI(w, r, id, session)
	case "capture":
		s.handleCaptureAPI(w, r, id, session)
	case "paste":
		s.handlePasteAPI(w, r, id, session)
	case "resize":
		s.handleResizeAPI(w, r, id, session)
	case "resize/viewer":
		s.handleResizeViewerAPI(w, r, id, session)
	case "tmux-control":
		s.handleTmuxControlAPI(w, r, id, session)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleScrollAPI(w http.ResponseWriter, r *http.Request, id string, session registry.Session) {
	switch r.Method {
	case http.MethodGet:
		state, err := s.tmux.StatusForProcess(r.Context(), session.TmuxName, session.PID)
		if err != nil {
			s.logger.Error("tmux scroll status failed", "session", id, "error", err)
			http.Error(w, "failed to read scroll state", http.StatusBadGateway)
			return
		}
		writeJSON(w, state)
	case http.MethodPost:
		var request tmux.ScrollRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid scroll request", http.StatusBadRequest)
			return
		}
		state, err := s.tmux.ScrollForProcess(r.Context(), session.TmuxName, session.PID, request)
		if err != nil {
			s.logger.Error("tmux scroll command failed", "session", id, "action", request.Action, "error", err)
			http.Error(w, "failed to scroll terminal", http.StatusBadGateway)
			return
		}
		writeJSON(w, state)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCaptureAPI(w http.ResponseWriter, r *http.Request, id string, session registry.Session) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	capture, err := s.tmux.Capture(r.Context(), session.TmuxName)
	if err != nil {
		s.logger.Error("tmux capture failed", "session", id, "error", err)
		http.Error(w, "failed to capture terminal text", http.StatusBadGateway)
		return
	}
	writeJSON(w, capture)
}

func (s *Server) handlePasteAPI(w http.ResponseWriter, r *http.Request, id string, session registry.Session) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request tmux.PasteRequest
	r.Body = http.MaxBytesReader(w, r.Body, int64(tmux.MaxPasteBytes*8+1024))
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid paste request", http.StatusBadRequest)
		return
	}
	if err := s.tmux.Paste(r.Context(), session.TmuxName, request); err != nil {
		if errors.Is(err, tmux.ErrInvalidPaste) {
			http.Error(w, "invalid paste", http.StatusBadRequest)
			return
		}
		s.logger.Error("tmux paste failed", "session", id, "error", err)
		http.Error(w, "failed to paste terminal text", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleKeysAPI(w http.ResponseWriter, r *http.Request, id string, session registry.Session) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request tmux.KeyRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid key request", http.StatusBadRequest)
		return
	}
	if err := s.tmux.SendKey(r.Context(), session.TmuxName, request); err != nil {
		if errors.Is(err, tmux.ErrUnsupportedKey) {
			http.Error(w, "unsupported key", http.StatusBadRequest)
			return
		}
		s.logger.Error("tmux key command failed", "session", id, "key", request.Key, "error", err)
		http.Error(w, "failed to send key", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleResizeAPI(w http.ResponseWriter, r *http.Request, id string, session registry.Session) {
	switch r.Method {
	case http.MethodGet:
		response, err := s.resizeResponse(r, id, session, nil)
		if err != nil {
			s.logger.Error("resize state failed", "session", id, "error", err)
			http.Error(w, "failed to read resize state", http.StatusInternalServerError)
			return
		}
		writeJSON(w, response)
	case http.MethodPost:
		var request resizeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid resize request", http.StatusBadRequest)
			return
		}
		applied, err := s.applyResizeRequest(r, id, session, request)
		if err != nil {
			if errors.Is(err, errInvalidResizeRequest) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.logger.Error("tmux resize failed", "session", id, "mode", request.Mode, "error", err)
			http.Error(w, "failed to resize terminal", http.StatusBadGateway)
			return
		}
		response, err := s.resizeResponse(r, id, session, applied)
		if err != nil {
			s.logger.Error("resize state failed", "session", id, "error", err)
			http.Error(w, "failed to read resize state", http.StatusInternalServerError)
			return
		}
		writeJSON(w, response)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleResizeViewerAPI(w http.ResponseWriter, r *http.Request, id string, session registry.Session) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request resizeViewerRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid resize viewer request", http.StatusBadRequest)
		return
	}
	viewer, err := s.resize.RecordViewer(id, resizeViewer{
		ID:        request.ViewerID,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		Width:     request.Width,
		Height:    request.Height,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var applied *tmux.ResizeState
	settings, err := s.resize.Load(id)
	if err != nil {
		s.logger.Error("resize settings load failed", "session", id, "error", err)
		http.Error(w, "failed to read resize settings", http.StatusInternalServerError)
		return
	}
	if settings.Mode == resizeModeWeb && settings.SelectedViewerID == viewer.ID && !request.Transient {
		state, err := s.tmux.ResizeManual(r.Context(), session.TmuxName, viewer.Width, viewer.Height)
		if err != nil {
			s.logger.Error("tmux web resize failed", "session", id, "viewer", viewer.ID, "error", err)
			http.Error(w, "failed to resize terminal", http.StatusBadGateway)
			return
		}
		state.Mode = resizeModeWeb
		applied = &state
	}
	response, err := s.resizeResponse(r, id, session, applied)
	if err != nil {
		s.logger.Error("resize state failed", "session", id, "error", err)
		http.Error(w, "failed to read resize state", http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func (s *Server) applyResizeRequest(r *http.Request, id string, session registry.Session, request resizeRequest) (*tmux.ResizeState, error) {
	mode := normalizeResizeMode(request.Mode)
	if err := validateResizeMode(mode); err != nil {
		return nil, fmtInvalidResizeRequest(err)
	}

	settings := resizeSettings{Mode: mode}
	var applied tmux.ResizeState
	switch mode {
	case resizeModeOff:
		if err := s.resize.Save(id, settings); err != nil {
			return nil, err
		}
		return nil, nil
	case resizeModeSmallest:
		state, err := s.tmux.ResizeSmallest(r.Context(), session.TmuxName)
		if err != nil {
			return nil, err
		}
		state.Mode = resizeModeSmallest
		applied = state
	case resizeModeWeb:
		viewerID := strings.TrimSpace(request.ViewerID)
		viewer, ok := s.resize.Viewer(id, viewerID)
		if viewerID == "" || !ok {
			return nil, fmtInvalidResizeRequest(errors.New("selected web viewer is not active"))
		}
		state, err := s.tmux.ResizeManual(r.Context(), session.TmuxName, viewer.Width, viewer.Height)
		if err != nil {
			return nil, err
		}
		state.Mode = resizeModeWeb
		applied = state
		settings.SelectedViewerID = viewerID
	case resizeModePrimary:
		client, err := s.tmux.PrimaryResizeClient(r.Context(), session.TmuxName, session.PID)
		if err != nil {
			return nil, fmtInvalidResizeRequest(err)
		}
		state, err := s.tmux.ResizeManual(r.Context(), session.TmuxName, client.Width, client.Height)
		if err != nil {
			return nil, err
		}
		state.Mode = resizeModePrimary
		state.ClientName = client.Name
		applied = state
	}

	if err := s.resize.Save(id, settings); err != nil {
		return nil, err
	}
	return &applied, nil
}

func (s *Server) resizeResponse(r *http.Request, id string, session registry.Session, applied *tmux.ResizeState) (resizeResponse, error) {
	settings, err := s.resize.Load(id)
	if err != nil {
		return resizeResponse{}, err
	}
	viewers := s.resize.Viewers(id)
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
	if primary, err := s.tmux.PrimaryResizeClient(r.Context(), session.TmuxName, session.PID); err == nil {
		response.PrimaryClient = &primary
	}
	return response, nil
}

func fmtInvalidResizeRequest(err error) error {
	return fmt.Errorf("%w: %v", errInvalidResizeRequest, err)
}

func (s *Server) handleTmuxControlAPI(w http.ResponseWriter, r *http.Request, id string, session registry.Session) {
	switch r.Method {
	case http.MethodGet:
		windows, err := s.tmux.Windows(r.Context(), session.TmuxName)
		if err != nil {
			s.logger.Error("tmux window list failed", "session", id, "error", err)
			http.Error(w, "failed to list tmux windows", http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"windows": windows})
	case http.MethodPost:
		var request tmux.ControlRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid tmux control request", http.StatusBadRequest)
			return
		}
		windows, err := s.tmux.Control(r.Context(), session.TmuxName, request)
		if err != nil {
			if errors.Is(err, tmux.ErrUnsupportedControlAction) || errors.Is(err, tmux.ErrInvalidControlRequest) {
				http.Error(w, "unsupported tmux control action", http.StatusBadRequest)
				return
			}
			s.logger.Error("tmux control command failed", "session", id, "action", request.Action, "error", err)
			http.Error(w, "failed to run tmux control action", http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"windows": windows})
	default:
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

func parseSessionAPIPath(path string) (string, string, bool) {
	trimmed := strings.TrimPrefix(path, "/api/sessions/")
	if trimmed == path {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || !registry.ValidID(parts[0]) {
		return "", "", false
	}
	return parts[0], strings.Join(parts[1:], "/"), true
}

func writeJSON(w http.ResponseWriter, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
