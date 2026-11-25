package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ostendorf-cloud/solobill/internal/auth"
)

// ErrOIDCRedirectPath is OIDC_REDIRECT_URL naming a path that does not end
// in auth.OIDCCallbackPath. The identity provider would send the browser to
// a route nothing answers, so the deployment refuses to start instead of
// failing every OIDC login.
var ErrOIDCRedirectPath = errors.New("redirect url does not name the oidc callback route")

// ErrOIDCRedirectScheme is OIDC_REDIRECT_URL with a scheme other than
// https, or plain http while SESSION_ALLOW_PLAIN_HTTP is off, or with no
// host at all.
var ErrOIDCRedirectScheme = errors.New("redirect url is not an absolute https url")

// checkOIDCRedirectURL validates OIDC_REDIRECT_URL against the route that
// answers it. The path has to end in auth.OIDCCallbackPath. A prefix in
// front of it is allowed, for a deployment behind a proxy that strips one.
//
// The scheme has to be https. Plain http is accepted only together with
// SESSION_ALLOW_PLAIN_HTTP, the one switch that already marks a local plain
// HTTP run. govue's oidc.New narrows that further to a loopback host.
func checkOIDCRedirectURL(raw string, allowPlainHTTP bool) error {
	if raw == "" {
		// l.Required has already reported the missing variable.
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("OIDC_REDIRECT_URL: not a URL: %w", ErrOIDCRedirectScheme)
	}

	plainHTTPAllowed := u.Scheme == "http" && allowPlainHTTP
	if u.Host == "" || (u.Scheme != "https" && !plainHTTPAllowed) {
		return fmt.Errorf("OIDC_REDIRECT_URL: want an absolute https URL ending in %s, plain http only with SESSION_ALLOW_PLAIN_HTTP: %w",
			auth.OIDCCallbackPath, ErrOIDCRedirectScheme)
	}

	if !strings.HasSuffix(u.Path, auth.OIDCCallbackPath) || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("OIDC_REDIRECT_URL: path %q does not end in %s, with no query or fragment: %w",
			u.Path, auth.OIDCCallbackPath, ErrOIDCRedirectPath)
	}

	return nil
}
