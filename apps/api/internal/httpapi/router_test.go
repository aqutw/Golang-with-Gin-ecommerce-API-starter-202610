package httpapi_test

import (
	"encoding/json"
	"example.com/ecommerce/api/internal/httpapi"
	"example.com/ecommerce/commerce/adapters/memory"
	catalogapp "example.com/ecommerce/commerce/application/catalog"
	"example.com/ecommerce/commerce/application/checkout"
	"example.com/ecommerce/commerce/domain/catalog"
	"example.com/ecommerce/platform/config"
	"example.com/ecommerce/platform/httpx"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func handler() http.Handler {
	store := memory.New([]catalog.Product{{ID: "a", Name: "A", Currency: "USD", PriceMinor: 1000, Active: true}}, map[string]int{"a": 2})
	return httpapi.New(httpapi.Dependencies{Catalog: catalogapp.Service{Store: store}, Checkout: checkout.Service{Store: store, NewID: httpx.NewID, Now: time.Now}, Config: config.Config{APIToken: "test-token-123456", Subject: "alice"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Storage: "memory"})
}
func request(h http.Handler, path, body, token, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCheckoutHTTP(t *testing.T) {
	h := handler()
	body := `{"items":[{"product_id":"a","quantity":1}]}`
	first := request(h, "/v1/checkouts", body, "test-token-123456", "request-001")
	if first.Code != 201 || first.Header().Get("Location") == "" || first.Header().Get("X-Request-ID") == "" {
		t.Fatalf("%d %s", first.Code, first.Body)
	}
	var result checkout.Result
	if err := json.Unmarshal(first.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Order.CustomerID != "alice" {
		t.Fatal("identity not derived from auth")
	}
	second := request(h, "/v1/checkouts", body, "test-token-123456", "request-001")
	if second.Code != 200 || !strings.Contains(second.Body.String(), `"replayed":true`) {
		t.Fatalf("%d %s", second.Code, second.Body)
	}
	r := httptest.NewRequest("GET", first.Header().Get("Location"), nil)
	r.Header.Set("Authorization", "Bearer test-token-123456")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("get order: %d %s", w.Code, w.Body)
	}
}

func TestHTTPValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body, token, key string
		status                 int
	}{
		{"auth", `{}`, "wrong", "request-001", 401},
		{"customer-injection", `{"customer_id":"bob","items":[]}`, "test-token-123456", "request-001", 400},
		{"trailing-json", `{"items":[]} {}`, "test-token-123456", "request-001", 400},
		{"empty-cart", `{"items":[]}`, "test-token-123456", "request-001", 422},
		{"missing-key", `{"items":[{"product_id":"a","quantity":1}]}`, "test-token-123456", "", 422},
		{"oversize", `{"coupon":"` + strings.Repeat("x", 1<<20) + `"}`, "test-token-123456", "request-001", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(handler(), "/v1/checkouts", tc.body, tc.token, tc.key)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body)
			}
			if !strings.Contains(w.Body.String(), `"request_id"`) {
				t.Fatal("missing structured error")
			}
		})
	}
}
