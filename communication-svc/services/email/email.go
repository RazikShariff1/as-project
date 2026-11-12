// Package email holds the business rules for sending: validate, render, queue, then deliver in the background.
package email

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/communication-svc/common/apperr"
	"github.com/zopping/communication-svc/common/httpx"
	"github.com/zopping/communication-svc/filters"
	"github.com/zopping/communication-svc/models"
	"github.com/zopping/communication-svc/render"
	"github.com/zopping/communication-svc/sender"
	"github.com/zopping/communication-svc/services"
	"github.com/zopping/communication-svc/stores"
)

const (
	defaultLanguage = "en"
	maxRecipients   = 50
	maxSubjectLen   = 500
	maxBodyLen      = 1 << 20
	batchSize       = 10
	claimSeconds    = 120 // a worker that dies mid-send leaves its emails to be re-claimed after this
	sendTimeout     = 90 * time.Second
	baseBackoff     = 30 // seconds; doubles with every attempt
	maxBackoff      = 3600
	maxReason       = 500
	skippedMessage  = "Template disabled; skipping this email"
	queuedMessage   = "Email will be delivered shortly"
)

var _ services.Email = (*Service)(nil)

// Service implements services.Email.
type Service struct {
	emails      stores.Email
	templates   stores.Template
	sender      sender.Sender
	maxAttempts int
}

// New returns an email service. An email is tried up to maxAttempts times.
func New(emails stores.Email, templates stores.Template, s sender.Sender, maxAttempts int) *Service {
	return &Service{emails: emails, templates: templates, sender: s, maxAttempts: maxAttempts}
}

// addresses parses a comma-separated list into plain addresses (display names dropped), refusing anything malformed.
func addresses(raw, param string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	list, err := mail.ParseAddressList(raw)
	if err != nil || len(list) > maxRecipients {
		return nil, httpx.InvalidParam(param)
	}

	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.Address
	}

	return out, nil
}

// oneLine strips line breaks, which would otherwise let a subject add mail headers.
func oneLine(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(s))
}

// content returns the subject and body for the request: its own for "no-template", otherwise the stored template rendered
// with the data. skipped is true for a disabled template.
func (s *Service) content(ctx *gofr.Context, req *models.SendRequest) (subject, body string, skipped bool, err error) {
	if req.Type == models.NoTemplate {
		subject, _ = req.Data["subject"].(string)
		body, _ = req.Data["body"].(string)

		if oneLine(subject) == "" {
			return "", "", false, httpx.BadRequest("Invalid data: Invalid subject")
		}

		if strings.TrimSpace(body) == "" {
			return "", "", false, httpx.BadRequest("Invalid data: Invalid body")
		}

		return oneLine(subject), body, false, nil
	}

	tpl, err := s.templates.Find(ctx, req.Type, req.Language)
	if errors.Is(err, apperr.ErrNotFound) && req.Language != defaultLanguage {
		tpl, err = s.templates.Find(ctx, req.Type, defaultLanguage) // fall back to English, as the reference does
	}

	if errors.Is(err, apperr.ErrNotFound) {
		return "", "", false, httpx.BadRequest("Invalid type: No template found")
	}

	if err != nil {
		return "", "", false, err
	}

	if tpl.Status == models.TemplateDisabled {
		return "", "", true, nil
	}

	if subject, err = render.Render(tpl.Subject, req.Data, false); err == nil {
		body, err = render.Render(tpl.Body, req.Data, true)
	}

	if err != nil {
		return "", "", false, httpx.BadRequest("Invalid data: " + err.Error())
	}

	return oneLine(subject), body, false, nil
}

// Send validates the request, renders the email and queues it.
func (s *Service) Send(ctx *gofr.Context, req *models.SendRequest) (models.SendResponse, error) {
	req.Type = strings.TrimSpace(req.Type)
	if req.Type == "" {
		return models.SendResponse{}, httpx.InvalidParam("type")
	}

	if req.Language = strings.ToLower(strings.TrimSpace(req.Language)); req.Language == "" {
		req.Language = defaultLanguage
	}

	to, err := addresses(req.To, "to")
	if err != nil || len(to) == 0 {
		return models.SendResponse{}, httpx.InvalidParam("to")
	}

	cc, err := addresses(req.Cc, "cc")
	if err != nil {
		return models.SendResponse{}, err
	}

	bcc, err := addresses(req.Bcc, "bcc")
	if err != nil {
		return models.SendResponse{}, err
	}

	if len(to)+len(cc)+len(bcc) > maxRecipients {
		return models.SendResponse{}, httpx.InvalidParam("to")
	}

	subject, body, skipped, err := s.content(ctx, req)
	if err != nil {
		return models.SendResponse{}, err
	}

	if skipped {
		return models.SendResponse{Status: "skipped", Message: skippedMessage}, nil
	}

	if len(subject) > maxSubjectLen || len(body) > maxBodyLen {
		return models.SendResponse{}, httpx.InvalidParam("data")
	}

	e, err := s.emails.Enqueue(ctx, &models.Outbound{
		Type: req.Type, Language: req.Language, To: strings.Join(to, ","), Cc: strings.Join(cc, ","), Bcc: strings.Join(bcc, ","),
		Subject: subject, Body: body,
	})
	if err != nil {
		return models.SendResponse{}, httpx.FromStore(err, "email", "")
	}

	return models.SendResponse{ID: e.ID, Status: e.Status, Message: queuedMessage}, nil
}

// Get returns one email from the log.
func (s *Service) Get(ctx *gofr.Context, id int64) (models.Email, error) {
	e, err := s.emails.Get(ctx, id)

	return e, httpx.FromStoreID(err, "email", id)
}

// List returns one page of the log.
func (s *Service) List(ctx *gofr.Context, f *filters.EmailFilter) (models.EmailList, error) {
	emails, count, err := s.emails.List(ctx, f)
	if err != nil {
		return models.EmailList{}, httpx.FromStore(err, "email", "")
	}

	return models.EmailList{Emails: emails, Count: count}, nil
}

func split(s string) []string {
	if s == "" {
		return nil
	}

	return strings.Split(s, ",")
}

// backoff is how long to wait before the next try: 30s, 60s, 120s... capped at an hour.
func backoff(attempts int) int {
	d := baseBackoff
	for i := 1; i < attempts && d < maxBackoff; i++ {
		d *= 2
	}

	return min(d, maxBackoff)
}

func reason(err error) string {
	r := err.Error()
	if len(r) > maxReason {
		r = r[:maxReason]
	}

	return r
}

// Deliver sends what is due. One email failing never stops the others.
func (s *Service) Deliver(ctx *gofr.Context) (sent, failed int, err error) {
	due, err := s.emails.Claim(ctx, batchSize, claimSeconds)
	if err != nil {
		return 0, 0, err
	}

	for i := range due {
		o := &due[i]

		sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
		sendErr := s.sender.Send(sendCtx, &sender.Message{To: split(o.To), Cc: split(o.Cc), Bcc: split(o.Bcc), Subject: o.Subject, HTML: o.Body})

		cancel()

		switch {
		case sendErr == nil:
			sent++

			err = errors.Join(err, s.emails.MarkSent(ctx, o.ID))
		case o.Attempts >= s.maxAttempts:
			failed++

			err = errors.Join(err, s.emails.MarkFailed(ctx, o.ID, reason(sendErr)))
		default:
			failed++

			err = errors.Join(err, s.emails.MarkRetry(ctx, o.ID, backoff(o.Attempts), reason(sendErr)))
		}
	}

	return sent, failed, err
}
