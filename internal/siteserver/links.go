package siteserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"testing/fstest"

	"github.com/ostendorf-cloud/govue/envconf"
)

// The environment variables of the site's runtime configuration, read by
// LoadLinks. The page cannot be rebuilt per environment, so the landing
// fetches ConfigFile at startup and builds its buttons from it.
const (
	EnvLoginURL            = "SITE_LOGIN_URL"
	EnvRegisterURL         = "SITE_REGISTER_URL"
	EnvRegistrationEnabled = "SITE_REGISTRATION_ENABLED"
)

// The defaults of an unset variable: the app on app.solobill.de, registration
// on. landing/src/siteConfig.ts holds the same URLs for a visitor whose
// request for ConfigFile fails, and hides the register button for that visitor.
const (
	DefaultLoginURL    = "https://app.solobill.de/login"
	DefaultRegisterURL = "https://app.solobill.de/register"
)

// ConfigFile is the name of the runtime configuration at the root of the
// site, served at /config.json.
const ConfigFile = "config.json"

// Links are the ways from the site into the app, and the switch that turns
// the way to the registration off. The JSON names are the contract with
// landing/src/siteConfig.ts.
type Links struct {
	LoginURL            string `json:"loginUrl"`
	RegisterURL         string `json:"registerUrl"`
	RegistrationEnabled bool   `json:"registrationEnabled"`
}

// DefaultLinks are the links of a site with no variable set.
func DefaultLinks() Links {
	return Links{
		LoginURL:            DefaultLoginURL,
		RegisterURL:         DefaultRegisterURL,
		RegistrationEnabled: true,
	}
}

// LoadLinks reads the three variables through getenv, with the defaults of
// DefaultLinks for an unset one. A nil getenv reads the process environment.
// A URL must be absolute http or https without credentials, query or
// fragment: it ends up in an href of every visitor's page. Every problem is
// returned together.
func LoadLinks(getenv func(string) string) (Links, error) {
	loader := envconf.New(getenv)
	defaults := DefaultLinks()

	links := Links{RegistrationEnabled: loader.Bool(EnvRegistrationEnabled, defaults.RegistrationEnabled)}
	if u := loader.URL(EnvLoginURL, defaults.LoginURL); u != nil {
		links.LoginURL = u.String()
	}
	if u := loader.URL(EnvRegisterURL, defaults.RegisterURL); u != nil {
		links.RegisterURL = u.String()
	}

	if err := loader.Err(); err != nil {
		return Links{}, fmt.Errorf("siteserver: %w", err)
	}

	return links, nil
}

// JSON is the body of ConfigFile.
func (l Links) JSON() ([]byte, error) {
	if l.LoginURL == "" || l.RegisterURL == "" {
		return nil, errors.New("siteserver: links: the login and the register URL must not be empty")
	}

	body, err := json.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("siteserver: links: %w", err)
	}

	return append(body, '\n'), nil
}

// withFile returns build with one more regular file, name, which hides a file
// of the same name in build. govue's frontend reads a public file from its
// build, so this is how a file made at startup reaches it, with its ETag, its
// content type and its security headers.
func withFile(build fs.FS, name string, body []byte) fs.FS {
	return overlay{FS: build, file: fstest.MapFS{name: {Data: body}}, name: name}
}

// overlay answers one name from file and every other name from the embedded
// FS. Only Open is overridden, which is all govue's frontend needs.
type overlay struct {
	fs.FS

	file fs.FS
	name string
}

// Open implements fs.FS.
func (o overlay) Open(name string) (fs.File, error) {
	if name == o.name {
		return o.file.Open(name)
	}

	return o.FS.Open(name)
}
