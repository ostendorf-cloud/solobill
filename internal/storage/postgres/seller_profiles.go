package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ostendorf-cloud/solobill/internal/storage"
	"github.com/ostendorf-cloud/solobill/internal/storage/postgres/sqlcgen"
)

// sellerProfileRepository satisfies storage.SellerProfileRepository.
// Every method requires a context storage.TenantScope.WithTenant
// produced; see txFromContext.
type sellerProfileRepository struct{}

// Get implements storage.SellerProfileRepository.
func (r *sellerProfileRepository) Get(ctx context.Context) (storage.SellerProfile, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.SellerProfile{}, err
	}

	row, err := sqlcgen.New(tx).GetSellerProfile(ctx)
	if err != nil {
		return storage.SellerProfile{}, mapReadError(err)
	}

	return sellerProfileFromRow(row)
}

// GetForUpdate implements storage.SellerProfileRepository with
// queries/seller_profiles.sql's GetSellerProfileForUpdate.
func (r *sellerProfileRepository) GetForUpdate(ctx context.Context) (storage.SellerProfile, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.SellerProfile{}, err
	}

	row, err := sqlcgen.New(tx).GetSellerProfileForUpdate(ctx)
	if err != nil {
		return storage.SellerProfile{}, mapReadError(err)
	}

	return sellerProfileFromRow(row)
}

// Put implements storage.SellerProfileRepository. The query names its
// columns and writes no logo column, so a save never drops the logo.
func (r *sellerProfileRepository) Put(ctx context.Context, profile storage.SellerProfile) (storage.SellerProfile, error) {
	tx, err := txFromContext(ctx)
	if err != nil {
		return storage.SellerProfile{}, err
	}

	formats := make(map[string]string, len(profile.NumberFormats))
	for docType, template := range profile.NumberFormats {
		formats[string(docType)] = template
	}
	rawFormats, err := json.Marshal(formats)
	if err != nil {
		return storage.SellerProfile{}, fmt.Errorf("postgres: encode number formats: %w", err)
	}

	row, err := sqlcgen.New(tx).PutSellerProfile(ctx, sqlcgen.PutSellerProfileParams{
		TenantID:           profile.TenantID,
		LegalName:          profile.LegalName,
		Street:             profile.Street,
		AddressLine2:       ptrToText(profile.AddressLine2),
		PostalCode:         profile.PostalCode,
		City:               profile.City,
		CountryCode:        profile.CountryCode,
		TaxNumber:          ptrToText(profile.TaxNumber),
		VatID:              ptrToText(profile.VATID),
		KleinunternehmerID: ptrToText(profile.KleinunternehmerID),
		DefaultVatRegime:   string(profile.DefaultVATRegime),
		DefaultLanguage:    string(profile.DefaultLanguage),
		BankName:           ptrToText(profile.BankName),
		Iban:               ptrToText(profile.IBAN),
		Bic:                ptrToText(profile.BIC),
		Email:              ptrToText(profile.Email),
		Phone:              ptrToText(profile.Phone),
		Website:            ptrToText(profile.Website),
		FooterText:         ptrToText(profile.FooterText),
		NumberFormats:      rawFormats,
	})
	if err != nil {
		return storage.SellerProfile{}, mapWriteError(err)
	}

	return sellerProfileFromRow(row)
}

func sellerProfileFromRow(row sqlcgen.SellerProfile) (storage.SellerProfile, error) {
	var formats map[string]string
	if err := json.Unmarshal(row.NumberFormats, &formats); err != nil {
		return storage.SellerProfile{}, fmt.Errorf("postgres: decode number formats: %w", err)
	}

	numberFormats := make(map[storage.DocumentType]string, len(formats))
	for docType, template := range formats {
		numberFormats[storage.DocumentType(docType)] = template
	}

	return storage.SellerProfile{
		TenantID:           row.TenantID,
		LegalName:          row.LegalName,
		Street:             row.Street,
		AddressLine2:       textToPtr(row.AddressLine2),
		PostalCode:         row.PostalCode,
		City:               row.City,
		CountryCode:        row.CountryCode,
		TaxNumber:          textToPtr(row.TaxNumber),
		VATID:              textToPtr(row.VatID),
		KleinunternehmerID: textToPtr(row.KleinunternehmerID),
		DefaultVATRegime:   storage.VATRegime(row.DefaultVatRegime),
		DefaultLanguage:    storage.DocumentLanguage(row.DefaultLanguage),
		BankName:           textToPtr(row.BankName),
		IBAN:               textToPtr(row.Iban),
		BIC:                textToPtr(row.Bic),
		Email:              textToPtr(row.Email),
		Phone:              textToPtr(row.Phone),
		Website:            textToPtr(row.Website),
		FooterText:         textToPtr(row.FooterText),
		NumberFormats:      numberFormats,
		CreatedAt:          timeFromTimestamptz(row.CreatedAt),
		UpdatedAt:          timeFromTimestamptz(row.UpdatedAt),
	}, nil
}
