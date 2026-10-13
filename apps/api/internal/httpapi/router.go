package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/ecommerce/commerce/application/catalog"
	"example.com/ecommerce/commerce/application/checkout"
	"example.com/ecommerce/commerce/domain/fault"
	identity "example.com/ecommerce/identity/application"
	identitydomain "example.com/ecommerce/identity/domain"
	"example.com/ecommerce/platform/httpx"
	"github.com/gin-gonic/gin"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"
)

type Dependencies struct {
	Catalog  catalog.Service
	Checkout checkout.Service
	Identity *identity.Service
	Logger   *slog.Logger
	Ready    func(context.Context) error
	Storage  string
}

func New(d Dependencies) http.Handler {
	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		panic(err)
	}
	r.HandleMethodNotAllowed = true
	r.NoRoute(func(c *gin.Context) { fail(c, 404, "not_found", "route not found") })
	r.NoMethod(func(c *gin.Context) { fail(c, 405, "method_not_allowed", "method not allowed") })
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if d.Ready != nil {
			if err := d.Ready(ctx); err != nil {
				d.Logger.Error("storage readiness failed", "error", err)
				fail(c, 503, "storage_unavailable", "storage unavailable")
				return
			}
		}
		c.JSON(200, gin.H{"status": "ready", "storage": d.Storage})
	})
	v1 := r.Group("/v1")
	registerAuth(v1, d)
	v1.GET("/products", func(c *gin.Context) {
		products, err := d.Catalog.List(c.Request.Context())
		if err != nil {
			respondError(c, d.Logger, err)
			return
		}
		c.JSON(200, gin.H{"products": products})
	})
	private := v1.Group("")
	private.Use(func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") || token == "" {
			fail(c, 401, "unauthorized", "valid bearer token required")
			return
		}
		u, err := d.Identity.Authenticate(c.Request.Context(), token)
		if err != nil {
			respondError(c, d.Logger, err)
			return
		}
		c.Set("subject", u.ID)
		c.Set("user", u)
		c.Next()
	})
	private.GET("/auth/me", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		u, _ := c.Get("user")
		c.JSON(200, u)
	})
	private.POST("/quotes", func(c *gin.Context) {
		var cmd checkout.Command
		if !decode(c, &cmd) {
			return
		}
		totals, err := d.Checkout.Quote(c.Request.Context(), cmd)
		if err != nil {
			respondError(c, d.Logger, err)
			return
		}
		c.JSON(200, totals)
	})
	private.POST("/checkouts", func(c *gin.Context) {
		var cmd checkout.Command
		if !decode(c, &cmd) {
			return
		}
		result, err := d.Checkout.Place(c.Request.Context(), c.GetString("subject"), c.GetHeader("Idempotency-Key"), cmd)
		if err != nil {
			respondError(c, d.Logger, err)
			return
		}
		status := 201
		if result.Replayed {
			status = 200
		}
		c.Header("Location", "/v1/orders/"+result.Order.ID)
		c.JSON(status, result)
	})
	private.GET("/orders/:id", func(c *gin.Context) {
		o, err := d.Checkout.GetOrder(c.Request.Context(), c.GetString("subject"), c.Param("id"))
		if err != nil {
			respondError(c, d.Logger, err)
			return
		}
		c.JSON(200, o)
	})
	return httpx.Middleware(d.Logger, r)
}

func decode(c *gin.Context, target any) bool {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(c, 415, "unsupported_media_type", "application/json required")
		return false
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(target)
	if err == nil {
		var extra any
		if trailing := decoder.Decode(&extra); trailing != io.EOF {
			if trailing == nil {
				err = errors.New("multiple JSON values")
			} else {
				err = trailing
			}
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(c, 413, "body_too_large", "request body exceeds 1 MiB")
		} else {
			fail(c, 400, "invalid_json", "invalid JSON or unknown fields")
		}
		return false
	}
	return true
}
func fail(c *gin.Context, status int, code, message string) {
	httpx.Error(c.Writer, c.Request, status, code, message)
	c.Abort()
}
func respondError(c *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, identitydomain.Unauthorized):
		fail(c, 401, "unauthorized", "invalid credentials or token")
	case errors.Is(err, identitydomain.Invalid):
		fail(c, 422, "invalid_auth_request", "valid email and password of 12+ characters required")
	case errors.Is(err, identitydomain.EmailTaken):
		fail(c, 409, "email_taken", "email already registered")
	case errors.Is(err, fault.Invalid):
		fail(c, 422, "invalid_request", err.Error())
	case errors.Is(err, fault.NotFound):
		fail(c, 404, "not_found", err.Error())
	case errors.Is(err, fault.OutOfStock):
		fail(c, 409, "out_of_stock", err.Error())
	case errors.Is(err, fault.IdempotencyConflict):
		fail(c, 409, "idempotency_conflict", err.Error())
	default:
		logger.Error("application error", "request_id", httpx.RequestID(c.Request.Context()), "error", err)
		fail(c, 500, "internal_error", "internal server error")
	}
}
