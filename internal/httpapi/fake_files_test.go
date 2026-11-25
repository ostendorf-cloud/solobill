package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/httpapi"
	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// init wires the fakes of issue #75: the file repository, the logo
// repository and an object store behind the real objectstore.Guarded and
// Monitor, so the tests exercise the real gate.
func init() {
	repositoryHooks = append(repositoryHooks, func(_ *testing.T, repos *httpapi.Repositories) {
		files := newFakeFiles()
		checker := &fakeBucket{versioning: true, lock: true}
		monitor := objectstore.NewMonitor(objectstore.MonitorConfig{
			Checker:           checker,
			RequireVersioning: true,
			RequireObjectLock: true,
		})
		monitor.Check(context.Background())

		repos.Files = files
		repos.Objects = &objectstore.Guarded{Store: newFakeObjects(), Monitor: monitor}
		repos.SellerProfileLogos = &fakeLogos{}
		fileFixtures.Store(repos.Objects, &fileFixture{files: files, checker: checker, monitor: monitor})
	})
}

// fileFixture is what a files test reaches through testApp.repos.
type fileFixture struct {
	files   *fakeFiles
	checker *fakeBucket
	monitor *objectstore.Monitor
}

// fileFixtures maps each app's Objects to its fixture.
var fileFixtures sync.Map

func filesOf(t *testing.T, app testApp) (*fileFixture, *fakeObjects, *fakeLogos) {
	t.Helper()

	v, ok := fileFixtures.Load(app.repos.Objects)
	if !ok {
		t.Fatal("no file fixture for this app")
	}
	guarded, ok := app.repos.Objects.(*objectstore.Guarded)
	if !ok {
		t.Fatalf("Objects is %T, want *objectstore.Guarded", app.repos.Objects)
	}
	objects, ok := guarded.Store.(*fakeObjects)
	if !ok {
		t.Fatalf("Store is %T, want *fakeObjects", guarded.Store)
	}
	logos, ok := app.repos.SellerProfileLogos.(*fakeLogos)
	if !ok {
		t.Fatalf("SellerProfileLogos is %T, want *fakeLogos", app.repos.SellerProfileLogos)
	}

	fixture, ok := v.(*fileFixture)
	if !ok {
		t.Fatalf("fixture is %T, want *fileFixture", v)
	}

	return fixture, objects, logos
}

// setBucket changes what the bucket reports and runs one check, as the
// periodic check would.
func (f *fileFixture) setBucket(versioning, lock bool, err error) {
	f.checker.mu.Lock()
	f.checker.versioning, f.checker.lock, f.checker.err = versioning, lock, err
	f.checker.mu.Unlock()
	f.monitor.Check(context.Background())
}

type fakeBucket struct {
	mu               sync.Mutex
	versioning, lock bool
	err              error
}

func (b *fakeBucket) BucketVersioning(context.Context) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.versioning, b.err
}

func (b *fakeBucket) BucketObjectLock(context.Context) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.lock, b.err
}

// fakeVersion is the version id of the n-th Put of fakeObjects. Every
// Put writes a new version, as a versioned bucket does, so a second Put
// of the same key never replaces the version an earlier row recorded.
func fakeVersion(n int) string { return fmt.Sprintf("v%d", n) }

// fakeObjects is an in-memory versioned bucket. objects is the current
// version of each key, versions every version Put wrote under
// "<key>@<version>", latest the newest version id of each key. reported
// overrides the size Get and GetVersion report, to model a store whose
// metadata and body disagree.
type fakeObjects struct {
	mu       sync.Mutex
	objects  map[string][]byte
	versions map[string][]byte
	latest   map[string]string
	reported map[string]int64
	puts     int
}

func newFakeObjects() *fakeObjects {
	return &fakeObjects{
		objects: map[string][]byte{}, versions: map[string][]byte{},
		latest: map[string]string{}, reported: map[string]int64{},
	}
}

func (o *fakeObjects) Put(_ context.Context, object storage.PutObject) (storage.ObjectInfo, error) {
	data, err := io.ReadAll(object.Body)
	if err != nil {
		return storage.ObjectInfo{}, err
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	o.puts++
	version := fakeVersion(o.puts)
	o.objects[object.Key] = data
	o.versions[object.Key+"@"+version] = data
	o.latest[object.Key] = version

	info := storage.ObjectInfo{SizeBytes: int64(len(data)), ContentType: object.ContentType, VersionID: version}
	if object.RetainUntil != nil {
		retain := *object.RetainUntil
		info.RetainUntil = &retain
	}

	return info, nil
}

func (o *fakeObjects) Get(_ context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	data, ok := o.objects[key]
	if !ok {
		return nil, storage.ObjectInfo{}, storage.ErrNotFound
	}

	return o.open(key, data)
}

func (o *fakeObjects) GetVersion(_ context.Context, key, versionID string) (io.ReadCloser, storage.ObjectInfo, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	data, ok := o.versions[key+"@"+versionID]
	if !ok {
		return nil, storage.ObjectInfo{}, storage.ErrNotFound
	}

	return o.open(key, data)
}

// open must be called with o.mu held.
func (o *fakeObjects) open(key string, data []byte) (io.ReadCloser, storage.ObjectInfo, error) {
	size := int64(len(data))
	if reported, ok := o.reported[key]; ok {
		size = reported
	}

	return io.NopCloser(bytes.NewReader(bytes.Clone(data))), storage.ObjectInfo{SizeBytes: size}, nil
}

func (o *fakeObjects) Stat(_ context.Context, key string) (storage.ObjectInfo, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	data, ok := o.objects[key]
	if !ok {
		return storage.ObjectInfo{}, storage.ErrNotFound
	}

	return storage.ObjectInfo{SizeBytes: int64(len(data))}, nil
}

func (o *fakeObjects) BucketVersioning(context.Context) (bool, error) { return true, nil }

func (o *fakeObjects) get(key string) ([]byte, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	data, ok := o.objects[key]

	return data, ok
}

// overwrite writes a new current version of key, as a client with the
// bucket key could. The version Put recorded stays.
func (o *fakeObjects) overwrite(key string, data []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.objects[key] = data
}

// tamper changes the bytes of key in place, its newest version included,
// as an admin with direct access to the storage could.
func (o *fakeObjects) tamper(key string, data []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.objects[key] = data
	if version, ok := o.latest[key]; ok {
		o.versions[key+"@"+version] = data
	}
}

// versionCount is how many versions of key Put wrote.
func (o *fakeObjects) versionCount(key string) int {
	o.mu.Lock()
	defer o.mu.Unlock()

	n := 0
	for k := range o.versions {
		if strings.HasPrefix(k, key+"@") {
			n++
		}
	}

	return n
}

// tamperKeepingSize is tamper, with the store still reporting reportedSize.
func (o *fakeObjects) tamperKeepingSize(key string, data []byte, reportedSize int64) {
	o.tamper(key, data)

	o.mu.Lock()
	defer o.mu.Unlock()
	o.reported[key] = reportedSize
}

func (o *fakeObjects) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.puts
}

// fakeFiles is an in-memory FileRepository. Like fakeStore.WithTenant it
// does not know the tenant of the scope, so it returns a row of any
// tenant: the handler's own tenant check is what a cross tenant test
// exercises here. Row level security is proven against Postgres in
// internal/storage/postgres.
type fakeFiles struct {
	mu    sync.Mutex
	files map[uuid.UUID]storage.File
}

func newFakeFiles() *fakeFiles { return &fakeFiles{files: map[uuid.UUID]storage.File{}} }

// Create refuses a taken id with storage.ErrConflict and, like Postgres
// after a unique violation, aborts the transaction: every later call in
// the same WithTenant fails.
func (f *fakeFiles) Create(ctx context.Context, file storage.File) (storage.File, error) {
	if err := txAborted(ctx); err != nil {
		return storage.File{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, dup := f.files[file.ID]; dup {
		abortTx(ctx)

		return storage.File{}, storage.ErrConflict
	}
	file.UploadedAt = file.UploadedAt.Truncate(time.Microsecond)
	f.files[file.ID] = file

	return file, nil
}

func (f *fakeFiles) FindByID(ctx context.Context, id uuid.UUID) (storage.File, error) {
	if err := txAborted(ctx); err != nil {
		return storage.File{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	file, ok := f.files[id]
	if !ok {
		return storage.File{}, storage.ErrNotFound
	}

	return file, nil
}

func (f *fakeFiles) FindBySHA256(ctx context.Context, sha256 string) ([]storage.File, error) {
	if err := txAborted(ctx); err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var out []storage.File
	for _, file := range f.files {
		if file.SHA256 == sha256 {
			out = append(out, file)
		}
	}

	return out, nil
}

func (f *fakeFiles) add(file storage.File) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.files[file.ID] = file
}

func (f *fakeFiles) all() []storage.File {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]storage.File, 0, len(f.files))
	for _, file := range f.files {
		out = append(out, file)
	}

	return out
}

// fakeLogos is the logo half of one seller profile. hasProfile false
// answers ErrNotFound, as before #68 saved a profile.
type fakeLogos struct {
	mu         sync.Mutex
	hasProfile bool
	logo       *uuid.UUID
}

func (l *fakeLogos) Logo(context.Context) (*uuid.UUID, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.hasProfile {
		return nil, storage.ErrNotFound
	}

	return l.logo, nil
}

func (l *fakeLogos) SetLogo(_ context.Context, fileID *uuid.UUID) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.hasProfile {
		return storage.ErrNotFound
	}
	l.logo = fileID

	return nil
}

// saveProfile stands in for #68 saving the tenant's seller profile.
func (l *fakeLogos) saveProfile() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.hasProfile = true
}

func (l *fakeLogos) current() *uuid.UUID {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.logo
}
