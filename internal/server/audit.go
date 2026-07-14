package server

import (
	"net/http"
	"time"
)

// auditTerminalAction records metadata only. Request bodies, response bodies,
// terminal text, paste text, and WebSocket frames are never audit fields.
func (s *Server) auditTerminalAction(start time.Time, ref SessionRef, status, bytes int, reason string) {
	if s.logger == nil {
		return
	}
	if reason == "" {
		reason = "ok"
	}
	s.logger.Info("terminal action",
		"opaque_id", ref,
		"status", status,
		"bytes", bytes,
		"duration_ms", time.Since(start).Milliseconds(),
		"reason_code", reason,
	)
}

func auditStatusReason(status int) string {
	switch status {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return "ok"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusConflict:
		return "stale_identity"
	case http.StatusRequestEntityTooLarge:
		return "byte_limit"
	case http.StatusTooManyRequests:
		return "rate_limit"
	case http.StatusGatewayTimeout:
		return "timeout"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return "dependency_failure"
	default:
		return "request_failed"
	}
}
