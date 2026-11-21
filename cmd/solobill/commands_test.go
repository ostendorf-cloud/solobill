package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// These tests touch the package level commands map, so they do not run
// in parallel.

func neverServe(t *testing.T) func() error {
	t.Helper()

	return func() error {
		t.Error("dispatch started the server")

		return nil
	}
}

func TestDispatchServesWithoutArguments(t *testing.T) {
	served := false
	if err := dispatch(nil, &bytes.Buffer{}, func() error { served = true; return nil }); err != nil || !served {
		t.Errorf("dispatch(nil) = %v, served = %v, want the server", err, served)
	}
}

func TestDispatchHelp(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		var out bytes.Buffer
		code, msg := exitWith(dispatch([]string{flag}, &out, neverServe(t)))
		if code != 0 || msg != "" {
			t.Errorf("dispatch(%s) exit = %d, %q, want 0 and no message", flag, code, msg)
		}
		if !strings.Contains(out.String(), "usage: solobill") {
			t.Errorf("dispatch(%s) printed %q, want the usage", flag, out.String())
		}
	}
}

func TestDispatchRefusesUnknownFlags(t *testing.T) {
	for _, flag := range []string{"-flag", "--port=8080", "-"} {
		var out bytes.Buffer
		code, msg := exitWith(dispatch([]string{flag}, &out, neverServe(t)))
		if code != 2 || !strings.Contains(msg, "unknown flag") {
			t.Errorf("dispatch(%s) exit = %d, %q, want 2 and an unknown flag message", flag, code, msg)
		}
		if !strings.Contains(out.String(), "usage: solobill") {
			t.Errorf("dispatch(%s) printed %q, want the usage", flag, out.String())
		}
	}
}

func TestDispatchRunsARegisteredCommand(t *testing.T) {
	var got []string
	registerCommand("probe", func(_ context.Context, args []string) error {
		got = args

		return nil
	})
	t.Cleanup(func() { delete(commands, "probe") })

	if err := dispatch([]string{"probe", "verify", "--all"}, &bytes.Buffer{}, neverServe(t)); err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if strings.Join(got, " ") != "verify --all" {
		t.Errorf("args = %q, want [verify --all]", got)
	}
}

func TestDispatchPassesACommandFailure(t *testing.T) {
	registerCommand("fails", func(context.Context, []string) error { return errors.New("broken") })
	t.Cleanup(func() { delete(commands, "fails") })

	code, msg := exitWith(dispatch([]string{"fails"}, &bytes.Buffer{}, neverServe(t)))
	if code != 1 || msg != "broken" {
		t.Errorf("exit = %d, %q, want 1 and broken", code, msg)
	}
}

func TestDispatchRefusesAnUnknownCommand(t *testing.T) {
	code, msg := exitWith(dispatch([]string{"nope"}, &bytes.Buffer{}, neverServe(t)))
	if code != 2 || !strings.Contains(msg, "unknown subcommand") {
		t.Errorf("dispatch(nope) exit = %d, %q, want 2 and an unknown subcommand message", code, msg)
	}
}

func TestRegisterCommandPanicsOnADuplicate(t *testing.T) {
	registerCommand("twice", func(context.Context, []string) error { return nil })
	t.Cleanup(func() { delete(commands, "twice") })

	defer func() {
		if recover() == nil {
			t.Error("registerCommand() of a duplicate did not panic")
		}
	}()
	registerCommand("twice", func(context.Context, []string) error { return nil })
}
