// Package httpx contains standard net/http plumbing, with no Gin or commerce dependency.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type requestIDKey struct{}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey{}).(string); return id }

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Middleware bounds body size, generates trusted request IDs, logs no query/body
// or auth headers, and recovers before a response has been written.
func Middleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := NewID()
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("request panic", "request_id", id)
				if sw.status == 0 {
					Error(sw, r, http.StatusInternalServerError, "internal_error", "internal server error")
				}
			}
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			logger.Info("request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(start))
		}()
		next.ServeHTTP(sw, r)
	})
}

func Error(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": RequestID(r.Context())}})
}
