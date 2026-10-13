package secure

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Fixed, versioned Argon2id parameters: 19 MiB, 2 passes, one lane.
// At most two operations execute concurrently; hashing cannot exceed 38 MiB.
type Passwords struct{ slots chan struct{} }

func NewPasswords() *Passwords { return &Passwords{slots: make(chan struct{}, 2)} }

func (p *Passwords) acquire(ctx context.Context) error {
	select {
	case p.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Passwords) Hash(ctx context.Context, password string) (string, error) {
	if err := p.acquire(ctx); err != nil {
		return "", err
	}
	defer func() { <-p.slots }()
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt[:], 2, 19*1024, 1, 32)
	return "$argon2id$v=19$m=19456,t=2,p=1$" + base64.RawStdEncoding.EncodeToString(salt[:]) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

func (p *Passwords) Verify(ctx context.Context, password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=19456,t=2,p=1" {
		return false, fmt.Errorf("unsupported password hash")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false, fmt.Errorf("invalid password salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return false, fmt.Errorf("invalid password hash")
	}
	if err := p.acquire(ctx); err != nil {
		return false, err
	}
	defer func() { <-p.slots }()
	actual := argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
