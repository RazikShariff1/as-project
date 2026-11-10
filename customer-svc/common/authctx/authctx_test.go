package authctx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
	gofrhttp "gofr.dev/pkg/gofr/http"
)

// run passes a request through the middleware and returns what a handler would read from its context.
func run(t *testing.T, mutate func(r *http.Request)) (token, agent, ip string) {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
	r.RemoteAddr = "10.1.2.3:5555"
	mutate(r)

	Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		ctx := &gofr.Context{Request: gofrhttp.NewRequest(req)}
		token, agent, ip = Token(ctx), UserAgent(ctx), IP(ctx)
	})).ServeHTTP(httptest.NewRecorder(), r)

	return token, agent, ip
}

func TestBearerToken(t *testing.T) {
	tests := map[string]string{
		"Bearer abc123":    "abc123",
		"bearer abc123":    "abc123",
		"BEARER   abc123 ": "abc123",
		"Basic abc123":     "",
		"abc123":           "",
		"Bearer":           "",
		"Bearer ":          "",
		"":                 "",
	}

	for header, want := range tests {
		token, _, _ := run(t, func(r *http.Request) {
			if header != "" {
				r.Header.Set("Authorization", header)
			}
		})
		assert.Equal(t, want, token, "Authorization: %q", header)
	}
}

func TestClientMetadata(t *testing.T) {
	_, agent, ip := run(t, func(r *http.Request) { r.Header.Set("User-Agent", "curl/8") })
	assert.Equal(t, "curl/8", agent)
	assert.Equal(t, "10.1.2.3", ip, "socket address without the port")

	_, _, ip = run(t, func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1") })
	assert.Equal(t, "203.0.113.9", ip, "the gateway's first forwarded hop is the real client")
}
