package httpapi_test

import (
	"context"
	"encoding/json"
	"example.com/ecommerce/api/internal/httpapi"
	"example.com/ecommerce/commerce/adapters/memory"
	catalogapp "example.com/ecommerce/commerce/application/catalog"
	"example.com/ecommerce/commerce/application/checkout"
	"example.com/ecommerce/commerce/domain/catalog"
	"example.com/ecommerce/identity/adapters/secure"
	identitysqlite "example.com/ecommerce/identity/adapters/sqlite"
	identity "example.com/ecommerce/identity/application"
	"example.com/ecommerce/platform/httpx"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func handler(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	ctx := context.Background()
	identities, err := identitysqlite.Open(ctx, filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = identities.Close() })
	if err := identities.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tokens, err := secure.NewJWT([]byte(strings.Repeat("k", 32)), "test-issuer", "test-client", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := identity.New(ctx, identities, secure.NewPasswords(), tokens, time.Now, httpx.NewID, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	user, err := auth.Register(ctx, "alice@example.test", "test-password-123456")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := auth.Login(ctx, user.Email, "test-password-123456")
	if err != nil {
		t.Fatal(err)
	}
	store := memory.New([]catalog.Product{{ID: "a", Name: "A", Currency: "USD", PriceMinor: 1000, Active: true}}, map[string]int{"a": 2})
	return httpapi.New(httpapi.Dependencies{Catalog: catalogapp.Service{Store: store}, Checkout: checkout.Service{Store: store, NewID: httpx.NewID, Now: time.Now}, Identity: auth, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Storage: "memory"}), pair.AccessToken, user.ID
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
	h, token, subject := handler(t)
	body := `{"items":[{"product_id":"a","quantity":1}]}`
	first := request(h, "/v1/checkouts", body, token, "request-001")
	if first.Code != 201 || first.Header().Get("Location") == "" || first.Header().Get("X-Request-ID") == "" {
		t.Fatalf("%d %s", first.Code, first.Body)
	}
	var result checkout.Result
	if err := json.Unmarshal(first.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Order.CustomerID != subject {
		t.Fatal("identity not derived from auth")
	}
	second := request(h, "/v1/checkouts", body, token, "request-001")
	if second.Code != 200 || !strings.Contains(second.Body.String(), `"replayed":true`) {
		t.Fatalf("%d %s", second.Code, second.Body)
	}
	r := httptest.NewRequest("GET", first.Header().Get("Location"), nil)
	r.Header.Set("Authorization", "Bearer "+token)
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
			h, token, _ := handler(t)
			if tc.token != "test-token-123456" {
				token = tc.token
			}
			w := request(h, "/v1/checkouts", tc.body, token, tc.key)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body)
			}
			if !strings.Contains(w.Body.String(), `"request_id"`) {
				t.Fatal("missing structured error")
			}
		})
	}
}
