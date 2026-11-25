package httpapi_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/ostendorf-cloud/solobill/internal/objectstore"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

func pngChunk(typ string, data []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
	b.WriteString(typ)
	b.Write(data)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(typ), data...)))

	return b.Bytes()
}

// craftedPNG is a PNG of a few dozen bytes whose header declares w by h
// pixels, with idat as its only image data.
func craftedPNG(w, h uint32, idat []byte) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	b.Write(pngChunk("IHDR", ihdr))
	b.Write(pngChunk("IDAT", idat))
	b.Write(pngChunk("IEND", nil))

	return b.Bytes()
}

// TestLogoUploadRefusesAnImageThatCannotBePrinted: the magic bytes and
// the size limit pass a decompression bomb and a corrupt PNG; the decode
// check refuses both with a field error and stores nothing.
func TestLogoUploadRefusesAnImageThatCannotBePrinted(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input []byte
	}{
		{"a small file declaring 100000 by 100000 pixels", craftedPNG(100000, 100000, []byte{0x78, 0x9c, 0x03, 0x00})},
		{"a png whose image data does not decode", craftedPNG(16, 16, []byte("not zlib data at all"))},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := buildTestApp(t, 1000)
			_, objects, logos := filesOf(t, app)
			logos.saveProfile()
			s := ownerSession(t, app)

			body, contentType := multipartBody(t, formPart{name: "file", filename: "logo.png", data: tt.input})
			rec := doRequest(t, app.handler, http.MethodPut, "/api/v1/profile/logo", body, contentType, s.cookie, s.token)
			got := wantErrorCode(t, rec, http.StatusBadRequest, "invalid_argument")
			if got.Error.Details["file"] == nil {
				t.Errorf("details = %v, want a field error for file", got.Error.Details)
			}
			if objects.count() != 0 || logos.current() != nil {
				t.Error("a logo that cannot be printed was stored or set")
			}
		})
	}
}

// TestDocumentPDFRefusesAStoredLogoThatCannotBePrinted: a logo stored
// before the upload check existed passes its hash but not the decode
// check before the render: 409, nothing rendered.
func TestDocumentPDFRefusesAStoredLogoThatCannotBePrinted(t *testing.T) {
	t.Parallel()

	f, renderer := newPDFFixture(t, "pdf-logo-corrupt")
	fx, objects, logos := filesOf(t, f.app)
	logos.saveProfile()
	corrupt := craftedPNG(16, 16, []byte("not zlib data at all"))
	id := uuid.New()
	key := objectstore.Key(f.tenant.ID, id)
	info, err := objects.Put(context.Background(), storage.PutObject{Key: key, Body: bytes.NewReader(corrupt), SizeBytes: int64(len(corrupt))})
	if err != nil {
		t.Fatal(err)
	}
	version := info.VersionID
	fx.files.add(storage.File{
		ID: id, TenantID: f.tenant.ID, Purpose: storage.FilePurposeLogo, ObjectKey: key, ObjectVersionID: &version,
		SHA256: sha256Of(corrupt), SizeBytes: int64(len(corrupt)), ContentType: "image/png",
	})
	if err := logos.SetLogo(context.Background(), &id); err != nil {
		t.Fatal(err)
	}
	docID, _ := f.issuedID(t)

	wantErrorCode(t, f.pdf(t, f.owner, docID), http.StatusConflict, "conflict")
	if len(renderer.renders()) != 0 || len(storedPDFs(t, f.app)) != 0 {
		t.Error("a logo that cannot be printed was rendered or stored")
	}
}
