package jobs

// QueueStats is a point-in-time view of the manager's capacity and load, for
// GET /api/v1/system. QueueDepth and RecoveryBacklog are read together under
// one lock; Active is read apart from them, so it can be momentarily
// inconsistent with the other two.
type QueueStats struct {
	// Workers is the number of worker goroutines, fixed when the manager is
	// built (download.max_running_jobs at startup).
	Workers int
	// Active is the number of jobs inside Processor.ProcessJob.
	Active int
	// QueueDepth is the number of submitted or retried jobs waiting for a
	// worker.
	QueueDepth int
	// QueueCapacity is how many such jobs may wait; a submission that finds
	// them all taken is refused with queue_full.
	QueueCapacity int
	// RecoveryBacklog is the number of jobs recovered at startup still
	// waiting for a worker. They do not count against QueueCapacity and run
	// in queue order like any other job.
	RecoveryBacklog int
}

// QueueStats reports the current worker and queue figures. Nil-safe.
func (m *Manager) QueueStats() QueueStats {
	if m == nil {
		return QueueStats{}
	}
	m.mu.Lock()
	depth, backlog := m.waiting.admitted(), m.waiting.recovered
	m.mu.Unlock()
	return QueueStats{
		Workers:         m.workers,
		Active:          int(m.active.Load()),
		QueueDepth:      depth,
		QueueCapacity:   m.capacity,
		RecoveryBacklog: backlog,
	}
}
