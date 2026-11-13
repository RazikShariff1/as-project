// Package gateway is the platform's single front door: every request to /api/{service}/{path} is authenticated and
// authorised by account-svc, checked against the caller's own seller or user id, and proxied to the backend with the
// verified identity as headers.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/zopping/gateway-svc/accountclient"
	"github.com/zopping/gateway-svc/config"
	"github.com/zopping/gateway-svc/customerclient"
	"github.com/zopping/gateway-svc/policy"
	"github.com/zopping/gateway-svc/ratelimit"
	"github.com/zopping/gateway-svc/routing"
	"github.com/zopping/gateway-svc/store"
)

const (
	bearerPrefix  = "bearer "
	correlationID = "X-Correlation-Id"
	loginService  = config.AccountService
	loginPath     = "/auth/login"
	googlePath    = "/auth/google"
)

// Gateway is the proxy middleware.
type Gateway struct {
	settings  config.Settings
	accounts  accountclient.Client
	customers customerclient.Client
	log       Logger
	limiter   *ratelimit.Limiter
	storeRate *ratelimit.Limiter
	transport http.RoundTripper
}

// New returns a gateway that asks accounts about every token.
func New(s config.Settings, accounts accountclient.Client) *Gateway {
	return &Gateway{
		settings:  s,
		accounts:  accounts,
		limiter:   ratelimit.New(s.LoginPerMinute),
		storeRate: ratelimit.New(s.LoginPerMinute),
		transport: &http.Transport{
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: s.ProxyTimeout,
		},
	}
}

// WithCustomers turns on the customer-facing /store realm, where shoppers are authenticated by customer-svc. Without it
// /store answers 404.
func (g *Gateway) WithCustomers(c customerclient.Client) *Gateway {
	g.customers = c

	return g
}

// Middleware serves everything under /api/ (the team) and /store/ (shoppers) and passes every other path (health, metrics) to
// the rest of the app.
func (g *Gateway) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasPrefix(r.URL.Path, routing.Prefix):
				g.logged(w, r, "api", g.serve)
			case strings.HasPrefix(r.URL.Path, store.Prefix):
				g.logged(w, r, "store", g.serveStore)
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent) // CORS preflight carries no credentials

		return
	}

	service, rest, ok := routing.Parse(r.URL.Path)
	target := g.settings.Services[service]

	note(r, func(l *record) { l.service, l.path = service, rest })

	if !ok || target == nil {
		note(r, func(l *record) { l.decision = "unknown-service" })
		writeError(w, http.StatusNotFound, "unknown service")

		return
	}

	rest, ok = cleanPath(rest)
	if !ok {
		note(r, func(l *record) { l.decision = "invalid-path" })
		writeError(w, http.StatusBadRequest, "invalid path")

		return
	}

	note(r, func(l *record) { l.path = rest })

	// Read the token before the credentials are stripped from what the backend will see.
	token := bearer(r)

	stripIdentity(r.Header)

	if r.Header.Get(correlationID) == "" {
		r.Header.Set(correlationID, newID())
	}

	w.Header().Set(correlationID, r.Header.Get(correlationID))

	class, force := policy.Classify(service, r.Method, rest)
	note(r, func(l *record) { l.decision = className(class) })

	switch class {
	case policy.Internal:
		writeError(w, http.StatusNotFound, "not found") // not revealed to outside callers
	case policy.Public:
		g.servePublic(w, r, target, service, rest, force)
	case policy.AuthOnly, policy.Protected:
		g.serveProtected(w, r, target, service, rest, token, class)
	}
}

func (g *Gateway) servePublic(w http.ResponseWriter, r *http.Request, target *url.URL, service, rest string, force map[string]string) {
	// Password and Google sign-in share one allowance per address, so alternating between them buys no extra attempts.
	if service == loginService && (rest == loginPath || rest == googlePath) && !g.limiter.Allow(g.clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts; try again in a minute")

		return
	}

	query := r.URL.Query()
	for k, v := range force {
		query.Set(k, v)
	}

	g.proxy(w, r, target, rest, query, nil, "")
}

func (g *Gateway) serveProtected(w http.ResponseWriter, r *http.Request, target *url.URL, service, rest, token string, class policy.Class) {
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing token")

		return
	}

	var (
		p   accountclient.Principal
		err error
	)

	if class == policy.AuthOnly {
		p, err = g.accounts.Me(r.Context(), token)
	} else {
		p, err = g.accounts.Check(r.Context(), token, service, r.Method, rest)
	}

	if err != nil {
		writeAccountError(w, err)

		return
	}

	note(r, func(l *record) { l.who = "user:" + strconv.FormatInt(p.User.ID, 10) })

	query, err := policy.Enforce(p, service, r.Method, rest, r, g.settings.MaxBodyBytes)
	if err != nil {
		note(r, func(l *record) { l.decision += "-denied" })
		writeScopeError(w, err)

		return
	}

	// account-svc identifies the caller of /auth/me and /auth/session by the bearer token itself, so only those AuthOnly
	// routes get it forwarded. Every other backend sees identity headers and never a credential.
	forward := ""
	if class == policy.AuthOnly {
		forward = token
	}

	g.proxy(w, r, target, rest, query, func(h http.Header) { setIdentity(h, p) }, forward)
}

// proxy forwards the request to the backend under its own path, with the verified identity (if any) in headers.
func (g *Gateway) proxy(w http.ResponseWriter, r *http.Request, target *url.URL, rest string, query url.Values, setID func(http.Header), forwardToken string) {
	ctx, cancel := context.WithTimeout(r.Context(), g.settings.ProxyTimeout)
	defer cancel()

	note(r, func(l *record) { l.target = strings.TrimRight(target.String(), "/") + rest })

	rp := &httputil.ReverseProxy{
		Transport: g.transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = strings.TrimRight(target.Path, "/") + rest
			pr.Out.URL.RawPath = ""
			pr.Out.URL.RawQuery = query.Encode()
			pr.SetXForwarded()

			if setID != nil {
				setID(pr.Out.Header)
			}

			if forwardToken != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+forwardToken)
			}
		},
		// The gateway answers CORS for every client; a second set from the backend makes browsers reject the response.
		ModifyResponse: func(resp *http.Response) error {
			for k := range resp.Header {
				if strings.HasPrefix(http.CanonicalHeaderKey(k), "Access-Control-") {
					resp.Header.Del(k)
				}
			}

			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if errors.Is(err, context.DeadlineExceeded) {
				writeError(w, http.StatusGatewayTimeout, "the service took too long to answer")

				return
			}

			writeError(w, http.StatusBadGateway, "the service is unavailable")
		},
	}

	rp.ServeHTTP(w, r.WithContext(ctx))
}

func className(c policy.Class) string {
	switch c {
	case policy.Public:
		return "public"
	case policy.AuthOnly:
		return "auth-only"
	case policy.Internal:
		return "internal"
	default:
		return "protected"
	}
}

// cleanPath rejects paths with empty, "." or ".." segments. The gateway checks permissions and ownership against the path it
// forwards, so it must be exactly the path the backend will route; a router that cleans "/a/../b" itself could otherwise
// reach a route the checks never saw.
func cleanPath(rest string) (string, bool) {
	trimmed := strings.TrimRight(rest, "/")
	if trimmed == "" {
		return "/", true
	}

	if path.Clean(trimmed) != trimmed || strings.ContainsAny(trimmed, "\x00\\") {
		return "", false
	}

	return trimmed, true
}

// bearer returns the token of an "Authorization: Bearer <token>" header, or "".
func bearer(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(h) <= len(bearerPrefix) || !strings.EqualFold(h[:len(bearerPrefix)], bearerPrefix) {
		return ""
	}

	return strings.TrimSpace(h[len(bearerPrefix):])
}

// clientIP is the address to rate-limit: the socket address, or the first X-Forwarded-For hop if a trusted proxy sets it.
func (g *Gateway) clientIP(r *http.Request) string {
	if g.settings.TrustForwarded {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			return strings.TrimSpace(strings.Split(fwd, ",")[0])
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)

	return hex.EncodeToString(b)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": msg}})
}

// writeAccountError passes account-svc's own refusal (401, 403, 429 ...) to the client unchanged. If account-svc cannot be
// reached the request is refused: the gateway never lets a call through because authentication was down.
func writeAccountError(w http.ResponseWriter, err error) {
	var refusal *accountclient.Error
	if errors.As(err, &refusal) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(refusal.Status)
		_, _ = w.Write(refusal.Body)

		return
	}

	writeError(w, http.StatusServiceUnavailable, "authentication service unavailable")
}

func writeScopeError(w http.ResponseWriter, err error) {
	var (
		denied *policy.Denied
		media  *policy.UnsupportedMedia
	)

	switch {
	case errors.As(err, &denied):
		writeError(w, http.StatusForbidden, denied.Reason)
	case errors.As(err, &media):
		writeError(w, http.StatusUnsupportedMediaType, media.Reason)
	case errors.Is(err, policy.TooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
	default:
		writeError(w, http.StatusBadRequest, "could not read the request")
	}
}
