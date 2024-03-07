package extendedapi

import (
	"context"
)

type extendedApiKey struct{}

func NewExtendedContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, extendedApiKey{}, struct{}{})
}

func IsExtendedAPIKey(ctx context.Context) bool {
	return ctx.Value(extendedApiKey{}) != nil
}
