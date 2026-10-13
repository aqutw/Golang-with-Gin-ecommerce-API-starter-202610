package secure

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"example.com/ecommerce/identity/domain"
	"github.com/golang-jwt/jwt/v5"
)

type JWT struct {
	key              []byte
	issuer, audience string
	ttl              time.Duration
}
type claims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

func NewJWT(key []byte, issuer, audience string, ttl time.Duration) (*JWT, error) {
	if len(key) < 32 || issuer == "" || audience == "" || ttl < time.Minute || ttl > time.Hour {
		return nil, fmt.Errorf("invalid JWT configuration")
	}
	return &JWT{key: append([]byte(nil), key...), issuer: issuer, audience: audience, ttl: ttl}, nil
}

func (j *JWT) Issue(session domain.Session, now time.Time) (string, int64, error) {
	expires := now.Add(j.ttl)
	if session.ExpiresAt.Before(expires) {
		expires = session.ExpiresAt
	}
	if !expires.After(now) || session.ID == "" || session.UserID == "" {
		return "", 0, domain.Unauthorized
	}
	var identifier [16]byte
	if _, err := rand.Read(identifier[:]); err != nil {
		return "", 0, err
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{SessionID: session.ID, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: j.issuer, Subject: session.UserID, Audience: jwt.ClaimStrings{j.audience},
		ExpiresAt: jwt.NewNumericDate(expires), IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ID: hex.EncodeToString(identifier[:]),
	}})
	token.Header["typ"] = "at+jwt"
	signed, err := token.SignedString(j.key)
	return signed, int64(expires.Sub(now) / time.Second), err
}

func (j *JWT) Verify(value string, now time.Time) (domain.Principal, error) {
	var c claims
	token, err := jwt.ParseWithClaims(value, &c, func(token *jwt.Token) (any, error) { return j.key, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithIssuedAt(),
		jwt.WithIssuer(j.issuer), jwt.WithAudience(j.audience), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil || !token.Valid || token.Header["typ"] != "at+jwt" || c.Subject == "" || c.SessionID == "" || c.ID == "" || c.IssuedAt == nil || c.NotBefore == nil {
		return domain.Principal{}, domain.Unauthorized
	}
	return domain.Principal{UserID: c.Subject, SessionID: c.SessionID}, nil
}
