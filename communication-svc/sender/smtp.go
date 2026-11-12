package sender

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

const (
	implicitTLSPort = "465"
	dialTimeout     = 15 * time.Second
	sessionTimeout  = 60 * time.Second
)

// SMTP delivers through an SMTP server: STARTTLS when the server offers it (port 587), implicit TLS on port 465.
type SMTP struct {
	Host, Port, Username, Password string
	From                           mail.Address
}

// Send delivers the message. Bcc recipients get the envelope but no header.
func (s *SMTP) Send(ctx context.Context, m *Message) error {
	addr := net.JoinHostPort(s.Host, s.Port)
	dialer := &net.Dialer{Timeout: dialTimeout}
	cfg := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}

	var (
		conn net.Conn
		err  error
	)

	if s.Port == implicitTLSPort {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: cfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}

	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}

	_ = conn.SetDeadline(time.Now().Add(sessionTimeout))

	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()

		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok && s.Port != implicitTLSPort {
		if err := c.StartTLS(cfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := s.deliver(c, m); err != nil {
		return err
	}

	return c.Quit()
}

func (s *SMTP) deliver(c *smtp.Client, m *Message) error {
	if err := c.Mail(s.From.Address); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}

	recipients := append(append(append([]string{}, m.To...), m.Cc...), m.Bcc...)
	if len(recipients) == 0 {
		return errors.New("no recipients")
	}

	for _, r := range recipients {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("smtp RCPT TO %s: %w", r, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}

	if _, err := w.Write(s.format(m)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}

	return w.Close()
}

// format builds the MIME message: a single UTF-8 HTML part.
func (s *SMTP) format(m *Message) []byte {
	var b strings.Builder

	b.WriteString("From: " + s.From.String() + "\r\n")
	b.WriteString("To: " + strings.Join(m.To, ", ") + "\r\n")

	if len(m.Cc) > 0 {
		b.WriteString("Cc: " + strings.Join(m.Cc, ", ") + "\r\n")
	}

	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", m.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	b.WriteString(quotedPrintable(m.HTML))

	return []byte(b.String())
}
