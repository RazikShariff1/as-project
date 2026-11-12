// Package lookup reads the few facts the order emails need from other services: a shopper's name and email from customer-svc, and
// product names from catalogue-svc. Everything here is best effort; the emails fall back to what the order itself knows.
package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"
)

const (
	timeout = 3 * time.Second
	maxBody = 1 << 20
)

// Contact is who a shopper is and where to email them.
type Contact struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Lookup is what the order emails need from other services.
type Lookup interface {
	// Customer returns the shopper's name and email.
	Customer(ctx *gofr.Context, id int64) (Contact, error)
	// ItemNames returns "Product - Variant" for each variant it could find; the rest are left out.
	ItemNames(ctx *gofr.Context, variantIDs []int64) map[int64]string
}

// HTTP implements Lookup over GoFr HTTP services registered under the given names.
type HTTP struct {
	Customers string
	Catalogue string
}

// get reads GET path from a service and decodes the {"data": ...} envelope into out.
func get(ctx *gofr.Context, service, path string, out any) error {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := ctx.GetHTTPService(service).Get(callCtx, path, nil)
	if err != nil {
		return fmt.Errorf("%s %s: %w", service, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: status %d", service, path, resp.StatusCode)
	}

	var body struct {
		Data json.RawMessage `json:"data"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return fmt.Errorf("%s %s: unreadable response: %w", service, path, err)
	}

	return json.Unmarshal(body.Data, out)
}

// Customer calls GET /customer/{id} on customer-svc.
func (h *HTTP) Customer(ctx *gofr.Context, id int64) (Contact, error) {
	var c Contact

	err := get(ctx, h.Customers, fmt.Sprintf("customer/%d", id), &c)

	return c, err
}

// ItemNames calls GET /variant/{id} and GET /product/{id} on catalogue-svc, once per distinct variant and product.
func (h *HTTP) ItemNames(ctx *gofr.Context, variantIDs []int64) map[int64]string {
	names := make(map[int64]string, len(variantIDs))
	products := map[int64]string{}

	for _, id := range variantIDs {
		if _, done := names[id]; done {
			continue
		}

		var v struct {
			Name      string `json:"name"`
			ProductID int64  `json:"productId"`
		}

		if err := get(ctx, h.Catalogue, fmt.Sprintf("variant/%d", id), &v); err != nil {
			ctx.Warnf("order email: item name for variant %d: %v", id, err)

			continue
		}

		product, seen := products[v.ProductID]
		if !seen {
			var p struct {
				Name string `json:"name"`
			}

			if err := get(ctx, h.Catalogue, fmt.Sprintf("product/%d", v.ProductID), &p); err != nil {
				ctx.Warnf("order email: product name for product %d: %v", v.ProductID, err)
			}

			product = p.Name
			products[v.ProductID] = product
		}

		names[id] = strings.Join(nonEmpty(product, v.Name), " - ")
	}

	return names
}

func nonEmpty(parts ...string) []string {
	out := parts[:0:0]

	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}

	return out
}
