package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"terminal-mirror/internal/auth"
	"terminal-mirror/internal/config"
	"terminal-mirror/internal/proxy"
	"terminal-mirror/internal/registry"
)

//go:embed static
var staticFS embed.FS

type Server struct {
	cfg      config.Config
	auth     *auth.Authenticator
	registry *registry.Store
	logger   *slog.Logger
	mux      *http.ServeMux
}

func New(cfg config.Config, logger *slog.Logger) (*Server, error) {
	store := registry.NewStore(cfg.StateDir)
	if err := store.Ensure(); err != nil {
		return nil, err
	}

	authenticator, err := auth.New(cfg.Password, time.Duration(cfg.CookieTTL)*time.Second, cfg.CookieSecure)
	if err != nil {
		return nil, err
	}

	server := &Server{
		cfg:      cfg,
		auth:     authenticator,
		registry: store,
		logger:   logger,
		mux:      http.NewServeMux(),
	}
	server.routes()
	return server, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/login", s.handleLogin)
	s.mux.HandleFunc("/logout", s.handleLogout)
	s.mux.Handle("/api/sessions", s.auth.RequireAPI(http.HandlerFunc(s.handleSessions)))
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
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if !s.auth.Login(w, r.FormValue("password")) {
			http.Redirect(w, r, "/login?error=1", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
