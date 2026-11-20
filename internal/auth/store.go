package auth

import "github.com/ostendorf-cloud/solobill/internal/storage"

// Store is what this package needs from a storage backend: tenant lookup,
// the tenant-scoping mechanism, and tenant-scoped account, login, passkey
// and invite access. It is defined here, at the consumer, per
// go-conventions, rather than in internal/storage:
// internal/storage/postgres.Store already satisfies it, with no adapter,
// because its method set includes exactly this.
//
// Nothing in this package imports internal/storage/postgres or pgx
// directly; a test substitutes a hand written fake.
type Store interface {
	storage.TenantScope

	Tenants() storage.TenantRepository
	Accounts() storage.AccountRepository
	LoginAccounts() storage.LoginAccountRepository
	Passkeys() storage.PasskeyRepository
	Invites() storage.InviteRepository
}
