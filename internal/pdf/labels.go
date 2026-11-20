package pdf

import "github.com/ostendorf-cloud/solobill/internal/docpayload"

// labels are the fixed texts of the template in one language. They are
// product content: German and English, chosen by the document language.
type labels struct {
	Invoice            string
	Correction         string
	Offer              string
	Watermark          string
	Number             map[string]string
	Date               string
	ServiceDate        string
	ServicePeriod      string
	PeriodTo           string
	CustomerNumber     string
	CustomerReference  string
	CustomerVATID      string
	ValidUntil         string
	DueDate            string
	Corrects           string
	CorrectsOf         string
	Position           string
	Description        string
	Quantity           string
	Unit               string
	UnitPrice          string
	VATRate            string
	Amount             string
	Discount           string
	NetTotal           string
	DocumentDiscount   string
	NetAfterDiscount   string
	VATOn              string
	VAT                string
	GrandTotal         string
	OfferTotal         string
	PaymentTerms       string
	Notes              string
	Page               string
	PageOf             string
	Phone              string
	Email              string
	Web                string
	TaxNumber          string
	VATID              string
	KleinunternehmerID string
	Bank               string
	IBAN               string
	BIC                string
	Countries          map[string]string
}

var labelsByLanguage = map[string]labels{
	docpayload.LanguageGerman: {
		Invoice:    "Rechnung",
		Correction: "Rechnungskorrektur",
		Offer:      "Angebot",
		Watermark:  "Entwurf",
		Number: map[string]string{
			docpayload.TypeInvoice:    "Rechnungsnummer",
			docpayload.TypeCorrection: "Rechnungsnummer",
			docpayload.TypeOffer:      "Angebotsnummer",
		},
		Date:               "Datum",
		ServiceDate:        "Leistungsdatum",
		ServicePeriod:      "Leistungszeitraum",
		PeriodTo:           "bis",
		CustomerNumber:     "Kundennummer",
		CustomerReference:  "Ihre Referenz",
		CustomerVATID:      "Ihre USt-IdNr.",
		ValidUntil:         "Gültig bis",
		DueDate:            "Zahlbar bis",
		Corrects:           "Diese Rechnungskorrektur ersetzt die Rechnung",
		CorrectsOf:         "vom",
		Position:           "Pos.",
		Description:        "Beschreibung",
		Quantity:           "Menge",
		Unit:               "Einheit",
		UnitPrice:          "Einzelpreis",
		VATRate:            "USt.",
		Amount:             "Betrag",
		Discount:           "Rabatt",
		NetTotal:           "Summe netto",
		DocumentDiscount:   "Rabatt",
		NetAfterDiscount:   "Summe netto nach Rabatt",
		VATOn:              "auf",
		VAT:                "Umsatzsteuer",
		GrandTotal:         "Gesamtbetrag",
		OfferTotal:         "Angebotssumme",
		PaymentTerms:       "Zahlungsbedingungen",
		Notes:              "Hinweise",
		Page:               "Seite",
		PageOf:             "von",
		Phone:              "Tel.",
		Email:              "E-Mail",
		Web:                "Web",
		TaxNumber:          "Steuernummer",
		VATID:              "USt-IdNr.",
		KleinunternehmerID: "Kleinunternehmer-ID",
		Bank:               "Bank",
		IBAN:               "IBAN",
		BIC:                "BIC",
		Countries: map[string]string{
			"AT": "Österreich", "BE": "Belgien", "CH": "Schweiz", "CZ": "Tschechien",
			"DE": "Deutschland", "DK": "Dänemark", "ES": "Spanien", "FI": "Finnland",
			"FR": "Frankreich", "GB": "Vereinigtes Königreich", "IE": "Irland",
			"IT": "Italien", "LU": "Luxemburg", "NL": "Niederlande", "NO": "Norwegen",
			"PL": "Polen", "PT": "Portugal", "SE": "Schweden", "US": "Vereinigte Staaten",
		},
	},
	docpayload.LanguageEnglish: {
		Invoice:    "Invoice",
		Correction: "Corrected invoice",
		Offer:      "Offer",
		Watermark:  "Draft",
		Number: map[string]string{
			docpayload.TypeInvoice:    "Invoice number",
			docpayload.TypeCorrection: "Invoice number",
			docpayload.TypeOffer:      "Offer number",
		},
		Date:               "Date",
		ServiceDate:        "Date of supply",
		ServicePeriod:      "Service period",
		PeriodTo:           "to",
		CustomerNumber:     "Customer number",
		CustomerReference:  "Your reference",
		CustomerVATID:      "Your VAT ID",
		ValidUntil:         "Valid until",
		DueDate:            "Due date",
		Corrects:           "This corrected invoice replaces invoice",
		CorrectsOf:         "of",
		Position:           "No.",
		Description:        "Description",
		Quantity:           "Qty",
		Unit:               "Unit",
		UnitPrice:          "Unit price",
		VATRate:            "VAT",
		Amount:             "Amount",
		Discount:           "Discount",
		NetTotal:           "Net total",
		DocumentDiscount:   "Discount",
		NetAfterDiscount:   "Net total after discount",
		VATOn:              "on",
		VAT:                "VAT",
		GrandTotal:         "Total",
		OfferTotal:         "Offer total",
		PaymentTerms:       "Payment terms",
		Notes:              "Notes",
		Page:               "Page",
		PageOf:             "of",
		Phone:              "Phone",
		Email:              "Email",
		Web:                "Web",
		TaxNumber:          "Tax number",
		VATID:              "VAT ID",
		KleinunternehmerID: "Small business ID",
		Bank:               "Bank",
		IBAN:               "IBAN",
		BIC:                "BIC",
		Countries: map[string]string{
			"AT": "Austria", "BE": "Belgium", "CH": "Switzerland", "CZ": "Czechia",
			"DE": "Germany", "DK": "Denmark", "ES": "Spain", "FI": "Finland",
			"FR": "France", "GB": "United Kingdom", "IE": "Ireland",
			"IT": "Italy", "LU": "Luxembourg", "NL": "Netherlands", "NO": "Norway",
			"PL": "Poland", "PT": "Portugal", "SE": "Sweden", "US": "United States",
		},
	},
}
