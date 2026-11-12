// Package email holds the HTTP handlers for /email.
package email

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/communication-svc/common/httpx"
	"github.com/zopping/communication-svc/filters"
	"github.com/zopping/communication-svc/models"
	"github.com/zopping/communication-svc/services"
)

// Handler serves the email endpoints.
type Handler struct {
	service services.Email
}

// New returns an email handler.
func New(service services.Email) *Handler { return &Handler{service: service} }

// Create renders an email and queues it for delivery (201).
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.SendRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Send(ctx, &req))
}

// Index lists emails, newest first: ?status=&type=&to=&limit=&page=.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.EmailFilter{
		Status: ctx.Param("status"), Type: ctx.Param("type"), To: ctx.Param("to"), Limit: limit, Offset: offset,
	}))
}

// Read returns one email.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}
