package auth

import (
	"errors"
	"testing"
	"time"
)

func TestInviteManagerIssueTOTPEnrolmentNamesTheInviteEmail(t *testing.T) {
	t.Parallel()

	f := newInviteTestFixture(t)
	manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })

	enrolment, err := manager.IssueTOTPEnrolment(t.Context(), f.tenant.ID, f.raw)
	if err != nil {
		t.Fatalf("IssueTOTPEnrolment: %v", err)
	}
	if enrolment.Email != f.invite.Email || enrolment.Secret == "" || enrolment.ProvisioningURI == "" {
		t.Errorf("enrolment = %+v, want the invite's email, a secret and a uri", enrolment)
	}
}

func TestSameInviteLogin(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		login, email string
		want         bool
	}{
		{"kim@acme.example", "kim@acme.example", true},
		{"KIM@Acme.Example", "kim@acme.example", true},
		{"", "", false},
		{"", "kim@acme.example", false},
		{"Kim@acme.example", "kim@acme.example", false}, // Kelvin sign
		{"kim@acme.example ", "kim@acme.example", false},
		{"Ümit@acme.example", "ümit@acme.example", false}, // non-ASCII case stays exact
		{"ümit@acme.example", "ümit@acme.example", true},
	} {
		if got := SameInviteLogin(tt.login, tt.email); got != tt.want {
			t.Errorf("SameInviteLogin(%q, %q) = %v, want %v", tt.login, tt.email, got, tt.want)
		}
	}
}

func TestInviteManagerAcceptInviteChecksTheLogin(t *testing.T) {
	t.Parallel()

	login := func(s string) *string { return &s }

	for _, tt := range []struct {
		name  string
		login *string
		want  error
	}{
		{"another address", login("someone-else@acme.example"), ErrInviteInvalid},
		{"an empty login", login(""), ErrInviteInvalid},
		{"a padded login", login(" invitee@acme.example"), ErrInviteInvalid},
		{"a fullwidth look alike that NFKC would fold", login("ｉnvitee@acme.example"), ErrInviteInvalid},
		{"the invite's address in another letter case", login("Invitee@ACME.example"), nil},
		{"no login, the json accept route", nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newInviteTestFixture(t)
			manager := NewInviteManager(f.store, testHasher(t), func() time.Time { return f.now })
			secret := f.issueSecret(t, manager)

			code, err := testTOTPCode(secret, f.now)
			if err != nil {
				t.Fatalf("TOTPCode: %v", err)
			}

			created, err := manager.AcceptInvite(t.Context(), AcceptInviteParams{
				TenantID: f.tenant.ID,
				RawToken: f.raw,
				Login:    tt.login,
				Password: "a brand new passphrase",
				TOTPCode: code,
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("AcceptInvite: err = %v, want %v", err, tt.want)
			}
			if tt.want != nil {
				if f.store.invites[f.invite.ID].UsedAt != nil {
					t.Error("a refused accept marked the invite used")
				}

				return
			}
			if created.Email != f.invite.Email {
				t.Errorf("account email = %q, want the invite's own %q", created.Email, f.invite.Email)
			}
		})
	}
}
