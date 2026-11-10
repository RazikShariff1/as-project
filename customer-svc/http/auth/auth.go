// Package auth holds the HTTP handlers for /auth, the surface the gateway and clients use.
package auth

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/common/authctx"
	"github.com/zopping/customer-svc/common/httpx"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/services"
)

// Handler serves the auth endpoints.
type Handler struct {
	service services.Auth
}

// New returns an auth handler.
func New(service services.Auth) *Handler { return &Handler{service: service} }

// Register creates a customer and signs them in (201).
func (h *Handler) Register(ctx *gofr.Context) (any, error) {
	var req models.RegisterRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Register(ctx, &req))
}

// Login exchanges email and password for a session token (201: GoFr answers a data-bearing POST that way).
func (h *Handler) Login(ctx *gofr.Context) (any, error) {
	var req models.LoginRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Login(ctx, &req))
}

// Me returns who the bearer token belongs to. The gateway calls it for every /store request to authenticate the
// caller, and clients call it to read their own profile.
func (h *Handler) Me(ctx *gofr.Context) (any, error) {
	return httpx.Respond(h.service.Authenticate(ctx, authctx.Token(ctx)))
}

// Logout revokes the bearer token's session. Repeating it is safe.
func (h *Handler) Logout(ctx *gofr.Context) (any, error) {
	return nil, h.service.Logout(ctx, authctx.Token(ctx))
}
