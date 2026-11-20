package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// These tests touch the package level commands map, so they do not run
// in parallel.

func TestDispatchServesWithoutASubcommand(t *testing.T) {
	served := false
	serve := func() error { served = true; return nil }

	for _, args := range [][]string{nil, {"-flag"}} {
		served = false
		if err := dispatch(args, serve); err != nil || !served {
			t.Errorf("dispatch(%q) = %v, served = %v, want the server", args, err, served)
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

	if err := dispatch([]string{"probe", "verify", "--all"}, func() error { return errors.New("served") }); err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if strings.Join(got, " ") != "verify --all" {
		t.Errorf("args = %q, want [verify --all]", got)
	}
}

func TestDispatchRefusesAnUnknownCommand(t *testing.T) {
	err := dispatch([]string{"nope"}, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
		t.Errorf("dispatch(nope) = %v, want an unknown subcommand error", err)
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
