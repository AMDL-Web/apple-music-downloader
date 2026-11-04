package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
	"amdl/internal/jobs"
)

// simProcessor downloads the way internal/media does in simulate mode, over
// a made-up catalog: it reads the record, plans, and settles each slot,
// writing a small file and honouring download.overwrite_existing when a
// file is already at the target. Links are "album|us|<id>",
// "song|us|<id>|<album id>" and "playlist|us|<id>".
type simProcessor struct {
	dir     string
	catalog *simCatalog
	cfg     *config.Store

	mu sync.Mutex
	// unavailable songs offer no codec: their main slot ends unavailable.
	unavailable map[string]bool
	// failing songs fail their slots.
	failing map[string]bool
	// hold, when set, blocks each record run after its plan until it is
	// closed or the run is cancelled; started gets the run id first.
	hold    chan struct{}
	started chan string
}

func (p *simProcessor) ValidateRequest(_ context.Context, url string) (jobs.ValidationResult, error) {
	parts := strings.Split(url, "|")
	if len(parts) < 3 || len(parts) > 4 {
		return jobs.ValidationResult{}, &jobs.RequestError{Code: "invalid_url", Message: "malformed test url"}
	}
	result := jobs.ValidationResult{Type: parts[0], Storefront: parts[1], ID: parts[2]}
	if len(parts) == 4 {
		result.AlbumID = parts[3]
	}
	return result, nil
}

func (p *simProcessor) setUnavailable(songID string, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.unavailable == nil {
		p.unavailable = map[string]bool{}
	}
	p.unavailable[songID] = on
}

func (p *simProcessor) setFailing(songID string, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failing == nil {
		p.failing = map[string]bool{}
	}
	p.failing[songID] = on
}

func (p *simProcessor) holdRuns() (release func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	hold := make(chan struct{})
	p.hold, p.started = hold, make(chan string, 16)
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			p.hold = nil
			p.mu.Unlock()
			close(hold)
		})
	}
}

func (p *simProcessor) ProcessJob(ctx context.Context, job domain.Job, reporter jobs.Reporter) error {
	if job.ResourceID == "" {
		return nil
	}
	rr := reporter.(jobs.RecordReporter)
	switch job.ResourceKind {
	case domain.ResourceAlbum:
		read, ok := p.catalog.albumRead(job.ResourceID)
		if !ok {
			if err := rr.SetCatalogState(ctx, job.ResourceKind, job.ResourceID, domain.CatalogNotInStorefront); err != nil {
				return err
			}
			return errors.New("album not in the storefront")
		}
		if err := rr.ApplyAlbumRead(ctx, read); err != nil {
			return err
		}
	case domain.ResourcePlaylist:
		read, ok := p.catalog.playlistRead(job.ResourceID)
		if !ok {
			if err := rr.SetCatalogState(ctx, job.ResourceKind, job.ResourceID, domain.CatalogNotInStorefront); err != nil {
				return err
			}
			return errors.New("playlist not in the storefront")
		}
		if err := rr.ApplyPlaylistRead(ctx, read); err != nil {
			return err
		}
	}
	plan, err := rr.PlanRun(ctx, job)
	if err != nil {
		return err
	}
	p.mu.Lock()
	hold, started := p.hold, p.started
	p.mu.Unlock()
	if hold != nil {
		started <- job.ID
		select {
		case <-hold:
		case <-ctx.Done():
			// A real run takes a moment to wind down after its cancel.
			time.Sleep(200 * time.Millisecond)
			return ctx.Err()
		}
	}
	items, err := reporter.ListItems(ctx, job.ID)
	if err != nil {
		return err
	}
	byID := map[string]domain.JobItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, slot := range plan.Slots {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := byID[slot.ItemID]
		if err := p.settle(ctx, job, plan, slot, &item, rr); err != nil {
			return err
		}
	}
	return nil
}

func (p *simProcessor) settle(ctx context.Context, job domain.Job, plan jobs.Plan, slot jobs.PlannedSlot, item *domain.JobItem, rr jobs.RecordReporter) error {
	finish := func(status domain.ItemStatus, ev domain.EventType) error {
		item.Status, item.UpdatedAt = status, time.Now().UTC()
		if err := rr.UpdateItem(ctx, item); err != nil {
			return err
		}
		return rr.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: ev, Payload: domain.MarshalEventPayload(*item, nil)})
	}
	if plan.Scope == jobs.ScopeLyrics {
		if err := rr.SlotLyrics(ctx, job.ID, slot.FileKey, domain.LyricsFetched); err != nil {
			return err
		}
		return finish(domain.ItemCompleted, domain.EventLyricsRefreshed)
	}
	p.mu.Lock()
	unavailable, failing := p.unavailable[slot.SongID], p.failing[slot.SongID]
	p.mu.Unlock()
	path := slot.FixedPath
	if path == "" {
		path = filepath.Join(p.dir, slot.OwnerID, slot.SongID+"-"+slot.Slot+".m4a")
	}
	if err := rr.ClaimSlot(ctx, job.ID, slot.FileKey, path); err != nil {
		return err
	}
	switch {
	case unavailable:
		if err := rr.SlotUnavailable(ctx, job.ID, slot.FileKey); err != nil {
			return err
		}
		return finish(domain.ItemUnavailable, domain.EventItemUnavailable)
	case failing:
		if _, err := rr.SlotFailed(ctx, job.ID, slot.FileKey, errors.New("simulated failure")); err != nil {
			return err
		}
		return finish(domain.ItemFailed, domain.EventItemFailed)
	}
	codec := "alac"
	if slot.Slot != domain.SlotMain {
		codec = slot.Slot
	}
	if _, err := os.Stat(path); err == nil && !slot.Replace && !p.cfg.Get().Download.OverwriteExisting {
		if err := rr.SlotDone(ctx, job.ID, slot.FileKey, db.FileOutcome{Codec: codec}); err != nil {
			return err
		}
		return finish(domain.ItemSkipped, domain.EventItemSkipped)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(job.ID), 0o644); err != nil {
		return err
	}
	if err := rr.SlotDone(ctx, job.ID, slot.FileKey, db.FileOutcome{Codec: codec, FileSize: int64(len(job.ID))}); err != nil {
		return err
	}
	return finish(domain.ItemCompleted, domain.EventItemCompleted)
}

// simCatalog is the made-up catalog: albums and playlists by id, each a
// list of song ids. It serves both the run's reads and the browse routes.
type simCatalog struct {
	mu        sync.Mutex
	albums    map[string][]string
	playlists map[string][]string
}

func (c *simCatalog) albumRead(id string) (db.AlbumRead, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	songs, ok := c.albums[id]
	if !ok {
		return db.AlbumRead{}, false
	}
	tracks := make([]domain.Track, 0, len(songs))
	for i, song := range songs {
		tracks = append(tracks, domain.Track{SongID: song, TrackNumber: i + 1, SongNumber: i + 1, Name: "Track " + song})
	}
	return db.AlbumRead{Album: domain.Album{ID: id, Name: "Album " + id, ReleaseTrackCount: len(tracks)}, Tracks: tracks, Storefront: "us"}, true
}

func (c *simCatalog) playlistRead(id string) (db.PlaylistRead, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	songs, ok := c.playlists[id]
	if !ok {
		return db.PlaylistRead{}, false
	}
	tracks := make([]domain.PlaylistTrack, 0, len(songs))
	for i, song := range songs {
		tracks = append(tracks, domain.PlaylistTrack{SongID: song, Position: i + 1, Name: "Track " + song})
	}
	return db.PlaylistRead{Playlist: domain.Playlist{ID: id, Name: "Playlist " + id}, Tracks: tracks, Storefront: "us"}, true
}

func (c *simCatalog) collection(id string, songs []string) applemusic.Collection {
	out := applemusic.Collection{ID: id, Type: applemusic.TypeAlbum, Name: "Album " + id, TrackCount: len(songs)}
	for i, song := range songs {
		out.Tracks = append(out.Tracks, applemusic.Song{ID: song, Name: "Track " + song, TrackNumber: i + 1, AlbumID: id})
	}
	return out
}

func (c *simCatalog) SongMetadata(_ context.Context, _, id string) (applemusic.Song, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for album, songs := range c.albums {
		for i, song := range songs {
			if song == id {
				return applemusic.Song{ID: id, Name: "Track " + id, TrackNumber: i + 1, AlbumID: album}, nil
			}
		}
	}
	return applemusic.Song{}, errors.New("no such song")
}

func (c *simCatalog) Album(_ context.Context, _, id string) (applemusic.Collection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	songs, ok := c.albums[id]
	if !ok {
		return applemusic.Collection{}, errors.New("no such album")
	}
	return c.collection(id, songs), nil
}

func (c *simCatalog) Playlist(context.Context, string, string, string) (applemusic.Collection, error) {
	return applemusic.Collection{}, errors.New("not served")
}

func (c *simCatalog) Station(context.Context, string, string) (applemusic.StationInfo, error) {
	return applemusic.StationInfo{}, errors.New("not served")
}

func (c *simCatalog) Artist(context.Context, string, string) (applemusic.Artist, error) {
	return applemusic.Artist{}, errors.New("not served")
}

// Search answers every term with every album and every song.
func (c *simCatalog) Search(context.Context, string, string, []applemusic.SearchType, int, int, string) (applemusic.SearchResults, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out applemusic.SearchResults
	ids := make([]string, 0, len(c.albums))
	for id := range c.albums {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		col := c.collection(id, c.albums[id])
		out.Albums = append(out.Albums, col)
		out.Songs = append(out.Songs, col.Tracks...)
	}
	return out, nil
}

func (c *simCatalog) ArtistAlbumsPage(context.Context, string, string, int, int) (applemusic.AlbumPage, error) {
	return applemusic.AlbumPage{}, errors.New("not served")
}

// rig is a real backend over a temp store with simProcessor, served on a
// test HTTP server, with one overview-feed subscriber that folds every
// message into what a client holds.
type rig struct {
	t       *testing.T
	base    string
	http    *http.Client
	store   *db.Store
	manager *jobs.Manager
	cfg     *config.Store
	proc    *simProcessor
	catalog *simCatalog
	feed    *feedModel
}

func newRig(t *testing.T, albums, playlists map[string][]string) *rig {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := config.Default()
	cfg.Download.DownloadsDir = t.TempDir()
	cfgStore := config.NewStore(cfg)
	catalog := &simCatalog{albums: albums, playlists: playlists}
	proc := &simProcessor{dir: cfg.Download.DownloadsDir, catalog: catalog, cfg: cfgStore}
	hub := events.NewHub()
	manager := jobs.NewManager(store, hub, proc, 2, slog.New(slog.NewTextHandler(io.Discard, nil)))
	manager.SetConfig(cfgStore.Get)
	server := newServer(t, Deps{Config: cfgStore, Store: store, Hub: hub, Manager: manager, Catalog: catalog})
	ts := httptest.NewServer(server.Routes())
	manager.Start(context.Background())
	t.Cleanup(func() {
		server.EndStreams()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
		ts.Close()
	})
	r := &rig{t: t, base: ts.URL + "/api/v1", http: ts.Client(), store: store, manager: manager, cfg: cfgStore, proc: proc, catalog: catalog}
	r.feed = r.subscribe()
	return r
}

// call sends a request and decodes a JSON answer into out (when non-nil),
// failing the test on a status other than want.
func (r *rig) call(method, path, body string, want int, out any) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		r.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		r.t.Fatalf("%s %s = %d %s, want %d", method, path, resp.StatusCode, raw, want)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			r.t.Fatalf("%s %s: decode %s: %v", method, path, raw, err)
		}
	}
}

func (r *rig) status(method, path, body string) int {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, bytes.NewBufferString(body))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		r.t.Fatalf("%s %s: %v", method, path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// add posts links to POST /downloads and returns the results.
func (r *rig) add(urls ...string) []domain.SubmitResult {
	r.t.Helper()
	raw, _ := json.Marshal(map[string]any{"urls": urls})
	var resp domain.BatchSubmitResponse
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, r.base+"/downloads", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	httpResp, err := r.http.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer httpResp.Body.Close()
	body, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode != http.StatusAccepted && httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusUnprocessableEntity {
		r.t.Fatalf("POST /downloads = %d %s", httpResp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		r.t.Fatalf("decode %s: %v", body, err)
	}
	return resp.Results
}

// settled waits up to 10 s until no run is queued or running, the picker
// queues nothing more, and the feed has delivered every event up to the
// cursor of that quiet GET /downloads: the point where a client's model
// and a fresh read must agree.
func (r *rig) settled() {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	quiet := 0
	var list struct {
		StatusCounts domain.JobStatusCounts `json:"status_counts"`
		EventCursor  int64                  `json:"event_cursor"`
	}
	for quiet < 5 {
		r.call(http.MethodGet, "/downloads?limit=1", "", http.StatusOK, &list)
		if list.StatusCounts.Queued == 0 && list.StatusCounts.Running == 0 {
			quiet++
		} else {
			quiet = 0
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("runs did not settle: %+v", list.StatusCounts)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The feed sends only milestones, so its cursor stops at the last one.
	milestones, err := r.store.ListMilestoneEventsAfterLimit(context.Background(), 0, 0)
	if err != nil {
		r.t.Fatal(err)
	}
	var want int64
	for _, ev := range milestones {
		if ev.ID <= list.EventCursor {
			want = ev.ID
		}
	}
	for {
		r.feed.mu.Lock()
		reached, err := r.feed.lastID >= want, r.feed.err
		r.feed.mu.Unlock()
		if err != nil {
			r.t.Fatal(err)
		}
		if reached {
			// Messages that share the cursor (a run's upsert and its
			// record's resource_updated) are written back to back.
			time.Sleep(50 * time.Millisecond)
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("the feed stopped short of milestone %d", want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// feedModel is what a client builds from the overview feed alone: the runs
// by id and the records by kind and id.
type feedModel struct {
	mu      sync.Mutex
	runs    map[string]domain.Job
	records map[string]json.RawMessage
	gone    map[string]bool
	counts  domain.JobStatusCounts
	lastID  int64
	err     error
}

func recordKeyOf(kind domain.ResourceKind, id string) string { return string(kind) + "/" + id }

func (r *rig) subscribe() *feedModel {
	r.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+"/downloads/events", nil)
	if err != nil {
		r.t.Fatal(err)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		r.t.Fatalf("feed = %d", resp.StatusCode)
	}
	m := &feedModel{runs: map[string]domain.Job{}, records: map[string]json.RawMessage{}, gone: map[string]bool{}}
	go func() {
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)
		var name string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if after, ok := strings.CutPrefix(line, "event: "); ok {
				name = strings.TrimSpace(after)
				continue
			}
			after, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				continue
			}
			var msg struct {
				domain.DownloadFeedMessage
				Resource json.RawMessage `json:"resource"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(after)), &msg); err != nil {
				m.mu.Lock()
				m.err = fmt.Errorf("bad feed message %q: %w", after, err)
				m.mu.Unlock()
				return
			}
			m.apply(name, msg.DownloadFeedMessage, msg.Resource)
		}
	}()
	return m
}

func (m *feedModel) apply(name string, msg domain.DownloadFeedMessage, resource json.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts = msg.StatusCounts
	if msg.EventID > m.lastID {
		m.lastID = msg.EventID
	}
	switch name {
	case "download_upserted":
		if msg.Job != nil {
			m.runs[msg.Job.ID] = *msg.Job
		}
	case "download_deleted":
		delete(m.runs, msg.JobID)
	case "resource_updated":
		key := recordKeyOf(msg.ResourceKind, msg.ResourceID)
		m.records[key] = resource
		delete(m.gone, key)
	case "resource_deleted":
		key := recordKeyOf(msg.ResourceKind, msg.ResourceID)
		delete(m.records, key)
		m.gone[key] = true
	}
}
