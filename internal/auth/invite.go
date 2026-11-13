package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// ErrInviteInvalid covers every reason InviteManager.AcceptInvite refuses
// a token: it does not exist, it has expired, or it is already used,
// including by a concurrent accept attempt that reached MarkUsed first.
// The three are deliberately not distinguished: an invite link is often
// shared by email, and telling a second visitor "someone already used
// this" versus "this does not exist" versus "this expired" gives an
// attacker probing tokens more to go on than a flat refusal does.
var ErrInviteInvalid = errors.New("auth: invite token is invalid, expired or already used")

// ErrTOTPEnrollmentNotConfirmed is returned by AcceptInvite when
// totpCode does not match totpSecret: see AcceptInviteParams.
var ErrTOTPEnrollmentNotConfirmed = errors.New("auth: totp code does not match the secret just enrolled")

// InviteManager runs the invite lifecycle: creating one, and a local
// account redeeming it. It is not where an OIDC account is admitted; see
// ResolveOIDCIdentity and the package doc for why that path is
// provisioned directly instead of through a redeemed token.
type InviteManager struct {
	store Store
	now   func() time.Time
}

// NewInviteManager builds an InviteManager against store. now is the
// clock CreateInvite and AcceptInvite use for expiry and TOTP checks;
// nil is time.Now.
func NewInviteManager(store Store, now func() time.Time) *InviteManager {
	if now == nil {
		now = time.Now
	}

	return &InviteManager{store: store, now: now}
}

// CreateInviteParams is CreateInvite's input.
type CreateInviteParams struct {
	TenantID  uuid.UUID
	Email     string
	Role      storage.AccountRole
	CreatedBy *uuid.UUID
	// TTL is how long the invite is valid for. The caller decides this,
	// rather than a package constant, since how long an owner's invite
	// link should stay live is a product decision a later step's HTTP
	// handler is better placed to own.
	TTL time.Duration
}

// CreateInvite generates a fresh invite token, stores only its hash (see
// GenerateInviteToken), and returns the raw token once, for the caller to
// send as the invite link; it is never kept anywhere after this call
// returns.
func (m *InviteManager) CreateInvite(ctx context.Context, params CreateInviteParams) (rawToken string, invite storage.Invite, err error) {
	rawToken, hash, err := GenerateInviteToken()
	if err != nil {
		return "", storage.Invite{}, err
	}

	err = m.store.WithTenant(ctx, params.TenantID, func(ctx context.Context) error {
		created, err := m.store.Invites().Create(ctx, storage.Invite{
			TenantID:  params.TenantID,
			Email:     params.Email,
			Role:      params.Role,
			TokenHash: hash,
			ExpiresAt: m.now().Add(params.TTL),
			CreatedBy: params.CreatedBy,
		})
		if err != nil {
			return err
		}

		invite = created

		return nil
	})
	if err != nil {
		return "", storage.Invite{}, fmt.Errorf("auth: create invite: %w", err)
	}

	return rawToken, invite, nil
}

// AcceptInviteParams is AcceptInvite's input.
//
// TOTPSecret is a secret the caller already generated with
// GenerateTOTPSecret and already showed the invitee as a QR code (via
// TOTPProvisioningURI), before this call: enrolling TOTP needs the
// invitee to scan a secret and type back a code before any of this can
// be confirmed, which is a request of its own and needs no database
// write at all. What has to be atomic, and is what this call makes
// atomic, is confirming that code against the secret, setting the
// password, creating the account and marking the invite used, as one
// unit: see AcceptInvite.
type AcceptInviteParams struct {
	TenantID   uuid.UUID
	RawToken   string
	Password   string
	TOTPSecret string
	TOTPCode   string
}

// AcceptInvite redeems a single-use invite token into a new, fully
// active local Account: password and TOTP are both required in this one
// call, rather than a password now and a TOTP enrollment deferred to
// after a first login. A foundation that let an account sit active with
// only a password would mean a window, of unbounded length if the
// invitee never comes back to finish enrolling, where the second factor
// docs/concepts/solobill.md's local path exists for is simply not there
// yet; requiring both up front removes that window rather than managing
// it.
//
// The whole operation, confirming TOTPCode, hashing Password, marking the
// invite used and creating the account, runs inside one
// TenantScope.WithTenant transaction, so a failure at any step rolls
// every earlier write in this call back too: an invite is never left
// marked used without the account that redemption was supposed to
// create, and an account is never created from a token that two
// concurrent callers both believed was still unused.
//
// That single-use guarantee is InviteRepository.MarkUsed's "WHERE used_at
// IS NULL" guard (see internal/storage/postgres/queries/invites.sql): of
// two concurrent calls racing the same token, the one whose UPDATE
// commits first wins, and the other's MarkUsed call affects zero rows,
// answers storage.ErrConflict, and this method turns that into
// ErrInviteInvalid before anything is created. See
// TestAcceptInviteIsSingleUseUnderConcurrentAccept for the test that
// actually races this against a real Postgres.
func (m *InviteManager) AcceptInvite(ctx context.Context, params AcceptInviteParams) (storage.Account, error) {
	if !VerifyTOTPCode(params.TOTPSecret, params.TOTPCode, m.now()) {
		return storage.Account{}, ErrTOTPEnrollmentNotConfirmed
	}

	passwordHash, err := HashPassword(params.Password)
	if err != nil {
		return storage.Account{}, fmt.Errorf("auth: hash invite accept password: %w", err)
	}

	tokenHash := HashInviteToken(params.RawToken)
	totpSecret := params.TOTPSecret

	var account storage.Account

	err = m.store.WithTenant(ctx, params.TenantID, func(ctx context.Context) error {
		invite, err := m.store.Invites().FindByTokenHash(ctx, tokenHash)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrInviteInvalid
			}

			return err
		}

		if !m.now().Before(invite.ExpiresAt) {
			return ErrInviteInvalid
		}

		if _, err := m.store.Invites().MarkUsed(ctx, invite.ID, m.now()); err != nil {
			if errors.Is(err, storage.ErrConflict) {
				return ErrInviteInvalid
			}

			return err
		}

		created, err := m.store.Accounts().Create(ctx, storage.Account{
			TenantID:     params.TenantID,
			Email:        invite.Email,
			PasswordHash: &passwordHash,
			TOTPSecret:   &totpSecret,
			Role:         invite.Role,
			Status:       storage.AccountStatusActive,
		})
		if err != nil {
			return err
		}

		account = created

		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInviteInvalid) {
			return storage.Account{}, ErrInviteInvalid
		}

		return storage.Account{}, fmt.Errorf("auth: accept invite: %w", err)
	}

	return account, nil
}
