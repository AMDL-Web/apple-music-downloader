package api

import (
	"net/http"
	"runtime"
	"time"

	"amdl/internal/buildinfo"
	"amdl/internal/domain"
	"amdl/internal/media"
)

// systemInfo is the SystemInfo schema of openapi.yaml.
type systemInfo struct {
	Version              string              `json:"version"`
	Commit               string              `json:"commit"`
	BuildDate            string              `json:"build_date"`
	GoVersion            string              `json:"go_version"`
	OS                   string              `json:"os"`
	Arch                 string              `json:"arch"`
	StartedAt            time.Time           `json:"started_at"`
	UptimeSeconds        int64               `json:"uptime_seconds"`
	Simulate             bool                `json:"simulate"`
	SigningMode          string              `json:"signing_mode"`
	MotionArtworkEnabled bool                `json:"motion_artwork_enabled"`
	Jobs                 systemJobs          `json:"jobs"`
	Pools                systemPools         `json:"pools"`
	Tools                []domain.Capability `json:"tools"`
	Storage              []storageInfo       `json:"storage"`
}

type systemJobs struct {
	Workers         int `json:"workers"`
	Active          int `json:"active"`
	QueueDepth      int `json:"queue_depth"`
	QueueCapacity   int `json:"queue_capacity"`
	RecoveryBacklog int `json:"recovery_backlog"`
}

type systemPools struct {
	Downloads       int `json:"downloads"`
	Decrypts        int `json:"decrypts"`
	WrapperRequests int `json:"wrapper_requests"`
}

// storageInfo is the free space of one configured directory. Either both
// byte counts or Error is set.
type storageInfo struct {
	Name       string  `json:"name"`
	Path       string  `json:"path"`
	FreeBytes  *uint64 `json:"free_bytes,omitempty"`
	TotalBytes *uint64 `json:"total_bytes,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// Signing modes of SystemInfo.signing_mode.
const (
	signingModeLocal  = "local"
	signingModeLegacy = "legacy"
)

// systemInfo reports the build, the runtime capacity and the host resources
// the pipeline depends on. It reads no secret: the signing mode says whether
// the .p8 keys are configured, never which.
func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	cfg := s.currentConfig()
	build := buildinfo.Get()
	signingMode := signingModeLegacy
	if cfg.Catalog.DeveloperTokenSigningEnabled() {
		signingMode = signingModeLocal
	}
	queue := s.manager.QueueStats()
	// tools.ffmpeg is a startup key, so the live snapshot names the binary
	// the pipeline's own checker was built with.
	tools := media.NewToolChecker(cfg.Tools).Check(r.Context())
	writeJSON(w, http.StatusOK, systemInfo{
		Version:              build.Version,
		Commit:               build.Commit,
		BuildDate:            build.BuildDate,
		GoVersion:            runtime.Version(),
		OS:                   runtime.GOOS,
		Arch:                 runtime.GOARCH,
		StartedAt:            buildinfo.StartedAt,
		UptimeSeconds:        int64(s.clock().Sub(buildinfo.StartedAt) / time.Second),
		Simulate:             cfg.Simulate.Enabled,
		SigningMode:          signingMode,
		MotionArtworkEnabled: cfg.Catalog.MotionArtworkEnabled,
		Jobs: systemJobs{
			Workers: queue.Workers, Active: queue.Active, QueueDepth: queue.QueueDepth,
			QueueCapacity: queue.QueueCapacity, RecoveryBacklog: queue.RecoveryBacklog,
		},
		// The pools treat a value <= 0 as 1 (limits.NewSemaphore), as the
		// config example documents; report the size in effect.
		Pools: systemPools{
			Downloads:       max(1, cfg.Download.MaxParallelDownloads),
			Decrypts:        max(1, cfg.Download.MaxParallelDecrypts),
			WrapperRequests: max(1, cfg.Download.MaxParallelWrapperRequests),
		},
		Tools: tools,
		Storage: []storageInfo{
			directorySpace("downloads_dir", cfg.Download.DownloadsDir),
			directorySpace("temp_dir", cfg.Download.TempDir),
		},
	})
}

// directorySpace reports the free and total bytes of the file system that
// holds path, or why they are unknown.
func directorySpace(name, path string) storageInfo {
	info := storageInfo{Name: name, Path: path}
	free, total, err := diskSpace(path)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.FreeBytes, info.TotalBytes = &free, &total
	return info
}

// clock returns the current time; tests replace Server.now.
func (s *Server) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
