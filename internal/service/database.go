package service

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Database is an interface that abstracts database operations
// This allows for easier testing and mocking
type Database interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}
