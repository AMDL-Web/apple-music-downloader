package media

import (
	"context"
	"sync"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/wrapper"
)

type fakeDownloaderWrapper struct {
	status    wrapper.Status
	lyrics    string
	lyricsErr error
}

func (f fakeDownloaderWrapper) Status(context.Context) (wrapper.Status, error) {
	return f.status, nil
}

func (f fakeDownloaderWrapper) M3U8(context.Context, string) (string, error) {
	return "", nil
}

func (f fakeDownloaderWrapper) Lyrics(context.Context, string, wrapper.LyricsRequestOptions) (string, error) {
	return f.lyrics, f.lyricsErr
}

func (f fakeDownloaderWrapper) WebPlayback(context.Context, string) (string, error) {
	return "", nil
}

func (f fakeDownloaderWrapper) NewDecryptSession(context.Context, string) (wrapper.DecryptSession, error) {
	return identityDecryptSession{}, nil
}

// identityDecryptSession returns each sample unchanged, standing in for a
// wrapper that "decrypts" test fixtures that were never encrypted.
type identityDecryptSession struct{}

func (identityDecryptSession) DecryptFragment(_ string, samples [][]byte) ([][]byte, error) {
	return samples, nil
}

func (identityDecryptSession) Close() error { return nil }

func (f fakeDownloaderWrapper) License(context.Context, string, string, string) (string, error) {
	return "", nil
}

type fakeDownloaderCatalog struct {
	song              applemusic.Song
	songErr           error
	album             applemusic.Collection
	playlist          applemusic.Collection
	artistAlbums      applemusic.ArtistAlbums
	station           applemusic.Collection
	stationErr        error
	stationToken      *string
	webTokenHLS       string
	webTokenErr       error
	webTokenCallCount *int
	motionArtwork     applemusic.MotionArtwork
	motionArtworkErr  error
}

func (f fakeDownloaderCatalog) Song(context.Context, string, string) (applemusic.Song, error) {
	return f.song, f.songErr
}

func (f fakeDownloaderCatalog) SongMetadata(context.Context, string, string) (applemusic.Song, error) {
	return f.song, f.songErr
}

func (f fakeDownloaderCatalog) Album(_ context.Context, _, id string) (applemusic.Collection, error) {
	if f.album.ID == id {
		return f.album, nil
	}
	for _, album := range f.artistAlbums.Albums {
		if album.ID == id {
			return album, nil
		}
	}
	return applemusic.Collection{}, nil
}

func (f fakeDownloaderCatalog) Playlist(context.Context, string, string, string) (applemusic.Collection, error) {
	return f.playlist, nil
}

func (f fakeDownloaderCatalog) StationTracks(_ context.Context, _, _, token string) (applemusic.Collection, error) {
	if f.stationToken != nil {
		*f.stationToken = token
	}
	return f.station, f.stationErr
}

func (f fakeDownloaderCatalog) ArtistAlbums(context.Context, string, string) (applemusic.ArtistAlbums, error) {
	return f.artistAlbums, nil
}

func (f fakeDownloaderCatalog) Artist(context.Context, string, string) (applemusic.Artist, error) {
	return applemusic.Artist{}, nil
}

func (f fakeDownloaderCatalog) FetchCover(context.Context, []string, string, string) ([]byte, error) {
	return nil, nil
}

func (f fakeDownloaderCatalog) MotionArtworkViaWebToken(context.Context, string, string) (applemusic.MotionArtwork, error) {
	return f.motionArtwork, f.motionArtworkErr
}

func (f fakeDownloaderCatalog) EnhancedHLSViaWebToken(context.Context, string, string) (string, error) {
	if f.webTokenCallCount != nil {
		*f.webTokenCallCount++
	}
	return f.webTokenHLS, f.webTokenErr
}

type recordingReporter struct {
	mu            sync.Mutex
	events        []domain.Event
	items         []domain.JobItem
	existing      []domain.JobItem
	added         []domain.JobItem
	removed       []string
	motionArtwork []motionArtworkWrite
	// missingJobs makes SetJobMotionArtwork report that no row matched, which
	// is what the store does once the job has been deleted.
	missingJobs bool
}

type motionArtworkWrite struct {
	JobID string
	Art   domain.MotionArtwork
}

type metadataCountingCatalog struct {
	fakeDownloaderCatalog

	mu            sync.Mutex
	songs         map[string]applemusic.Song
	metadataSongs map[string]applemusic.Song
	albums        map[string]applemusic.Collection
	songCalls     map[string]int
	metadataCalls map[string]int
	albumCalls    map[string]int
	albumWait     <-chan struct{}
	albumStarted  chan<- struct{}
}

func (c *metadataCountingCatalog) Song(ctx context.Context, _, id string) (applemusic.Song, error) {
	c.mu.Lock()
	c.songCalls[id]++
	song := c.songs[id]
	c.mu.Unlock()
	return song, ctx.Err()
}

func (c *metadataCountingCatalog) SongMetadata(ctx context.Context, _, id string) (applemusic.Song, error) {
	c.mu.Lock()
	c.metadataCalls[id]++
	song := c.metadataSongs[id]
	c.mu.Unlock()
	return song, ctx.Err()
}

func (c *metadataCountingCatalog) Album(ctx context.Context, _, id string) (applemusic.Collection, error) {
	c.mu.Lock()
	c.albumCalls[id]++
	album := c.albums[id]
	c.mu.Unlock()
	if c.albumStarted != nil {
		select {
		case c.albumStarted <- struct{}{}:
		default:
		}
	}
	if c.albumWait != nil {
		select {
		case <-ctx.Done():
			return applemusic.Collection{}, ctx.Err()
		case <-c.albumWait:
		}
	}
	return album, nil
}

func (c *metadataCountingCatalog) counts() (map[string]int, map[string]int, map[string]int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	songs := make(map[string]int, len(c.songCalls))
	for id, count := range c.songCalls {
		songs[id] = count
	}
	metadata := make(map[string]int, len(c.metadataCalls))
	for id, count := range c.metadataCalls {
		metadata[id] = count
	}
	albums := make(map[string]int, len(c.albumCalls))
	for id, count := range c.albumCalls {
		albums[id] = count
	}
	return songs, metadata, albums
}

func (*recordingReporter) SetJob(_ context.Context, job *domain.Job) error {
	job.UpdatedAt = time.Now().UTC()
	return nil
}

// missingJobs makes SetJobMotionArtwork report "no such row", standing in for a
// job deleted while the detached lookup was in flight.
func (r *recordingReporter) SetJobMotionArtwork(_ context.Context, jobID string, art domain.MotionArtwork) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.motionArtwork = append(r.motionArtwork, motionArtworkWrite{JobID: jobID, Art: art})
	return !r.missingJobs, nil
}

func (r *recordingReporter) AddItem(_ context.Context, item *domain.JobItem) error {
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	r.mu.Lock()
	defer r.mu.Unlock()
	r.added = append(r.added, *item)
	return nil
}

func (r *recordingReporter) RemoveItem(_ context.Context, itemID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removed = append(r.removed, itemID)
	return nil
}

func (r *recordingReporter) ListItems(context.Context, string) ([]domain.JobItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.JobItem(nil), r.existing...), nil
}

func (r *recordingReporter) UpdateItem(_ context.Context, item *domain.JobItem) error {
	item.UpdatedAt = time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, *item)
	return nil
}

func (r *recordingReporter) Event(_ context.Context, ev domain.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return nil
}
