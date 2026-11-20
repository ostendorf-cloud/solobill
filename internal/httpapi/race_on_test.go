//go:build race

package httpapi_test

// raceEnabled is true when the tests run under the race detector, which
// slows the XML parse about tenfold. See race_off_test.go.
const raceEnabled = true
