// Package models holds the request and response structs (camelCase JSON, int64 ids).
package models

import "time"

// Email statuses: queued until a worker takes it, sending while one holds it, then sent or (out of attempts) failed.
const (
	StatusQueued  = "queued"
	StatusSending = "sending"
	StatusSent    = "sent"
	StatusFailed  = "failed"
)

// NoTemplate is the email type that carries its own subject and body in data instead of using a stored template.
const NoTemplate = "no-template"

// Template statuses.
const (
	TemplateEnabled  = "ENABLED"
	TemplateDisabled = "DISABLED"
)

// SendRequest is the body of POST /email, as in the reference communication-service. To, Cc and Bcc are comma-separated
// addresses. Type names the template ("customer/welcome"), or is "no-template" with data.subject and data.body.
type SendRequest struct {
	To       string         `json:"to"`
	Cc       string         `json:"cc"`
	Bcc      string         `json:"bcc"`
	Type     string         `json:"type"`
	Language string         `json:"language"`
	Data     map[string]any `json:"data"`
}

// SendResponse answers POST /email: the email is stored and will be delivered shortly. ID is 0 when nothing was queued
// because the template is disabled.
type SendResponse struct {
	ID      int64  `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Email is a stored email, as the log shows it.
type Email struct {
	ID        int64      `json:"id"`
	Type      string     `json:"type"`
	Language  string     `json:"language"`
	To        string     `json:"to"`
	Cc        string     `json:"cc"`
	Bcc       string     `json:"bcc"`
	Subject   string     `json:"subject"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"lastError"`
	SentAt    *time.Time `json:"sentAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

// EmailList is one page of the log.
type EmailList struct {
	Emails []Email `json:"emails"`
	Count  int     `json:"count"`
}

// Outbound is an email being stored or delivered, with its rendered body.
type Outbound struct {
	ID       int64
	Type     string
	Language string
	To       string
	Cc       string
	Bcc      string
	Subject  string
	Body     string
	Attempts int
}

// Template is a stored email template.
type Template struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Language  string    `json:"language"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TemplateList is one page of templates.
type TemplateList struct {
	Templates []Template `json:"templates"`
	Count     int        `json:"count"`
}

// TemplateRequest creates or replaces a template. Language defaults to "en" and status to ENABLED.
type TemplateRequest struct {
	Name     string `json:"name"`
	Language string `json:"language"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Status   string `json:"status"`
}
