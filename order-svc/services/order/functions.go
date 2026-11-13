package order

import (
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/order-svc/filters"
	"github.com/zopping/order-svc/models"
)

// maxParallel bounds the calls one request makes to a sibling service at once.
const maxParallel = 8

// appendExtraFields adds what other services know to each order: its customer and the name of each item. The two are read at the
// same time, and so are the calls within each. Every lookup is best effort: a failure is logged and leaves that part out, it never
// fails the read.
func (s *service) appendExtraFields(ctx *gofr.Context, orders []*models.OrderDetails, filter *filters.OrderDetails) {
	var (
		wg                   sync.WaitGroup
		customerIDToCustomer map[int64]*models.Customer
		variantIDToInfo      map[int64]models.ItemInfo
	)

	if !filter.ExcludeCustomerDetail && s.customers != nil {
		wg.Add(1)

		go func() {
			defer wg.Done()

			customerIDToCustomer = s.getCustomerIDToCustomer(ctx, uniqueCustomerIDs(orders))
		}()
	}

	if filter.IncludeItemNames && s.catalogue != nil {
		wg.Add(1)

		go func() {
			defer wg.Done()

			variantIDToInfo = s.getVariantIDToInfo(ctx, uniqueVariantIDs(orders))
		}()
	}

	wg.Wait()

	for _, o := range orders {
		o.Customer = customerIDToCustomer[o.CustomerID]
		o.Items = make([]*models.OrderItemDetails, 0, len(o.Order.Items))

		for _, it := range o.Order.Items {
			o.Items = append(o.Items, itemDetails(it, variantIDToInfo[it.VariantID]))
		}
	}
}

// getCustomerIDToCustomer reads each customer from customer-svc, in parallel.
func (s *service) getCustomerIDToCustomer(ctx *gofr.Context, ids []int64) map[int64]*models.Customer {
	var (
		mu     sync.Mutex
		g      errgroup.Group
		result = make(map[int64]*models.Customer, len(ids))
	)

	g.SetLimit(maxParallel)

	for _, id := range ids {
		g.Go(func() error {
			c, err := s.customers.Customer(ctx, id)
			if err != nil {
				ctx.Errorf("order read: customer %d: %v", id, err)

				return nil
			}

			mu.Lock()
			result[id] = c
			mu.Unlock()

			return nil
		})
	}

	_ = g.Wait()

	return result
}

// getVariantIDToInfo reads each variant from catalogue-svc in parallel, then the products they belong to in parallel. The name is the
// product's, then " - " and the variant's if the product has several. A variant that could not be read is left out.
func (s *service) getVariantIDToInfo(ctx *gofr.Context, variantIDs []int64) map[int64]models.ItemInfo {
	var (
		mu       sync.Mutex
		g        errgroup.Group
		variants = make(map[int64]*models.CatalogueVariant, len(variantIDs))
		products = make(map[int64]*models.CatalogueProduct)
	)

	g.SetLimit(maxParallel)

	for _, id := range variantIDs {
		g.Go(func() error {
			v, err := s.catalogue.Variant(ctx, id)
			if err != nil {
				ctx.Errorf("order read: variant %d: %v", id, err)

				return nil
			}

			mu.Lock()
			variants[id] = v
			mu.Unlock()

			return nil
		})
	}

	_ = g.Wait()

	productIDs := make(map[int64]struct{})
	for _, v := range variants {
		productIDs[v.ProductID] = struct{}{}
	}

	for id := range productIDs {
		g.Go(func() error {
			p, err := s.catalogue.Product(ctx, id)
			if err != nil {
				ctx.Errorf("order read: product %d: %v", id, err)

				return nil
			}

			mu.Lock()
			products[id] = p
			mu.Unlock()

			return nil
		})
	}

	_ = g.Wait()

	info := make(map[int64]models.ItemInfo, len(variants))

	for id, v := range variants {
		p := products[v.ProductID]
		if p == nil {
			p = &models.CatalogueProduct{}
		}

		it := models.ItemInfo{Name: joinName(p.Name, v.Name, p.HasVariants), SKU: v.ClientItemID}

		if len(v.Images) > 0 {
			it.ImageURL = v.Images[0]
		} else if len(p.Images) > 0 {
			it.ImageURL = p.Images[0]
		}

		info[id] = it
	}

	return info
}

// itemDetails is one order line with its catalogue details; a line without any is returned as it is.
func itemDetails(it models.OrderItem, info models.ItemInfo) *models.OrderItemDetails {
	return &models.OrderItemDetails{OrderItem: it, Name: info.Name, SKU: info.SKU, ImageURL: info.ImageURL}
}

// joinName is the product's name, with the variant's after it when the product has several variants.
func joinName(product, variant string, hasVariants bool) string {
	product, variant = strings.TrimSpace(product), strings.TrimSpace(variant)

	switch {
	case product == "":
		return variant
	case variant == "" || !hasVariants:
		return product
	default:
		return product + " - " + variant
	}
}

func uniqueCustomerIDs(orders []*models.OrderDetails) []int64 {
	seen := make(map[int64]struct{}, len(orders))
	ids := make([]int64, 0, len(orders))

	for _, o := range orders {
		if _, dup := seen[o.CustomerID]; dup || o.CustomerID == 0 {
			continue
		}

		seen[o.CustomerID] = struct{}{}
		ids = append(ids, o.CustomerID)
	}

	return ids
}

func uniqueVariantIDs(orders []*models.OrderDetails) []int64 {
	seen := make(map[int64]struct{})

	var ids []int64

	for _, o := range orders {
		for _, it := range o.Order.Items {
			if _, dup := seen[it.VariantID]; !dup {
				seen[it.VariantID] = struct{}{}
				ids = append(ids, it.VariantID)
			}
		}
	}

	return ids
}
