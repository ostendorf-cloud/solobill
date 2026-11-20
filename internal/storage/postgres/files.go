package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// fileRepository satisfies storage.FileRepository. Every method requires a
// context storage.TenantScope.WithTenant produced; see txFromContext.
// There is no update and no delete: a files row is written once.
type fileRepository struct{}

// Create implements storage.FileRepository. The object key shape, the
// hash shape, the purpose and the content type are checked again by the
// table's own constraints.
func (r *fileRepository) Create(ctx context.Context, file storage.File) (storage.File, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.File{}, err
	}

	uploadedAt := file.UploadedAt
	if uploadedAt.IsZero() {
		uploadedAt = time.Now()
	}

	row, err := sqlcgen.New(tx).CreateFile(ctx, sqlcgen.CreateFileParams{
		ID:               file.ID,
		TenantID:         file.TenantID,
		Purpose:          string(file.Purpose),
		ObjectKey:        file.ObjectKey,
		ObjectVersionID:  ptrToText(file.ObjectVersionID),
		Sha256:           file.SHA256,
		SizeBytes:        file.SizeBytes,
		ContentType:      file.ContentType,
		OriginalFilename: file.OriginalFilename,
		UploadedBy:       ptrToUUID(file.UploadedBy),
		UploadedAt:       toTimestamptz(uploadedAt),
		RetainUntil:      ptrToTimestamptz(file.RetainUntil),
	})
	if err != nil {
		return storage.File{}, mapWriteError(err)
	}

	return fileFromRow(row), nil
}

// FindByID implements storage.FileRepository.
func (r *fileRepository) FindByID(ctx context.Context, id uuid.UUID) (storage.File, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.File{}, err
	}

	row, err := sqlcgen.New(tx).GetFileByID(ctx, id)
	if err != nil {
		return storage.File{}, mapReadError(err)
	}

	return fileFromRow(row), nil
}

// FindBySHA256 implements storage.FileRepository.
func (r *fileRepository) FindBySHA256(ctx context.Context, sha256 string) ([]storage.File, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := sqlcgen.New(tx).ListFilesBySHA256(ctx, sha256)
	if err != nil {
		return nil, mapReadError(err)
	}

	return filesFromRows(rows), nil
}

func filesFromRows(rows []sqlcgen.File) []storage.File {
	files := make([]storage.File, len(rows))
	for i, row := range rows {
		files[i] = fileFromRow(row)
	}

	return files
}

func fileFromRow(row sqlcgen.File) storage.File {
	return storage.File{
		ID:               row.ID,
		TenantID:         row.TenantID,
		Purpose:          storage.FilePurpose(row.Purpose),
		ObjectKey:        row.ObjectKey,
		ObjectVersionID:  textToPtr(row.ObjectVersionID),
		SHA256:           row.Sha256,
		SizeBytes:        row.SizeBytes,
		ContentType:      row.ContentType,
		OriginalFilename: row.OriginalFilename,
		UploadedBy:       uuidToPtr(row.UploadedBy),
		UploadedAt:       timeFromTimestamptz(row.UploadedAt),
		RetainUntil:      timeToPtr(row.RetainUntil),
	}
}
