package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"gofr.dev/pkg/gofr"
)

const (
	callTimeout = 3 * time.Second
	maxBody     = 1 << 20
)

// GetData reads GET path from a GoFr HTTP service registered under service and decodes the {"data": ...} envelope into out. A
// sibling that is slow or down costs at most callTimeout.
func GetData(ctx *gofr.Context, service, path string, out any) error {
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	resp, err := ctx.GetHTTPService(service).Get(callCtx, path, nil)
	if err != nil {
		return fmt.Errorf("%s %s: %w", service, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: status %d", service, path, resp.StatusCode)
	}

	var body struct {
		Data json.RawMessage `json:"data"`
	}

	if err = json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return fmt.Errorf("%s %s: unreadable response: %w", service, path, err)
	}

	return json.Unmarshal(body.Data, out)
}
