// Package events publishes JSON events through GoFr's configured pub/sub backend.
package events

import (
	"encoding/json"
	"errors"

	"gofr.dev/pkg/gofr"
)

// Publisher publishes an event to a topic. It is an interface so services can be tested without a broker.
type Publisher interface {
	Publish(ctx *gofr.Context, topic string, payload any) error
}

// ErrNotConfigured means no pub/sub backend is configured (PUBSUB_BACKEND is unset).
var ErrNotConfigured = errors.New("pub/sub is not configured: set PUBSUB_BACKEND")

// GoFr publishes through the pub/sub backend GoFr was configured with.
type GoFr struct{}

// Publish marshals the payload to JSON and publishes it.
func (GoFr) Publish(ctx *gofr.Context, topic string, payload any) error {
	p := ctx.GetPublisher()
	if p == nil {
		return ErrNotConfigured
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return p.Publish(ctx, topic, b)
}
