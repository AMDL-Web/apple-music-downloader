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
	"amdl/internal/domain"
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
	defer logSystem.Close()
	slog.SetDefault(logSystem.Logger)
	logger := logSystem.Logger.With("component", "main")
	defer func() {
		if err != nil {
			logger.Error("amdl backend stopped on error", "error", err)
			err = loggedError{err}
		}
	}()
	logger.Info("logging initialized", "level", cfg.Logging.Level, "format", cfg.Logging.Format, "file_enabled", cfg.Logging.FileEnabled)
	// Registered early so a SIGHUP during startup is queued instead of
	// terminating the process (the default action).
	reloadSignals := make(chan os.Signal, 1)
	signal.Notify(reloadSignals, syscall.SIGHUP)
	defer signal.Stop(reloadSignals)

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
	wrapperClient, err := wrapper.NewClient(cfg.Wrapper, wrapper.WithDataConcurrencyLimit(cfg.Download.MaxParallelWrapperRequests))
	if err != nil {
		return fmt.Errorf("connect wrapper-manager: %w", err)
	}
	defer closeLogged(logger, "close wrapper-manager client", wrapperClient.Close)

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
	// Shares the same live config as the API layer and the download pipeline,
	// so a submission's dedup key reflects the downloads dir the job will
	// actually run under.
	manager.SetConfigStore(cfgStore)
	// The library watcher submits through the same manager as the REST API, so
	// anything it queues is an ordinary job (same dedup, same hooks), and it
	// reads catalog metadata with the backend's internal developer token,
	// which carries no origin claim.
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
	sweepRecoveredTempOverrides(startupCtx, cfg, store, logger, logSystem.Logger.With("component", "media"))

	httpHandlerCtx, cancelHTTPHandlers := context.WithCancel(context.Background())
	defer cancelHTTPHandlers()
	var activeHTTPHandlers sync.WaitGroup
	apiServer := api.New(api.Deps{
		Config: cfgStore, Store: store, Hub: hub, Manager: manager,
		Logger: logSystem.Logger, Logging: logSystem,
		Wrapper: wrapperClient, Quality: qualityService, DeveloperToken: catalog, LibrarySync: libraryWatcher,
	})
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
serve:
	for {
		select {
		case <-reloadSignals:
			reloadConfig(cfgStore, logSystem, logger)
		case <-ctx.Done():
			break serve
		case err := <-serveErr:
			// Serve returns only on failure until Shutdown has been called.
			runErr = fmt.Errorf("http server: %w", err)
			break serve
		}
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

// sweepRecoveredTempOverrides cleans the per-request temp_dir overrides of
// jobs left queued or running. The configured-dir sweep doesn't cover them,
// and they are exactly the jobs whose in-flight scratch could have leaked on a
// crash. It runs before the worker pool starts, so nothing writes to them.
func sweepRecoveredTempOverrides(ctx context.Context, cfg config.Config, store *db.Store, logger, mediaLogger *slog.Logger) {
	recoverable, err := store.ListRecoverableJobs(ctx)
	if err != nil {
		logger.Warn("list recoverable jobs; skipping the sweep of their temp_dir overrides", "error", err)
		return
	}
	swept := map[string]bool{cfg.Download.TempDir: true}
	for _, job := range recoverable {
		dir, ok, err := recoveryTempOverride(cfg, job)
		if err != nil {
			logger.Warn("skip unsafe recovered temp override", "job_id", job.ID, "error", err)
			continue
		}
		if ok && !swept[dir] {
			swept[dir] = true
			media.CleanupStaleTemp(dir, mediaLogger)
		}
	}
}

// reloadConfig handles SIGHUP like POST /api/v1/config/reload: it re-reads the
// config file and applies its runtime keys and the logging level. An
// unreadable or invalid file is logged and the previous snapshot stays.
func reloadConfig(cfgStore *config.Store, logSystem *logging.System, logger *slog.Logger) {
	if err := cfgStore.Reload(); err != nil {
		logger.Error("config reload on SIGHUP failed; keeping the previous config", "error", err)
		return
	}
	level := cfgStore.Get().Logging.Level
	if err := logSystem.SetLevel(level); err != nil {
		logger.Error("apply reloaded logging level", "level", level, "error", err)
	}
	logger.Info("config reloaded on SIGHUP", "logging_level", level)
}

// closeLogged runs a deferred close and logs its error, which has no caller
// left to return to.
func closeLogged(logger *slog.Logger, what string, closeFn func() error) {
	if err := closeFn(); err != nil {
		logger.Error(what, "error", err)
	}
}

// recoveryTempOverride applies the same filesystem trust boundary used when
// accepting and running a job. Persisted rows may predate that validation, so
// startup cleanup must not trust their raw temp_dir values.
func recoveryTempOverride(base config.Config, job domain.Job) (string, bool, error) {
	if job.Overrides == nil || job.Overrides.TempDir == nil || *job.Overrides.TempDir == "" {
		return "", false, nil
	}
	effective, err := job.Overrides.ApplyValidated(base)
	if err != nil {
		return "", false, err
	}
	return effective.Download.TempDir, true, nil
}
