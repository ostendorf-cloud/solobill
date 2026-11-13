package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// TestRepositoriesRequireTenantScope proves the fail-loud contract: an
// AccountRepository or InviteRepository method called on a context
// WithTenant did not produce fails immediately with
// storage.ErrNoTenantScope, before it ever touches a connection. None of
// these need a live database, by construction: txFromContext rejects the
// context before any query would run.
func TestRepositoriesRequireTenantScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	id := uuid.New()

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "AccountRepository.Create",
			call: func() error {
				_, err := (&accountRepository{}).Create(ctx, storage.Account{})
				return err
			},
		},
		{
			name: "AccountRepository.FindByID",
			call: func() error {
				_, err := (&accountRepository{}).FindByID(ctx, id)
				return err
			},
		},
		{
			name: "AccountRepository.FindByEmail",
			call: func() error {
				_, err := (&accountRepository{}).FindByEmail(ctx, "someone@example.com")
				return err
			},
		},
		{
			name: "AccountRepository.List",
			call: func() error {
				_, err := (&accountRepository{}).List(ctx)
				return err
			},
		},
		{
			name: "AccountRepository.Update",
			call: func() error {
				_, err := (&accountRepository{}).Update(ctx, storage.Account{})
				return err
			},
		},
		{
			name: "InviteRepository.Create",
			call: func() error {
				_, err := (&inviteRepository{}).Create(ctx, storage.Invite{})
				return err
			},
		},
		{
			name: "InviteRepository.FindByID",
			call: func() error {
				_, err := (&inviteRepository{}).FindByID(ctx, id)
				return err
			},
		},
		{
			name: "InviteRepository.FindByTokenHash",
			call: func() error {
				_, err := (&inviteRepository{}).FindByTokenHash(ctx, "some-hash")
				return err
			},
		},
		{
			name: "InviteRepository.MarkUsed",
			call: func() error {
				_, err := (&inviteRepository{}).MarkUsed(ctx, id, time.Now())
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			if !errors.Is(err, storage.ErrNoTenantScope) {
				t.Errorf("error = %v, want errors.Is(err, storage.ErrNoTenantScope)", err)
			}
		})
	}
}
