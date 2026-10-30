// Command amdl-server is the AMDL backend as a process: internal/server.Run
// with the config paths from the environment, the web console mounted, and
// SIGINT/SIGTERM as the shutdown signal.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"amdl/apps/console"
	"amdl/internal/server"
)

const (
	defaultConfigPath = "configs/config.yaml"
	defaultHooksPath  = "configs/hooks.yaml"
)

func main() {
	// SIGHUP is ignored rather than left to Go's default exit: 1.x reloaded the
	// config on it, so a leftover `docker kill -s HUP` habit must not stop the
	// backend. A hand-edited config file is applied through
	// POST /api/v1/config/reload.
	signal.Ignore(syscall.SIGHUP)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := server.Run(ctx, options(os.Getenv))
	stop()
	if err != nil {
		if !server.Logged(err) {
			slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("amdl backend failed", "error", err)
		}
		os.Exit(1)
	}
}

// options reads AMDL_CONFIG and AMDL_HOOKS_CONFIG, falling back to the paths
// under configs/ relative to the working directory, and mounts the console.
func options(getenv func(string) string) server.Options {
	opts := server.Options{ConfigPath: getenv("AMDL_CONFIG"), HooksPath: getenv("AMDL_HOOKS_CONFIG"), Console: console.Handler()}
	if opts.ConfigPath == "" {
		opts.ConfigPath = defaultConfigPath
	}
	if opts.HooksPath == "" {
		opts.HooksPath = defaultHooksPath
	}
	return opts
}
