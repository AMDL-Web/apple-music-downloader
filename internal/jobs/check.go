package jobs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sync"
	"time"

	"amdl/internal/domain"
)

// ErrCheckRunning refuses a disk check while another runs.
var ErrCheckRunning = errors.New("a disk check is running")

// ErrDownloadsDirUnavailable refuses a disk check when download.downloads_dir
// is missing or not a directory: an unmounted share would read as a library
// with every file gone.
var ErrDownloadsDirUnavailable = errors.New("download.downloads_dir is missing or not a directory")

// checkProgressEvery is how many stats a check makes between two progress
// updates of its in-memory state.
const checkProgressEvery = 200

// CheckRequest starts a disk check (design §3.9).
type CheckRequest struct {
	// Resource limits the check to one record; nil checks the library.
	Resource *domain.ResourceRef
	// Force marks what it found even when more than half the checked files
	// are missing.
	Force   bool
	Trigger domain.CheckTrigger
}

// checkState is the disk check in memory: the running or last check and a
// version that moves on every change, for the overview feed.
type checkState struct {
	mu      sync.Mutex
	current *domain.CheckRun
	version uint64
}

func (c *checkState) set(run domain.CheckRun) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = &run
	c.version++
}

// CheckStatus returns the running or last disk check this process ran, with
// a version that changes whenever it does; false before the first check.
func (m *Manager) CheckStatus() (domain.CheckRun, uint64, bool) {
	m.check.mu.Lock()
	defer m.check.mu.Unlock()
	if m.check.current == nil {
		return domain.CheckRun{}, 0, false
	}
	return *m.check.current, m.check.version, true
}

// StartCheck starts a disk check in the background and returns its row. It
// stats the stored path of every done slot in scope and marks the missing
// ones missing; it never marks a found file, moves files or queues a
// download. More than half missing aborts without marking unless forced.
func (m *Manager) StartCheck(ctx context.Context, req CheckRequest) (domain.CheckRun, error) {
	dir := m.config().Download.DownloadsDir
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return domain.CheckRun{}, ErrDownloadsDirUnavailable
	}
	scope := "all"
	if req.Resource != nil {
		if _, err := m.recordStatus(ctx, req.Resource.Kind, req.Resource.ID); err != nil {
			return domain.CheckRun{}, err
		}
		scope = string(req.Resource.Kind) + ":" + req.Resource.ID
	}
	if req.Trigger == "" {
		req.Trigger = domain.CheckManual
	}
	m.check.mu.Lock()
	if m.check.current != nil && m.check.current.FinishedAt == nil {
		m.check.mu.Unlock()
		return domain.CheckRun{}, ErrCheckRunning
	}
	run, err := m.store.StartCheckRun(ctx, req.Trigger, scope, time.Now().UTC())
	if err != nil {
		m.check.mu.Unlock()
		return domain.CheckRun{}, err
	}
	m.check.current = &run
	m.check.version++
	m.check.mu.Unlock()
	go m.runCheck(run, req)
	return run, nil
}

func (m *Manager) runCheck(run domain.CheckRun, req CheckRequest) {
	ctx := context.Background()
	finish := func(aborted string) {
		now := time.Now().UTC()
		run.FinishedAt, run.AbortedReason = &now, aborted
		if err := m.store.FinishCheckRun(ctx, run.ID, run.Checked, run.Missing, aborted, now); err != nil {
			m.logger.Error("record the end of a disk check", "check_id", run.ID, "error", err)
		}
		m.check.set(run)
		m.logger.Info("disk check finished", "check_id", run.ID, "scope", run.Scope, "checked", run.Checked, "missing", run.Missing, "aborted", aborted)
	}
	var kind domain.ResourceKind
	var id string
	if req.Resource != nil {
		kind, id = req.Resource.Kind, req.Resource.ID
	}
	files, err := m.store.DoneFiles(ctx, kind, id)
	if err != nil {
		m.logger.Error("list files for a disk check", "check_id", run.ID, "error", err)
		finish(domain.CheckAbortedFailed)
		return
	}
	var missing []domain.FileKey
	for i, f := range files {
		if _, err := os.Stat(f.Path); errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, f.Key)
		}
		run.Checked, run.Missing = i+1, len(missing)
		if run.Checked%checkProgressEvery == 0 {
			m.check.set(run)
		}
	}
	if !req.Force && len(missing)*2 > len(files) {
		finish(domain.CheckAbortedTooManyMissing)
		return
	}
	now := time.Now().UTC()
	for _, key := range missing {
		if _, err := m.store.MarkFileMissing(ctx, key, now); err != nil {
			m.logger.Error("mark a missing file", "check_id", run.ID, "song_id", key.SongID, "error", err)
			finish(domain.CheckAbortedFailed)
			return
		}
	}
	finish("")
}
