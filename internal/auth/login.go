package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/passkey"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// TOTPIssuer names solobill in the entry an authenticator app shows, for a
// secret enrolled through an invite and for govue's account service alike.
const TOTPIssuer = "solobill"

// ErrAccountNotActive is what StartSession answers for an account that is
// no longer active by the time a verified login wants a session for it.
var ErrAccountNotActive = errors.New("auth: account is not active")

// LoginConfig configures NewLogin.
type LoginConfig struct {
	// Store is the storage backend. Required.
	Store Store

	// Sessions is the cookie session manager every login path ends at.
	// Required.
	Sessions *session.Manager

	// Hasher is the one Argon2id hasher of the process, shared with the
	// InviteManager so its concurrency cap holds for both. Required.
	Hasher *account.Hasher

	// Logger receives govue's operational events. Required.
	Logger *slog.Logger

	// TrustedOrigins are the cross origin callers the govue handlers
	// accept, the same list as the CORS and CSRF configuration.
	TrustedOrigins []string

	// Passkey enables passkeys. Nil leaves them off.
	Passkey *PasskeyConfig

	// Now is the clock. Nil is time.Now.
	Now func() time.Time
}

// PasskeyConfig is the relying party of the passkeys: required
// configuration, never derived from a request. See govue's passkey.Config.
type PasskeyConfig struct {
	RPID           string
	RPDisplayName  string
	Origins        []string
	AllowPlainHTTP bool
}

// Login holds govue's account and passkey handlers, wired to solobill's
// accounts, tenants and sessions. It is the one place that connects govue's
// account, passkey and session packages, which import nothing from each
// other.
type Login struct {
	// Accounts serves the password and TOTP login, the logout and the
	// password change. No signup and no TOTP confirm handler is mounted:
	// accounts arrive through invites, which enrol the password and the
	// TOTP secret in one step.
	Accounts *account.Handlers

	// Passkeys serves the passkey ceremonies, the list and the delete. Nil
	// when LoginConfig.Passkey is nil.
	Passkeys *passkey.Handlers

	store    Store
	sessions *session.Manager
}

// NewLogin builds the govue service and handlers over AccountStore and
// PasskeyStore.
func NewLogin(cfg LoginConfig) (*Login, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("auth: login needs a store")
	case cfg.Sessions == nil:
		return nil, errors.New("auth: login needs a session manager")
	case cfg.Hasher == nil:
		return nil, errors.New("auth: login needs a password hasher")
	case cfg.Logger == nil:
		return nil, errors.New("auth: login needs a logger")
	}

	l := &Login{store: cfg.Store, sessions: cfg.Sessions}

	service, err := account.NewService(account.ServiceConfig{
		Store:  NewAccountStore(cfg.Store),
		Hasher: cfg.Hasher,
		Issuer: TOTPIssuer,
		// The session subject is the accounts.id UUID for every login
		// path, OIDC included (see Identity), so ending every session of
		// an account is EndAll on that id.
		EndSessions: func(ctx context.Context, accountID string) error {
			return cfg.Sessions.EndAll(ctx, accountID)
		},
		Logger: cfg.Logger,
		Now:    cfg.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: account service: %w", err)
	}

	l.Accounts, err = account.NewHandlers(account.HandlerConfig{
		Service:         service,
		StartSession:    l.StartSession,
		EndSession:      l.endSession,
		CurrentIdentity: l.currentIdentity,
		TrustedOrigins:  cfg.TrustedOrigins,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	if cfg.Passkey != nil {
		l.Passkeys, err = passkey.New(passkey.Config{
			RPID:           cfg.Passkey.RPID,
			RPDisplayName:  cfg.Passkey.RPDisplayName,
			Origins:        cfg.Passkey.Origins,
			Store:          NewPasskeyStore(cfg.Store),
			StartSession:   l.StartSession,
			CurrentAccount: l.currentAccount,
			OtherFactors:   l.otherFactors,
			TrustedOrigins: cfg.TrustedOrigins,
			AllowPlainHTTP: cfg.Passkey.AllowPlainHTTP,
			Now:            cfg.Now,
		})
		if err != nil {
			return nil, fmt.Errorf("auth: %w", err)
		}
	}

	return l, nil
}

// StartSession turns an identity govue verified, by password and TOTP or
// by passkey, into a solobill session: it reads the account in the tenant
// the request is scoped to, requires it to be active, ends the session the
// request already carries, and starts the session Identity.Session builds,
// the same one an OIDC login and an invite accept start.
func (l *Login) StartSession(ctx context.Context, w http.ResponseWriter, r *http.Request, id account.Identity) error {
	accountID, err := uuid.Parse(id.ID)
	if err != nil {
		return fmt.Errorf("auth: start session: account id: %w", err)
	}

	var found storage.Account

	err = withScope(ctx, l.store, func(ctx context.Context) error {
		var err error
		found, err = l.store.Accounts().FindByID(ctx, accountID)

		return err
	})
	if err != nil {
		return fmt.Errorf("auth: start session: %w", err)
	}
	if found.Status != storage.AccountStatusActive {
		return ErrAccountNotActive
	}

	// govue does not end a session the request already carries. Ending it
	// first, and failing closed, keeps a browser from holding two. A
	// request without a session cookie has nothing to end, and the answer
	// then carries only the new cookie.
	if c, err := r.Cookie(l.sessions.CookieName()); err == nil && c.Value != "" {
		if err := l.sessions.End(ctx, w, r); err != nil {
			return fmt.Errorf("auth: end the previous session: %w", err)
		}
	}

	if _, err := l.sessions.Start(ctx, w, IdentityFromAccount(found, "", nil).Session()); err != nil {
		return fmt.Errorf("auth: start session: %w", err)
	}

	return nil
}

func (l *Login) endSession(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	return l.sessions.End(ctx, w, r)
}

// currentIdentity is the account a request is logged in as, for the
// password change. Login is the email the session carries, which govue
// looks the account up by again before it writes.
func (l *Login) currentIdentity(r *http.Request) (account.Identity, bool, error) {
	s, ok, err := l.sessions.Read(r.Context(), r)
	if err != nil || !ok {
		return account.Identity{}, false, err
	}

	return account.Identity{ID: s.Subject, Login: s.Email}, true, nil
}

// currentAccount is currentIdentity plus the time of the login, which
// govue's passkey handlers compare with their reauthentication window.
func (l *Login) currentAccount(r *http.Request) (account.Identity, time.Time, bool, error) {
	s, ok, err := l.sessions.Read(r.Context(), r)
	if err != nil || !ok {
		return account.Identity{}, time.Time{}, false, err
	}

	return account.Identity{ID: s.Subject, Login: s.Email}, s.IssuedAt, true, nil
}

// otherFactors counts the ways the account logs in besides its passkeys:
// a password with a confirmed TOTP secret, and a bound OIDC subject.
// govue refuses to delete the last passkey when this is zero.
func (l *Login) otherFactors(ctx context.Context, id account.Identity) (int, error) {
	accountID, err := uuid.Parse(id.ID)
	if err != nil {
		return 0, fmt.Errorf("auth: other factors: account id: %w", err)
	}

	var found storage.Account

	err = withScope(ctx, l.store, func(ctx context.Context) error {
		var err error
		found, err = l.store.Accounts().FindByID(ctx, accountID)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("auth: other factors: %w", err)
	}

	return OtherFactors(found), nil
}

// OtherFactors counts the login factors of an account besides its
// passkeys: 1 for a password with a confirmed TOTP secret (a NULL counter
// counts as confirmed, see storage.LoginAccount), 1 for an OIDC subject.
func OtherFactors(a storage.Account) int {
	n := 0
	if a.PasswordHash != nil && *a.PasswordHash != "" && a.TOTPSecret != nil && *a.TOTPSecret != "" &&
		(a.TOTPLastCounter == nil || *a.TOTPLastCounter > 0) {
		n++
	}
	if a.OIDCSubject != nil && *a.OIDCSubject != "" {
		n++
	}

	return n
}
