package compress

import (
	"compress/gzip"
	"net/http"
	"strings"
)

type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (w gzipResponseWriter) Write(data []byte) (int, error) {
	return w.writer.Write(data)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shouldCompress(r) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Del("Content-Length")

		gzipWriter := gzip.NewWriter(w)
		defer gzipWriter.Close()

		next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, writer: gzipWriter}, r)
	})
}

func shouldCompress(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/terminal/") {
		return false
	}
	return acceptsGzip(r.Header.Get("Accept-Encoding"))
}

func acceptsGzip(value string) bool {
	for _, part := range strings.Split(value, ",") {
		token := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if strings.EqualFold(token, "gzip") {
			return true
		}
	}
	return false
}

