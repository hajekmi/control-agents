package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

const appContentSecurityPolicy = "default-src 'self'; base-uri 'none'; object-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-src 'self'; frame-ancestors 'self'; form-action 'self'"
const csrfHeader = "X-Control-Agents-CSRF-Token"

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

func requiresCSRF(r *http.Request) bool {
	return r.URL.Path != "/login" && isUnsafeMethod(r.Method)
}

func sameOrigin(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	if len(origins) > 0 {
		if len(origins) != 1 {
			return false
		}
		origin := origins[0]
		if origin != strings.TrimSpace(origin) || strings.Contains(origin, "#") {
			return false
		}
		originURL, err := url.Parse(origin)
		if err != nil || !isSerializedOrigin(originURL) {
			return false
		}
		return matchesRequestOrigin(r, originURL.Scheme, originURL.Host)
	}
	// A browser WebSocket handshake supplies Origin. Do not accept Referer as a
	// fallback for terminal upgrades because Origin is the authorization input.
	if strings.HasPrefix(r.URL.Path, "/terminal/") && isWebSocketUpgrade(r) {
		return false
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

func isSerializedOrigin(origin *url.URL) bool {
	return origin.Scheme != "" && origin.Host != "" && origin.User == nil && origin.Opaque == "" &&
		origin.Path == "" && origin.RawPath == "" && origin.RawQuery == "" && !origin.ForceQuery &&
		origin.Fragment == "" && origin.RawFragment == ""
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
