// Package stores defines the persistence contracts the services depend on.
package stores

import (
	"gofr.dev/pkg/gofr"

	"github.com/zopping/communication-svc/filters"
	"github.com/zopping/communication-svc/models"
)

// Email persists the outbox and the log of what became of each email.
type Email interface {
	Enqueue(ctx *gofr.Context, o *models.Outbound) (models.Email, error)
	// Claim takes up to limit emails that are due (or whose worker died), marks them sending, counts the attempt and holds
	// them for lockSeconds. Concurrent workers never get the same email.
	Claim(ctx *gofr.Context, limit, lockSeconds int) ([]models.Outbound, error)
	MarkSent(ctx *gofr.Context, id int64) error
	// MarkRetry puts the email back in the queue to be tried again after delaySeconds.
	MarkRetry(ctx *gofr.Context, id int64, delaySeconds int, reason string) error
	MarkFailed(ctx *gofr.Context, id int64, reason string) error
	Get(ctx *gofr.Context, id int64) (models.Email, error)
	List(ctx *gofr.Context, f *filters.EmailFilter) ([]models.Email, int, error)
}

// Template persists email templates.
type Template interface {
	// Find returns the template for a name and language; a missing one is apperr.ErrNotFound.
	Find(ctx *gofr.Context, name, language string) (models.Template, error)
	Get(ctx *gofr.Context, id int64) (models.Template, error)
	List(ctx *gofr.Context, f *filters.TemplateFilter) ([]models.Template, int, error)
	Create(ctx *gofr.Context, req *models.TemplateRequest) (models.Template, error)
	Update(ctx *gofr.Context, id int64, req *models.TemplateRequest) (models.Template, error)
	Delete(ctx *gofr.Context, id int64) error
}
