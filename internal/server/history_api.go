package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"control-agents/internal/registry"
	"control-agents/internal/tmux"
)

const historyViewerHeader = "X-Control-Agents-Viewer-ID"

type historySnapshotRequest struct {
	Mode string `json:"mode"`
}

func (s *Server) handleHistoryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, ok := s.auth.LoginScope(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	viewer := ViewerID(strings.TrimSpace(r.Header.Get(historyViewerHeader)))
	if !viewerIDPattern.MatchString(string(viewer)) {
		http.Error(w, "invalid viewer identity", http.StatusBadRequest)
		return
	}

	if paneRef, ok := parseHistoryCreatePath(r.URL.Path); ok {
		s.handleCreateHistorySnapshot(w, r, user, viewer, paneRef)
		return
	}
	if snapshotID, pages, ok := parseHistorySnapshotPath(r.URL.Path); ok {
		if pages {
			s.handleHistorySnapshotPage(w, r, user, viewer, snapshotID)
		} else {
			s.handleHistorySnapshot(w, r, user, viewer, snapshotID)
		}
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleCreateHistorySnapshot(w http.ResponseWriter, r *http.Request, user string, viewer ViewerID, paneRef PaneRef) {
	started := time.Now()
	auditStatus := http.StatusCreated
	auditBytes := 0
	auditRef := SessionRef("")
	defer func() {
		s.auditTerminalAction(started, auditRef, auditStatus, auditBytes, auditStatusReason(auditStatus))
	}()
	if r.Method != http.MethodPost {
		auditStatus = http.StatusMethodNotAllowed
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request historySnapshotRequest
	if err := decodeLifecycleRequest(w, r, &request); err != nil {
		auditStatus = http.StatusBadRequest
		http.Error(w, "invalid history snapshot request", http.StatusBadRequest)
		return
	}
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" {
		mode = "reflow"
	}
	if mode != "reflow" && mode != "fixed" {
		auditStatus = http.StatusBadRequest
		http.Error(w, "invalid history snapshot mode", http.StatusBadRequest)
		return
	}

	managed, pane, err := s.findHistoryPane(r, paneRef)
	if err != nil {
		auditStatus = http.StatusConflict
		http.Error(w, "stale terminal identity", http.StatusConflict)
		return
	}
	auditRef = SessionRef(managed.PublicRef)
	key := historyCaptureKey{User: user, Viewer: viewer, SessionRef: auditRef, PaneRef: paneRef, Generation: pane.generation, Mode: mode}
	product, err := s.historyCaptures.Do(r.Context(), key, func(captureContext context.Context) (historyCaptureProduct, error) {
		var product historyCaptureProduct
		err := s.sessions.WithSession(captureContext, managed.ID, func(current registry.Session) error {
			if current.PublicRef != managed.PublicRef {
				return errStaleTerminalIdentity
			}
			resolved, err := s.identity.resolvePane(captureContext, s.tmux, current, paneRef, true)
			if err != nil || resolved.generation != pane.generation {
				return errStaleTerminalIdentity
			}
			product.Capture, err = s.tmux.CaptureHistory(captureContext, resolved.rawID, s.cfg.SnapshotMaxBytes, func() int64 {
				return s.activity.Epoch(SessionRef(current.PublicRef))
			})
			return err
		})
		if err != nil {
			return historyCaptureProduct{}, err
		}
		parseStarted := time.Now()
		product.Lines, err = parseHistoryANSIContext(captureContext, product.Capture.Text)
		product.ParseDuration = time.Since(parseStarted)
		product.ParseBytes = len(product.Capture.Text)
		product.NodeEstimate = historyNodeEstimate(product.Lines)
		return product, err
	})
	if err != nil {
		auditStatus = http.StatusBadGateway
		if errors.Is(err, errHistoryCreateRate) || errors.Is(err, errHistoryRateScopes) ||
			errors.Is(err, errHistoryProcessCaptures) || errors.Is(err, errHistoryLoginCaptures) ||
			errors.Is(err, errHistoryCaptureWaiters) {
			auditStatus = http.StatusTooManyRequests
			w.Header().Set("Retry-After", "10")
		} else if errors.Is(err, context.DeadlineExceeded) {
			auditStatus = http.StatusGatewayTimeout
		} else if errors.Is(err, errHistoryANSIResourceLimit) {
			auditStatus = http.StatusRequestEntityTooLarge
		} else if errors.Is(err, errStaleTerminalIdentity) {
			auditStatus = http.StatusConflict
		} else if errors.Is(err, tmux.ErrSnapshotTooLarge) {
			auditStatus = http.StatusRequestEntityTooLarge
		}
		if product.ParseBytes > 0 {
			s.logger.Info("history parse", "opaque_id", auditRef, "bytes", product.ParseBytes,
				"duration_ms", product.ParseDuration.Milliseconds(), "reason_code", "resource_limit")
		}
		http.Error(w, "failed to capture terminal history", auditStatus)
		return
	}
	capture := product.Capture
	auditBytes = len(capture.Text)
	if capture.Before.AlternateScreen || capture.After.AlternateScreen {
		mode = "fixed"
		capture.Before.AlternateScreen = true
	}
	s.logger.Info("history parse", "opaque_id", auditRef, "bytes", product.ParseBytes,
		"duration_ms", product.ParseDuration.Milliseconds(), "reason_code", "ok")
	page, err := s.snapshots.Create(snapshotCreate{
		User:         user,
		Viewer:       viewer,
		SessionRef:   SessionRef(managed.PublicRef),
		PaneRef:      paneRef,
		Generation:   pane.generation,
		Mode:         mode,
		Capture:      capture,
		Lines:        product.Lines,
		NodeEstimate: product.NodeEstimate,
	})
	if err != nil {
		switch {
		case errors.Is(err, errSnapshotCapacity):
			auditStatus = http.StatusServiceUnavailable
			http.Error(w, "history snapshot capacity reached", auditStatus)
		case errors.Is(err, errHistoryANSIResourceLimit):
			auditStatus = http.StatusRequestEntityTooLarge
			http.Error(w, "terminal history exceeds rendering limits", auditStatus)
		default:
			auditStatus = http.StatusInternalServerError
			http.Error(w, "failed to create history snapshot", auditStatus)
		}
		return
	}
	writeJSONStatus(w, http.StatusCreated, page)
}

func (s *Server) handleHistorySnapshotPage(w http.ResponseWriter, r *http.Request, user string, viewer ViewerID, snapshotID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	binding, err := s.snapshots.Binding(snapshotID, user, viewer)
	if err != nil {
		writeSnapshotLookupError(w, err)
		return
	}
	if _, err := s.resolveSnapshotPane(r, binding); err != nil {
		_ = s.snapshots.Delete(snapshotID, user, viewer)
		http.Error(w, "history snapshot is gone", http.StatusGone)
		return
	}
	page, err := s.snapshots.Page(snapshotID, r.URL.Query().Get("before"), user, viewer)
	if err != nil {
		writeSnapshotLookupError(w, err)
		return
	}
	writeJSON(w, page)
}

func (s *Server) handleHistorySnapshot(w http.ResponseWriter, r *http.Request, user string, viewer ViewerID, snapshotID string) {
	if r.Method == http.MethodGet {
		binding, err := s.snapshots.Binding(snapshotID, user, viewer)
		if err != nil {
			writeSnapshotLookupError(w, err)
			return
		}
		pane, err := s.resolveSnapshotPane(r, binding)
		if err != nil {
			_ = s.snapshots.Delete(snapshotID, user, viewer)
			http.Error(w, "history snapshot is gone", http.StatusGone)
			return
		}
		activity, err := s.tmux.HistoryActivity(r.Context(), pane.rawID)
		if err != nil {
			http.Error(w, "failed to read terminal activity", http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]bool{
			"newOutput": s.activity.Epoch(binding.SessionRef) != binding.OutputEpoch || activity.HistoryBytes != binding.HistoryBytes,
		})
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := requireEmptyMutationBody(w, r); err != nil {
		http.Error(w, "invalid history snapshot delete request", http.StatusBadRequest)
		return
	}
	if err := s.snapshots.Delete(snapshotID, user, viewer); err != nil {
		writeSnapshotLookupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resolveSnapshotPane(r *http.Request, binding snapshotBinding) (paneBinding, error) {
	managed, err := s.findPublicSession(r.Context(), binding.SessionRef)
	if err != nil {
		return paneBinding{}, err
	}
	pane, err := s.identity.resolvePane(r.Context(), s.tmux, managed, binding.PaneRef, false)
	if err != nil || pane.generation != binding.Generation {
		return paneBinding{}, errStaleTerminalIdentity
	}
	return pane, nil
}

func (s *Server) findHistoryPane(r *http.Request, ref PaneRef) (registry.Session, paneBinding, error) {
	sessions, err := s.sessions.List(r.Context())
	if err != nil {
		return registry.Session{}, paneBinding{}, err
	}
	for _, managed := range sessions {
		var current registry.Session
		var pane paneBinding
		err := s.sessions.WithSession(r.Context(), managed.ID, func(candidate registry.Session) error {
			if candidate.PublicRef != managed.PublicRef {
				return errStaleTerminalIdentity
			}
			resolved, err := s.identity.resolvePane(r.Context(), s.tmux, candidate, ref, true)
			if err != nil {
				return err
			}
			current = candidate
			pane = resolved
			return nil
		})
		if err == nil {
			return current, pane, nil
		}
		if !errors.Is(err, errStaleTerminalIdentity) {
			return registry.Session{}, paneBinding{}, err
		}
	}
	return registry.Session{}, paneBinding{}, errStaleTerminalIdentity
}

func parseHistoryCreatePath(path string) (PaneRef, bool) {
	const prefix = "/api/v1/panes/"
	const suffix = "/history-snapshots"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if value == "" || len(value) > maxOpaqueReferenceBytes || strings.Contains(value, "/") || !strings.HasPrefix(value, "p_") {
		return "", false
	}
	return PaneRef(value), true
}

func parseHistorySnapshotPath(path string) (string, bool, bool) {
	const prefix = "/api/v1/history-snapshots/"
	if !strings.HasPrefix(path, prefix) {
		return "", false, false
	}
	value := strings.TrimPrefix(path, prefix)
	pages := strings.HasSuffix(value, "/pages")
	if pages {
		value = strings.TrimSuffix(value, "/pages")
	}
	if value == "" || strings.Contains(value, "/") {
		return "", false, false
	}
	return value, pages, true
}

func writeSnapshotLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSnapshotGone):
		http.Error(w, "history snapshot is gone", http.StatusGone)
	case errors.Is(err, errSnapshotCursor):
		http.Error(w, "invalid history cursor", http.StatusBadRequest)
	default:
		http.Error(w, "history snapshot not found", http.StatusNotFound)
	}
}
