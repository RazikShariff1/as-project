package sender

import (
	"bytes"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
)

// formatMIME builds one UTF-8 HTML message. withBcc adds a Bcc header, for APIs (Gmail) that read the recipients from the
// message itself and strip that header before sending.
func formatMIME(from mail.Address, m *Message, withBcc bool) []byte {
	var b strings.Builder

	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + strings.Join(m.To, ", ") + "\r\n")

	if len(m.Cc) > 0 {
		b.WriteString("Cc: " + strings.Join(m.Cc, ", ") + "\r\n")
	}

	if withBcc && len(m.Bcc) > 0 {
		b.WriteString("Bcc: " + strings.Join(m.Bcc, ", ") + "\r\n")
	}

	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", m.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")

	var body bytes.Buffer

	w := quotedprintable.NewWriter(&body)
	_, _ = w.Write([]byte(m.HTML))
	_ = w.Close()

	b.Write(body.Bytes())

	return []byte(b.String())
}
