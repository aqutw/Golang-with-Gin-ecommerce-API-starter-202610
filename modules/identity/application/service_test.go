package application_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/ecommerce/identity/adapters/secure"
	"example.com/ecommerce/identity/adapters/sqlite"
	"example.com/ecommerce/identity/application"
	"example.com/ecommerce/identity/domain"
)

type fixture struct {
	service *application.Service
	store   *sqlite.Store
	now     *time.Time
	path    string
}

func setup(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "auth.db")
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0).UTC()
	tokens, err := secure.NewJWT([]byte(strings.Repeat("k", 32)), "issuer", "client", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var ids atomic.Int64
	service, err := application.New(ctx, store, secure.NewPasswords(), tokens, func() time.Time { return now }, func() string { return fmt.Sprint(ids.Add(1)) }, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{service, store, &now, path}
}

func login(t *testing.T, f fixture) domain.Pair {
	t.Helper()
	ctx := context.Background()
	if _, err := f.service.Register(ctx, "alice@example.test", "a-long-password-123"); err != nil {
		t.Fatal(err)
	}
	pair, err := f.service.Login(ctx, " ALICE@EXAMPLE.TEST ", "a-long-password-123")
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

func TestRegistrationLoginRotationLogoutAndDeviceIsolation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first := login(t, f)
	if _, err := f.service.Register(ctx, "ALICE@example.test", "a-long-password-123"); !errors.Is(err, domain.EmailTaken) {
		t.Fatal(err)
	}
	if _, err := f.service.Login(ctx, "alice@example.test", "wrong-password"); !errors.Is(err, domain.Unauthorized) {
		t.Fatal(err)
	}
	if _, err := f.service.Login(ctx, "missing@example.test", "a-long-password-123"); !errors.Is(err, domain.Unauthorized) {
		t.Fatal(err)
	}
	other, err := f.service.Login(ctx, "alice@example.test", "a-long-password-123")
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := f.service.Refresh(ctx, first.RefreshToken)
	if err != nil || rotated.RefreshToken == first.RefreshToken {
		t.Fatalf("rotation: %v", err)
	}
	if _, err := f.service.Authenticate(ctx, rotated.AccessToken); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Logout(ctx, rotated.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Logout(ctx, rotated.RefreshToken); err != nil {
		t.Fatal(err)
	}
	for _, access := range []string{first.AccessToken, rotated.AccessToken} {
		if _, err := f.service.Authenticate(ctx, access); !errors.Is(err, domain.Unauthorized) {
			t.Fatal("logout left access valid", err)
		}
	}
	if _, err := f.service.Refresh(ctx, rotated.RefreshToken); !errors.Is(err, domain.Unauthorized) {
		t.Fatal("logout left refresh valid", err)
	}
	if _, err := f.service.Authenticate(ctx, other.AccessToken); err != nil {
		t.Fatal("logout revoked another device", err)
	}
}

func TestRefreshReuseRevokesTheWholeFamily(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first := login(t, f)
	rotated, err := f.service.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Refresh(ctx, first.RefreshToken); !errors.Is(err, domain.Unauthorized) {
		t.Fatal("reuse accepted", err)
	}
	if _, err := f.service.Authenticate(ctx, rotated.AccessToken); !errors.Is(err, domain.Unauthorized) {
		t.Fatal("reuse revocation rolled back", err)
	}
	if _, err := f.service.Refresh(ctx, rotated.RefreshToken); !errors.Is(err, domain.Unauthorized) {
		t.Fatal("descendant remained valid", err)
	}
}

func TestConcurrentRefreshRevokesFamily(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first := login(t, f)
	var wg sync.WaitGroup
	var winners atomic.Int64
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.service.Refresh(ctx, first.RefreshToken)
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, domain.Unauthorized) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("refresh consumed %d times", winners.Load())
	}
	if _, err := f.service.Authenticate(ctx, first.AccessToken); !errors.Is(err, domain.Unauthorized) {
		t.Fatal("family not revoked after race", err)
	}
}

func TestExpiryAndAbsoluteRefreshLifetime(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first := login(t, f)
	*f.now = f.now.Add(16 * time.Minute)
	if _, err := f.service.Authenticate(ctx, first.AccessToken); !errors.Is(err, domain.Unauthorized) {
		t.Fatal("expired access accepted", err)
	}
	rotated, err := f.service.Refresh(ctx, first.RefreshToken)
	if err != nil || !rotated.RefreshExpiresAt.Equal(first.RefreshExpiresAt) {
		t.Fatalf("sliding expiry: %v", err)
	}
	*f.now = first.RefreshExpiresAt
	if _, err := f.service.Refresh(ctx, rotated.RefreshToken); !errors.Is(err, domain.Unauthorized) {
		t.Fatal("expired refresh accepted", err)
	}
}

func TestInvalidCredentialsAndRefreshFormat(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, tc := range []struct{ email, password string }{{"bad", "a-long-password-123"}, {"Display <a@example.test>", "a-long-password-123"}, {"a@example.test", "short"}} {
		if _, err := f.service.Register(ctx, tc.email, tc.password); !errors.Is(err, domain.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := f.service.Refresh(ctx, "invalid"); !errors.Is(err, domain.Unauthorized) {
		t.Fatal(err)
	}
}
