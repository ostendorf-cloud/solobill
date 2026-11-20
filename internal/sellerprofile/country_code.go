package sellerprofile

// ValidCountryCode reports whether code is an officially assigned ISO
// 3166-1 alpha-2 code, upper case, from the same list the profile save
// checks. Issue #70 calls it at issue time for the customer's country,
// which the customers API (#69) checks for its shape only.
func ValidCountryCode(code string) bool {
	return validCountry(code)
}
