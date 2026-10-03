package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is a connection pool to the Argus event store.
type DB struct {
	pool *pgxpool.Pool
}

// Open connects to the database at dsn and checks it is reachable, so a
// missing database fails at startup rather than on the first flush.
func Open(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Insert writes a batch of rows (in columns order) with COPY, which sends the
// whole batch in one round trip. Its signature matches FlushFunc.
func (db *DB) Insert(ctx context.Context, rows [][]any) error {
	_, err := db.pool.CopyFrom(ctx, pgx.Identifier{"events"}, columns, pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("copy %d rows into events: %w", len(rows), err)
	}
	return nil
}

// Close releases every pooled connection.
func (db *DB) Close() {
	db.pool.Close()
}
