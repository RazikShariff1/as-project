// Package services defines the business-logic contracts the HTTP handlers depend on.
package services

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/communication-svc/filters"
	"github.com/zopping/communication-svc/models"
)

// Email accepts emails for delivery and runs the delivery worker.
type Email interface {
	// Send renders the email and queues it; delivery happens in the background.
	Send(ctx *gofr.Context, req *models.SendRequest) (models.SendResponse, error)
	Get(ctx *gofr.Context, id int64) (models.Email, error)
	List(ctx *gofr.Context, f *filters.EmailFilter) (models.EmailList, error)
	// Deliver sends the emails that are due, retrying failures with a back-off. It returns how many were sent and how many
	// failed this time; the error is only for a problem with the queue itself.
	Deliver(ctx *gofr.Context) (sent, failed int, err error)
}

// Template manages the stored email templates.
type Template interface {
	List(ctx *gofr.Context, f *filters.TemplateFilter) (models.TemplateList, error)
	Get(ctx *gofr.Context, id int64) (models.Template, error)
	Create(ctx *gofr.Context, req *models.TemplateRequest) (models.Template, error)
	Update(ctx *gofr.Context, id int64, req *models.TemplateRequest) (models.Template, error)
	Delete(ctx *gofr.Context, id int64) error
}
