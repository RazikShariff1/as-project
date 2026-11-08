// Package endpoint holds the business rules for endpoints.
package endpoint

import (
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/httpx"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/services"
	"github.com/zopping/account-svc/stores"
)

var _ services.Endpoint = (*Service)(nil)

// Service implements services.Endpoint.
type Service struct {
	store stores.Endpoint
}

// New returns an endpoint service.
func New(store stores.Endpoint) *Service { return &Service{store: store} }

// NormalisePath validates an endpoint path pattern and returns it without a trailing slash. A path starts with "/",
// has no query string or spaces, and each segment is either literal text or one {name} placeholder.
func NormalisePath(raw string) (string, bool) {
	p := strings.TrimSpace(raw)
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "? #") {
		return "", false
	}

	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}

	for _, seg := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		if !strings.ContainsAny(seg, "{}") {
			continue
		}

		name := strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
		if !strings.HasPrefix(seg, "{") || !strings.HasSuffix(seg, "}") || name == "" || strings.ContainsAny(name, "{}") {
			return "", false
		}
	}

	return p, true
}

// normalise validates the request, upper-casing the method and tidying the path.
func normalise(req *models.EndpointRequest) error {
	req.Service = strings.TrimSpace(req.Service)
	if req.Service == "" {
		return httpx.InvalidParam("service")
	}

	req.Method = strings.ToUpper(strings.TrimSpace(req.Method))
	if !models.ValidMethod(req.Method) {
		return httpx.InvalidParam("method")
	}

	path, ok := NormalisePath(req.Path)
	if !ok {
		return httpx.InvalidParam("path")
	}

	req.Path = path

	return nil
}

// Create validates and registers an endpoint.
func (s *Service) Create(ctx *gofr.Context, req *models.EndpointRequest) (models.Endpoint, error) {
	if err := normalise(req); err != nil {
		return models.Endpoint{}, err
	}

	e, err := s.store.Create(ctx, req)
	if err != nil {
		return models.Endpoint{}, httpx.FromStore(err, "endpoint", req.Path)
	}

	return e, nil
}

// Get returns one endpoint.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Endpoint, error) {
	e, err := s.store.Get(ctx, id)
	if err != nil {
		return models.Endpoint{}, httpx.FromStoreID(err, "id", id)
	}

	return e, nil
}

// List returns one page of endpoints.
func (s *Service) List(ctx *gofr.Context, f *filters.EndpointFilter) (models.EndpointList, error) {
	f.Method = strings.ToUpper(f.Method)
	if f.Method != "" && !models.ValidMethod(f.Method) {
		return models.EndpointList{}, httpx.InvalidParam("method")
	}

	endpoints, count, err := s.store.List(ctx, f)
	if err != nil {
		return models.EndpointList{}, httpx.FromStore(err, "filter", "")
	}

	return models.EndpointList{Endpoints: endpoints, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Update validates and replaces an endpoint.
func (s *Service) Update(ctx *gofr.Context, id int64, req *models.EndpointRequest) (models.Endpoint, error) {
	if err := normalise(req); err != nil {
		return models.Endpoint{}, err
	}

	e, err := s.store.Update(ctx, id, req)
	if err != nil {
		return models.Endpoint{}, httpx.FromStoreID(err, "id", id)
	}

	return e, nil
}

// Delete soft-deletes an endpoint and removes it from every role.
func (s *Service) Delete(ctx *gofr.Context, id int64) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	return nil
}
