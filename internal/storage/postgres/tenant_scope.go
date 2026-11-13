package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// txContextKey is the unexported type that keys the scoped transaction in
// a context, per go-conventions: a context value carries request
// metadata, keyed by a type nothing outside this package can construct.
type txContextKey struct{}

// WithTenant implements storage.TenantScope. It opens a transaction,
// scopes it to tenantID for its entire duration via
// set_config('app.tenant_id', $1, true) (the trailing true argument is
// what makes the setting transaction-local, equivalent to SET LOCAL, in a
// form that accepts a bound parameter; a literal "SET LOCAL
// app.tenant_id = $1" is not valid SQL, since SET does not take query
// parameters), runs fn with a context carrying that transaction, commits
// on a nil return and rolls back otherwise.
//
// Running set_config as the transaction's first statement, before fn runs
// anything else, is also what makes this safe under connection pooling: a
// pooled physical connection that served a different tenant's transaction
// a moment ago carries no state into this one. set_config's third
// argument ties the setting to the transaction, not the connection, so
// the pool handing back a "dirty" connection cannot leak a previous
// tenant's scope into this one; every transaction sets its own scope
// before it does anything else, and Postgres clears a transaction-local
// setting at COMMIT or ROLLBACK regardless of which connection carries it
// next.
func (s *Store) WithTenant(ctx context.Context, tenantID uuid.UUID, fn func(ctx context.Context) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}

	defer func() {
		// Rollback after a successful Commit returns pgx.ErrTxClosed; that
		// is expected, not a failure to report, so it is ignored here.
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		return fmt.Errorf("scope transaction to tenant: %w", err)
	}

	if err := fn(context.WithValue(ctx, txContextKey{}, tx)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant transaction: %w", err)
	}

	return nil
}

// txFromContext retrieves the transaction WithTenant placed on ctx. A
// tenant-scoped repository method calls this first and fails with
// storage.ErrNoTenantScope instead of falling back to the unscoped pool
// when it is missing: see storage.TenantScope.
func txFromContext(ctx context.Context) (pgx.Tx, error) {
	tx, ok := ctx.Value(txContextKey{}).(pgx.Tx)
	if !ok {
		return nil, storage.ErrNoTenantScope
	}

	return tx, nil
}
