package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"example.com/ecommerce/identity/domain"
	"example.com/ecommerce/identity/ports"
	driver "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Store struct{ db *sql.DB }

var _ ports.Store = (*Store)(nil)

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("identity database path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return nil, err
	}
	params := url.Values{}
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)"} {
		params.Add("_pragma", pragma)
	}
	params.Set("_txlock", "immediate")
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: params.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) Migrate(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, schema); return err })
}

func (s *Store) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateUser(ctx context.Context, u domain.User) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO auth_users(id, email, password_hash) VALUES (?, ?, ?)", u.ID, u.Email, u.PasswordHash)
	var constraint *driver.Error
	if errors.As(err, &constraint) && constraint.Code() == 2067 {
		return domain.EmailTaken
	}
	return err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	var u domain.User
	err := s.db.QueryRowContext(ctx, "SELECT id, email, password_hash, disabled FROM auth_users WHERE email = ?", email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.NotFound
	}
	return u, err
}

func (s *Store) CreateSession(ctx context.Context, session domain.Session, hash string, now time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		// A user might be disabled between password verification and this write.
		var disabled bool
		if err := tx.QueryRowContext(ctx, "SELECT disabled FROM auth_users WHERE id = ?", session.UserID).Scan(&disabled); err != nil {
			return err
		}
		if disabled {
			return domain.Unauthorized
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO auth_sessions(id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)", session.ID, session.UserID, now.Unix(), session.ExpiresAt.Unix()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO auth_refresh_tokens(token_hash, session_id, created_at) VALUES (?, ?, ?)", hash, session.ID, now.Unix())
		return err
	})
}

func (s *Store) Rotate(ctx context.Context, oldHash, newHash string, now time.Time) (domain.Session, error) {
	var session domain.Session
	denied := false
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var expires int64
		var used, revoked sql.NullInt64
		var disabled bool
		err := tx.QueryRowContext(ctx, `SELECT s.id, s.user_id, s.expires_at, s.revoked_at, r.used_at, u.disabled
			FROM auth_refresh_tokens r JOIN auth_sessions s ON s.id = r.session_id JOIN auth_users u ON u.id = s.user_id
			WHERE r.token_hash = ?`, oldHash).Scan(&session.ID, &session.UserID, &expires, &revoked, &used, &disabled)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Unauthorized
		}
		if err != nil {
			return err
		}
		session.ExpiresAt = time.Unix(expires, 0).UTC()
		if used.Valid {
			// Return nil from the transaction callback so revocation COMMITs.
			// Returning Unauthorized here would roll the security action back.
			_, err := tx.ExecContext(ctx, "UPDATE auth_sessions SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?", now.Unix(), session.ID)
			denied = true
			return err
		}
		if revoked.Valid || disabled || expires <= now.Unix() {
			return domain.Unauthorized
		}
		if _, err := tx.ExecContext(ctx, "UPDATE auth_refresh_tokens SET used_at = ? WHERE token_hash = ?", now.Unix(), oldHash); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO auth_refresh_tokens(token_hash, session_id, created_at) VALUES (?, ?, ?)", newHash, session.ID, now.Unix())
		return err
	})
	if err != nil {
		return domain.Session{}, err
	}
	if denied {
		return domain.Session{}, domain.Unauthorized
	}
	return session, nil
}

func (s *Store) ValidateSession(ctx context.Context, p domain.Principal, now time.Time) (domain.User, error) {
	var u domain.User
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.email FROM auth_sessions s JOIN auth_users u ON u.id = s.user_id
		WHERE s.id = ? AND s.user_id = ? AND s.revoked_at IS NULL AND s.expires_at > ? AND u.disabled = 0`, p.SessionID, p.UserID, now.Unix()).Scan(&u.ID, &u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.Unauthorized
	}
	return u, err
}

// Logout is idempotent and can use a known older refresh token in the family.
func (s *Store) RevokeRefresh(ctx context.Context, hash string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at = COALESCE(revoked_at, ?)
		WHERE id = (SELECT session_id FROM auth_refresh_tokens WHERE token_hash = ?)`, now.Unix(), hash)
	return err
}
