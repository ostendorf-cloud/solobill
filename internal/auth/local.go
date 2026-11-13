package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// ErrInvalidCredentials is the one failure LocalAuthenticator.Login
// returns: no such tenant, no such account, an account that is pending
// or disabled, a wrong password, or a wrong or missing TOTP code all
// answer identically. Distinguishing any of them to the caller would
// tell an attacker which part of a guess was right; see Login.
var ErrInvalidCredentials = errors.New("auth: invalid tenant, email, password or totp code")

// dummyPasswordHash and dummyTOTPSecret are a well formed hash and
// secret that no real account uses, computed once (argon2id hashing is
// deliberately expensive, so this is not redone on every login that
// turns out to be against a nonexistent account). Login compares against
// these when the tenant or the account was not found at all, so that
// case still pays the same argon2id and HMAC cost a real wrong-password
// or wrong-code attempt does: see Login for why that is the point.
var (
	dummyPasswordHash = sync.OnceValue(func() string {
		hash, err := HashPassword("not-a-real-account-timing-equalizer-only")
		if err != nil {
			// HashPassword only fails if crypto/rand itself fails, which
			// means nothing on this machine can be trusted to run safely
			// at all; there is no reasonable fallback value to return
			// instead; see the package doc for this already being the
			// required failure mode for a reading of crypto/rand.
			panic(fmt.Errorf("auth: compute the dummy password hash: %w", err))
		}

		return hash
	})
	dummyTOTPSecret = sync.OnceValue(func() string {
		secret, err := GenerateTOTPSecret()
		if err != nil {
			panic(fmt.Errorf("auth: compute the dummy totp secret: %w", err))
		}

		return secret
	})
)

// LocalAuthenticator runs the local account login path: a tenant slug, an
// email, a password and a TOTP code, verified together in one call.
//
// One call rather than a password step followed by a separate TOTP step
// with some short lived intermediate state, which was the other option:
// a two step flow needs somewhere to keep "this browser already proved
// the password" between the two requests, which is itself a small session
// to design, secure and expire, for a Vue shell this milestone explicitly
// describes as "a shell", not a finished login UX. A later HTTP step is
// free to still show two screens (password, then a TOTP prompt) and have
// the second screen resubmit both values together; that is a UI choice,
// and it still ends at this one call.
type LocalAuthenticator struct {
	store Store
	now   func() time.Time
}

// NewLocalAuthenticator builds a LocalAuthenticator against store. now is
// the clock the TOTP check runs against; nil is time.Now.
func NewLocalAuthenticator(store Store, now func() time.Time) *LocalAuthenticator {
	if now == nil {
		now = time.Now
	}

	return &LocalAuthenticator{store: store, now: now}
}

// Login verifies tenantSlug, email, password and totpCode together and
// returns the resulting Identity. Every failure, whichever step actually
// caused it, is ErrInvalidCredentials: a caller (and the person on the
// other end of the request) learns only that the attempt failed, never
// which of "no such tenant", "no such account", "a pending or disabled
// account", "wrong password" or "wrong totp code" it was.
//
// The password and TOTP checks always run, even when the tenant or the
// account was not found at all, against dummyPasswordHash and
// dummyTOTPSecret instead of a real account's values. Skipping them
// for a nonexistent account would make that case answer faster than a
// wrong-password attempt against a real one, which is itself the
// signal an attacker needs to enumerate which email addresses have an
// account at all; running the same argon2id hash and the same HMAC
// comparisons regardless keeps the two cases taking the same shape of
// work. Tenant resolution itself (TenantRepository.FindBySlug) is not
// equalized the same way: a tenant slug is not a secret, it is the
// subdomain or path segment the login page itself is already serving,
// so its existence leaks nothing a browser did not already know.
func (a *LocalAuthenticator) Login(ctx context.Context, tenantSlug, email, password, totpCode string) (Identity, error) {
	tenant, tenantErr := a.store.Tenants().FindBySlug(ctx, tenantSlug)

	var (
		account      storage.Account
		accountFound bool
	)

	if tenantErr == nil {
		// A read-only lookup: the closure always returns nil so
		// WithTenant commits rather than rolling back an otherwise
		// successful, if empty, read.
		if err := a.store.WithTenant(ctx, tenant.ID, func(ctx context.Context) error {
			found, err := a.store.Accounts().FindByEmail(ctx, email)
			if err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					return nil
				}

				return err
			}

			account = found
			accountFound = found.Status == storage.AccountStatusActive &&
				found.PasswordHash != nil && found.TOTPSecret != nil

			return nil
		}); err != nil {
			return Identity{}, fmt.Errorf("auth: look up local account: %w", err)
		}
	}

	passwordHash := dummyPasswordHash()
	totpSecret := dummyTOTPSecret()

	if accountFound {
		passwordHash = *account.PasswordHash
		totpSecret = *account.TOTPSecret
	}

	// Both checks run unconditionally, whether or not an account was
	// found, and neither short-circuits the other: see the method doc.
	passwordOK, _ := VerifyPassword(passwordHash, password)
	totpCounter, totpOK := VerifyTOTPCode(totpSecret, totpCode, a.now())

	// A code that otherwise matches is still refused if it is not newer
	// than the last one this account already consumed: RFC 6238 Section
	// 5.2 requires a verifier to reject a second use of an already
	// validated OTP, so the exact same code (observed over someone's
	// shoulder, or via a compromised link) cannot be replayed for the
	// rest of its up-to-90-second skew window. This check is gated on
	// accountFound, which the function already branches on above, so it
	// adds no new timing signal beyond what already distinguishes a real
	// account from a nonexistent one.
	if accountFound && totpOK && account.TOTPLastCounter != nil && totpCounter <= uint64(*account.TOTPLastCounter) {
		totpOK = false
	}

	if !accountFound || !passwordOK || !totpOK {
		return Identity{}, ErrInvalidCredentials
	}

	matchedCounter := int64(totpCounter)
	account.TOTPLastCounter = &matchedCounter

	if err := a.store.WithTenant(ctx, account.TenantID, func(ctx context.Context) error {
		updated, err := a.store.Accounts().Update(ctx, account)
		if err != nil {
			return err
		}

		account = updated

		return nil
	}); err != nil {
		return Identity{}, fmt.Errorf("auth: persist totp counter: %w", err)
	}

	return IdentityFromAccount(account, "", nil), nil
}
