package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"
	"github.com/ostendorf-cloud/govue/session"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// govue's account and passkey packages are tenant agnostic: their Store
// contracts leave scoping to the adapter, read from ctx. This file is
// where solobill puts its tenant on that ctx, and AccountStore and
// PasskeyStore are where they read it back.
//
// A request reaches the adapters with one of three states:
//
//   - a known tenant: every call runs inside TenantScope.WithTenant for it,
//     so row level security scopes every statement.
//   - an unknown tenant: the login request named a slug no tenant has. A
//     lookup answers not found, so govue runs the same work and gives the
//     same 401 as for a wrong password. A tenant slug is not a secret, but
//     the answer still does not tell the two apart, as before #86.
//   - no tenant at all: a wiring mistake. The adapters fail closed with
//     ErrNoTenant, never an unscoped query.

// ErrNoTenant is what AccountStore and PasskeyStore answer for a context
// that carries no tenant scope at all.
var ErrNoTenant = errors.New("auth: no tenant scope on the request context")

// errUnknownTenant is the internal outcome of a scope whose slug matched no
// tenant. The adapters turn it into their package's not found.
var errUnknownTenant = errors.New("auth: unknown tenant")

type tenantContextKey struct{}

type tenantScope struct {
	id    uuid.UUID
	known bool
}

// ContextWithTenant returns ctx scoped to tenantID for AccountStore and
// PasskeyStore.
func ContextWithTenant(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantScope{id: tenantID, known: true})
}

// ContextWithUnknownTenant returns ctx scoped to a tenant that does not
// exist: every lookup through AccountStore and PasskeyStore answers not
// found.
func ContextWithUnknownTenant(ctx context.Context) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantScope{})
}

// TenantFromContext returns the tenant ctx is scoped to. ok is false for no
// scope and for an unknown tenant.
func TenantFromContext(ctx context.Context) (uuid.UUID, bool) {
	scope, ok := ctx.Value(tenantContextKey{}).(tenantScope)
	if !ok || !scope.known {
		return uuid.Nil, false
	}

	return scope.id, true
}

// withScope runs fn inside store.WithTenant for the tenant ctx carries.
func withScope(ctx context.Context, store Store, fn func(ctx context.Context) error) error {
	scope, ok := ctx.Value(tenantContextKey{}).(tenantScope)
	if !ok {
		return ErrNoTenant
	}
	if !scope.known {
		return errUnknownTenant
	}

	return store.WithTenant(ctx, scope.id, fn)
}

// TenantFromPath scopes a request to the tenant whose slug is the path
// value name, for the routes that run before a session exists: the
// password login and the passkey login. A slug that matches no tenant
// scopes the request to an unknown tenant instead of answering here, so
// the login answers exactly like a wrong password. A store failure is 500.
func TenantFromPath(store Store, name string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue(name)

		ctx := ContextWithUnknownTenant(r.Context())
		if slug != "" {
			tenant, err := store.Tenants().FindBySlug(r.Context(), slug)
			switch {
			case err == nil:
				ctx = ContextWithTenant(r.Context(), tenant.ID)
			case errors.Is(err, storage.ErrNotFound):
			default:
				api.WriteError(w, r, api.Internal(fmt.Errorf("auth: resolve tenant %q: %w", slug, err)))

				return
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TenantFromSession scopes a request to the tenant of the session it
// carries, for the routes of a logged in account: logout, password change
// and passkey management. A request without a session passes unscoped,
// and the handler behind answers 401 itself. A session store failure
// passes unscoped as well: the handler reads the session again and
// answers 503.
func TenantFromSession(sessions *session.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok, err := sessions.Read(r.Context(), r)
		if err == nil && ok {
			if tenantID, parseErr := uuid.Parse(s.Data[tenantIDDataKey]); parseErr == nil {
				r = r.WithContext(ContextWithTenant(r.Context(), tenantID))
			}
		}

		next.ServeHTTP(w, r)
	})
}
