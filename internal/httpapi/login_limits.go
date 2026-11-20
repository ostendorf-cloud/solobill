package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ostendorf-cloud/govue/account"
	"github.com/ostendorf-cloud/govue/api"
	"github.com/ostendorf-cloud/govue/clientip"
	"github.com/ostendorf-cloud/govue/middleware"
	"github.com/ostendorf-cloud/govue/ratelimit"
)

// The defaults of LoginLimits, the values of the wishlist adoption of
// govue's account package. Every attempt counts, a success too.
const (
	DefaultLoginClientBurst  = 5
	DefaultLoginClientPer    = time.Minute
	DefaultLoginBurst        = 10
	DefaultLoginPer          = 15 * time.Minute
	DefaultLoginCeilingBurst = 100
	DefaultLoginCeilingPer   = time.Hour
)

// LoginLimits are the rate limits in front of the routes that cost an
// Argon2id derivation or a passkey ceremony. govue's hasher runs one
// derivation at a time by default, so without a per client limit anyone
// keeps it busy and every login answers 503.
//
//   - Client: per client address, on every route that verifies a password,
//     a TOTP code or a passkey, and on the invite routes.
//   - Login: per tenant, login and client address, on the password login.
//     One address spends only its own budget and cannot lock an account
//     out for another address.
//   - Ceiling: per tenant and login alone, far above what a person types.
//     It bounds guesses from many addresses. A distributed attacker can
//     hold it used up, and the account then cannot log in with a password
//     until it refills; a passkey or OIDC login still works.
//
// A zero field takes its default above.
type LoginLimits struct {
	ClientBurst  int
	ClientPer    time.Duration
	LoginBurst   int
	LoginPer     time.Duration
	CeilingBurst int
	CeilingPer   time.Duration
}

func (l LoginLimits) withDefaults() LoginLimits {
	l.ClientBurst = orInt(l.ClientBurst, DefaultLoginClientBurst)
	l.ClientPer = orDuration(l.ClientPer, DefaultLoginClientPer)
	l.LoginBurst = orInt(l.LoginBurst, DefaultLoginBurst)
	l.LoginPer = orDuration(l.LoginPer, DefaultLoginPer)
	l.CeilingBurst = orInt(l.CeilingBurst, DefaultLoginCeilingBurst)
	l.CeilingPer = orDuration(l.CeilingPer, DefaultLoginCeilingPer)

	return l
}

func orInt(v, fallback int) int {
	if v == 0 {
		return fallback
	}

	return v
}

func orDuration(v, fallback time.Duration) time.Duration {
	if v == 0 {
		return fallback
	}

	return v
}

// loginLimiter holds the three limits of LoginLimits.
type loginLimiter struct {
	clientIPs *clientip.Resolver
	client    *middleware.RateLimit
	login     *ratelimit.Limiter
	ceiling   *ratelimit.Limiter
}

func newLoginLimiter(limits LoginLimits, clientIPs *clientip.Resolver, now func() time.Time) (*loginLimiter, error) {
	limits = limits.withDefaults()

	clientLimiter := ratelimit.New(limits.ClientBurst, limits.ClientPer, maxRateLimitKeys)
	client, err := middleware.NewRateLimit(middleware.RateLimitConfig{Limiter: clientLimiter, ClientIP: clientIPs})
	if err != nil {
		return nil, err
	}

	login := ratelimit.New(limits.LoginBurst, limits.LoginPer, maxRateLimitKeys)
	ceiling := ratelimit.New(limits.CeilingBurst, limits.CeilingPer, maxRateLimitKeys)
	if now != nil {
		clientLimiter.Now = now
		login.Now = now
		ceiling.Now = now
	}

	return &loginLimiter{clientIPs: clientIPs, client: client, login: login, ceiling: ceiling}, nil
}

// checked adds the per client login limit in front of h.
func (a *app) checked(h http.Handler) http.Handler {
	return a.limits.client.Middleware(h)
}

// perLogin takes one token per request from the buckets of the login in
// the body, before govue spends a password derivation on it. It reads the
// body the way govue's handler does (encoding/json, the same case
// insensitive field match), keyed by the tenant slug of the path and the
// login folded to lower case like the store compares it, and puts the body
// back. A body it cannot read a login from is left to the handler: govue
// runs one derivation for it, and only the client limit covers that.
func (a *app) perLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			next.ServeHTTP(w, r)

			return
		}

		limit := account.DefaultMaxBodyBytes
		head, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
		original := r.Body
		r.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(head), original), original}

		if err == nil && int64(len(head)) <= limit {
			var probe struct {
				Login string `json:"login"`
			}
			if json.Unmarshal(head, &probe) == nil && probe.Login != "" {
				sum := sha256.Sum256([]byte(r.PathValue(tenantPathValue) + "\x00" + strings.ToLower(probe.Login)))
				key := hex.EncodeToString(sum[:])
				client := clientip.Key(a.limits.clientIPs.Resolve(r))

				if ok, wait := a.limits.login.Take(key + "|" + client); !ok {
					api.WriteError(w, r, api.TooManyRequests(wait))

					return
				}
				if ok, wait := a.limits.ceiling.Take(key); !ok {
					api.WriteError(w, r, api.TooManyRequests(wait))

					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}
