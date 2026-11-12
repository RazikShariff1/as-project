package sender

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"html"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

var (
	breakTags = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</tr>|</h[1-6]>|</li>`)
	cellEnds  = regexp.MustCompile(`(?i)</t[dh]>`)
	anyTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	spaces    = regexp.MustCompile(`[ \t]+`)
	blankRuns = regexp.MustCompile(`\n[ \t]*(\n[ \t]*)+`)
)

// PlainText turns an HTML body into the text/plain alternative: mail with no text part, only HTML, is marked down by spam filters
// and unreadable in text-only clients. Table cells are separated by " | " and rows by line breaks.
func PlainText(h string) string {
	t := cellEnds.ReplaceAllString(h, " | ")
	t = breakTags.ReplaceAllString(t, "\n")
	t = anyTag.ReplaceAllString(t, "")
	t = html.UnescapeString(t)
	t = spaces.ReplaceAllString(t, " ")
	t = strings.ReplaceAll(t, " | \n", "\n")
	t = strings.ReplaceAll(t, " \n", "\n")
	t = strings.ReplaceAll(t, "\n ", "\n")
	t = blankRuns.ReplaceAllString(t, "\n\n")

	return strings.TrimSpace(t)
}

func qp(s string) string {
	var buf bytes.Buffer

	w := quotedprintable.NewWriter(&buf)
	_, _ = w.Write([]byte(s))
	_ = w.Close()

	return buf.String()
}

func boundary() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)

	return "=_" + hex.EncodeToString(b)
}

// formatMIME builds one UTF-8 message with a text/plain part and the HTML part (multipart/alternative). withBcc adds a Bcc header,
// for APIs (Gmail) that read the recipients from the message itself and strip that header before sending.
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

	bound := boundary()

	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", m.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"" + bound + "\"\r\n\r\n")

	part := func(contentType, body string) {
		b.WriteString("--" + bound + "\r\nContent-Type: " + contentType + "; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(qp(body))
		b.WriteString("\r\n")
	}

	part("text/plain", PlainText(m.HTML)) // the simplest alternative first; clients show the last one they understand
	part("text/html", m.HTML)
	b.WriteString("--" + bound + "--\r\n")

	return []byte(b.String())
}
