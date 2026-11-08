// Package role holds the business rules for roles.
package role

import (
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/httpx"
	"github.com/zopping/account-svc/filters"
	"github.com/zopping/account-svc/models"
	"github.com/zopping/account-svc/services"
	"github.com/zopping/account-svc/stores"
)

var _ services.Role = (*Service)(nil)

// Service implements services.Role.
type Service struct {
	store stores.Role
}

// New returns a role service.
func New(store stores.Role) *Service { return &Service{store: store} }

// normalise validates the request and removes duplicate endpoint ids.
func normalise(req *models.RoleRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return httpx.InvalidParam("name")
	}

	seen := make(map[int64]bool, len(req.EndpointIDs))
	ids := make([]int64, 0, len(req.EndpointIDs))

	for _, id := range req.EndpointIDs {
		if id < 1 {
			return httpx.InvalidParam("endpointIds")
		}

		if !seen[id] {
			seen[id] = true

			ids = append(ids, id)
		}
	}

	req.EndpointIDs = ids

	return nil
}

// Create validates and creates a role.
func (s *Service) Create(ctx *gofr.Context, req *models.RoleRequest) (models.Role, error) {
	if err := normalise(req); err != nil {
		return models.Role{}, err
	}

	r, err := s.store.Create(ctx, req)
	if err != nil {
		return models.Role{}, httpx.FromStore(err, "role", req.Name)
	}

	return r, nil
}

// Get returns one role.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Role, error) {
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return models.Role{}, httpx.FromStoreID(err, "id", id)
	}

	return r, nil
}

// List returns one page of roles.
func (s *Service) List(ctx *gofr.Context, f *filters.RoleFilter) (models.RoleList, error) {
	roles, count, err := s.store.List(ctx, f)
	if err != nil {
		return models.RoleList{}, httpx.FromStore(err, "filter", "")
	}

	return models.RoleList{Roles: roles, Count: count, Offset: f.Offset, Limit: f.Limit}, nil
}

// Update validates and replaces a role.
func (s *Service) Update(ctx *gofr.Context, id int64, req *models.RoleRequest) (models.Role, error) {
	if err := normalise(req); err != nil {
		return models.Role{}, err
	}

	r, err := s.store.Update(ctx, id, req)
	if err != nil {
		return models.Role{}, httpx.FromStoreID(err, "id", id)
	}

	return r, nil
}

// Delete soft-deletes a role and removes it from every user and endpoint.
func (s *Service) Delete(ctx *gofr.Context, id int64) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return httpx.FromStoreID(err, "id", id)
	}

	return nil
}
