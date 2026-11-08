package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zopping/gateway-svc/permissions"
)

// fakeAccount is a tiny in-memory account-svc: just enough of /auth/login, /endpoint and /role to check what the seeder does.
type fakeAccount struct {
	mu        sync.Mutex
	endpoints []map[string]any
	roles     []map[string]any
	puts      map[string]map[string]any
	posts     int
}

func (f *fakeAccount) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	write := func(status int, data any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}

	var body map[string]any

	_ = json.NewDecoder(r.Body).Decode(&body)

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	paged := func(all []map[string]any) []map[string]any {
		lo := (page - 1) * limit
		if lo > len(all) {
			lo = len(all)
		}

		hi := lo + limit
		if hi > len(all) {
			hi = len(all)
		}

		return all[lo:hi]
	}

	switch {
	case r.URL.Path == "/auth/login":
		if body["password"] != "right" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid credentials"}}`))

			return
		}

		write(http.StatusCreated, map[string]any{"token": "tok"})
	case r.Header.Get("Authorization") != "Bearer tok":
		w.WriteHeader(http.StatusUnauthorized)
	case r.URL.Path == "/endpoint" && r.Method == http.MethodGet:
		write(200, map[string]any{"endpoints": paged(f.endpoints)})
	case r.URL.Path == "/endpoint" && r.Method == http.MethodPost:
		body["id"] = float64(len(f.endpoints) + 1)
		f.endpoints = append(f.endpoints, body)
		f.posts++
		write(http.StatusCreated, body)
	case r.URL.Path == "/role" && r.Method == http.MethodGet:
		write(200, map[string]any{"roles": paged(f.roles)})
	case r.URL.Path == "/role" && r.Method == http.MethodPost:
		body["id"] = float64(len(f.roles) + 1)
		f.roles = append(f.roles, body)
		f.posts++
		write(http.StatusCreated, body)
	case strings.HasPrefix(r.URL.Path, "/role/") && r.Method == http.MethodPut:
		f.puts[r.URL.Path] = body
		write(200, body)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func setup(t *testing.T) (*fakeAccount, Config) {
	t.Helper()

	f := &fakeAccount{puts: map[string]map[string]any{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	return f, Config{AccountURL: srv.URL, Email: "a@b.co", Password: "right"}
}

func TestSeedRegistersEveryEndpointAndRole(t *testing.T) {
	f, cfg := setup(t)

	sum, err := Run(context.Background(), cfg)
	assert.NoError(t, err)

	assert.Equal(t, len(permissions.Endpoints()), sum.EndpointsCreated)
	assert.Zero(t, sum.EndpointsExisting)
	assert.Equal(t, len(permissions.Roles()), sum.RolesCreated)
	assert.Len(t, f.endpoints, len(permissions.Endpoints()), "more than one page of endpoints (100 per page)")

	names := []string{}
	for _, r := range f.roles {
		names = append(names, r["name"].(string))
	}

	assert.ElementsMatch(t, []string{"admin", "seller", "catalogue-manager"}, names)
}

func TestSeedIsIdempotentAndRewritesGrantsEachTime(t *testing.T) {
	f, cfg := setup(t)

	_, err := Run(context.Background(), cfg)
	assert.NoError(t, err)

	postsAfterFirst := f.posts

	sum, err := Run(context.Background(), cfg)
	assert.NoError(t, err)

	assert.Zero(t, sum.EndpointsCreated, "nothing registered twice")
	assert.Equal(t, len(permissions.Endpoints()), sum.EndpointsExisting)
	assert.Zero(t, sum.RolesCreated)
	assert.Equal(t, len(permissions.Roles()), sum.RolesUpdated, "each role's grants are re-applied so manual drift is repaired")
	assert.Equal(t, postsAfterFirst, f.posts, "no creates on the second run")
}

func TestSellerRoleGetsExactlyItsGrantsAndAdminAllAccess(t *testing.T) {
	f, cfg := setup(t)

	_, err := Run(context.Background(), cfg)
	assert.NoError(t, err)

	idOf := map[string]float64{}
	for _, r := range f.roles {
		idOf[r["name"].(string)] = r["id"].(float64)
	}

	_, err = Run(context.Background(), cfg)
	assert.NoError(t, err)

	admin := f.puts[fmt.Sprintf("/role/%d", int(idOf["admin"]))]
	assert.Equal(t, true, admin["allAccess"])
	assert.Empty(t, admin["endpointIds"])

	seller := f.puts[fmt.Sprintf("/role/%d", int(idOf["seller"]))]
	assert.Equal(t, false, seller["allAccess"])

	var want int

	for _, r := range permissions.Roles() {
		if r.Name == "seller" {
			want = len(r.Grants)
		}
	}

	assert.Len(t, seller["endpointIds"], want)
}

func TestSeedStopsOnBadCredentials(t *testing.T) {
	f, cfg := setup(t)
	cfg.Password = "wrong"

	_, err := Run(context.Background(), cfg)
	assert.ErrorContains(t, err, "sign in")
	assert.ErrorContains(t, err, "invalid credentials")
	assert.Empty(t, f.endpoints, "nothing is registered without signing in")
}

func TestSeedReportsAnUnreachableAccountService(t *testing.T) {
	_, err := Run(context.Background(), Config{AccountURL: "http://127.0.0.1:1", Email: "a@b.co", Password: "x"})
	assert.Error(t, err)
}
