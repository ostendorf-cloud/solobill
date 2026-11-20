package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/sellerprofile"
	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerProfile mounts the seller profile. Owned by issue #68 (I2).
// See docs/api.md, "Seller profile".
//
// The profile's has_logo field is computed here from
// a.repos.SellerProfileLogos (false while it is nil, before issue #75).
// The logo itself is never read or written through these routes: issue
// #75 owns the logo routes in routes_profile_logo.go and the logo column.
func registerProfile(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/profile", route(api.Handle(a.handleGetProfile)))
	mux.Handle("PUT /api/v1/profile", route(api.Handle(a.handlePutProfile)))
}

// profileOwner is requireSession plus the owner role.
func (a *app) profileOwner(r *http.Request) (caller, error) {
	c, err := a.requireSession(r)
	if err != nil {
		return caller{}, err
	}
	if c.role != storage.AccountRoleOwner {
		return caller{}, api.PermissionDenied("owner role required")
	}

	return c, nil
}

// profileInTenant runs fn in the caller's tenant scope. The tenant comes
// from the session only. An *api.Error from fn passes through, anything
// else is an internal error with the cause logged and never sent.
func (a *app) profileInTenant(ctx context.Context, c caller, fn func(ctx context.Context) error) error {
	err := a.repos.Scope.WithTenant(ctx, c.tenantID, fn)
	if err == nil {
		return nil
	}
	if apiErr := api.AsError(err); apiErr != nil {
		return apiErr
	}

	return api.Internal(err)
}

// profileBody is the request of PUT /api/v1/profile: the profile JSON
// without its read only fields, which are refused as unknown fields.
type profileBody struct {
	LegalName          string            `json:"legal_name"`
	Street             string            `json:"street"`
	AddressLine2       *string           `json:"address_line2"`
	PostalCode         string            `json:"postal_code"`
	City               string            `json:"city"`
	CountryCode        string            `json:"country_code"`
	TaxNumber          *string           `json:"tax_number"`
	VATID              *string           `json:"vat_id"`
	KleinunternehmerID *string           `json:"kleinunternehmer_id"`
	DefaultVATRegime   string            `json:"default_vat_regime"`
	DefaultLanguage    string            `json:"default_language"`
	BankName           *string           `json:"bank_name"`
	IBAN               *string           `json:"iban"`
	BIC                *string           `json:"bic"`
	Email              *string           `json:"email"`
	Phone              *string           `json:"phone"`
	Website            *string           `json:"website"`
	FooterText         *string           `json:"footer_text"`
	NumberFormats      map[string]string `json:"number_formats"`
}

// profileResponse is the profile JSON of docs/api.md.
type profileResponse struct {
	LegalName          string            `json:"legal_name"`
	Street             string            `json:"street"`
	AddressLine2       *string           `json:"address_line2"`
	PostalCode         string            `json:"postal_code"`
	City               string            `json:"city"`
	CountryCode        string            `json:"country_code"`
	TaxNumber          *string           `json:"tax_number"`
	VATID              *string           `json:"vat_id"`
	KleinunternehmerID *string           `json:"kleinunternehmer_id"`
	DefaultVATRegime   string            `json:"default_vat_regime"`
	DefaultLanguage    string            `json:"default_language"`
	BankName           *string           `json:"bank_name"`
	IBAN               *string           `json:"iban"`
	BIC                *string           `json:"bic"`
	Email              *string           `json:"email"`
	Phone              *string           `json:"phone"`
	Website            *string           `json:"website"`
	FooterText         *string           `json:"footer_text"`
	NumberFormats      map[string]string `json:"number_formats"`
	HasLogo            bool              `json:"has_logo"`
	IssueReady         map[string]bool   `json:"issue_ready"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

func newProfileResponse(p storage.SellerProfile, hasLogo bool) profileResponse {
	formats := map[string]string{}
	for docType, template := range sellerprofile.WithDefaultFormats(p.NumberFormats) {
		formats[string(docType)] = template
	}

	ready := map[string]bool{}
	for _, regime := range []storage.VATRegime{storage.VATRegimeStandard, storage.VATRegimeKleinunternehmer} {
		ready[string(regime)] = len(sellerprofile.Validate(p, regime)) == 0
	}

	return profileResponse{
		LegalName:          p.LegalName,
		Street:             p.Street,
		AddressLine2:       p.AddressLine2,
		PostalCode:         p.PostalCode,
		City:               p.City,
		CountryCode:        p.CountryCode,
		TaxNumber:          p.TaxNumber,
		VATID:              p.VATID,
		KleinunternehmerID: p.KleinunternehmerID,
		DefaultVATRegime:   string(p.DefaultVATRegime),
		DefaultLanguage:    string(p.DefaultLanguage),
		BankName:           p.BankName,
		IBAN:               p.IBAN,
		BIC:                p.BIC,
		Email:              p.Email,
		Phone:              p.Phone,
		Website:            p.Website,
		FooterText:         p.FooterText,
		NumberFormats:      formats,
		HasLogo:            hasLogo,
		IssueReady:         ready,
		UpdatedAt:          p.UpdatedAt.UTC(),
	}
}

// profileHasLogo reads the logo state through SellerProfileLogos, false
// while issue #75 has not wired it.
func (a *app) profileHasLogo(ctx context.Context) (bool, error) {
	if a.repos.SellerProfileLogos == nil {
		return false, nil
	}

	logo, err := a.repos.SellerProfileLogos.Logo(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return logo != nil, nil
}

// handleGetProfile answers GET /api/v1/profile.
func (a *app) handleGetProfile(w http.ResponseWriter, r *http.Request) error {
	c, err := a.profileOwner(r)
	if err != nil {
		return err
	}

	var out profileResponse
	err = a.profileInTenant(r.Context(), c, func(ctx context.Context) error {
		p, err := a.repos.SellerProfiles.Get(ctx)
		if errors.Is(err, storage.ErrNotFound) {
			return api.NotFound("no seller profile saved yet")
		}
		if err != nil {
			return err
		}

		logo, err := a.profileHasLogo(ctx)
		if err != nil {
			return err
		}
		out = newProfileResponse(p, logo)

		return nil
	})
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusOK, out)
}

// handlePutProfile answers PUT /api/v1/profile. The whole profile is
// replaced, except the logo, which this route never touches.
func (a *app) handlePutProfile(w http.ResponseWriter, r *http.Request) error {
	c, err := a.profileOwner(r)
	if err != nil {
		return err
	}

	var body profileBody
	if err := api.Decode(r, &body); err != nil {
		return err
	}

	formats := map[storage.DocumentType]string{}
	problems := map[string]string{}
	for name, template := range body.NumberFormats {
		docType := storage.DocumentType(name)
		if docType != storage.DocumentTypeInvoice && docType != storage.DocumentTypeOffer {
			problems["number_formats."+name] = "unknown document type"

			continue
		}
		formats[docType] = template
	}

	p := sellerprofile.Normalize(storage.SellerProfile{
		TenantID:           c.tenantID,
		LegalName:          body.LegalName,
		Street:             body.Street,
		AddressLine2:       body.AddressLine2,
		PostalCode:         body.PostalCode,
		City:               body.City,
		CountryCode:        body.CountryCode,
		TaxNumber:          body.TaxNumber,
		VATID:              body.VATID,
		KleinunternehmerID: body.KleinunternehmerID,
		DefaultVATRegime:   storage.VATRegime(body.DefaultVATRegime),
		DefaultLanguage:    storage.DocumentLanguage(body.DefaultLanguage),
		BankName:           body.BankName,
		IBAN:               body.IBAN,
		BIC:                body.BIC,
		Email:              body.Email,
		Phone:              body.Phone,
		Website:            body.Website,
		FooterText:         body.FooterText,
		NumberFormats:      sellerprofile.WithDefaultFormats(formats),
	})
	for key, reason := range sellerprofile.CheckFormat(p) {
		problems[key] = reason
	}

	var out profileResponse
	err = a.profileInTenant(r.Context(), c, func(ctx context.Context) error {
		old, err := a.repos.SellerProfiles.Get(ctx)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}

		issued, err := a.profileIssuedDocumentTypes(ctx)
		if err != nil {
			return err
		}
		for key, reason := range sellerprofile.CheckNumberFormats(p.NumberFormats, old.NumberFormats, issued) {
			problems[key] = reason
		}
		if len(problems) > 0 {
			return profileInvalid("the profile has invalid values", problems)
		}

		saved, err := a.repos.SellerProfiles.Put(ctx, p)
		if err != nil {
			return err
		}
		logo, err := a.profileHasLogo(ctx)
		if err != nil {
			return err
		}
		out = newProfileResponse(saved, logo)

		return nil
	})
	if err != nil {
		return err
	}

	return api.Write(w, http.StatusOK, out)
}

// profileIssuedDocumentTypes reports which numbered document types
// already have an issued document, so a template change is limited to its
// prefix. Without a document repository (a test app that wires none)
// nothing is issued.
//
// The check and the save are not one atomic step: a document issued by
// another request between them keeps its number, and the unique index on
// documents.display_number stays the last guard (issue #70).
func (a *app) profileIssuedDocumentTypes(ctx context.Context) (map[storage.DocumentType]bool, error) {
	issued := map[storage.DocumentType]bool{}
	if a.repos.Documents == nil {
		return issued, nil
	}

	documents, err := a.repos.Documents.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range documents {
		if d.State == storage.DocumentStateIssued {
			issued[d.Type] = true
		}
	}

	return issued, nil
}

// profileInvalid is the 400 invalid_argument error with its field paths
// directly in details, as docs/api.md's error shape says.
func profileInvalid(message string, problems map[string]string) *api.Error {
	details := make(map[string]any, len(problems))
	for key, reason := range problems {
		details[key] = reason
	}

	return api.Invalid(message).WithDetails(details)
}
