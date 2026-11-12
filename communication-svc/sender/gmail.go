package sender

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	googleTokenURL = "https://oauth2.googleapis.com/token"
	gmailSendURL   = "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"
	gmailTimeout   = 30 * time.Second
	tokenLeeway    = time.Minute
	maxAPIBody     = 1 << 16
)

// Gmail sends through the Gmail API over HTTPS, as the mailbox the refresh token belongs to (the From address is always that
// mailbox: Gmail rewrites any other). It needs no SMTP port, so it works where those are blocked, such as Render's free plan.
// The refresh token needs the https://www.googleapis.com/auth/gmail.send scope.
type Gmail struct {
	ClientID, ClientSecret, RefreshToken string
	From                                 mail.Address

	// TokenURL, SendURL and Client are for tests; the zero values use Google and a client with a timeout.
	TokenURL, SendURL string
	Client            *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

func (g *Gmail) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}

	return &http.Client{Timeout: gmailTimeout}
}

func orDefault(v, d string) string {
	if v != "" {
		return v
	}

	return d
}

// accessToken returns a live access token, refreshing it shortly before it expires.
func (g *Gmail) accessToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.token != "" && time.Now().Add(tokenLeeway).Before(g.expires) {
		return g.token, nil
	}

	form := url.Values{
		"client_id": {g.ClientID}, "client_secret": {g.ClientSecret}, "refresh_token": {g.RefreshToken}, "grant_type": {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, orDefault(g.TokenURL, googleTokenURL), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("gmail token refresh: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody))

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}

	_ = json.Unmarshal(raw, &out)

	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return "", fmt.Errorf("gmail token refresh: status %d: %s %s", resp.StatusCode, out.Error, out.Description)
	}

	g.token, g.expires = out.AccessToken, time.Now().Add(time.Duration(out.ExpiresIn)*time.Second)

	return g.token, nil
}

// Send delivers the message with users.messages.send.
func (g *Gmail) Send(ctx context.Context, m *Message) error {
	if len(m.To)+len(m.Cc)+len(m.Bcc) == 0 {
		return errors.New("no recipients")
	}

	token, err := g.accessToken(ctx)
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]string{"raw": base64.RawURLEncoding.EncodeToString(formatMIME(g.From, m, true))})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, orDefault(g.SendURL, gmailSendURL), bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client().Do(req)
	if err != nil {
		return fmt.Errorf("gmail send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	if resp.StatusCode == http.StatusUnauthorized {
		g.mu.Lock()
		g.token = "" // refresh next time
		g.mu.Unlock()
	}

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxAPIBody))

	return fmt.Errorf("gmail send: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
}
