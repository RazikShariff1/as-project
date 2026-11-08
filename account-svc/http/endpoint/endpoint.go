// Package endpoint holds the HTTP handlers for /endpoint.
package endpoint

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/httpx"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/services"
)

// Handler serves the endpoint endpoints.
type Handler struct {
	service services.Endpoint
}

// New returns an endpoint handler.
func New(service services.Endpoint) *Handler { return &Handler{service: service} }

// Index lists endpoints. Query: service, method, limit, page, offset.
func (h *Handler) Index(ctx *gofr.Context) (any, error) {
	limit, offset, err := httpx.Paginate(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.List(ctx, &filters.EndpointFilter{
		Service: ctx.Param("service"), Method: ctx.Param("method"), Limit: limit, Offset: offset,
	}))
}

// Read returns one endpoint.
func (h *Handler) Read(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return httpx.Respond(h.service.Get(ctx, id))
}

// Create registers an endpoint.
func (h *Handler) Create(ctx *gofr.Context) (any, error) {
	var req models.EndpointRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Create(ctx, &req))
}

// Update replaces an endpoint.
func (h *Handler) Update(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	var req models.EndpointRequest
	if err := ctx.Bind(&req); err != nil {
		return nil, httpx.InvalidParam("body")
	}

	return httpx.Respond(h.service.Update(ctx, id, &req))
}

// Delete soft-deletes an endpoint and removes it from every role.
func (h *Handler) Delete(ctx *gofr.Context) (any, error) {
	id, err := httpx.PathID(ctx)
	if err != nil {
		return nil, err
	}

	return nil, h.service.Delete(ctx, id)
}
