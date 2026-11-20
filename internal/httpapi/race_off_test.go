//go:build !race

package httpapi_test

// raceEnabled is false without the race detector. See race_on_test.go.
const raceEnabled = false
