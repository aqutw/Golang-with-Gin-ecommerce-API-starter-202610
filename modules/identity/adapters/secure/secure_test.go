package secure

import (
	"context"
	"strings"
	"testing"
	"time"

	"example.com/ecommerce/identity/domain"
	"github.com/golang-jwt/jwt/v5"
)

func TestArgon2SaltVerificationAndUntrustedHashParameters(t *testing.T) {
	p := NewPasswords()
	ctx := context.Background()
	a, err := p.Hash(ctx, "a-long-password-123")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Hash(ctx, "a-long-password-123")
	if err != nil || a == b {
		t.Fatal("passwords not uniquely salted")
	}
	ok, err := p.Verify(ctx, "a-long-password-123", a)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	ok, err = p.Verify(ctx, "wrong-password", a)
	if err != nil || ok {
		t.Fatal("wrong password accepted")
	}
	if _, err := p.Verify(ctx, "password", strings.Replace(a, "m=19456", "m=999999999", 1)); err == nil {
		t.Fatal("unbounded hash parameters accepted")
	}
}

func TestJWTRejectsWrongSignatureIssuerAudienceAlgorithmTypeAndExpiry(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	j, err := NewJWT(key, "issuer", "client", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	session := domain.Session{ID: "sid", UserID: "uid", ExpiresAt: now.Add(time.Hour)}
	good, _, err := j.Issue(session, now)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := j.Verify(good, now); err != nil || p.UserID != "uid" || p.SessionID != "sid" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := j.Verify(good, now.Add(15*time.Minute)); err == nil {
		t.Fatal("expired JWT accepted")
	}
	for _, tc := range []struct {
		name, issuer, audience, typ string
		method                      jwt.SigningMethod
		key                         []byte
		noExpiry                    bool
	}{
		{"signature", "issuer", "client", "at+jwt", jwt.SigningMethodHS256, []byte(strings.Repeat("x", 32)), false},
		{"issuer", "wrong", "client", "at+jwt", jwt.SigningMethodHS256, key, false},
		{"audience", "issuer", "wrong", "at+jwt", jwt.SigningMethodHS256, key, false},
		{"algorithm", "issuer", "client", "at+jwt", jwt.SigningMethodHS384, key, false},
		{"type", "issuer", "client", "JWT", jwt.SigningMethodHS256, key, false},
		{"missing-expiry", "issuer", "client", "at+jwt", jwt.SigningMethodHS256, key, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := claims{SessionID: "sid", RegisteredClaims: jwt.RegisteredClaims{Issuer: tc.issuer, Subject: "uid", Audience: jwt.ClaimStrings{tc.audience}, IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ID: "jti"}}
			if !tc.noExpiry {
				c.ExpiresAt = jwt.NewNumericDate(now.Add(time.Minute))
			}
			token := jwt.NewWithClaims(tc.method, c)
			token.Header["typ"] = tc.typ
			value, err := token.SignedString(tc.key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := j.Verify(value, now); err == nil {
				t.Fatal("invalid JWT accepted")
			}
		})
	}
}
