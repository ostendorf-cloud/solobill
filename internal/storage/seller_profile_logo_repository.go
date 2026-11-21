package storage

import (
	"context"

	"github.com/google/uuid"
)

// SellerProfileLogoRepository reads and sets the logo of the tenant's
// seller profile, the seller_profiles.logo_file_id column. Every method
// must run inside a context TenantScope.WithTenant produced.
//
// It is the logo half of the seller profile contract, split from
// SellerProfileRepository on purpose: issue #68 implements
// SellerProfileRepository (queries in queries/seller_profiles.sql,
// store_seller_profiles.go) before the logo column exists, and issue #75
// adds the column and implements this interface in its own files
// (queries/seller_profile_logo.sql, seller_profile_logo.go,
// store_seller_profile_logo.go). One interface with both halves could not
// be implemented by #68 alone.
//
// SellerProfileRepository.Put never writes logo_file_id: its UPDATE names
// its columns and leaves the logo out, so saving the profile never drops
// the logo. The logo changes only through SetLogo.
type SellerProfileLogoRepository interface {
	// Logo returns the logo's file id, nil when no logo is set.
	// ErrNotFound when no profile exists yet.
	Logo(ctx context.Context) (*uuid.UUID, error)

	// SetLogo sets or, with nil, clears the logo. The file must be a
	// files row of purpose FilePurposeLogo. ErrNotFound when no profile
	// exists yet.
	SetLogo(ctx context.Context, fileID *uuid.UUID) error
}
