// Package order is the read side of orders: finding a page of them and reading one, with who may see what.
package order

import (
	"golang.org/x/sync/errgroup"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/clients/catalogue"
	"github.com/zopping/order-svc/clients/customer"
	"github.com/zopping/order-svc/common/apperr"
	"github.com/zopping/order-svc/common/caller"
	"github.com/zopping/order-svc/common/httpx"
	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
	"github.com/zopping/order-svc/services"
	"github.com/zopping/order-svc/stores"
	"github.com/zopping/order-svc/utils"
)

type service struct {
	store     stores.Order
	customers customer.Client
	catalogue catalogue.Client
}

// New returns the order read service. A nil customers or catalogue client leaves that part out of the answers.
func New(store stores.Order, customers customer.Client, catalogue catalogue.Client) services.Order {
	return &service{store: store, customers: customers, catalogue: catalogue}
}

// Find returns a page of orders and how many match; the page and the count are read at the same time. Each order then gets its
// customer (and item names) from the other services, all at the same time.
func (s *service) Find(ctx *gofr.Context, filter *filters.OrderDetails, pagination *models.Pagination) (*models.OrderList, error) {
	if id, restricted := caller.Customer(ctx); restricted {
		filter.CustomerID = id
	}

	var (
		orders []*models.Order
		count  int
		g      errgroup.Group
	)

	g.Go(func() (err error) {
		orders, err = s.store.Index(ctx, filter, pagination.Limit, pagination.Offset)

		return err
	})

	g.Go(func() (err error) {
		count, err = s.store.Count(ctx, filter)

		return err
	})

	if err := g.Wait(); err != nil {
		return nil, httpx.FromStore(err, "order", "")
	}

	details := make([]*models.OrderDetails, 0, len(orders))
	for _, o := range orders {
		details = append(details, &models.OrderDetails{Order: o})
	}

	s.appendExtraFields(ctx, details, filter)

	return &models.OrderList{Orders: details, Count: count, Offset: pagination.Offset, Limit: pagination.Limit}, nil
}

// Read returns one order, or a 404 if it is not the caller's.
func (s *service) Read(ctx *gofr.Context, filter *filters.OrderDetails) (*models.OrderDetails, error) {
	ref := filter.ReferenceNumbers[0]

	o, err := s.store.Retrieve(ctx, ref)
	if err != nil {
		return nil, httpx.FromStoreID(err, "order", ref)
	}

	if !utils.Visible(ctx, o) {
		return nil, httpx.FromStoreID(apperr.ErrNotFound, "order", ref)
	}

	details := []*models.OrderDetails{{Order: o}}

	s.appendExtraFields(ctx, details, filter)

	return details[0], nil
}
