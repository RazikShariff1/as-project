package authctx

import (
	"context"

	"gofr.dev/pkg/gofr"
)

func withValue(ctx context.Context, k key, v string) context.Context {
	return context.WithValue(ctx, k, v)
}

func value(ctx *gofr.Context, k key) string {
	v, _ := ctx.Request.Context().Value(k).(string)

	return v
}
