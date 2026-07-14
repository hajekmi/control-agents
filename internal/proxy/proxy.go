package proxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"control-agents/internal/registry"
)

type SessionStore interface {
	ReadByPublicRef(ref string) (registry.Session, error)
	Alive(session registry.Session) bool
}

type Handler struct {
	store         SessionStore
	observeOutput func(sessionRef string, bytes int)
}

const maxTerminalHTMLBytes = 32 << 20

const terminalTransportObserver = `<script src="/terminal-observer.js"></script>`

func New(store SessionStore) *Handler {
	return &Handler{store: store}
}

func NewWithOutputObserver(store SessionStore, observe func(sessionRef string, bytes int)) *Handler {
	return &Handler{store: store, observeOutput: observe}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ref, ok := sessionRefFromPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	session, err := h.store.ReadByPublicRef(ref)
	if err != nil || !h.store.Alive(session) {
		http.NotFound(w, r)
		return
	}

	target := &url.URL{Scheme: "http", Host: "control-agents-ttyd"}
	upstream := httputil.NewSingleHostReverseProxy(target)
	observeWebSocket := h.observeOutput != nil && strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	injectTransportObserver := r.Method == http.MethodGet && r.URL.Path == "/terminal/"+ref+"/" && !strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	upstream.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 5 * time.Second}
			connection, err := dialer.DialContext(ctx, "unix", session.Socket)
			if err != nil || !observeWebSocket {
				return connection, err
			}
			return &outputObservingConn{
				Conn: connection,
				observe: func(bytes int) {
					h.observeOutput(ref, bytes)
				},
			}, nil
		},
	}
	originalDirector := upstream.Director
	upstream.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Scheme = "http"
		req.URL.Host = target.Host
		req.Host = r.Host
		if injectTransportObserver {
			req.Header.Set("Accept-Encoding", "identity")
		}
	}
	upstream.ModifyResponse = func(response *http.Response) error {
		response.Header.Set("Content-Security-Policy", "frame-ancestors 'self'")
		response.Header.Set("X-Frame-Options", "SAMEORIGIN")
		if injectTransportObserver {
			return injectTerminalTransportObserver(response)
		}
		return nil
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

func injectTerminalTransportObserver(response *http.Response) error {
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTerminalHTMLBytes+1))
	if err != nil {
		return fmt.Errorf("read terminal HTML: %w", err)
	}
	if err := response.Body.Close(); err != nil {
		return fmt.Errorf("close terminal HTML: %w", err)
	}
	if len(body) > maxTerminalHTMLBytes {
		return errors.New("terminal HTML exceeds observer injection limit")
	}

	insertion := 0
	lower := bytes.ToLower(body)
	if head := bytes.Index(lower, []byte("<head")); head >= 0 {
		if end := bytes.IndexByte(lower[head:], '>'); end >= 0 {
			insertion = head + end + 1
		}
	}
	modified := make([]byte, 0, len(body)+len(terminalTransportObserver))
	modified = append(modified, body[:insertion]...)
	modified = append(modified, terminalTransportObserver...)
	modified = append(modified, body[insertion:]...)
	response.Body = io.NopCloser(bytes.NewReader(modified))
	response.ContentLength = int64(len(modified))
	response.Header.Set("Content-Length", fmt.Sprintf("%d", len(modified)))
	response.Header.Del("ETag")
	response.Header.Del("Content-MD5")
	return nil
}

// outputObservingConn counts server-to-browser WebSocket data payload bytes
// without retaining or logging their contents. WebSocket control frames are
// excluded so keepalives do not appear as terminal activity.
type outputObservingConn struct {
	net.Conn
	observe func(bytes int)

	httpHeaderMatch int
	upgraded        bool
	frameHeader     [14]byte
	frameHeaderLen  int
	frameHeaderNeed int
	payloadBytes    uint64
	dataFrame       bool
}

func (c *outputObservingConn) Read(buffer []byte) (int, error) {
	read, err := c.Conn.Read(buffer)
	if read > 0 {
		c.inspect(buffer[:read])
	}
	return read, err
}

func (c *outputObservingConn) inspect(data []byte) {
	if !c.upgraded {
		const headerEnd = "\r\n\r\n"
		for index, value := range data {
			if value == headerEnd[c.httpHeaderMatch] {
				c.httpHeaderMatch++
			} else if value == headerEnd[0] {
				c.httpHeaderMatch = 1
			} else {
				c.httpHeaderMatch = 0
			}
			if c.httpHeaderMatch == len(headerEnd) {
				c.upgraded = true
				c.inspectFrames(data[index+1:])
				return
			}
		}
		return
	}
	c.inspectFrames(data)
}

func (c *outputObservingConn) inspectFrames(data []byte) {
	for len(data) > 0 {
		if c.payloadBytes > 0 {
			consumed := uint64(len(data))
			if consumed > c.payloadBytes {
				consumed = c.payloadBytes
			}
			if c.dataFrame {
				c.observe(int(consumed))
			}
			c.payloadBytes -= consumed
			data = data[int(consumed):]
			if c.payloadBytes == 0 {
				c.frameHeaderLen = 0
				c.frameHeaderNeed = 0
			}
			continue
		}

		if c.frameHeaderNeed == 0 {
			c.frameHeaderNeed = 2
		}
		needed := c.frameHeaderNeed - c.frameHeaderLen
		if needed > len(data) {
			needed = len(data)
		}
		copy(c.frameHeader[c.frameHeaderLen:], data[:needed])
		c.frameHeaderLen += needed
		data = data[needed:]
		if c.frameHeaderLen < c.frameHeaderNeed {
			continue
		}
		if c.frameHeaderNeed == 2 {
			lengthCode := c.frameHeader[1] & 0x7f
			extendedBytes := 0
			switch lengthCode {
			case 126:
				extendedBytes = 2
			case 127:
				extendedBytes = 8
			}
			maskBytes := 0
			if c.frameHeader[1]&0x80 != 0 {
				maskBytes = 4
			}
			c.frameHeaderNeed = 2 + extendedBytes + maskBytes
			if c.frameHeaderLen < c.frameHeaderNeed {
				continue
			}
		}
		c.beginFrame()
	}
}

func (c *outputObservingConn) beginFrame() {
	lengthCode := c.frameHeader[1] & 0x7f
	switch lengthCode {
	case 126:
		c.payloadBytes = uint64(binary.BigEndian.Uint16(c.frameHeader[2:4]))
	case 127:
		c.payloadBytes = binary.BigEndian.Uint64(c.frameHeader[2:10])
	default:
		c.payloadBytes = uint64(lengthCode)
	}
	opcode := c.frameHeader[0] & 0x0f
	c.dataFrame = opcode == 0 || opcode == 1 || opcode == 2
	if c.payloadBytes == 0 {
		c.frameHeaderLen = 0
		c.frameHeaderNeed = 0
	}
}

func sessionRefFromPath(path string) (string, bool) {
	trimmed := strings.TrimPrefix(path, "/terminal/")
	if trimmed == path || trimmed == "" {
		return "", false
	}
	ref := trimmed
	if slash := strings.IndexByte(trimmed, '/'); slash >= 0 {
		ref = trimmed[:slash]
	}
	if !registry.ValidPublicRef(ref) {
		return "", false
	}
	return ref, true
}
