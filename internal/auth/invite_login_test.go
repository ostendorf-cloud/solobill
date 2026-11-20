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

func TestInviteManagerAcceptInviteChecksTheLogin(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		login string
		want  error
	}{
		{"another address", "someone-else@acme.example", ErrInviteInvalid},
		{"the invite's address in another letter case", "Invitee@ACME.example", nil},
		{"no login, the json accept route", "", nil},
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
