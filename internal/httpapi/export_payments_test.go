package httpapi

import "time"

// SetPaymentsNow replaces the clock of the payment routes until the
// returned function restores it. A test that calls it must not run in
// parallel: the clock is package state.
func SetPaymentsNow(now func() time.Time) (restore func()) {
	previous := paymentsNow
	paymentsNow = now

	return func() { paymentsNow = previous }
}
