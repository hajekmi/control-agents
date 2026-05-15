package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"terminal-mirror/internal/registry"
)

type SessionStore interface {
	Read(id string) (registry.Session, error)
	Alive(session registry.Session) bool
}

type Handler struct {
	store SessionStore
}

func New(store SessionStore) *Handler {
	return &Handler{store: store}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionIDFromPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	session, err := h.store.Read(id)
	if err != nil || !h.store.Alive(session) {
		http.NotFound(w, r)
		return
	}

	target := &url.URL{Scheme: "http", Host: "terminal-mirror-ttyd"}
	upstream := httputil.NewSingleHostReverseProxy(target)
	upstream.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, "unix", session.Socket)
		},
	}
	originalDirector := upstream.Director
	upstream.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Scheme = "http"
		req.URL.Host = target.Host
		req.Host = r.Host
	}
	upstream.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		status := http.StatusBadGateway
		if errors.Is(err, context.Canceled) {
			status = 499
		}
		http.Error(w, http.StatusText(status), status)
	}
	upstream.ServeHTTP(w, r)
}

func sessionIDFromPath(path string) (string, bool) {
	trimmed := strings.TrimPrefix(path, "/terminal/")
	if trimmed == path || trimmed == "" {
		return "", false
	}
	id := trimmed
	if slash := strings.IndexByte(trimmed, '/'); slash >= 0 {
		id = trimmed[:slash]
	}
	if !registry.ValidID(id) {
		return "", false
	}
	return id, true
}
