package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"example.com/ecommerce/identity/domain"
	"example.com/ecommerce/identity/ports"
)

type Service struct {
	store      ports.Store
	passwords  ports.Passwords
	tokens     ports.AccessTokens
	now        func() time.Time
	newID      func() string
	refreshTTL time.Duration
	dummyHash  string
}

func New(ctx context.Context, store ports.Store, passwords ports.Passwords, tokens ports.AccessTokens, now func() time.Time, newID func() string, refreshTTL time.Duration) (*Service, error) {
	if store == nil || passwords == nil || tokens == nil || now == nil || newID == nil || refreshTTL < time.Hour {
		return nil, fmt.Errorf("invalid identity dependencies")
	}
	dummy, err := passwords.Hash(ctx, "dummy-password-not-a-user-credential")
	if err != nil {
		return nil, err
	}
	return &Service{store: store, passwords: passwords, tokens: tokens, now: now, newID: newID, refreshTTL: refreshTTL, dummyHash: dummy}, nil
}

func emailAddress(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || len(email) > 254 || parsed.Address != email {
		return "", domain.Invalid
	}
	return email, nil
}

func validPassword(password string) bool {
	return utf8.ValidString(password) && utf8.RuneCountInString(password) >= 12 && len(password) <= 1024
}

func (s *Service) Register(ctx context.Context, email, password string) (domain.User, error) {
	email, err := emailAddress(email)
	if err != nil || !validPassword(password) {
		return domain.User{}, domain.Invalid
	}
	hash, err := s.passwords.Hash(ctx, password)
	if err != nil {
		return domain.User{}, err
	}
	u := domain.User{ID: s.newID(), Email: email, PasswordHash: hash}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return domain.User{}, err
	}
	u.PasswordHash = ""
	return u, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (domain.Pair, error) {
	email, err := emailAddress(email)
	if err != nil || len(password) > 1024 {
		return domain.Pair{}, domain.Unauthorized
	}
	u, lookupErr := s.store.UserByEmail(ctx, email)
	if lookupErr != nil && !errors.Is(lookupErr, domain.NotFound) {
		return domain.Pair{}, lookupErr
	}
	hash := u.PasswordHash
	if lookupErr != nil {
		hash = s.dummyHash
	}
	valid, err := s.passwords.Verify(ctx, password, hash)
	if err != nil {
		return domain.Pair{}, err
	}
	if !valid || lookupErr != nil || u.Disabled {
		return domain.Pair{}, domain.Unauthorized
	}
	now := s.now().UTC()
	session := domain.Session{ID: s.newID(), UserID: u.ID, ExpiresAt: now.Add(s.refreshTTL)}
	refresh, hash, err := refreshToken()
	if err != nil {
		return domain.Pair{}, err
	}
	pair, err := s.pair(session, now, refresh)
	if err != nil {
		return domain.Pair{}, err
	}
	if err := s.store.CreateSession(ctx, session, hash, now); err != nil {
		return domain.Pair{}, err
	}
	return pair, nil
}

func (s *Service) pair(session domain.Session, now time.Time, refresh string) (domain.Pair, error) {
	access, expiry, err := s.tokens.Issue(session, now)
	if err != nil {
		return domain.Pair{}, err
	}
	return domain.Pair{AccessToken: access, RefreshToken: refresh, TokenType: "Bearer", ExpiresIn: expiry, RefreshExpiresAt: session.ExpiresAt}, nil
}

func refreshToken() (string, string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", "", err
	}
	value := base64.RawURLEncoding.EncodeToString(secret[:])
	hash, err := tokenHash(value)
	return value, hash, err
}

func tokenHash(value string) (string, error) {
	if len(value) != 43 {
		return "", domain.Unauthorized
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return "", domain.Unauthorized
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) Refresh(ctx context.Context, old string) (domain.Pair, error) {
	oldHash, err := tokenHash(old)
	if err != nil {
		return domain.Pair{}, err
	}
	refresh, hash, err := refreshToken()
	if err != nil {
		return domain.Pair{}, err
	}
	now := s.now().UTC()
	session, err := s.store.Rotate(ctx, oldHash, hash, now)
	if err != nil {
		return domain.Pair{}, err
	}
	// If response delivery / signing fails after rotation, the old token stays
	// consumed: clients must log in again, not retry a consumed refresh token.
	return s.pair(session, now, refresh)
}

func (s *Service) Authenticate(ctx context.Context, access string) (domain.User, error) {
	if len(access) > 8192 {
		return domain.User{}, domain.Unauthorized
	}
	now := s.now().UTC()
	p, err := s.tokens.Verify(access, now)
	if err != nil {
		return domain.User{}, err
	}
	u, err := s.store.ValidateSession(ctx, p, now)
	u.PasswordHash = ""
	return u, err
}

func (s *Service) Logout(ctx context.Context, refresh string) error {
	hash, err := tokenHash(refresh)
	if err != nil {
		return domain.Unauthorized
	}
	return s.store.RevokeRefresh(ctx, hash, s.now().UTC())
}
