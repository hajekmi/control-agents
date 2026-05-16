package server

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"terminal-mirror/internal/auth"
	"terminal-mirror/internal/compress"
	"terminal-mirror/internal/config"
	"terminal-mirror/internal/proxy"
	"terminal-mirror/internal/registry"
	"terminal-mirror/internal/tmux"
	"terminal-mirror/internal/version"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	cfg      config.Config
	auth     *auth.Authenticator
	registry *registry.Store
	tmux     *tmux.Client
	logger   *slog.Logger
	mux      *http.ServeMux
	limiter  *loginLimiter
}

func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
	store := registry.NewStore(cfg.StateDir)
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
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
	if len(parts) != 2 || !registry.ValidID(parts[0]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func writeJSON(w http.ResponseWriter, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
