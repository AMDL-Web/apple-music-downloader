package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

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

const (
	defaultConfigPath = "configs/config.yaml"
	defaultHooksPath  = "configs/hooks.yaml"
)

// configFiles are the two files the backend reads at startup.
type configFiles struct {
	config string // AMDL_CONFIG
	hooks  string // AMDL_HOOKS_CONFIG
}

func main() {
	files := configFiles{config: os.Getenv("AMDL_CONFIG"), hooks: os.Getenv("AMDL_HOOKS_CONFIG")}
	if files.config == "" {
		files.config = defaultConfigPath
	}
	if files.hooks == "" {
		files.hooks = defaultHooksPath
	}
	// SIGHUP is ignored rather than left to Go's default exit: 1.x reloaded the
	// config on it, so a leftover `docker kill -s HUP` habit must not stop the
	// backend. A hand-edited config file is applied through
	// POST /api/v1/config/reload.
	signal.Ignore(syscall.SIGHUP)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, files)
	stop()
	if err != nil {
		var logged loggedError
		if !errors.As(err, &logged) {
			slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("amdl backend failed", "error", err)
		}
		os.Exit(1)
	}
}

// loggedError is an error run has already written to the configured log, so
// main exits without printing it a second time.
type loggedError struct{ error }

func (e loggedError) Unwrap() error { return e.error }

// run starts the backend, serves until ctx ends (SIGINT/SIGTERM) or the HTTP
// server fails, and then stops every component in shutdownSteps order. Every
// deferred close runs on every return path; a startup failure is returned.
func run(ctx context.Context, files configFiles) (err error) {
	bootstrapLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	created, err := config.EnsureFile(files.config)
	if err != nil {
		return fmt.Errorf("bootstrap config: %w", err)
	}
	if created {
		bootstrapLogger.Info("created config from example", "path", files.config)
	}
	cfg, err := config.Load(files.config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logSystem, err := logging.New(cfg.Logging)
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

	hooksCfg, err := hooks.LoadConfig(files.hooks)
	if err != nil {
		return fmt.Errorf("load hooks config %s: %w", files.hooks, err)
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
	cfgStore := config.NewFileStore(cfg, files.config)
	wrapperClient, err := wrapper.NewClient(cfg.Wrapper)
	if err != nil {
		return fmt.Errorf("set up wrapper client: %w", err)
	}
	defer closeLogged(logger, "close wrapper client", wrapperClient.Close)

	catalog := applemusic.NewCatalogClient(cfg.Catalog, logSystem.Logger.With("component", "catalog"))
	if err := catalog.InitDeveloperToken(); err != nil {
		return fmt.Errorf("sign apple music developer token: %w", err)
	}
	toolChecker := media.NewToolChecker(cfg.Tools)
	downloader := media.NewDownloader(cfgStore, catalog, wrapperClient, toolChecker, logSystem.Logger.With("component", "media"))
	qualityService := media.NewQualityService(downloader)
	manager := jobs.NewManager(store, hub, downloader, cfg.Download.MaxRunningJobs, logSystem.Logger.With("component", "jobs"))
	hookDispatcher := hooks.NewDispatcher(hooksCfg, manager.Event, logSystem.Logger.With("component", "hooks"))
	manager.SetHooks(hookDispatcher)
	// The library watcher submits through the same manager as the REST API, so
	// anything it queues is an ordinary job (same dedup, same hooks).
	libraryWatcher := librarysync.New(cfgStore, catalog, store, manager, logSystem.Logger.With("component", "librarysync"))

	listener, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Server.Listen, err)
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
		Catalog: catalog,
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
	// Keeps the scraped music.apple.com web tokens (legacy mode and
	// signed_mode_hls_source=web_token) warm, so requests never pay the
	// inline scrape.
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
