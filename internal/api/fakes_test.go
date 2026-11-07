package api

// The fake services the handler tests serve through: each stands in for one
// Deps field (motionCatalog adds an optional catalog interface).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"amdl/internal/applelibrary"
	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/librarysync"
	"amdl/internal/media"
	"amdl/internal/wrapper"
)

type fakeWrapperService struct {
	statusCalls  int
	statusResult wrapper.Status
	statusErr    error
	startResult  wrapper.LoginResult
	startErr     error
	verifyResult wrapper.LoginResult
	verifyErr    error
	logoutErr    error
	username     string
	password     string
	loginID      string
	twoStepCode  string
	loggedOutIDs []string
}

func (f *fakeWrapperService) LogoutAccount(_ context.Context, id string) error {
	f.loggedOutIDs = append(f.loggedOutIDs, id)
	return f.logoutErr
}

// Status returns statusResult; like wrapper.Client it never returns nil
// Regions.
func (f *fakeWrapperService) Status(context.Context) (wrapper.Status, error) {
	f.statusCalls++
	st := f.statusResult
	if st.Regions == nil {
		st.Regions = []string{}
	}
	return st, f.statusErr
}

func (f *fakeWrapperService) StartLogin(_ context.Context, username, password string) (wrapper.LoginResult, error) {
	f.username, f.password = username, password
	return f.startResult, f.startErr
}

func (f *fakeWrapperService) SubmitTwoStepCode(_ context.Context, loginID, code string) (wrapper.LoginResult, error) {
	f.loginID, f.twoStepCode = loginID, code
	return f.verifyResult, f.verifyErr
}

func (f *fakeWrapperService) Logout(_ context.Context, username string) error {
	f.username = username
	return f.logoutErr
}

type fakeQualityService struct {
	result media.QualityResult
	err    error
	req    media.QualityRequest
}

func (f *fakeQualityService) QueryQuality(_ context.Context, req media.QualityRequest) (media.QualityResult, error) {
	f.req = req
	return f.result, f.err
}

// stubProcessor parses "type|storefront|id" test URLs into a ValidationResult
// so createDownload can be exercised against a real jobs.Manager without
// hitting Apple Music.
type stubProcessor struct{}

func (stubProcessor) ValidateRequest(_ context.Context, url string) (jobs.ValidationResult, error) {
	parts := strings.SplitN(url, "|", 3)
	if len(parts) != 3 {
		return jobs.ValidationResult{}, &jobs.RequestError{Code: "invalid_url", Message: "malformed test url"}
	}
	return jobs.ValidationResult{Type: parts[0], Storefront: parts[1], ID: parts[2]}, nil
}

func (stubProcessor) ProcessJob(context.Context, domain.Job, jobs.Reporter) error { return nil }

// fakeCatalog records the reads POST /lookup makes and answers from its
// fields; a read whose result is unset fails the test.
type fakeCatalog struct {
	t        *testing.T
	song     *applemusic.Song
	album    *applemusic.Collection
	playlist *applemusic.Collection
	station  *applemusic.StationInfo
	artist   *applemusic.Artist
	search   *applemusic.SearchResults
	albums   *applemusic.AlbumPage
	err      error
	// mu guards calls: GET /artists/{id}/albums reads the artist and the
	// albums page at once.
	mu    sync.Mutex
	calls []string
	token string
	// searchToken is the media-user-token the last search carried.
	searchToken string
}

func (f *fakeCatalog) record(method, storefront, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("%s %s/%s", method, storefront, id))
	return f.err
}

func (f *fakeCatalog) SongMetadata(_ context.Context, storefront, id string) (applemusic.Song, error) {
	if err := f.record("song", storefront, id); err != nil || f.song == nil {
		return applemusic.Song{}, f.missing(err, "song")
	}
	return *f.song, nil
}

func (f *fakeCatalog) Album(_ context.Context, storefront, id string) (applemusic.Collection, error) {
	if err := f.record("album", storefront, id); err != nil || f.album == nil {
		return applemusic.Collection{}, f.missing(err, "album")
	}
	return *f.album, nil
}

func (f *fakeCatalog) Playlist(_ context.Context, storefront, id, mediaUserToken string) (applemusic.Collection, error) {
	f.token = mediaUserToken
	if err := f.record("playlist", storefront, id); err != nil || f.playlist == nil {
		return applemusic.Collection{}, f.missing(err, "playlist")
	}
	return *f.playlist, nil
}

func (f *fakeCatalog) Station(_ context.Context, storefront, id string) (applemusic.StationInfo, error) {
	if err := f.record("station", storefront, id); err != nil || f.station == nil {
		return applemusic.StationInfo{}, f.missing(err, "station")
	}
	return *f.station, nil
}

func (f *fakeCatalog) Artist(_ context.Context, storefront, id string) (applemusic.Artist, error) {
	if err := f.record("artist", storefront, id); err != nil || f.artist == nil {
		return applemusic.Artist{}, f.missing(err, "artist")
	}
	return *f.artist, nil
}

func (f *fakeCatalog) Search(_ context.Context, storefront, term string, types []applemusic.SearchType, limit, offset int, mediaUserToken string) (applemusic.SearchResults, error) {
	f.mu.Lock()
	f.searchToken = mediaUserToken
	f.mu.Unlock()
	if err := f.record("search", storefront, fmt.Sprintf("%s %v %d+%d", term, types, limit, offset)); err != nil || f.search == nil {
		return applemusic.SearchResults{}, f.missing(err, "search")
	}
	return *f.search, nil
}

func (f *fakeCatalog) ArtistAlbumsPage(_ context.Context, storefront, id string, limit, offset int) (applemusic.AlbumPage, error) {
	if err := f.record("artist albums", storefront, fmt.Sprintf("%s %d+%d", id, limit, offset)); err != nil || f.albums == nil {
		return applemusic.AlbumPage{}, f.missing(err, "artist albums")
	}
	return *f.albums, nil
}

func (f *fakeCatalog) missing(err error, what string) error {
	if err != nil {
		return err
	}
	f.t.Errorf("unexpected %s read", what)
	return errors.New("unexpected read")
}

// notFoundError stands in for Apple's 404, which internal/applemusic reports
// through a NotFound marker.
type notFoundError struct{}

func (notFoundError) Error() string {
	return "Apple Music has no album 1463409338 in storefront us (404)"
}

func (notFoundError) NotFound() bool { return true }

// motionCatalog is fakeCatalog with the optional animated-cover read.
type motionCatalog struct {
	*fakeCatalog
	art   applemusic.MotionArtwork
	err   error
	asked int
}

func (m *motionCatalog) MotionArtworkViaWebToken(_ context.Context, storefront, id string) (applemusic.MotionArtwork, error) {
	m.asked++
	return m.art, m.err
}

type fakeLibrarySync struct {
	status     librarysync.Status
	cleared    int64
	resetCalls []string
	forgotten  []string
}

func (f *fakeLibrarySync) Status(context.Context) (librarysync.Status, error) { return f.status, nil }

func (f *fakeLibrarySync) ResetAnchor(_ context.Context, account string) (int64, error) {
	f.resetCalls = append(f.resetCalls, account)
	for i := range f.status.Accounts {
		if f.status.Accounts[i].AccountID == account {
			f.status.Accounts[i].AnchorSize = 0
		}
	}
	return f.cleared, nil
}

func (f *fakeLibrarySync) Forget(account string) { f.forgotten = append(f.forgotten, account) }

// fakeAppleLibrary answers membership from a fixed map; the other methods are
// not used by the routes that take it.
type fakeAppleLibrary struct {
	applelibrary.Set
	in map[string]map[string]bool
}

func (f *fakeAppleLibrary) Membership(_ context.Context, _, kind string, ids []string) map[string]bool {
	if f.in[kind] == nil {
		return nil
	}
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = f.in[kind][id]
	}
	return out
}

// pagedApple serves a fixed Apple library the way amp-api pages it.
type pagedApple struct {
	items    map[applemusic.LibraryKind][]applemusic.LibraryItem
	tracks   map[string][]applemusic.LibraryItem
	requests int
	err      error
}

func (a *pagedApple) page(all []applemusic.LibraryItem, offset, limit int) applemusic.LibraryPage {
	end := min(offset+limit, len(all))
	return applemusic.LibraryPage{Items: all[min(offset, end):end], Total: len(all), HasMore: end < len(all)}
}

func (a *pagedApple) LibraryItems(_ context.Context, _ string, kind applemusic.LibraryKind, _ bool, offset, limit int) (applemusic.LibraryPage, error) {
	a.requests++
	if a.err != nil {
		return applemusic.LibraryPage{}, a.err
	}
	return a.page(a.items[kind], offset, limit), nil
}

func (a *pagedApple) LibraryPlaylistTracks(_ context.Context, _ string, id string, offset, limit int) (applemusic.LibraryPage, error) {
	a.requests++
	return a.page(a.tracks[id], offset, limit), nil
}

func (a *pagedApple) AccountStorefront(context.Context, string) (string, error) {
	a.requests++
	return "cn", a.err
}

type refusedToken struct{}

func (refusedToken) Error() string           { return "403 Forbidden" }
func (refusedToken) UserTokenRejected() bool { return true }
