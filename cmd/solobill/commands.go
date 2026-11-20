package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
)

// command is a subcommand of the solobill binary, for example
// "solobill files verify" (issue #75). args are the arguments after the
// subcommand's own name.
type command func(ctx context.Context, args []string) error

// commands maps a subcommand name to its implementation. A feature adds
// one file cmd_<name>.go whose init registers it here, and never edits
// main.go or this file: see AGENTS.md's conflict zones.
var commands = map[string]command{}

// registerCommand adds a subcommand. It panics on a duplicate name: two
// files claiming the same name is a programming error found at start.
func registerCommand(name string, c command) {
	if _, dup := commands[name]; dup {
		panic(fmt.Sprintf("solobill: subcommand %q registered twice", name))
	}
	commands[name] = c
}

// dispatch runs the subcommand args[0] names, or serve when args is empty
// or starts with a flag. An unknown name is an error listing the known
// ones, never a silent start of the server.
func dispatch(args []string, serve func() error) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return serve()
	}

	c, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("solobill: unknown subcommand %q, known: %s", args[0], strings.Join(knownCommands(), ", "))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return c(ctx, args[1:])
}

func knownCommands() []string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)

	if len(names) == 0 {
		return []string{"none"}
	}

	return names
}
