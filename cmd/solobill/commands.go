package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// exitCode is an error that ends the process with a specific code. A nil
// err with code 0 is a clean exit that must not start the server.
type exitCode struct {
	code int
	err  error
}

func (e exitCode) Error() string {
	if e.err == nil {
		return ""
	}

	return e.err.Error()
}

// usage is printed for -h and --help. The server itself takes no flags:
// it is configured through the environment (internal/config).
func usage(w io.Writer) {
	fmt.Fprintf(w, "usage: solobill [subcommand [args]]\n\n"+
		"Without arguments solobill starts the server, configured through the environment.\n"+
		"Subcommands: %s\n", strings.Join(knownCommands(), ", "))
}

// dispatch decides what the process does:
//
//   - no argument: serve.
//   - -h or --help: print usage to out, exit 0, never serve.
//   - any other argument starting with "-": the server takes no flags, so
//     print usage to out and exit 2, never serve.
//   - a registered subcommand name: run it.
//   - anything else: an unknown subcommand, exit 2.
func dispatch(args []string, out io.Writer, serve func() error) error {
	if len(args) == 0 {
		return serve()
	}

	switch first := args[0]; {
	case first == "-h" || first == "--help":
		usage(out)

		return exitCode{code: 0}
	case strings.HasPrefix(first, "-"):
		usage(out)

		return exitCode{code: 2, err: fmt.Errorf("solobill: unknown flag %q", first)}
	}

	c, ok := commands[args[0]]
	if !ok {
		return exitCode{code: 2, err: fmt.Errorf("solobill: unknown subcommand %q, known: %s", args[0], strings.Join(knownCommands(), ", "))}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return c(ctx, args[1:])
}

// exitWith turns dispatch's result into the process exit code and the
// message to print, if any.
func exitWith(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var e exitCode
	if errors.As(err, &e) {
		return e.code, e.Error()
	}

	return 1, err.Error()
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
