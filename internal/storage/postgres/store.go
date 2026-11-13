// Package postgres is the only implementation of the repository
// interfaces internal/storage defines. Nothing outside this package
// imports pgx or the generated sqlc code directly.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// Store wires a connection pool into the three repositories and the
// tenant-scoping mechanism. Store satisfies storage.TenantScope itself;
// Tenants, Accounts and Invites return the repository interfaces.
type Store struct {
	pool *pgxpool.Pool
}

// NewPool opens a pgxpool.Pool against connString and verifies it can
// reach the database before returning. ctx bounds only this initial
// connection attempt, not the pool's later lifetime.
//
// connString is expected to already be a fully formed connection string.
// cmd/solobill's production path does not call this: see
// NewPoolWithCredentials for why a single string embedding the real
// database password is never constructed there. NewPool remains for
// callers, today only tests, that already have a complete, non-secret
// connection string (a fixed, documented-fake test password) to pass.
func NewPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping: %w", err)
	}

	return pool, nil
}

// NewPoolWithCredentials opens a pool the same way NewPool does, but never
// constructs a single string embedding password: user and password are set
// directly on the parsed pgxpool.Config's ConnConfig, so no error path that
// might echo the connection string (pgx's own redaction is documented as
// best-effort, not something to rely on) can ever carry the real secret.
// This is the production call path (internal/config's real secret);
// NewPool's existing test callers use a fixed, documented-fake test
// password and don't need this.
func NewPoolWithCredentials(ctx context.Context, databaseURL, user, password string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}

	cfg.ConnConfig.User = user
	cfg.ConnConfig.Password = password

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping: %w", err)
	}

	return pool, nil
}

// NewStore wires an already-open pool into a Store. Most callers reach
// this through NewPool; a test that shares one pool across many stores,
// or across restricted and admin roles, calls it directly.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Close releases the underlying pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Tenants returns the tenant repository. Its methods are not
// tenant-scoped; see storage.TenantRepository.
func (s *Store) Tenants() storage.TenantRepository {
	return &tenantRepository{pool: s.pool}
}

// Accounts returns the account repository. Every method on it requires a
// context WithTenant produced; see storage.AccountRepository.
func (s *Store) Accounts() storage.AccountRepository {
	return &accountRepository{}
}

// Invites returns the invite repository. Every method on it requires a
// context WithTenant produced; see storage.InviteRepository.
func (s *Store) Invites() storage.InviteRepository {
	return &inviteRepository{}
}
