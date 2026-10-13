// Package sqlite implements durable commerce storage using database/sql and a
// pure Go driver. Business calculations remain in domain/application packages.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"example.com/ecommerce/commerce/ports"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql seed.sql
var files embed.FS

type Store struct{ db *sql.DB }

var _ ports.Store = (*Store)(nil)

func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("SQLite path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return nil, err
	}
	// Encode the filename; a DB_PATH containing '?' must not become driver SQL.
	params := url.Values{}
	params.Add("_pragma", "foreign_keys(1)")
	params.Add("_pragma", "busy_timeout(5000)")
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "synchronous(FULL)")
	params.Set("_txlock", "immediate")
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: params.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Intentionally serialize this starter's transactions. SQLite also enforces
	// BEGIN IMMEDIATE between separate Store instances / processes.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Migrate applies each embedded migration once, within one atomic transaction.
// Filenames are ordered NNN_description.sql. Existing files must not be edited.
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := files.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
			return err
		}
		for _, entry := range entries {
			version, err := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
			if err != nil {
				return fmt.Errorf("migration filename %s: %w", entry.Name(), err)
			}
			var exists int
			err = tx.QueryRowContext(ctx, "SELECT 1 FROM schema_migrations WHERE version = ?", version).Scan(&exists)
			if err == nil {
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			source, err := files.ReadFile("migrations/" + entry.Name())
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, string(source)); err != nil {
				return fmt.Errorf("migration %s: %w", entry.Name(), err)
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)", version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		return nil
	})
}

// Seed is explicitly demo data. Re-running it inserts missing records only.
func (s *Store) Seed(ctx context.Context) error {
	source, err := files.ReadFile("seed.sql")
	if err != nil {
		return err
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, string(source))
		return err
	})
}

func (s *Store) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // Also rolls back if the callback panics.
	if err := fn(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) View(ctx context.Context, fn func(ports.Reader) error) error {
	return s.transaction(ctx, func(tx *sql.Tx) error { return fn(&reader{ctx: ctx, tx: tx}) })
}

func (s *Store) Update(ctx context.Context, fn func(ports.Tx) error) error {
	return s.transaction(ctx, func(tx *sql.Tx) error { return fn(&writer{reader: reader{ctx: ctx, tx: tx}}) })
}
