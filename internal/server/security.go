package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

const appContentSecurityPolicy = "default-src 'self'; base-uri 'none'; object-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self' ws: wss:; frame-src 'self'; frame-ancestors 'self'; form-action 'self'"

func applySecurityHeaders(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "SAMEORIGIN")
	header.Set("Referrer-Policy", "same-origin")
	header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")

	if !strings.HasPrefix(r.URL.Path, "/terminal/") {
		header.Set("Content-Security-Policy", appContentSecurityPolicy)
	}
}

func requiresSameOrigin(r *http.Request) bool {
	if r.URL.Path == "/login" {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/terminal/") && isWebSocketUpgrade(r) {
		return true
	}
	return isUnsafeMethod(r.Method)
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		originURL, err := url.Parse(origin)
		if err != nil || originURL.Scheme == "" || originURL.Host == "" || originURL.User != nil {
			return false
		}
		return matchesRequestOrigin(r, originURL.Scheme, originURL.Host)
	}

	referer := strings.TrimSpace(r.Header.Get("Referer"))
	if referer == "" {
		return false
	}

	refererURL, err := url.Parse(referer)
	if err != nil || refererURL.Scheme == "" || refererURL.Host == "" || refererURL.User != nil {
		return false
	}
	return matchesRequestOrigin(r, refererURL.Scheme, refererURL.Host)
}

func matchesRequestOrigin(r *http.Request, scheme, host string) bool {
	return strings.EqualFold(scheme, requestScheme(r)) &&
		normalizeOriginHost(host, scheme) == normalizeOriginHost(r.Host, requestScheme(r))
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, part := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
			return true
		}
	}
	return false
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func normalizeOriginHost(host, scheme string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	withoutPort, port, err := net.SplitHostPort(host)
	if err == nil {
		withoutPort = strings.Trim(strings.TrimSuffix(withoutPort, "."), "[]")
		if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
			return withoutPort
		}
		return net.JoinHostPort(withoutPort, port)
	}
	return strings.Trim(strings.TrimSuffix(host, "."), "[]")
}
