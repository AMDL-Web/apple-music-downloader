package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"amdl/internal/config"
	"amdl/internal/domain"
)

// encodeOverrides serializes a job's config overrides for the jobs row;
// jobs without overrides store the empty string.
func encodeOverrides(o *config.DownloadOverrides) (string, error) {
	if o == nil {
		return "", nil
	}
	raw, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// decodeOverrides reads a stored overrides column. A corrupt column fails
// loudly: silently dropping it would run the download with the global config
// instead of the settings it was added with. A column that held only removed
// keys decodes empty, which is no overrides.
func decodeOverrides(raw string) (*config.DownloadOverrides, error) {
	if raw == "" {
		return nil, nil
	}
	parsed := &config.DownloadOverrides{}
	if err := json.Unmarshal([]byte(raw), parsed); err != nil {
		return nil, fmt.Errorf("decode overrides: %w", err)
	}
	if *parsed == (config.DownloadOverrides{}) {
		return nil, nil
	}
	return parsed, nil
}

// CreateJob inserts a new run row. An empty Trigger stores manual; an empty
// ResourceKind or ResourceID stores NULL (stations and record-less history). Its item counters are not stored; reads
// derive them from run_items. A nil QueueOrder puts the job at the end of the
// run order (NextQueueOrder). ErrDuplicateActive reports a canonical_key or a
// record that another queued or running run already holds.
func (s *Store) CreateJob(ctx context.Context, job domain.Job) error {
	overrides, err := encodeOverrides(job.Overrides)
	if err != nil {
		return err
	}
	var queueOrder any
	if job.QueueOrder != nil {
		queueOrder = *job.QueueOrder
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO runs(id,input,type,storefront,title,artwork_url,artist_name,artist_url,curator_name,release_date,genre,artwork_bg_color,artwork_text_color1,artwork_text_color2,artwork_text_color3,artwork_text_color4,canonical_key,overrides,status,total_items,error,started_at,finished_at,created_at,updated_at,resource_kind,resource_id,trigger,priority,plan,queue_order)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,COALESCE(?,(`+nextQueueOrderSelect+`)))`, job.ID, job.Input, job.Type, job.Storefront, job.Title, job.ArtworkURL, job.ArtistName, job.ArtistURL, job.CuratorName, job.ReleaseDate, job.Genre,
		job.ArtworkBgColor, job.ArtworkTextColor1, job.ArtworkTextColor2, job.ArtworkTextColor3, job.ArtworkTextColor4, job.CanonicalKey, overrides, string(job.Status), job.TotalItems,
		job.Error, formatOptionalTime(job.StartedAt), formatOptionalTime(job.FinishedAt), formatTime(job.CreatedAt), formatTime(job.UpdatedAt),
		nullIfEmpty(string(job.ResourceKind)), nullIfEmpty(job.ResourceID), runTrigger(job.Trigger), job.Priority, job.Plan, queueOrder)
	if err != nil && isUniqueConstraintErr(err) {
		return ErrDuplicateActive
	}
	return err
}

// jobColumns is the select list of every query that feeds scanJob, over
// "runs j". The four item counters are derived from run_items here and nowhere
// else, so every Job the store returns carries current counters.
const jobColumns = `j.id, j.input, j.type, j.storefront, j.title, j.artwork_url, j.artist_name, j.artist_url, j.curator_name, j.release_date, j.genre,
	j.artwork_bg_color, j.artwork_text_color1, j.artwork_text_color2, j.artwork_text_color3, j.artwork_text_color4,
	j.motion_artwork_url, j.motion_artwork_tall_url,
	j.motion_artwork_bg_color, j.motion_artwork_text_color1, j.motion_artwork_text_color2, j.motion_artwork_text_color3, j.motion_artwork_text_color4,
	j.motion_artwork_tall_bg_color, j.motion_artwork_tall_text_color1, j.motion_artwork_tall_text_color2, j.motion_artwork_tall_text_color3, j.motion_artwork_tall_text_color4,
	j.canonical_key, j.overrides, j.status, j.queue_order, j.total_items,
	COALESCE(j.resource_kind, ''), COALESCE(j.resource_id, ''), j.trigger, j.priority, j.plan,
	(SELECT COUNT(*) FROM run_items i WHERE i.job_id = j.id AND i.status = '` + string(domain.ItemCompleted) + `'),
	(SELECT COUNT(*) FROM run_items i WHERE i.job_id = j.id AND i.status IN ('` + string(domain.ItemSkipped) + `', '` + string(domain.ItemUnavailable) + `')),
	(SELECT COUNT(*) FROM run_items i WHERE i.job_id = j.id AND i.status = '` + string(domain.ItemFailed) + `'),
	(SELECT COUNT(*) FROM run_items i WHERE i.job_id = j.id AND i.status = '` + string(domain.ItemCancelled) + `'),
	j.error, j.started_at, j.finished_at, j.created_at, j.updated_at`

// FindActiveJobByKey returns the queued/running job matching canonicalKey, if any.
func (s *Store) FindActiveJobByKey(ctx context.Context, canonicalKey string) (domain.Job, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM runs j WHERE j.canonical_key=? AND j.status IN (?,?)`,
		canonicalKey, string(domain.JobQueued), string(domain.JobRunning))
	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Job{}, false, nil
		}
		return domain.Job{}, false, err
	}
	return job, true, nil
}

func updateJob(ctx context.Context, x execer, job domain.Job) error {
	_, err := x.ExecContext(ctx, `UPDATE runs SET type=?, storefront=?, title=?, artwork_url=?, artist_name=?, artist_url=?, curator_name=?, release_date=?, genre=?, artwork_bg_color=?, artwork_text_color1=?, artwork_text_color2=?, artwork_text_color3=?, artwork_text_color4=?, status=?, total_items=?, error=?, started_at=?, finished_at=?, updated_at=? WHERE id=?`,
		job.Type, job.Storefront, job.Title, job.ArtworkURL, job.ArtistName, job.ArtistURL, job.CuratorName, job.ReleaseDate, job.Genre,
		job.ArtworkBgColor, job.ArtworkTextColor1, job.ArtworkTextColor2, job.ArtworkTextColor3, job.ArtworkTextColor4,
		string(job.Status), job.TotalItems, job.Error, formatOptionalTime(job.StartedAt), formatOptionalTime(job.FinishedAt), formatTime(job.UpdatedAt), job.ID)
	return err
}

// updateFinalJob also rewrites the overrides column from job.Overrides. A row
// stored before the overrides shrank loses its removed keys (a
// media_user_token among them) here, at its terminal write.
func updateFinalJob(ctx context.Context, x execer, job domain.Job) error {
	overrides, err := encodeOverrides(job.Overrides)
	if err != nil {
		return err
	}
	_, err = x.ExecContext(ctx, `UPDATE runs SET type=?, storefront=?, title=?, artwork_url=?, artist_name=?, artist_url=?, curator_name=?, release_date=?, genre=?, artwork_bg_color=?, artwork_text_color1=?, artwork_text_color2=?, artwork_text_color3=?, artwork_text_color4=?, overrides=?, status=?, total_items=?, error=?, started_at=?, finished_at=?, updated_at=? WHERE id=?`,
		job.Type, job.Storefront, job.Title, job.ArtworkURL, job.ArtistName, job.ArtistURL, job.CuratorName, job.ReleaseDate, job.Genre,
		job.ArtworkBgColor, job.ArtworkTextColor1, job.ArtworkTextColor2, job.ArtworkTextColor3, job.ArtworkTextColor4,
		overrides, string(job.Status), job.TotalItems, job.Error, formatOptionalTime(job.StartedAt), formatOptionalTime(job.FinishedAt), formatTime(job.UpdatedAt), job.ID)
	return err
}

// UpdateJob writes a job's mutable columns from job, including started_at and
// finished_at. It leaves overrides, the animated-cover columns and
// queue_order alone (see FinalizeJob, SetJobMotionArtwork and SetQueueOrder):
// a running pipeline writes the Job it holds through here, and that struct's
// queue_order is stale. The item counters are not stored.
func (s *Store) UpdateJob(ctx context.Context, job domain.Job) error {
	return updateJob(ctx, s.db, job)
}

// SetJobMotionArtwork writes only the animated-cover columns and reports
// whether a row matched.
//
// It is separate from UpdateJob because the motion artwork resolves
// asynchronously: the Job a running pipeline holds predates it, and a generic
// UPDATE carrying that stale struct would blank the columns right after this
// fills them. updated_at is left alone too, because gaining an animated cover
// is not job progress and should not reorder a list sorted by it. The caller
// runs detached from the job's context, so the job can be deleted before this
// lands; an UPDATE that matches nothing is not an error, but returns false.
func (s *Store) SetJobMotionArtwork(ctx context.Context, id string, art domain.MotionArtwork) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET
		motion_artwork_url=?, motion_artwork_tall_url=?,
		motion_artwork_bg_color=?, motion_artwork_text_color1=?, motion_artwork_text_color2=?, motion_artwork_text_color3=?, motion_artwork_text_color4=?,
		motion_artwork_tall_bg_color=?, motion_artwork_tall_text_color1=?, motion_artwork_tall_text_color2=?, motion_artwork_tall_text_color3=?, motion_artwork_tall_text_color4=?
		WHERE id=?`,
		art.SquareURL, art.TallURL,
		art.SquareColors.BgColor, art.SquareColors.TextColor1, art.SquareColors.TextColor2, art.SquareColors.TextColor3, art.SquareColors.TextColor4,
		art.TallColors.BgColor, art.TallColors.TextColor1, art.TallColors.TextColor2, art.TallColors.TextColor3, art.TallColors.TextColor4,
		id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// GetJob returns the job with id, or an error wrapping sql.ErrNoRows.
func (s *Store) GetJob(ctx context.Context, id string) (domain.Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM runs j WHERE j.id=?`, id))
}

// JobListFilter controls GET /downloads listing: pagination, status and type
// filters, and title/input/artist substring search. Zero values mean "no
// constraint" except Limit, which the store clamps to [1, 200] with a default
// of 50.
type JobListFilter struct {
	Limit    int
	Offset   int
	Statuses []domain.JobStatus
	Types    []string
	Query    string
	Sort     string // created_at (default), updated_at or queue_order
	Order    string // desc (default) or asc
	// ResourceKind, ResourceID and Triggers select runs of a record and
	// runs started a given way.
	ResourceKind domain.ResourceKind
	ResourceID   string
	Triggers     []domain.RunTrigger
	// Current keeps the runs nothing has replaced: no newer run of the same
	// record (or, for a station, of the same link), and a record that is
	// not complete.
	Current bool
}

const (
	JobListSortCreatedAt = "created_at"
	JobListSortUpdatedAt = "updated_at"
	// JobListSortQueueOrder lists the queued jobs in run order (asc: next to
	// run first) ahead of every other job; the others follow in insertion
	// order.
	JobListSortQueueOrder = "queue_order"
	JobListOrderAsc       = "asc"
	JobListOrderDesc      = "desc"
)

// Normalize clamps pagination and fills default sort/order. ListJobs calls
// this; API handlers may also call it so response echo fields match the
// values actually applied.
func (f *JobListFilter) Normalize() {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	switch f.Sort {
	case JobListSortUpdatedAt, JobListSortQueueOrder:
	default:
		f.Sort = JobListSortCreatedAt
	}
	switch f.Order {
	case JobListOrderAsc:
	default:
		f.Order = JobListOrderDesc
	}
}

func (f JobListFilter) whereClause() (string, []any) {
	var (
		conds []string
		args  []any
	)
	if len(f.Statuses) > 0 {
		placeholders := make([]string, len(f.Statuses))
		for i, st := range f.Statuses {
			placeholders[i] = "?"
			args = append(args, string(st))
		}
		conds = append(conds, "j.status IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(f.Types) > 0 {
		placeholders := make([]string, len(f.Types))
		for i, t := range f.Types {
			placeholders[i] = "?"
			args = append(args, t)
		}
		conds = append(conds, "j.type IN ("+strings.Join(placeholders, ",")+")")
	}
	if f.ResourceKind != "" {
		conds = append(conds, "j.resource_kind = ?")
		args = append(args, string(f.ResourceKind))
	}
	if f.ResourceID != "" {
		conds = append(conds, "j.resource_id = ?")
		args = append(args, f.ResourceID)
	}
	if len(f.Triggers) > 0 {
		conds = append(conds, "j.trigger IN ("+placeholders(len(f.Triggers))+")")
		for _, t := range f.Triggers {
			args = append(args, string(t))
		}
	}
	if f.Current {
		conds = append(conds,
			`(j.resource_id IS NULL OR NOT EXISTS (SELECT 1 FROM runs n
				WHERE n.resource_kind = j.resource_kind AND n.resource_id = j.resource_id AND n.rowid > j.rowid))`,
			`(j.resource_id IS NOT NULL OR j.canonical_key = '' OR NOT EXISTS (SELECT 1 FROM runs n
				WHERE n.canonical_key = j.canonical_key AND n.rowid > j.rowid))`,
			`COALESCE(CASE j.resource_kind
				WHEN 'album' THEN (SELECT a.state FROM albums a WHERE a.id = j.resource_id)
				WHEN 'playlist' THEN (SELECT p.state FROM playlists p WHERE p.id = j.resource_id) END, '') <> 'complete'`)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + escapeLike(q) + "%"
		conds = append(conds, `(j.title LIKE ? ESCAPE '\' OR j.input LIKE ? ESCAPE '\' OR j.artist_name LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like)
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func escapeLike(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

func (s *Store) ListJobs(ctx context.Context, filter JobListFilter) ([]domain.Job, int, error) {
	filter.Normalize()
	where, args := filter.whereClause()

	var total int
	countQuery := `SELECT COUNT(*) FROM runs j` + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	orderDir := "DESC"
	if filter.Order == JobListOrderAsc {
		orderDir = "ASC"
	}
	orderBy := "j.created_at " + orderDir
	switch filter.Sort {
	case JobListSortUpdatedAt:
		orderBy = "j.updated_at " + orderDir
	case JobListSortQueueOrder:
		// Only a queued job's queue_order orders anything; the others keep
		// their last value, so they follow the queue in insertion order.
		orderBy = "CASE WHEN j.status = '" + string(domain.JobQueued) + "' THEN 0 ELSE 1 END, " +
			"CASE WHEN j.status = '" + string(domain.JobQueued) + "' THEN j.queue_order END " + orderDir
	}
	listArgs := append(args, filter.Limit, filter.Offset)
	// Jobs submitted together share created_at (and often updated_at), and job
	// ids are random, so the tie-break is the insert order (rowid): a batch
	// lists in submission order in either direction, and the order is total, so
	// pages of an unchanged list never skip or repeat a job inside it. A job
	// submitted or updated between two page requests still shifts the offsets,
	// as with any offset paging. rowid is stable here because the store never
	// runs VACUUM and jobs is not WITHOUT ROWID.
	rows, err := s.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM runs j`+where+` ORDER BY `+orderBy+`, j.rowid ASC LIMIT ? OFFSET ?`,
		listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]domain.Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, job)
	}
	return out, total, rows.Err()
}

// CountJobsByStatus returns the number of jobs in each persisted status after
// applying the same non-pagination filters as ListJobs. Statuses with no
// matching jobs are omitted from the map so callers can choose their own
// stable zero-valued response shape.
func (s *Store) CountJobsByStatus(ctx context.Context, filter JobListFilter) (map[domain.JobStatus]int, error) {
	where, args := filter.whereClause()
	rows, err := s.db.QueryContext(ctx, `SELECT j.status, COUNT(*) FROM runs j`+where+` GROUP BY j.status`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[domain.JobStatus]int)
	for rows.Next() {
		var (
			status domain.JobStatus
			count  int
		)
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

// DeleteJob removes a terminal (completed/failed/cancelled) job together with
// its items and old per-job events, then persists a job_deleted tombstone event
// so overview feeds can replay the deletion from a snapshot cursor. Queued or
// running jobs are refused with ErrJobNotTerminal so an in-flight worker never
// loses the rows it is still updating; cancel the job first. The status check,
// deletes, and tombstone insert run in one transaction.
//
// The row status alone cannot tell whether the manager's finalize sequence
// (terminal event insert + hook dispatch) has finished — callers must go
// through jobs.Manager.Delete, which additionally refuses while a finalize is
// still in flight.
func (s *Store) DeleteJob(ctx context.Context, id string) (domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM runs WHERE id=?`, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Event{}, ErrJobNotFound
		}
		return domain.Event{}, err
	}
	if !domain.JobStatus(status).IsTerminal() {
		return domain.Event{}, ErrJobNotTerminal
	}
	tombstone, err := deleteRun(ctx, tx, id)
	if err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	return tombstone, nil
}

// deleteRun removes a run with its items and events and records its
// job_deleted tombstone.
func deleteRun(ctx context.Context, tx *sql.Tx, id string) (domain.Event, error) {
	for _, stmt := range []string{`DELETE FROM run_events WHERE job_id=?`, `DELETE FROM run_items WHERE job_id=?`, `DELETE FROM runs WHERE id=?`} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return domain.Event{}, err
		}
	}
	return addEvent(ctx, tx, domain.Event{JobID: id, Type: domain.EventJobDeleted})
}

// ListRecoverableJobs returns the work a previous process left unfinished:
// the running jobs, then the queued ones, each in run order (queue_order,
// then insertion).
func (s *Store) ListRecoverableJobs(ctx context.Context) ([]domain.Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM runs j WHERE j.status IN (?,?)
		ORDER BY CASE WHEN j.status = ? THEN 0 ELSE 1 END, j.queue_order ASC, j.rowid ASC`,
		string(domain.JobQueued), string(domain.JobRunning), string(domain.JobRunning))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

type jobScanner interface {
	Scan(dest ...any) error
}

// scanJob reads one row selected with jobColumns.
func scanJob(row jobScanner) (domain.Job, error) {
	var job domain.Job
	var overrides, status, created, updated, resourceKind, trigger string
	var started, finished sql.NullString
	var queueOrder int64
	err := row.Scan(&job.ID, &job.Input, &job.Type, &job.Storefront, &job.Title, &job.ArtworkURL, &job.ArtistName, &job.ArtistURL, &job.CuratorName, &job.ReleaseDate, &job.Genre,
		&job.ArtworkBgColor, &job.ArtworkTextColor1, &job.ArtworkTextColor2, &job.ArtworkTextColor3, &job.ArtworkTextColor4,
		&job.MotionArtworkURL, &job.MotionArtworkTallURL,
		&job.MotionArtworkBgColor, &job.MotionArtworkTextColor1, &job.MotionArtworkTextColor2, &job.MotionArtworkTextColor3, &job.MotionArtworkTextColor4,
		&job.MotionArtworkTallBgColor, &job.MotionArtworkTallTextColor1, &job.MotionArtworkTallTextColor2, &job.MotionArtworkTallTextColor3, &job.MotionArtworkTallTextColor4,
		&job.CanonicalKey, &overrides, &status, &queueOrder, &job.TotalItems,
		&resourceKind, &job.ResourceID, &trigger, &job.Priority, &job.Plan,
		&job.CompletedItems, &job.SkippedItems, &job.FailedItems, &job.CancelledItems,
		&job.Error, &started, &finished, &created, &updated)
	if err == nil {
		var decodeErr error
		if job.Overrides, decodeErr = decodeOverrides(overrides); decodeErr != nil {
			return job, fmt.Errorf("job %s: %w", job.ID, decodeErr)
		}
	}
	job.Status = domain.JobStatus(status)
	job.ResourceKind = domain.ResourceKind(resourceKind)
	job.Trigger = domain.RunTrigger(trigger)
	job.QueueOrder = &queueOrder
	job.StartedAt = parseOptionalTime(started)
	job.FinishedAt = parseOptionalTime(finished)
	job.CreatedAt = parseTime(created)
	job.UpdatedAt = parseTime(updated)
	return job, err
}

// FinalizeJob persists a job's terminal status together with its terminal
// event in one transaction, so no reader observes a terminal status without
// the terminal event already committed. The same transaction deletes the
// job's item_progress events: they are live telemetry, and each item row keeps
// its final state.
func (s *Store) FinalizeJob(ctx context.Context, job domain.Job, event domain.Event) (domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := updateFinalJob(ctx, tx, job); err != nil {
		return domain.Event{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM run_events WHERE job_id=? AND type=?`, job.ID, string(domain.EventItemProgress)); err != nil {
		return domain.Event{}, fmt.Errorf("prune item_progress events of job %s: %w", job.ID, err)
	}
	stored, err := addEvent(ctx, tx, event)
	if err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	return stored, nil
}

// nullIfEmpty maps "" to SQL NULL.
func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func runTrigger(t domain.RunTrigger) string {
	if t == "" {
		return string(domain.TriggerManual)
	}
	return string(t)
}
