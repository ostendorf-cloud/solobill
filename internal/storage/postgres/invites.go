package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// inviteRepository satisfies storage.InviteRepository. Every method
// requires a context storage.TenantScope.WithTenant produced; see
// txFromContext. FindByTokenHash is not an exception: the caller resolves
// the tenant first (for example from a slug via Store.Tenants, which
// carries no tenant scope of its own) and opens its own WithTenant scope
// before calling it, exactly like every other method here.
type inviteRepository struct{}

// Create implements storage.InviteRepository.
func (r *inviteRepository) Create(ctx context.Context, invite storage.Invite) (storage.Invite, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Invite{}, err
	}

	row, err := sqlcgen.New(tx).CreateInvite(ctx, sqlcgen.CreateInviteParams{
		TenantID:  invite.TenantID,
		Email:     invite.Email,
		Role:      string(invite.Role),
		TokenHash: invite.TokenHash,
		ExpiresAt: toTimestamptz(invite.ExpiresAt),
		CreatedBy: ptrToUUID(invite.CreatedBy),
	})
	if err != nil {
		return storage.Invite{}, mapWriteError(err)
	}

	return inviteFromRow(row), nil
}

// FindByID implements storage.InviteRepository.
func (r *inviteRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.Invite, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Invite{}, err
	}

	row, err := sqlcgen.New(tx).GetInviteByID(ctx, id)
	if err != nil {
		return storage.Invite{}, mapReadError(err)
	}

	return inviteFromRow(row), nil
}

// FindByTokenHash implements storage.InviteRepository.
func (r *inviteRepository) FindByTokenHash(ctx context.Context, tokenHash string) (storage.Invite, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Invite{}, err
	}

	row, err := sqlcgen.New(tx).GetInviteByTokenHash(ctx, tokenHash)
	if err != nil {
		return storage.Invite{}, mapReadError(err)
	}

	return inviteFromRow(row), nil
}

// MarkUsed implements storage.InviteRepository. The underlying query is
// guarded by "WHERE ... AND used_at IS NULL" (see
// internal/storage/postgres/queries/invites.sql), so a call for an invite
// that is already used, including one a concurrent call just used a moment
// ago, affects zero rows. A :one sqlc query with zero rows answers
// pgx.ErrNoRows on Scan, which is not a unique violation and so would
// otherwise fall through mapWriteError's generic wrap; it is checked for
// here instead and mapped to storage.ErrConflict, the same sentinel a
// caller already checks for any other write conflict.
func (r *inviteRepository) MarkUsed(ctx context.Context, id uuid.UUID, usedAt time.Time) (storage.Invite, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Invite{}, err
	}

	row, err := sqlcgen.New(tx).MarkInviteUsed(ctx, sqlcgen.MarkInviteUsedParams{
		ID:     id,
		UsedAt: toTimestamptz(usedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.Invite{}, storage.ErrConflict
		}

		return storage.Invite{}, mapWriteError(err)
	}

	return inviteFromRow(row), nil
}

// SetTOTPSecret implements storage.InviteRepository. The underlying query
// is guarded the same way MarkInviteUsed's is ("WHERE ... AND used_at IS
// NULL"): a zero-row RETURNING, meaning the invite is already used, maps
// to storage.ErrConflict the same way.
func (r *inviteRepository) SetTOTPSecret(ctx context.Context, id uuid.UUID, secret string) (storage.Invite, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.Invite{}, err
	}

	row, err := sqlcgen.New(tx).SetInviteTOTPSecret(ctx, sqlcgen.SetInviteTOTPSecretParams{
		ID:         id,
		TotpSecret: ptrToText(&secret),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.Invite{}, storage.ErrConflict
		}

		return storage.Invite{}, mapWriteError(err)
	}

	return inviteFromRow(row), nil
}

func inviteFromRow(row sqlcgen.Invite) storage.Invite {
	return storage.Invite{
		ID:         row.ID,
		TenantID:   row.TenantID,
		Email:      row.Email,
		Role:       storage.AccountRole(row.Role),
		TokenHash:  row.TokenHash,
		ExpiresAt:  timeFromTimestamptz(row.ExpiresAt),
		UsedAt:     timeToPtr(row.UsedAt),
		CreatedBy:  uuidToPtr(row.CreatedBy),
		CreatedAt:  timeFromTimestamptz(row.CreatedAt),
		TOTPSecret: textToPtr(row.TotpSecret),
	}
}
