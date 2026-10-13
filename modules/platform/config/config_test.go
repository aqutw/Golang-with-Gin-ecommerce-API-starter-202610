package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestJWTConfigurationRejectsMissingOrWeakKeysAndInvalidTTL(t *testing.T) {
	for _, tc := range []struct {
		name, secret, access, refresh string
		valid                         bool
	}{
		{"missing-key", "", "15m", "720h", false},
		{"invalid-base64", "bad", "15m", "720h", false},
		{"weak-key", base64.StdEncoding.EncodeToString([]byte("short")), "15m", "720h", false},
		{"short-access", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "1s", "720h", false},
		{"long-access", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "2h", "720h", false},
		{"refresh-too-short", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "1h", "1h", false},
		{"valid", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "15m", "720h", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", "development")
			t.Setenv("JWT_SECRET", tc.secret)
			t.Setenv("ACCESS_TOKEN_TTL", tc.access)
			t.Setenv("REFRESH_TOKEN_TTL", tc.refresh)
			_, err := Load()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
