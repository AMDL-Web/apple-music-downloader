// Package server runs the whole backend in-process: it loads the config,
// opens the store, starts the job manager, the download pipeline, hooks and
// library sync, serves the HTTP API on a listener, and stops every component
// in order when its context ends. amdl serve (apps/amdl) wraps Run in a process; an
// embedding app such as the desktop app calls it with its own listener.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"amdl/configs"
	"amdl/internal/api"
	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/events"
	"amdl/internal/hooks"
	"amdl/internal/jobs"
	"amdl/internal/librarysync"
	"amdl/internal/logging"
	"amdl/internal/media"
	"amdl/internal/wrapper"
)

// Options are what Run needs from its caller.
type Options struct {
	// ConfigPath is config.yaml. A missing file is created from the embedded
	// example (configs.Example).
	ConfigPath string
	// HooksPath is hooks.yaml. A missing file disables hooks.
	HooksPath string
	// Listener is served when non-nil; otherwise Run listens on server.listen.
	// Run closes it.
	Listener net.Listener
	// Console serves the web console under /console/ (api.Deps.Console); nil
	// serves the API alone.
	Console http.Handler
	// LogOutput receives the console log (logging.console); nil is stdout. A
	// client that embeds the backend and prints its own results there passes
	// another writer.
	LogOutput io.Writer
}

// loggedError is an error Run has already written to the configured log.
type loggedError struct{ error }

func (e loggedError) Unwrap() error { return e.error }

// Logged reports whether err, returned by Run, is already in the configured
// log, so a caller does not print it a second time.
func Logged(err error) bool {
	var logged loggedError
	return errors.As(err, &logged)
}

// Run starts the backend, serves until ctx ends or the HTTP server fails, and
// then stops every component in shutdownSteps order. Every deferred close runs
// on every return path; a startup failure is returned. Run sets the process's
// default slog logger to the configured one.
func Run(ctx context.Context, opts Options) (err error) {
	if opts.Listener != nil {
		// Covers the returns before Serve owns it; a second Close is harmless.
		defer func() { _ = opts.Listener.Close() }()
	}
	logOut := opts.LogOutput
	if logOut == nil {
		logOut = os.Stdout
	}
	bootstrapLogger := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelInfo}))
	created, err := config.EnsureFile(opts.ConfigPath, configs.Example)
	if err != nil {
		return fmt.Errorf("bootstrap config: %w", err)
	}
	if created {
		bootstrapLogger.Info("created config from example", "path", opts.ConfigPath)
	}
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logSystem, err := logging.NewTo(cfg.Logging, logOut)
	if err != nil {
		return fmt.Errorf("initialize logging: %w", err)
	}
	slog.SetDefault(logSystem.Logger)
	logger := logSystem.Logger.With("component", "main")
	defer func() {
		if err != nil {
			logger.Error("amdl backend stopped on error", "error", err)
			err = loggedError{err}
		}
	}()
	logger.Info("logging initialized", "level", cfg.Logging.Level)
	for _, name := range config.UnreadEnv(os.Environ()) {
		logger.Warn("environment variable is no longer read; set its key in config.yaml",
			"variable", name, "see", config.UpgradeGuide)
	}

	hooksCfg, err := hooks.LoadConfig(opts.HooksPath)
	if err != nil {
		return fmt.Errorf("load hooks config %s: %w", opts.HooksPath, err)
	}
	if err := os.MkdirAll(cfg.Download.DownloadsDir, 0o755); err != nil {
		return fmt.Errorf("create downloads dir: %w", err)
	}
	if err := os.MkdirAll(cfg.Download.TempDir, 0o755); err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	// Remove non-resumable scratch files a previous run left behind. Encrypted
	// resume-* checkpoints deliberately survive this sweep so recovered jobs can
	// continue their HLS transfer. Safe before any job has started because the
	// temp dir is single-writer.
	media.CleanupStaleTemp(cfg.Download.TempDir, logSystem.Logger.With("component", "media"))
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o755); err != nil {
		return fmt.Errorf("create database dir: %w", err)
	}

	store, err := db.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer closeLogged(logger, "close database", store.Close)

	hub := events.NewHub()
	// cfgStore is the live runtime config shared by the API layer and download
	// pipeline. Process-wide concurrency pools are sized once from this startup
	// snapshot; runtime-mutable fields are read when each job starts.
	cfgStore := config.NewFileStore(cfg, opts.ConfigPath)
	wrapperClient, err := wrapper.NewClient(cfg.Wrapper)
	if err != nil {
		return fmt.Errorf("set up wrapper client: %w", err)
	}
	defer closeLogged(logger, "close wrapper client", wrapperClient.Close)

	catalog := applemusic.NewCatalogClient(cfg.Catalog, logSystem.Logger.With("component", "catalog"))
	toolChecker := media.NewToolChecker(cfg.Tools)
	downloader := media.NewDownloader(cfgStore, catalog, wrapperClient, toolChecker, logSystem.Logger.With("component", "media"))
	qualityService := media.NewQualityService(downloader)
	manager := jobs.NewManager(store, hub, downloader, cfg.Download.MaxRunningJobs, logSystem.Logger.With("component", "jobs"))
	hookDispatcher := hooks.NewDispatcher(hooksCfg, manager.Event, logSystem.Logger.With("component", "hooks"))
	manager.SetHooks(hookDispatcher)
	// The library watcher submits through the same manager as the REST API, so
	// anything it queues is an ordinary job (same dedup, same hooks).
	libraryWatcher := librarysync.New(cfgStore, catalog, store, manager, logSystem.Logger.With("component", "librarysync"))

	listener := opts.Listener
	if listener == nil {
		if listener, err = net.Listen("tcp", cfg.Server.Listen); err != nil {
			return fmt.Errorf("listen on %s: %w", cfg.Server.Listen, err)
		}
	}
	// Serve owns the listener once it runs; this close covers the returns
	// before that, and a second Close only reports it was already closed.
	defer func() { _ = listener.Close() }()

	// Startup store work runs to completion even if a signal has already
	// arrived; the wait loop below then goes straight to shutdown.
	startupCtx := context.WithoutCancel(ctx)
	if recovered, err := manager.RecoverUnfinished(startupCtx); err != nil {
		return fmt.Errorf("recover unfinished jobs: %w", err)
	} else if recovered > 0 {
		logger.Info("recovered unfinished jobs", "count", recovered)
	}

	httpHandlerCtx, cancelHTTPHandlers := context.WithCancel(context.Background())
	defer cancelHTTPHandlers()
	var activeHTTPHandlers sync.WaitGroup
	apiServer, err := api.New(api.Deps{
		Config: cfgStore, Store: store, Hub: hub, Manager: manager,
		Logger: logSystem.Logger, Logging: logSystem,
		Wrapper: wrapperClient, Quality: qualityService, LibrarySync: libraryWatcher,
		Catalog: catalog, Console: opts.Console,
	})
	if err != nil {
		return fmt.Errorf("build api server: %w", err)
	}
	routes := apiServer.Routes()
	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			activeHTTPHandlers.Add(1)
			defer activeHTTPHandlers.Done()
			routes.ServeHTTP(w, r)
		}),
		ErrorLog: slog.NewLogLogger(logSystem.Logger.With("component", "http").Handler(), slog.LevelError),
		// Request contexts derive from httpHandlerCtx, which shutdown cancels
		// after the drain as a backstop for handlers that outlive it.
		BaseContext:       func(net.Listener) context.Context { return httpHandlerCtx },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
		// No WriteTimeout: SSE responses are long-lived on purpose and must
		// not be cut off by a whole-response deadline.
	}
	// SSE streams end as soon as Shutdown starts, so the drain waits only for
	// ordinary requests; clients reconnect and resume from Last-Event-ID.
	httpServer.RegisterOnShutdown(apiServer.EndStreams)

	// Workers live until shutdownSteps stops them with jobs.ErrShutdown, not
	// until the signal: cancelling Start's context would read as a user cancel.
	manager.Start(context.Background())
	// The watcher and the token refresher get their own context so they keep
	// running while HTTP drains, and stop in their own step.
	backgroundCtx, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	var background sync.WaitGroup
	// Keeps the scraped music.apple.com web token warm, so requests never pay
	// the inline scrape.
	background.Go(func() { catalog.RunTokenRefresher(backgroundCtx) })
	background.Go(func() { libraryWatcher.Run(backgroundCtx) })
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()
	logger.Info("amdl backend listening", "addr", listener.Addr().String())

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		// Serve returns only on failure until Shutdown has been called.
		runErr = fmt.Errorf("http server: %w", err)
	}

	shutdownSteps{
		logger:           logger,
		timeouts:         defaultShutdownTimeouts,
		http:             httpServer,
		cancelHTTPBase:   cancelHTTPHandlers,
		waitHTTPHandlers: func(ctx context.Context) error { return waitGroup(ctx, &activeHTTPHandlers) },
		stopBackground:   stopBackground,
		waitBackground:   func(ctx context.Context) error { return waitGroup(ctx, &background) },
		jobs:             manager,
		downloader:       downloader,
		hooks:            hookDispatcher,
	}.run()
	return runErr
}

// closeLogged runs a deferred close and logs its error, which has no caller
// left to return to.
func closeLogged(logger *slog.Logger, what string, closeFn func() error) {
	if err := closeFn(); err != nil {
		logger.Error(what, "error", err)
	}
}
