package control

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
)

func acceptsGzip(header string) bool {
	for _, value := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(value), ";")
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(key, "q") {
				parsed, err := strconv.ParseFloat(value, 64)
				if err != nil {
					quality = 0
				} else {
					quality = parsed
				}
			}
		}
		return quality > 0
	}
	return false
}
func compressResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebSocket upgrades and audio/range transfers must keep their original writer and bytes.
		if r.URL.Path == "/ws/agent" || strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Range") != "" || (strings.HasPrefix(r.URL.Path, "/api/audio/") && strings.HasSuffix(r.URL.Path, "/content")) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		response := &compressedWriter{ResponseWriter: w, head: r.Method == http.MethodHead}
		defer func() {
			if response.gzip != nil {
				_ = response.gzip.Close()
			}
		}()
		next.ServeHTTP(response, r)
	})
}

type compressedWriter struct {
	http.ResponseWriter
	gzip        *gzip.Writer
	wroteHeader bool
	head        bool
}

func (w *compressedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *compressedWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	contentType := strings.ToLower(w.Header().Get("Content-Type"))
	compressible := strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "javascript") || strings.Contains(contentType, "json") || strings.Contains(contentType, "svg")
	if status != http.StatusNoContent && status != http.StatusNotModified && status >= 200 && w.Header().Get("Content-Encoding") == "" && compressible {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		if !w.head {
			w.gzip = gzip.NewWriter(w.ResponseWriter)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *compressedWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(data))
		}
		w.WriteHeader(http.StatusOK)
	}
	if w.head {
		return len(data), nil
	}
	if w.gzip != nil {
		return w.gzip.Write(data)
	}
	return w.ResponseWriter.Write(data)
}
func (w *compressedWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.gzip != nil {
		_ = w.gzip.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
