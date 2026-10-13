package ports

import (
	"context"
	"example.com/ecommerce/identity/domain"
	"time"
)

type Passwords interface {
	Hash(context.Context, string) (string, error)
	Verify(context.Context, string, string) (bool, error)
}

type AccessTokens interface {
	Issue(domain.Session, time.Time) (string, int64, error)
	Verify(string, time.Time) (domain.Principal, error)
}

type Store interface {
	CreateUser(context.Context, domain.User) error
	UserByEmail(context.Context, string) (domain.User, error)
	CreateSession(context.Context, domain.Session, string, time.Time) error
	// Rotate atomically consumes the old hash, inserts the replacement, and
	// returns the session. Reusing a consumed hash commits session revocation
	// before returning Unauthorized. The session has an absolute expiry.
	Rotate(context.Context, string, string, time.Time) (domain.Session, error)
	ValidateSession(context.Context, domain.Principal, time.Time) (domain.User, error)
	RevokeRefresh(context.Context, string, time.Time) error
}
