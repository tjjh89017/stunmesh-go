package main

import (
	"context"
	"errors"
)

var (
	ErrEndpointDataNotFound = errors.New("endpoint data not found")
)

type Store interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string) error
}
