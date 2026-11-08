// Package seed registers the platform's endpoints and roles in account-svc. It is idempotent: running it again changes nothing
// unless the catalogue changed, and it always leaves each role with exactly the grants the catalogue lists.
package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zopping/gateway-svc/permissions"
)

const pageSize = 100

// Config says where account-svc is and who to sign in as. The account needs to be allowed to manage endpoints and roles.
type Config struct {
	AccountURL string
	Email      string
	Password   string
}

// Summary says what a run did.
type Summary struct {
	EndpointsCreated, EndpointsExisting int
	RolesCreated, RolesUpdated          int
}

// Run applies the permissions catalogue to account-svc.
func Run(ctx context.Context, cfg Config) (Summary, error) {
	c := &client{base: strings.TrimRight(cfg.AccountURL, "/"), http: &http.Client{Timeout: 15 * time.Second}}

	if err := c.login(ctx, cfg.Email, cfg.Password); err != nil {
		return Summary{}, fmt.Errorf("sign in: %w", err)
	}

	var sum Summary

	ids, err := c.ensureEndpoints(ctx, &sum)
	if err != nil {
		return sum, err
	}

	return sum, c.ensureRoles(ctx, ids, &sum)
}

type client struct {
	base  string
	token string
	http  *http.Client
}

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("account-svc answered %d: %s", e.Status, e.Message)
}

// call sends a JSON request and decodes the "data" of a successful response into out (if given).
func (c *client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader

	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}

		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode >= http.StatusMultipleChoices {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}

		_ = json.Unmarshal(raw, &e)

		return &apiError{Status: resp.StatusCode, Message: e.Error.Message}
	}

	if out == nil {
		return nil
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("%s %s: unreadable response: %w", method, path, err)
	}

	return json.Unmarshal(envelope.Data, out)
}

func (c *client) login(ctx context.Context, email, password string) error {
	var session struct {
		Token string `json:"token"`
	}

	if err := c.call(ctx, http.MethodPost, "/auth/login", map[string]string{"email": email, "password": password}, &session); err != nil {
		return err
	}

	if session.Token == "" {
		return errors.New("no token in the login response")
	}

	c.token = session.Token

	return nil
}

// listEndpoints returns every registered endpoint keyed by its catalogue key.
func (c *client) listEndpoints(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}

	for page := 1; ; page++ {
		var list struct {
			Endpoints []struct {
				ID      int64  `json:"id"`
				Service string `json:"service"`
				Method  string `json:"method"`
				Path    string `json:"path"`
			} `json:"endpoints"`
		}

		q := url.Values{"limit": {fmt.Sprint(pageSize)}, "page": {fmt.Sprint(page)}}
		if err := c.call(ctx, http.MethodGet, "/endpoint?"+q.Encode(), nil, &list); err != nil {
			return nil, err
		}

		for _, e := range list.Endpoints {
			out[permissions.Key(e.Service, e.Method, e.Path)] = e.ID
		}

		if len(list.Endpoints) < pageSize {
			return out, nil
		}
	}
}

func (c *client) ensureEndpoints(ctx context.Context, sum *Summary) (map[string]int64, error) {
	ids, err := c.listEndpoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("list endpoints: %w", err)
	}

	for _, e := range permissions.Endpoints() {
		if _, ok := ids[e.Key()]; ok {
			sum.EndpointsExisting++

			continue
		}

		var created struct {
			ID int64 `json:"id"`
		}

		body := map[string]string{"service": e.Service, "method": e.Method, "path": e.Path, "description": e.Description}
		if err := c.call(ctx, http.MethodPost, "/endpoint", body, &created); err != nil {
			return nil, fmt.Errorf("register %s: %w", e.Key(), err)
		}

		ids[e.Key()] = created.ID
		sum.EndpointsCreated++
	}

	return ids, nil
}

// listRoles returns the id of every role by name.
func (c *client) listRoles(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}

	for page := 1; ; page++ {
		var list struct {
			Roles []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"roles"`
		}

		q := url.Values{"limit": {fmt.Sprint(pageSize)}, "page": {fmt.Sprint(page)}}
		if err := c.call(ctx, http.MethodGet, "/role?"+q.Encode(), nil, &list); err != nil {
			return nil, err
		}

		for _, r := range list.Roles {
			out[r.Name] = r.ID
		}

		if len(list.Roles) < pageSize {
			return out, nil
		}
	}
}

func (c *client) ensureRoles(ctx context.Context, endpointIDs map[string]int64, sum *Summary) error {
	existing, err := c.listRoles(ctx)
	if err != nil {
		return fmt.Errorf("list roles: %w", err)
	}

	for _, r := range permissions.Roles() {
		grantIDs := make([]int64, 0, len(r.Grants))

		for _, g := range r.Grants {
			id, ok := endpointIDs[g]
			if !ok {
				return fmt.Errorf("role %s grants %q, which is not registered", r.Name, g)
			}

			grantIDs = append(grantIDs, id)
		}

		body := map[string]any{"name": r.Name, "description": r.Description, "allAccess": r.AllAccess, "endpointIds": grantIDs}

		if id, ok := existing[r.Name]; ok {
			if err := c.call(ctx, http.MethodPut, fmt.Sprintf("/role/%d", id), body, nil); err != nil {
				return fmt.Errorf("update role %s: %w", r.Name, err)
			}

			sum.RolesUpdated++

			continue
		}

		if err := c.call(ctx, http.MethodPost, "/role", body, nil); err != nil {
			return fmt.Errorf("create role %s: %w", r.Name, err)
		}

		sum.RolesCreated++
	}

	return nil
}
