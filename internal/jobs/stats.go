package jobs

// QueueStats is a point-in-time view of the manager's capacity and load, for
// GET /api/v1/system. The fields are read one after another, not under one
// lock, so they can be momentarily inconsistent with each other.
type QueueStats struct {
	// Workers is the number of worker goroutines, fixed when the manager is
	// built (download.max_running_jobs at startup).
	Workers int
	// Active is the number of jobs inside Processor.ProcessJob.
	Active int
	// QueueDepth is the number of admitted jobs waiting for a worker.
	QueueDepth int
	// QueueCapacity is the size of the admission queue; a submission that
	// finds it full is refused with queue_full.
	QueueCapacity int
	// RecoveryBacklog is the number of jobs recovered at startup still
	// waiting for a worker. That queue is unbounded and drains first.
	RecoveryBacklog int
}

// QueueStats reports the current worker and queue figures. Nil-safe.
func (m *Manager) QueueStats() QueueStats {
	if m == nil {
		return QueueStats{}
	}
	m.recoveryMu.Lock()
	backlog := len(m.recoveryQueue)
	m.recoveryMu.Unlock()
	return QueueStats{
		Workers:         m.workers,
		Active:          int(m.active.Load()),
		QueueDepth:      len(m.queue),
		QueueCapacity:   cap(m.queue),
		RecoveryBacklog: backlog,
	}
}
