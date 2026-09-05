package web

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"sync"
)

var gzipPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return w
	},
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer      *gzip.Writer
	wroteHeader bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Vary", "Accept-Encoding")
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.writer.Write(b)
}

// withGzip wraps an http.Handler with lightweight gzip compression for clients supporting it.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Do not compress SSE event streams or websocket requests
		if r.Header.Get("Accept") == "text/event-stream" || strings.Contains(r.URL.Path, "/stream") {
			next.ServeHTTP(w, r)
			return
		}

		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		gz := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gz)
		gz.Reset(w)
		defer gz.Close()

		gw := &gzipResponseWriter{
			ResponseWriter: w,
			writer:         gz,
		}
		next.ServeHTTP(gw, r)
	})
}

// withStaticCache sets long-term immutable caching and ETag headers for static versioned assets.
func withStaticCache(contentVersion string, next http.Handler) http.Handler {
	etag := `"` + computeETag(contentVersion) + `"`

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", etag)

		// Conditional 304 Not Modified
		if match := r.Header.Get("If-None-Match"); match != "" {
			if match == etag || match == "*" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func computeETag(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}
