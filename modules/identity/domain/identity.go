package domain

import (
	"errors"
	"time"
)

var (
	Invalid      = errors.New("invalid authentication request")
	Unauthorized = errors.New("invalid credentials or token")
	NotFound     = errors.New("identity not found")
	EmailTaken   = errors.New("email already registered")
)

type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	Disabled     bool   `json:"-"`
}

type Session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
}

type Principal struct{ UserID, SessionID string }

type Pair struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	TokenType        string    `json:"token_type"`
	ExpiresIn        int64     `json:"expires_in"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}
