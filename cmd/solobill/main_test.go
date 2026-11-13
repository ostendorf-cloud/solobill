package main

import (
	"context"
	"testing"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// TestCheckEmbeddedDist proves the embedded dist directory is shaped the
// way fs.Sub expects: a build with an empty or mis-shaped frontend build
// fails here instead of deep inside httpapi.NewHandler. run itself needs a
// full environment (a database, both signing keys) and a real Postgres to
// reach meaningfully, which this package does not have in a unit test; see
// internal/config and internal/httpapi for what is actually tested here.
func TestCheckEmbeddedDist(t *testing.T) {
	t.Parallel()

	if err := checkEmbeddedDist(); err != nil {
		t.Fatalf("checkEmbeddedDist() error = %v", err)
	}
}

// panicIfTenantsCalled is a tenantSeeder whose Tenants() panics: proof
// that seedTenant, called with an empty slug (production's default, see
// internal/config.Config.SeedTenantSlug), returns before it ever reads
// the store at all, rather than merely happening to find nothing to do.
type panicIfTenantsCalled struct{}

func (panicIfTenantsCalled) Tenants() storage.TenantRepository {
	panic("seedTenant must not call Tenants() when slug is empty")
}

// TestSeedTenantNoopWhenSlugEmpty covers the common, production case:
// SEED_TENANT_SLUG unset leaves Config.SeedTenantSlug empty, and
// seedTenant must do nothing at all, every docker-compose.yaml-only
// behaviour included.
func TestSeedTenantNoopWhenSlugEmpty(t *testing.T) {
	t.Parallel()

	if err := seedTenant(context.Background(), panicIfTenantsCalled{}, "", ""); err != nil {
		t.Fatalf("seedTenant() error = %v, want nil", err)
	}
}
