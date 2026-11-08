// Command amdl is the AMDL client and server in one binary: commands for
// agents and scripts with JSON results (internal/capability lists them), and
// amdl serve, the backend and web console.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"amdl/apps/amdl/internal/cli"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		// 1.x reloaded the config on SIGHUP, so a leftover `docker kill -s HUP`
		// habit must not stop the backend; POST /api/v1/config/reload applies a
		// hand-edited config.
		signal.Ignore(syscall.SIGHUP)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], cli.IO{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		OutTTY: cli.IsTerminal(os.Stdout), ErrTTY: cli.IsTerminal(os.Stderr), InTTY: cli.IsTerminal(os.Stdin),
		ReadSecret: cli.TerminalSecret(os.Stdin),
	})
	stop()
	os.Exit(code)
}
