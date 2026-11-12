package sender

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGmail_SendsRawMIMEAndCachesTheToken(t *testing.T) {
	var tokenCalls int

	var raw string

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls++

			require.NoError(t, r.ParseForm())
			assert.Equal(t, "refresh_token", r.Form.Get("grant_type"))
			assert.Equal(t, "rt", r.Form.Get("refresh_token"))
			_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600}`))
		case "/send":
			assert.Equal(t, "Bearer at", r.Header.Get("Authorization"))

			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

			b, err := base64.RawURLEncoding.DecodeString(body["raw"])
			require.NoError(t, err)

			raw = string(b)

			_, _ = w.Write([]byte(`{"id":"1"}`))
		}
	}))
	defer api.Close()

	g := &Gmail{
		ClientID: "id", ClientSecret: "secret", RefreshToken: "rt", From: mail.Address{Name: "Zopping", Address: "me@gmail.com"},
		TokenURL: api.URL + "/token", SendURL: api.URL + "/send",
	}
	m := &Message{To: []string{"a@example.com"}, Cc: []string{"c@example.com"}, Bcc: []string{"b@example.com"}, Subject: "Hi", HTML: "<p>x</p>"}

	require.NoError(t, g.Send(context.Background(), m))
	require.NoError(t, g.Send(context.Background(), m))

	assert.Equal(t, 1, tokenCalls, "the access token is reused until it nearly expires")
	assert.Contains(t, raw, "From: \"Zopping\" <me@gmail.com>")
	assert.Contains(t, raw, "To: a@example.com")
	assert.Contains(t, raw, "Cc: c@example.com")
	assert.Contains(t, raw, "Bcc: b@example.com", "the Gmail API reads Bcc from the message and strips it")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(raw), "<p>x</p>"))
}

func TestGmail_Errors(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600}`))

			return
		}

		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"Insufficient Permission"}}`))
	}))
	defer api.Close()

	g := &Gmail{RefreshToken: "rt", From: mail.Address{Address: "me@gmail.com"}, TokenURL: api.URL + "/token", SendURL: api.URL + "/send"}

	err := g.Send(context.Background(), &Message{To: []string{"a@example.com"}})
	assert.ErrorContains(t, err, "status 403")
	assert.ErrorContains(t, err, "Insufficient Permission")

	assert.ErrorContains(t, g.Send(context.Background(), &Message{}), "no recipients")

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	}))
	defer bad.Close()

	g = &Gmail{RefreshToken: "rt", TokenURL: bad.URL}
	assert.ErrorContains(t, g.Send(context.Background(), &Message{To: []string{"a@example.com"}}), "invalid_grant")
}
