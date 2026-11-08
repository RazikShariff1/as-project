// Package auth holds the HTTP handlers for /auth, the surface the gateway and clients use.
package auth

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/authctx"
	"github.com/zopping/account-svc/common/httpx"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/services"
)

// Handler serves the auth endpoints.
type Handler struct {
	service services.Auth
}

// New returns an auth handler.
func New(service services.Auth) *Handler { return &Handler{service: service} }

// Login exchanges email and password for a session token (201: GoFr answers a data-bearing POST that way).
func (h *Handler) Login(ctx *gofr.Context) (any, error) {
	var req models.LoginRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Login(ctx, &req))
}

// Me returns who the bearer token belongs to.
func (h *Handler) Me(ctx *gofr.Context) (any, error) {
	return httpx.Respond(h.service.Authenticate(ctx, authctx.Token(ctx)))
}

// Check is the gateway's per-request call: it validates the bearer token and checks the user may call `method` on
// `path` of `service`. 401 means log in again, 403 means not allowed, 200 returns the user and their attributes.
func (h *Handler) Check(ctx *gofr.Context) (any, error) {
	return httpx.Respond(h.service.Authorize(ctx, authctx.Token(ctx), ctx.Param("service"), ctx.Param("method"), ctx.Param("path")))
}

// Logout revokes the bearer token's session. Repeating it is safe.
func (h *Handler) Logout(ctx *gofr.Context) (any, error) {
	return nil, h.service.Logout(ctx, authctx.Token(ctx))
}
