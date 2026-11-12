// Package sender delivers one rendered email. SMTP is the real thing; Log is for runs without mail credentials.
package sender

import "context"

// Message is one email ready to go out. To, Cc and Bcc are already validated addresses.
type Message struct {
	To      []string
	Cc      []string
	Bcc     []string
	Subject string
	HTML    string
}

// Sender delivers a message. An error means it was not delivered and may be tried again.
type Sender interface {
	Send(ctx context.Context, m *Message) error
}
