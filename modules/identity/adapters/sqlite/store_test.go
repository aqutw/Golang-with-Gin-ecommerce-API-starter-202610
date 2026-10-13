package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/ecommerce/identity/domain"
)

func TestCrossConnectionRotationPersistsRevocation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "auth.db")
	a, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u := domain.User{ID: "uid", Email: "user@example.test", PasswordHash: "encoded-only"}
	if err := a.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	session := domain.Session{ID: "sid", UserID: u.ID, ExpiresAt: now.Add(time.Hour)}
	oldHash := strings.Repeat("a", 64)
	if err := a.CreateSession(ctx, session, oldHash, now); err != nil {
		t.Fatal(err)
	}
	var winners atomic.Int64
	var wg sync.WaitGroup
	for i, store := range []*Store{a, b} {
		wg.Add(1)
		go func(i int, store *Store) {
			defer wg.Done()
			_, err := store.Rotate(ctx, oldHash, strings.Repeat(string(rune('b'+i)), 64), now)
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, domain.Unauthorized) {
				t.Error(err)
			}
		}(i, store)
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("winners=%d", winners.Load())
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	b, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := b.ValidateSession(ctx, domain.Principal{UserID: u.ID, SessionID: session.ID}, now); !errors.Is(err, domain.Unauthorized) {
		t.Fatal("revocation did not survive reopen", err)
	}
}

func TestRotationFailureRollsBackConsumptionAndDisabledUsersAreRejected(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser(ctx, domain.User{ID: "uid", Email: "user@example.test", PasswordHash: "hash"}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	session := domain.Session{ID: "sid", UserID: "uid", ExpiresAt: now.Add(time.Hour)}
	oldHash := strings.Repeat("a", 64)
	if err := s.CreateSession(ctx, session, oldHash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rotate(ctx, oldHash, oldHash, now); err == nil {
		t.Fatal("duplicate replacement accepted")
	}
	if _, err := s.Rotate(ctx, oldHash, strings.Repeat("b", 64), now); err != nil {
		t.Fatal("failed rotation consumed token", err)
	}
	if _, err := s.db.Exec("UPDATE auth_users SET disabled=1 WHERE id='uid'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateSession(ctx, domain.Principal{UserID: "uid", SessionID: "sid"}, now); !errors.Is(err, domain.Unauthorized) {
		t.Fatal(err)
	}
	if _, err := s.Rotate(ctx, strings.Repeat("b", 64), strings.Repeat("c", 64), now); !errors.Is(err, domain.Unauthorized) {
		t.Fatal(err)
	}
}
