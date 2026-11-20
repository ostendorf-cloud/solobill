package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ostendorf-cloud/govue/api"

	"github.com/ostendorf-cloud/solobill/internal/storage"
)

// registerCustomers mounts the customers API. Owned by issue #69 (I3).
// See docs/api.md, "Customers".
func registerCustomers(mux *http.ServeMux, route routeFunc, a *app) {
	mux.Handle("GET /api/v1/customers", route(api.Handle(a.handleListCustomers)))
	mux.Handle("POST /api/v1/customers", route(api.Handle(a.handleCreateCustomer)))
	mux.Handle("GET /api/v1/customers/{id}", route(api.Handle(a.handleGetCustomer)))
	mux.Handle("PUT /api/v1/customers/{id}", route(api.Handle(a.handleUpdateCustomer)))
	mux.Handle("DELETE /api/v1/customers/{id}", route(api.Handle(a.handleDeleteCustomer)))
}

// customerJSON is the customer of docs/api.md.
type customerJSON struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	Number           *string    `json:"number"`
	Email            *string    `json:"email"`
	VATID            *string    `json:"vat_id"`
	Street           *string    `json:"street"`
	AddressLine2     *string    `json:"address_line2"`
	PostalCode       *string    `json:"postal_code"`
	City             *string    `json:"city"`
	CountryCode      *string    `json:"country_code"`
	DocumentLanguage string     `json:"document_language"`
	DeletedAt        *time.Time `json:"deleted_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// customerRequest is the body of POST and PUT. The tenant is never part
// of it: it comes from the session only.
type customerRequest struct {
	Name             string  `json:"name"`
	Number           *string `json:"number"`
	Email            *string `json:"email"`
	VATID            *string `json:"vat_id"`
	Street           *string `json:"street"`
	AddressLine2     *string `json:"address_line2"`
	PostalCode       *string `json:"postal_code"`
	City             *string `json:"city"`
	CountryCode      *string `json:"country_code"`
	DocumentLanguage string  `json:"document_language"`
}

func toCustomerJSON(c storage.Customer) customerJSON {
	lang := string(c.DocumentLanguage)
	if lang == "" {
		lang = string(storage.DocumentLanguageGerman)
	}

	return customerJSON{
		ID:               c.ID,
		Name:             c.Name,
		Number:           c.Number,
		Email:            c.Email,
		VATID:            c.VATID,
		Street:           c.Street,
		AddressLine2:     c.AddressLine2,
		PostalCode:       c.PostalCode,
		City:             c.City,
		CountryCode:      c.CountryCode,
		DocumentLanguage: lang,
		DeletedAt:        masterDataUTC(c.DeletedAt),
		CreatedAt:        c.CreatedAt.UTC(),
		UpdatedAt:        c.UpdatedAt.UTC(),
	}
}

// masterDataUTC converts an optional timestamp to UTC: docs/api.md
// requires RFC 3339 in UTC.
func masterDataUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()

	return &u
}

// customer validates the request and builds the domain value, without
// id or tenant. A blank optional field means "not set" and becomes nil,
// because the table refuses an empty string.
func (r customerRequest) customer() (storage.Customer, error) {
	details := map[string]any{}

	name := strings.TrimSpace(r.Name)
	if name == "" {
		details["name"] = "required"
	}

	lang := storage.DocumentLanguage(r.DocumentLanguage)
	switch lang {
	case "":
		lang = storage.DocumentLanguageGerman
	case storage.DocumentLanguageGerman, storage.DocumentLanguageEnglish:
	default:
		details["document_language"] = "must be de or en"
	}

	country := masterDataText(r.CountryCode)
	if country != nil {
		upper := strings.ToUpper(*country)
		if len(upper) != 2 || upper[0] < 'A' || upper[0] > 'Z' || upper[1] < 'A' || upper[1] > 'Z' {
			details["country_code"] = "must be an ISO 3166-1 alpha-2 code"
		}
		country = &upper
	}

	if len(details) > 0 {
		return storage.Customer{}, masterDataInvalid(details)
	}

	return storage.Customer{
		Name:             name,
		Number:           masterDataText(r.Number),
		Email:            masterDataText(r.Email),
		VATID:            masterDataText(r.VATID),
		Street:           masterDataText(r.Street),
		AddressLine2:     masterDataText(r.AddressLine2),
		PostalCode:       masterDataText(r.PostalCode),
		City:             masterDataText(r.City),
		CountryCode:      country,
		DocumentLanguage: lang,
	}, nil
}

// masterDataText trims s and maps blank to nil.
func masterDataText(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}

	return &v
}

// masterDataInvalid is the 400 invalid_argument of docs/api.md, with the
// field errors directly in details.
func masterDataInvalid(details map[string]any) error {
	return api.Invalid("the request is invalid").WithDetails(details)
}

// masterDataID parses the {id} path value. An id that is no UUID names no row,
// so it answers 404 like a missing one.
func masterDataID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, api.NotFound("not found")
	}

	return id, nil
}

// masterDataError maps a repository error to the API error. conflict is the
// message of a 409.
func masterDataError(err error, conflict string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, storage.ErrNotFound):
		return api.NotFound("not found")
	case errors.Is(err, storage.ErrConflict):
		return api.Conflict(conflict)
	default:
		return api.Internal(fmt.Errorf("httpapi: %w", err))
	}
}

// scopedTenantKey carries the tenant inTenant opened the scope for. The
// real repositories read the tenant from the database transaction, not
// from here. The key exists so a test fake can see which tenant the
// handler asked for: the fake store's WithTenant does not record it.
type scopedTenantKey struct{}

// inTenant runs fn inside the tenant scope of tenantID and tags fn's
// context with that same tenant.
func (a *app) inTenant(ctx context.Context, tenantID uuid.UUID, fn func(ctx context.Context) error) error {
	return a.repos.Scope.WithTenant(ctx, tenantID, func(ctx context.Context) error {
		return fn(context.WithValue(ctx, scopedTenantKey{}, tenantID))
	})
}

func (a *app) handleListCustomers(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.Customers == nil || a.repos.Scope == nil {
		return a.notImplemented(w, r)
	}

	var list []storage.Customer
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		list, err = a.repos.Customers.List(ctx)

		return err
	})
	if err != nil {
		return masterDataError(err, "")
	}

	out := make([]customerJSON, len(list))
	for i, item := range list {
		out[i] = toCustomerJSON(item)
	}

	return api.Write(w, http.StatusOK, map[string]any{"customers": out})
}

func (a *app) handleCreateCustomer(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.Customers == nil || a.repos.Scope == nil {
		return a.notImplemented(w, r)
	}

	var req customerRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	customer, err := req.customer()
	if err != nil {
		return err
	}
	// The tenant comes from the session, never from the body.
	customer.TenantID = c.tenantID

	var created storage.Customer
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		created, err = a.repos.Customers.Create(ctx, customer)

		return err
	})
	if err != nil {
		return masterDataError(err, "a customer with this number exists already")
	}

	return api.Write(w, http.StatusCreated, toCustomerJSON(created))
}

func (a *app) handleGetCustomer(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.Customers == nil || a.repos.Scope == nil {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var found storage.Customer
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		found, err = a.repos.Customers.FindByID(ctx, id)

		return err
	})
	if err != nil {
		return masterDataError(err, "")
	}

	return api.Write(w, http.StatusOK, toCustomerJSON(found))
}

func (a *app) handleUpdateCustomer(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.Customers == nil || a.repos.Scope == nil {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	var req customerRequest
	if err := api.Decode(r, &req); err != nil {
		return err
	}
	customer, err := req.customer()
	if err != nil {
		return err
	}
	customer.ID = id
	customer.TenantID = c.tenantID

	var updated storage.Customer
	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		var err error
		updated, err = a.repos.Customers.Update(ctx, customer)

		return err
	})
	if err != nil {
		return masterDataError(err, "a customer with this number exists already")
	}

	return api.Write(w, http.StatusOK, toCustomerJSON(updated))
}

func (a *app) handleDeleteCustomer(w http.ResponseWriter, r *http.Request) error {
	c, err := a.requireSession(r)
	if err != nil {
		return err
	}
	if a.repos.Customers == nil || a.repos.Scope == nil {
		return a.notImplemented(w, r)
	}
	id, err := masterDataID(r)
	if err != nil {
		return err
	}

	err = a.inTenant(r.Context(), c.tenantID, func(ctx context.Context) error {
		return a.repos.Customers.Delete(ctx, id)
	})
	if err != nil {
		return masterDataError(err, "")
	}

	api.NoContent(w)

	return nil
}
