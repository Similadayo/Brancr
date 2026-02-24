package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// Compression middleware compresses responses using gzip
func Compression() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if client supports gzip
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				next.ServeHTTP(w, r)
				return
			}

			// Skip compression for already compressed content
			contentType := w.Header().Get("Content-Type")
			if strings.Contains(contentType, "image/") ||
				strings.Contains(contentType, "video/") ||
				strings.Contains(contentType, "application/zip") ||
				strings.Contains(contentType, "application/gzip") {
				next.ServeHTTP(w, r)
				return
			}

			// Create gzip writer
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			defer gz.Close()

			// Wrap response writer
			gzw := &gzipResponseWriter{
				ResponseWriter: w,
				Writer:         gz,
			}

			next.ServeHTTP(gzw, r)
		})
	}
}

// gzipResponseWriter wraps http.ResponseWriter with gzip compression
type gzipResponseWriter struct {
	http.ResponseWriter
	io.Writer
}

func (gzw *gzipResponseWriter) Write(b []byte) (int, error) {
	return gzw.Writer.Write(b)
}
