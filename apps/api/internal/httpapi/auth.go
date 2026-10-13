package httpapi

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

func registerAuth(v1 *gin.RouterGroup, d Dependencies) {
	auth := v1.Group("/auth")
	auth.Use(authThrottle())
	auth.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Header("Pragma", "no-cache"); c.Next() })
	type credentials struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	type refreshBody struct {
		RefreshToken string `json:"refresh_token"`
	}
	auth.POST("/register", func(c *gin.Context) {
		var body credentials
		if !decode(c, &body) {
			return
		}
		u, err := d.Identity.Register(c.Request.Context(), body.Email, body.Password)
		if err != nil {
			respondError(c, d.Logger, err)
			return
		}
		c.JSON(201, u)
	})
	auth.POST("/login", func(c *gin.Context) {
		var body credentials
		if !decode(c, &body) {
			return
		}
		pair, err := d.Identity.Login(c.Request.Context(), body.Email, body.Password)
		if err != nil {
			respondError(c, d.Logger, err)
			return
		}
		c.JSON(200, pair)
	})
	auth.POST("/refresh", func(c *gin.Context) {
		var body refreshBody
		if !decode(c, &body) {
			return
		}
		pair, err := d.Identity.Refresh(c.Request.Context(), body.RefreshToken)
		if err != nil {
			respondError(c, d.Logger, err)
			return
		}
		c.JSON(200, pair)
	})
	auth.POST("/logout", func(c *gin.Context) {
		var body refreshBody
		if !decode(c, &body) {
			return
		}
		if err := d.Identity.Logout(c.Request.Context(), body.RefreshToken); err != nil {
			respondError(c, d.Logger, err)
			return
		}
		c.Status(204)
	})
}

// A bounded, process-local throttle for credential / refresh endpoints.
// Gin trusts no proxy by default, so spoofed X-Forwarded-For cannot bypass it.
func authThrottle() gin.HandlerFunc {
	type bucket struct {
		start time.Time
		count int
	}
	var mu sync.Mutex
	buckets := map[string]bucket{}
	return func(c *gin.Context) {
		now := time.Now()
		ip := c.ClientIP()
		mu.Lock()
		for key, b := range buckets {
			if now.Sub(b.start) >= time.Minute {
				delete(buckets, key)
			}
		}
		b, exists := buckets[ip]
		if !exists {
			b.start = now
		}
		denied := b.count >= 30 || (!exists && len(buckets) >= 4096)
		if !denied {
			b.count++
			buckets[ip] = b
		}
		mu.Unlock()
		if denied {
			c.Header("Retry-After", "60")
			fail(c, 429, "rate_limited", "too many authentication requests")
			return
		}
		c.Next()
	}
}
