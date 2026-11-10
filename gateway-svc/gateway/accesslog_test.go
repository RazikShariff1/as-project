package gateway

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/gateway-svc/accountclient"
	"github.com/zopping/gateway-svc/config"
	"github.com/zopping/gateway-svc/customerclient"
)

// memLog records the gateway's log lines.
type memLog struct {
	mu    sync.Mutex
	lines []string
}

func (m *memLog) add(level, f string, a ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lines = append(m.lines, level+" "+fmt.Sprintf(f, a...))
}

func (m *memLog) Infof(f string, a ...any) { m.add("INFO", f, a...) }
func (m *memLog) Warnf(f string, a ...any) { m.add("WARN", f, a...) }

func (m *memLog) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.lines) == 0 {
		return ""
	}

	return m.lines[len(m.lines)-1]
}

// loggedRig is the store rig with an access log attached.
func loggedRig(t *testing.T) (*storeRig, *memLog) {
	t.Helper()

	r := &storeRig{customer: newBackend(t), order: newBackend(t), catalogue: newBackend(t), inventory: newBackend(t), account: newBackend(t)}
	r.customers = &fakeCustomers{shoppers: map[string]customerclient.Principal{"asha": {Customer: customerclient.Customer{ID: 7, Status: "active"}}}}
	r.accounts = &fakeAccounts{principals: map[string]accountclient.Principal{
		"seller7": {User: accountclient.User{ID: 5, Attributes: map[string]any{"sellerId": 7.0}}},
	}}

	parse := func(b *backend) *url.URL { u, _ := url.Parse(b.srv.URL); return u }

	settings := config.Settings{
		Services: map[string]*url.URL{
			"customer-svc": parse(r.customer), "order-svc": parse(r.order), "catalogue-svc": parse(r.catalogue),
			"inventory-svc": parse(r.inventory), "account-svc": parse(r.account),
		},
		ProxyTimeout: 2 * time.Second, MaxBodyBytes: 1 << 10, LoginPerMinute: 5,
	}

	log := &memLog{}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	r.h = New(settings, r.accounts).WithCustomers(r.customers).WithLogger(log).Middleware()(next)

	return r, log
}

func TestLogShowsWhereAShopperRequestWasRouted(t *testing.T) {
	r, log := loggedRig(t)

	assert.Equal(t, 200, r.do("GET", "/store/order-svc/order/ABC123?secret=1", "asha", "").Code)

	line := log.last()
	assert.True(t, strings.HasPrefix(line, "INFO gateway "), line)
	assert.Contains(t, line, `realm=store`)
	assert.Contains(t, line, `method=GET`)
	assert.Contains(t, line, `service="order-svc"`)
	assert.Contains(t, line, `decision=allowlisted-signed`)
	assert.Contains(t, line, `who=customer:7`)
	assert.Contains(t, line, `target="`+r.order.srv.URL+`/order/ABC123"`, "routed to order-svc, on the path it serves")
	assert.Contains(t, line, `status=200`)
	assert.Contains(t, line, `cid=`)

	assert.NotContains(t, line, "secret", "query strings are never logged")
	assert.NotContains(t, line, "asha", "tokens are never logged")
	assert.Equal(t, 1, r.order.calls, "and it really did arrive at order-svc")
	assert.Zero(t, r.customer.calls+r.catalogue.calls+r.inventory.calls+r.account.calls, "and at no other service")
}

func TestLogShowsTheTeamRealmRouteToo(t *testing.T) {
	r, log := loggedRig(t)

	assert.Equal(t, 200, r.do("GET", "/api/catalogue-svc/product", "", "").Code)

	line := log.last()
	assert.Contains(t, line, `realm=api`)
	assert.Contains(t, line, `service="catalogue-svc"`)
	assert.Contains(t, line, `decision=public`)
	assert.Contains(t, line, `who=anonymous`)
	assert.Contains(t, line, `target="`+r.catalogue.srv.URL+`/product"`)

	r.do("PUT", "/api/inventory-svc/stock/1/seller/7/price", "seller7", `{}`)
	line = log.last()
	assert.Contains(t, line, `decision=protected`)
	assert.Contains(t, line, `who=user:5`)
	assert.Contains(t, line, `target="`+r.inventory.srv.URL+`/stock/1/seller/7/price"`)
}

func TestLogShowsWhyARequestNeverLeftTheGateway(t *testing.T) {
	r, log := loggedRig(t)

	cases := []struct {
		method, path, token, want string
	}{
		{"GET", "/store/account-svc/user", "asha", `decision=not-listed`},
		{"GET", "/store/nothing-svc/x", "asha", `decision=unknown-service`},
		{"GET", "/store/order-svc/order?customerId=8", "asha", `decision=own-data-denied`},
		{"GET", "/store/order-svc/order/../x", "asha", `decision=invalid-path`},
		{"GET", "/api/nothing-svc/x", "", `decision=unknown-service`},
		{"GET", "/api/account-svc/auth/check", "", `decision=internal`},
	}

	for _, c := range cases {
		r.do(c.method, c.path, c.token, "")

		line := log.last()
		assert.True(t, strings.HasPrefix(line, "WARN "), "a refusal is a warning: %s", line)
		assert.Contains(t, line, c.want, c.path)
		assert.Contains(t, line, `target=""`, "nothing was proxied: "+c.path)
	}

	assert.Zero(t, r.order.calls+r.customer.calls+r.account.calls, "none of them reached a backend")
}

func TestOnlyGatewayPathsAreLogged(t *testing.T) {
	r, log := loggedRig(t)

	r.do("GET", "/.well-known/health", "", "")

	assert.Empty(t, log.last(), "health checks pass through unlogged")
}
