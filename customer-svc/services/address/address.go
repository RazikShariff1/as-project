// Package address holds the business rules for a customer's saved addresses.
package address

import (
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/customer-svc/common/httpx"
	"github.com/zopping/customer-svc/models"
	"github.com/zopping/customer-svc/services"
	"github.com/zopping/customer-svc/stores"
)

const (
	maxAddresses = 20
	maxFieldLen  = 200
)

var _ services.Address = (*Service)(nil)

// Service implements services.Address.
type Service struct {
	addresses stores.Address
}

// New returns an address service.
func New(addresses stores.Address) *Service { return &Service{addresses: addresses} }

// clean trims every field and checks the required ones.
func clean(req *models.AddressRequest) (*models.AddressRequest, error) {
	out := *req
	fields := map[string]*string{
		"label": &out.Label, "recipient": &out.Recipient, "phone": &out.Phone, "line1": &out.Line1, "line2": &out.Line2,
		"city": &out.City, "state": &out.State, "postalCode": &out.PostalCode, "country": &out.Country,
	}

	for name, f := range fields {
		*f = strings.TrimSpace(*f)
		if len(*f) > maxFieldLen {
			return nil, httpx.InvalidParam(name)
		}
	}

	switch {
	case out.Recipient == "":
		return nil, httpx.InvalidParam("recipient")
	case out.Line1 == "":
		return nil, httpx.InvalidParam("line1")
	case out.City == "":
		return nil, httpx.InvalidParam("city")
	case out.PostalCode == "":
		return nil, httpx.InvalidParam("postalCode")
	}

	return &out, nil
}

// List returns the customer's addresses, default first.
func (s *Service) List(ctx *gofr.Context, customerID int64) ([]models.Address, error) {
	a, err := s.addresses.List(ctx, customerID)

	return a, httpx.FromStore(err, "address", "")
}

// Get returns one of the customer's addresses.
func (s *Service) Get(ctx *gofr.Context, customerID, id int64) (models.Address, error) {
	a, err := s.addresses.Get(ctx, customerID, id)

	return a, httpx.FromStoreID(err, "id", id)
}

// Create saves an address, up to a cap per customer.
func (s *Service) Create(ctx *gofr.Context, customerID int64, req *models.AddressRequest) (models.Address, error) {
	r, err := clean(req)
	if err != nil {
		return models.Address{}, err
	}

	existing, err := s.addresses.List(ctx, customerID)
	if err != nil {
		return models.Address{}, httpx.FromStore(err, "address", "")
	}

	if len(existing) >= maxAddresses {
		return models.Address{}, httpx.InvalidParam("address limit reached")
	}

	a, err := s.addresses.Create(ctx, customerID, r)

	return a, httpx.FromStore(err, "address", "")
}

// Update replaces one of the customer's addresses.
func (s *Service) Update(ctx *gofr.Context, customerID, id int64, req *models.AddressRequest) (models.Address, error) {
	r, err := clean(req)
	if err != nil {
		return models.Address{}, err
	}

	a, err := s.addresses.Update(ctx, customerID, id, r)

	return a, httpx.FromStoreID(err, "id", id)
}

// Delete removes one of the customer's addresses.
func (s *Service) Delete(ctx *gofr.Context, customerID, id int64) error {
	return httpx.FromStoreID(s.addresses.Delete(ctx, customerID, id), "id", id)
}
