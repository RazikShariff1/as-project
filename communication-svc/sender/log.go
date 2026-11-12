package sender

import (
	"context"
	"strings"

	"gofr.dev/pkg/gofr/logging"
)

// Log writes each email to the log instead of delivering it.
type Log struct{ Logger logging.Logger }

// Send logs the message.
func (l Log) Send(_ context.Context, m *Message) error {
	l.Logger.Infof("email not delivered (no SMTP configured): to=%s subject=%q", strings.Join(m.To, ","), m.Subject)

	return nil
}
