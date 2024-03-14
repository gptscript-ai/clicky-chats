package extendedapi

import (
	"context"
)

type extendedAPIKey struct{}

func NewExtendedContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, extendedAPIKey{}, struct{}{})
}

func IsExtendedAPIKey(ctx context.Context) bool {
	return ctx.Value(extendedAPIKey{}) != nil
}
