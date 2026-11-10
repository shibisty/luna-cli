// Command luna-cli is the command-line tool of the Luna stack.
//
//	luna-cli key:generate                  set AUTH_JWT_SECRET in ./.env
//	luna-cli key:generate --show           print a key, change nothing
//	luna-cli key:generate --name LUNA_DASHBOARD_TOKEN --env ../luna-auth/.env
package main

import (
	"fmt"
	"io"
	"os"
	"sort"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

// command is one subcommand.
type command struct {
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

var commands = map[string]command{
	"key:generate": {"generate a random secret and set it in a .env file", keyGenerate},
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without the process: tests call it directly.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "list", "help", "-h", "--help", "-help":
		usage(stdout)
		return 0
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "luna-cli", version)
		return 0
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "luna-cli: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
	return cmd.run(args[1:], stdout, stderr)
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "luna-cli %s\n\nUsage: luna-cli <command> [flags]\n\nCommands:\n", version)
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-16s %s\n", n, commands[n].summary)
	}
	fmt.Fprintf(w, "  %-16s %s\n  %-16s %s\n\nRun luna-cli <command> --help for its flags.\n", "list", "show this list", "version", "print the version")
}
