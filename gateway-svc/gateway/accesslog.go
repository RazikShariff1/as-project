package gateway

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// Logger is the part of GoFr's logger the gateway uses. A nil Logger logs nothing.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}

// record is what the gateway learned about one request as it handled it. It is logged once, when the response is done:
// one line says where the request came in, what was decided and where it went, so a misrouted call can be found at a glance.
type record struct {
	realm    string // "api" (the team) or "store" (shoppers)
	service  string // the service the client asked for
	path     string // the path the backend sees
	decision string // public, protected, auth-only, internal, allowlisted, not-listed, unknown-service, ...
	who      string // user:<id>, customer:<id> or anonymous
	target   string // scheme://host + path the request was proxied to; empty when it never left the gateway
}

type recordKey struct{}

// note updates the record of the request being handled; it does nothing for a request that has none.
func note(r *http.Request, f func(*record)) {
	if rec, ok := r.Context().Value(recordKey{}).(*record); ok {
		f(rec)
	}
}

// statusWriter remembers the status code a handler wrote.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer (flushing, deadlines).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// WithLogger turns on the gateway's access log.
func (g *Gateway) WithLogger(l Logger) *Gateway {
	g.log = l

	return g
}

// logged runs handle for a /api or /store request and writes one line when it is done. Tokens, query strings and bodies are
// never logged: the path only, and the caller's id once they have been authenticated.
func (g *Gateway) logged(w http.ResponseWriter, r *http.Request, realm string, handle func(http.ResponseWriter, *http.Request)) {
	if g.log == nil {
		handle(w, r)

		return
	}

	start := time.Now()
	rec := &record{realm: realm, who: "anonymous"}
	sw := &statusWriter{ResponseWriter: w}

	handle(sw, r.WithContext(context.WithValue(r.Context(), recordKey{}, rec)))

	status := sw.status
	if status == 0 {
		status = http.StatusOK
	}

	line := "gateway realm=" + rec.realm + " method=" + r.Method + " path=" + strconv.Quote(r.URL.Path) +
		" service=" + strconv.Quote(rec.service) + " decision=" + rec.decision + " who=" + rec.who +
		" target=" + strconv.Quote(rec.target) + " status=" + strconv.Itoa(status) +
		" ms=" + strconv.FormatInt(time.Since(start).Milliseconds(), 10) + " cid=" + sw.Header().Get(correlationID)

	if status >= http.StatusBadRequest {
		g.log.Warnf("%s", line)

		return
	}

	g.log.Infof("%s", line)
}
