// Package template holds the business rules for email templates.
package template

import (
	"regexp"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/communication-svc/common/httpx"
	"github.com/zopping/communication-svc/filters"
	"github.com/zopping/communication-svc/models"
	"github.com/zopping/communication-svc/services"
	"github.com/zopping/communication-svc/stores"
)

const (
	defaultLanguage = "en"
	maxName         = 200
	maxSubject      = 500
	maxBody         = 1 << 20
)

var (
	_         services.Template = (*Service)(nil)
	nameRegex                   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./-]*$`)
	langRegex                   = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})?$`)
)

// Service implements services.Template.
type Service struct {
	templates stores.Template
}

// New returns a template service.
func New(templates stores.Template) *Service { return &Service{templates: templates} }

// clean trims and checks a request, filling the defaults.
func clean(req *models.TemplateRequest) (*models.TemplateRequest, error) {
	out := *req
	out.Name = strings.TrimSpace(out.Name)
	out.Language = strings.ToLower(strings.TrimSpace(out.Language))
	out.Subject = strings.TrimSpace(out.Subject)
	out.Status = strings.ToUpper(strings.TrimSpace(out.Status))

	if out.Language == "" {
		out.Language = defaultLanguage
	}

	if out.Status == "" {
		out.Status = models.TemplateEnabled
	}

	switch {
	case !nameRegex.MatchString(out.Name) || len(out.Name) > maxName || out.Name == models.NoTemplate:
		return nil, httpx.InvalidParam("name")
	case !langRegex.MatchString(out.Language):
		return nil, httpx.InvalidParam("language")
	case out.Subject == "" || len(out.Subject) > maxSubject || strings.ContainsAny(out.Subject, "\r\n"):
		return nil, httpx.InvalidParam("subject")
	case strings.TrimSpace(out.Body) == "" || len(out.Body) > maxBody:
		return nil, httpx.InvalidParam("body")
	case out.Status != models.TemplateEnabled && out.Status != models.TemplateDisabled:
		return nil, httpx.InvalidParam("status")
	}

	return &out, nil
}

// List returns one page of templates.
func (s *Service) List(ctx *gofr.Context, f *filters.TemplateFilter) (models.TemplateList, error) {
	templates, count, err := s.templates.List(ctx, f)
	if err != nil {
		return models.TemplateList{}, httpx.FromStore(err, "template", "")
	}

	return models.TemplateList{Templates: templates, Count: count}, nil
}

// Get returns one template.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Template, error) {
	t, err := s.templates.Get(ctx, id)
	if err != nil {
		return models.Template{}, httpx.FromStoreID(err, "template", id)
	}

	return t, nil
}

// Create stores a template; one per name and language.
func (s *Service) Create(ctx *gofr.Context, req *models.TemplateRequest) (models.Template, error) {
	r, err := clean(req)
	if err != nil {
		return models.Template{}, err
	}

	t, err := s.templates.Create(ctx, r)
	if err != nil {
		return models.Template{}, httpx.FromStore(err, "template", r.Name)
	}

	return t, nil
}

// Update replaces a template.
func (s *Service) Update(ctx *gofr.Context, id int64, req *models.TemplateRequest) (models.Template, error) {
	r, err := clean(req)
	if err != nil {
		return models.Template{}, err
	}

	t, err := s.templates.Update(ctx, id, r)
	if err != nil {
		return models.Template{}, httpx.FromStoreID(err, "template", id)
	}

	return t, nil
}

// Delete removes a template.
func (s *Service) Delete(ctx *gofr.Context, id int64) error {
	if err := s.templates.Delete(ctx, id); err != nil {
		return httpx.FromStoreID(err, "template", id)
	}

	return nil
}
