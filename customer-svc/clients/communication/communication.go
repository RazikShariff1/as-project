// Package communication is customer-svc's client for communication-svc, which sends the emails. Sending is best effort: a
// shopper's registration never waits on, or fails because of, an email.
package communication

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"gofr.dev/pkg/gofr"
)

const timeout = 3 * time.Second

// Notifier tells shoppers things by email.
type Notifier interface {
	// Welcome greets a new shopper. It never returns an error: a failure is logged.
	Welcome(ctx *gofr.Context, name, email string)
}

// HTTP implements Notifier over a GoFr HTTP service registered under Service.
type HTTP struct {
	Service string
}

// New returns a notifier for the HTTP service registered under the given name.
func New(service string) *HTTP { return &HTTP{Service: service} }

// Welcome posts a "customer/welcome" email to communication-svc.
func (h *HTTP) Welcome(ctx *gofr.Context, name, email string) {
	body, err := json.Marshal(map[string]any{"to": email, "type": "customer/welcome", "data": map[string]any{"name": name}})
	if err != nil {
		return
	}

	// Its own deadline, so a slow communication-svc cannot hold the registration; the request's cancellation is kept.
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := ctx.GetHTTPService(h.Service).Post(callCtx, "email", nil, body)
	if err != nil {
		ctx.Errorf("welcome email to customer not queued: %v", err)

		return
	}

	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		ctx.Errorf("welcome email to customer not queued: communication-svc answered %d", resp.StatusCode)
	}
}

// Nop is the Notifier used when no communication-svc is configured.
type Nop struct{}

// Welcome does nothing.
func (Nop) Welcome(*gofr.Context, string, string) {}
