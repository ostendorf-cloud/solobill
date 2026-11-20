package objectstore

import (
	"context"
	"io"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// Guarded is the storage.ObjectStore every file route gets. It passes each
// call to Store, unless the Monitor's last check gated the file routes, in
// which case it refuses with storage.ErrObjectStoreUnavailable. The gate
// therefore holds for any later caller too (the PDF download of #74, the
// import attachment of #81), not only for the handlers that check it.
type Guarded struct {
	Store   storage.ObjectStore
	Monitor *Monitor
}

var _ storage.ObjectStore = (*Guarded)(nil)

// Status returns the Monitor's last check, for GET /api/v1/status and the
// 503 decision of the file routes.
func (g *Guarded) Status() storage.ObjectStoreStatus {
	return g.Monitor.Status()
}

func (g *Guarded) open() error {
	if g.Store == nil || !g.Monitor.Status().FileRoutesAvailable() {
		return storage.ErrObjectStoreUnavailable
	}

	return nil
}

// Put implements storage.ObjectStore.
func (g *Guarded) Put(ctx context.Context, object storage.PutObject) (storage.ObjectInfo, error) {
	if err := g.open(); err != nil {
		return storage.ObjectInfo{}, err
	}

	return g.Store.Put(ctx, object)
}

// Get implements storage.ObjectStore.
func (g *Guarded) Get(ctx context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	if err := g.open(); err != nil {
		return nil, storage.ObjectInfo{}, err
	}

	return g.Store.Get(ctx, key)
}

// Stat implements storage.ObjectStore.
func (g *Guarded) Stat(ctx context.Context, key string) (storage.ObjectInfo, error) {
	if err := g.open(); err != nil {
		return storage.ObjectInfo{}, err
	}

	return g.Store.Stat(ctx, key)
}

// BucketVersioning implements storage.ObjectStore. It reports the last
// check instead of asking the store again.
func (g *Guarded) BucketVersioning(context.Context) (bool, error) {
	status := g.Monitor.Status()
	if !status.Reachable {
		return false, storage.ErrObjectStoreUnavailable
	}

	return status.Versioning, nil
}
