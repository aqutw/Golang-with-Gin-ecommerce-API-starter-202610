package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/ecommerce/identity/domain"
)

func TestAuthHTTPRotationLogoutAndOwnership(t *testing.T) {
	h, _, _ := handler(t)
	register := request(h, "/v1/auth/register", `{"email":"bob@example.test","password":"bob-password-123456"}`, "", "")
	if register.Code != 201 || strings.Contains(register.Body.String(), "password") || register.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("register: %d %s", register.Code, register.Body)
	}
	login := request(h, "/v1/auth/login", `{"email":"bob@example.test","password":"bob-password-123456"}`, "", "")
	var first domain.Pair
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	if err := json.Unmarshal(login.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.AccessToken == "" || first.RefreshToken == "" || first.TokenType != "Bearer" {
		t.Fatal("missing tokens")
	}
	refresh := request(h, "/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, first.RefreshToken), "", "")
	var rotated domain.Pair
	if refresh.Code != 200 {
		t.Fatalf("refresh: %d %s", refresh.Code, refresh.Body)
	}
	if err := json.Unmarshal(refresh.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.RefreshToken == first.RefreshToken {
		t.Fatal("refresh did not rotate")
	}
	r := httptest.NewRequest("GET", "/v1/auth/me", nil)
	r.Header.Set("Authorization", "Bearer "+rotated.AccessToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "bob@example.test") || strings.Contains(w.Body.String(), "password") {
		t.Fatalf("me: %d %s", w.Code, w.Body)
	}
	logout := request(h, "/v1/auth/logout", fmt.Sprintf(`{"refresh_token":%q}`, rotated.RefreshToken), "", "")
	if logout.Code != 204 {
		t.Fatalf("logout: %d %s", logout.Code, logout.Body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("revoked access accepted: %d", w.Code)
	}
	refresh = request(h, "/v1/auth/refresh", fmt.Sprintf(`{"refresh_token":%q}`, rotated.RefreshToken), "", "")
	if refresh.Code != 401 {
		t.Fatal("revoked refresh accepted")
	}
}

func TestOtherUserCannotReadAnOrder(t *testing.T) {
	h, aliceToken, _ := handler(t)
	order := request(h, "/v1/checkouts", `{"items":[{"product_id":"a","quantity":1}]}`, aliceToken, "request-001")
	if order.Code != 201 {
		t.Fatalf("%d %s", order.Code, order.Body)
	}
	if w := request(h, "/v1/auth/register", `{"email":"bob@example.test","password":"bob-password-123456"}`, "", ""); w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	login := request(h, "/v1/auth/login", `{"email":"bob@example.test","password":"bob-password-123456"}`, "", "")
	var pair domain.Pair
	if err := json.Unmarshal(login.Body.Bytes(), &pair); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", order.Header().Get("Location"), nil)
	r.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("other user's order exposed: %d %s", w.Code, w.Body)
	}
}

func TestAuthThrottleCannotBeBypassedWithForwardedFor(t *testing.T) {
	h, _, _ := handler(t)
	for i := 0; i < 31; i++ {
		r := httptest.NewRequest("POST", "/v1/auth/refresh", strings.NewReader(`{"refresh_token":"invalid"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", i))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 401
		if i == 30 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt=%d got=%d want=%d", i, w.Code, want)
		}
	}
}
