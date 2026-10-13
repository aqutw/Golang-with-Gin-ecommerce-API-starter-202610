package httpx_test

import (
	"example.com/ecommerce/platform/httpx"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecoveryAndRequestID(t *testing.T) {
	h := httpx.Middleware(slog.New(slog.NewTextHandler(io.Discard, nil)), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if httpx.RequestID(r.Context()) == "" {
			t.Fatal("missing context ID")
		}
		panic("secret must not leak")
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-ID", "untrusted")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 500 || w.Header().Get("X-Request-ID") == "untrusted" || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
