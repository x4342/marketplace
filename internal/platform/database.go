package platform

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func OpenDatabase(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		// Driver configuration errors can contain the supplied DSN.
		return nil, errors.New("invalid database configuration")
	}
	return pool, nil
}
