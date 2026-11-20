package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/account"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// ErrInviteInvalid covers every reason InviteManager.AcceptInvite or
// IssueTOTPSecret refuses a token: it does not exist, it has expired, or
// it is already used, including by a concurrent accept attempt that
// reached MarkUsed first. The three are deliberately not distinguished:
// an invite link is often shared by email, and telling a second visitor
// "someone already used this" versus "this does not exist" versus "this
// expired" gives an attacker probing tokens more to go on than a flat
// refusal does.
var ErrInviteInvalid = errors.New("auth: invite token is invalid, expired or already used")

// ErrTOTPEnrollmentNotConfirmed is returned by AcceptInvite when
// totpCode does not match the invite's server-issued TOTP secret (see
// IssueTOTPSecret), including when no secret was ever issued for it.
var ErrTOTPEnrollmentNotConfirmed = errors.New("auth: totp code does not match the secret just enrolled")

// InviteManager runs the invite lifecycle: creating one, a local account
// redeeming it (AcceptInvite), and an OIDC-provisioned account redeeming
// it (AcceptInviteOIDC). The owner's intent to admit this email is
// already proven by the invite having been created and the raw token
// having been delivered out of band; AcceptInviteOIDC's job is only to
// bind the identity provider's subject for that invite's email once the
// provider itself has vouched for it, never to trust the email a token
// holder's identity claims on its own. See the package doc's "OIDC on a
// first-time subject" section for why that distinction is the whole
// point.
//
// Password hashing and TOTP run through govue's account package, the same
// code a login verifies against: hasher is the process's one
// account.Hasher, shared with Login, so the cap on concurrent Argon2id
// derivations holds for both.
type InviteManager struct {
	store  Store
	hasher *account.Hasher
	now    func() time.Time
}

// NewInviteManager builds an InviteManager against store. hasher is the
// process's account.Hasher. now is the clock CreateInvite and AcceptInvite
// use for expiry and TOTP checks; nil is time.Now.
func NewInviteManager(store Store, hasher *account.Hasher, now func() time.Time) *InviteManager {
	if now == nil {
		now = time.Now
	}

	return &InviteManager{store: store, hasher: hasher, now: now}
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

// IssueTOTPSecret generates a fresh TOTP secret for the invite rawToken
// identifies, persists it on the invite row (storage.InviteRepository.
// SetTOTPSecret), and returns it along with its provisioning URI, for the
// caller to show the invitee as a QR code before AcceptInvite runs.
//
// The server owns this secret end to end: it is generated here, never
// accepted from the client, and only ever reaches the client as the
// response to this call. Each call regenerates and overwrites the
// invite's secret, so it is safe to call again if the invitee reloads the
// enrolment page; only the secret an authenticator app was most recently
// shown can ever confirm the invite. Refuses with ErrInviteInvalid for
// the same reasons AcceptInvite already does: no such token, expired, or
// already used.
func (m *InviteManager) IssueTOTPSecret(ctx context.Context, tenantID uuid.UUID, rawToken string) (secret, provisioningURI string, err error) {
	enrolment, err := m.IssueTOTPEnrolment(ctx, tenantID, rawToken)
	if err != nil {
		return "", "", err
	}

	return enrolment.Secret, enrolment.ProvisioningURI, nil
}

// InviteEnrolment is what IssueTOTPEnrolment answers: the fresh secret,
// its provisioning URI and the invite's email, the login the account will
// have.
type InviteEnrolment struct {
	Secret          string
	ProvisioningURI string
	Email           string
}

// IssueTOTPEnrolment is IssueTOTPSecret with the invite's email as well,
// for govue's TOTP enrolment component, which shows the login next to the
// secret.
func (m *InviteManager) IssueTOTPEnrolment(ctx context.Context, tenantID uuid.UUID, rawToken string) (InviteEnrolment, error) {
	tokenHash := HashInviteToken(rawToken)

	secret, err := account.GenerateTOTPSecret()
	if err != nil {
		return InviteEnrolment{}, fmt.Errorf("auth: generate invite totp secret: %w", err)
	}

	var invite storage.Invite

	err = m.store.WithTenant(ctx, tenantID, func(ctx context.Context) error {
		found, err := m.store.Invites().FindByTokenHash(ctx, tokenHash)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrInviteInvalid
			}

			return err
		}

		if found.UsedAt != nil || !m.now().Before(found.ExpiresAt) {
			return ErrInviteInvalid
		}

		updated, err := m.store.Invites().SetTOTPSecret(ctx, found.ID, secret)
		if err != nil {
			if errors.Is(err, storage.ErrConflict) {
				return ErrInviteInvalid
			}

			return err
		}

		invite = updated

		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInviteInvalid) {
			return InviteEnrolment{}, ErrInviteInvalid
		}

		return InviteEnrolment{}, fmt.Errorf("auth: issue invite totp secret: %w", err)
	}

	provisioningURI, err := account.TOTPProvisioningURI(TOTPIssuer, invite.Email, secret)
	if err != nil {
		return InviteEnrolment{}, fmt.Errorf("auth: build invite totp provisioning uri: %w", err)
	}

	return InviteEnrolment{Secret: secret, ProvisioningURI: provisioningURI, Email: invite.Email}, nil
}

// AcceptInviteParams is AcceptInvite's input.
//
// There is no TOTPSecret field: the secret is server-owned, issued by a
// prior call to IssueTOTPSecret and stored on the invite row itself.
// TOTPCode is only what the invitee's authenticator app now computes from
// that secret; confirming it against the invite's stored secret, setting
// the password, creating the account and marking the invite used all run
// as one atomic unit: see AcceptInvite.
type AcceptInviteParams struct {
	TenantID uuid.UUID
	RawToken string
	// Login, when set, has to be the invite's email, compared case
	// insensitively, or the invite is refused like an invalid one. govue's
	// TOTP enrolment component sends the login it showed; the account's
	// email is always the invite's own either way.
	Login    string
	Password string
	TOTPCode string
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
	// govue's policy applies here as at every other password: at least
	// account.MinPasswordLength characters. ErrPasswordTooShort,
	// ErrPasswordTooLong and ErrHashingBusy stay matchable for the caller.
	passwordHash, err := m.hasher.HashPassword(ctx, params.Password)
	if err != nil {
		return storage.Account{}, fmt.Errorf("auth: hash invite accept password: %w", err)
	}

	tokenHash := HashInviteToken(params.RawToken)

	var accepted storage.Account

	err = m.store.WithTenant(ctx, params.TenantID, func(ctx context.Context) error {
		invite, err := m.store.Invites().FindByTokenHash(ctx, tokenHash)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrInviteInvalid
			}

			return err
		}

		if invite.UsedAt != nil || !m.now().Before(invite.ExpiresAt) {
			return ErrInviteInvalid
		}

		if params.Login != "" && !strings.EqualFold(params.Login, invite.Email) {
			return ErrInviteInvalid
		}

		// The server, not the client, owns the secret this code is
		// checked against: it was persisted by a prior IssueTOTPSecret
		// call. An invite that never had one issued, or whose secret is
		// empty for any other reason, must refuse exactly like a code
		// mismatch would, never accept an unenrolled invite.
		if invite.TOTPSecret == nil || *invite.TOTPSecret == "" {
			return ErrTOTPEnrollmentNotConfirmed
		}

		matchedCounter, totpOK := account.VerifyTOTPCode(*invite.TOTPSecret, params.TOTPCode, m.now())
		if !totpOK {
			return ErrTOTPEnrollmentNotConfirmed
		}

		if _, err := m.store.Invites().MarkUsed(ctx, invite.ID, m.now()); err != nil {
			if errors.Is(err, storage.ErrConflict) {
				return ErrInviteInvalid
			}

			return err
		}

		totpSecret := *invite.TOTPSecret

		created, err := m.store.Accounts().Create(ctx, storage.Account{
			TenantID:     params.TenantID,
			Email:        invite.Email,
			PasswordHash: &passwordHash,
			TOTPSecret:   &totpSecret,
			// Set at creation time to the counter the enrolment code
			// itself matched, so that same code cannot immediately be
			// replayed as a first login: govue's login consumes a code
			// only through a conditional write that requires a higher
			// counter (account.Store.AdvanceTOTPCounter). A counter above
			// zero is also what makes the secret confirmed for govue.
			TOTPLastCounter: &matchedCounter,
			Role:            invite.Role,
			Status:          storage.AccountStatusActive,
		})
		if err != nil {
			return err
		}

		accepted = created

		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInviteInvalid) {
			return storage.Account{}, ErrInviteInvalid
		}
		if errors.Is(err, ErrTOTPEnrollmentNotConfirmed) {
			return storage.Account{}, ErrTOTPEnrollmentNotConfirmed
		}

		return storage.Account{}, fmt.Errorf("auth: accept invite: %w", err)
	}

	return accepted, nil
}

// PeekInvite checks that rawToken names an invite that is not yet used
// and not expired, without marking it used or creating anything:
// internal/httpapi's invite-accept-via-OIDC endpoint calls this before it
// commits the browser to a full OIDC round trip, so an unknown, expired
// or already-used token fails fast, before the identity provider is ever
// involved.
//
// A successful PeekInvite proves nothing by itself and is advisory only:
// the real check is AcceptInviteOIDC's own, which re-validates and marks
// the invite used atomically at the one moment that is supposed to be
// final. Nothing relies on PeekInvite having been called at all.
func (m *InviteManager) PeekInvite(ctx context.Context, tenantID uuid.UUID, rawToken string) error {
	tokenHash := HashInviteToken(rawToken)

	err := m.store.WithTenant(ctx, tenantID, func(ctx context.Context) error {
		found, err := m.store.Invites().FindByTokenHash(ctx, tokenHash)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrInviteInvalid
			}

			return err
		}

		if found.UsedAt != nil || !m.now().Before(found.ExpiresAt) {
			return ErrInviteInvalid
		}

		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInviteInvalid) {
			return ErrInviteInvalid
		}

		return fmt.Errorf("auth: peek invite: %w", err)
	}

	return nil
}

// AcceptInviteOIDCParams is AcceptInviteOIDC's input. There is no email
// field: the account's email is always the invite's own, never anything
// the identity provider's token claims, since that claim is exactly what
// the vulnerability this type exists to avoid fixing was built on.
type AcceptInviteOIDCParams struct {
	TenantID uuid.UUID
	RawToken string
	// Subject is the identity provider's verified, stable subject claim
	// for the login that is redeeming this invite (govue oidc.Identity's
	// Subject, already checked by Provider.Exchange before this runs).
	// It is bound to the new account as OIDCSubject, the only value
	// ResolveOIDCIdentity ever matches a later login against.
	Subject string
}

// AcceptInviteOIDC redeems a single-use invite token into a new, active
// OIDC-provisioned Account: no password, no TOTP secret, OIDCSubject set
// to Subject. It is AcceptInvite's counterpart for the OIDC login path,
// sharing the same single-use token lifecycle (unknown, expired or
// already-used all answer ErrInviteInvalid, and MarkUsed's "WHERE
// used_at IS NULL" guard is the same concurrency proof
// TestAcceptInviteIsSingleUseUnderConcurrentAccept already establishes
// for AcceptInvite) and differing only in what admits the invitee: an
// identity provider's verified subject instead of a password and a TOTP
// code.
//
// The whole operation, marking the invite used and creating the account,
// runs inside one TenantScope.WithTenant transaction, exactly like
// AcceptInvite: a failure at either step, including the account
// creation itself, rolls the invite's MarkUsed back too, so a token is
// never left burned without the account its redemption was supposed to
// create.
//
// Account creation can still fail with storage.ErrConflict when an
// account already exists for the invite's email (accounts_tenant_id_
// email_lower_idx) or for Subject within this tenant (accounts_tenant_
// id_oidc_subject_idx, see db/migrations). Both are the schema's own
// backstop against exactly the takeover this method's design is meant
// to rule out by construction: a second invite redeemed by a different
// subject for an email an account already holds creates nothing and
// changes nothing about the account that already exists, it only burns
// the second invite's own token and returns the conflict to the caller.
func (m *InviteManager) AcceptInviteOIDC(ctx context.Context, params AcceptInviteOIDCParams) (storage.Account, error) {
	tokenHash := HashInviteToken(params.RawToken)

	var account storage.Account

	err := m.store.WithTenant(ctx, params.TenantID, func(ctx context.Context) error {
		invite, err := m.store.Invites().FindByTokenHash(ctx, tokenHash)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return ErrInviteInvalid
			}

			return err
		}

		if invite.UsedAt != nil || !m.now().Before(invite.ExpiresAt) {
			return ErrInviteInvalid
		}

		if _, err := m.store.Invites().MarkUsed(ctx, invite.ID, m.now()); err != nil {
			if errors.Is(err, storage.ErrConflict) {
				return ErrInviteInvalid
			}

			return err
		}

		subject := params.Subject

		created, err := m.store.Accounts().Create(ctx, storage.Account{
			TenantID:    params.TenantID,
			Email:       invite.Email,
			OIDCSubject: &subject,
			Role:        invite.Role,
			Status:      storage.AccountStatusActive,
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

		return storage.Account{}, fmt.Errorf("auth: accept oidc invite: %w", err)
	}

	return account, nil
}
