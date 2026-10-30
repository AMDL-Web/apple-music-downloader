package store

import (
	"context"
	"sync"
	"time"

	"amdl/internal/client"
)

// ServerEvery is how often the server's state is read while a view shows
// it.
const ServerEvery = 10 * time.Second

// StatsDays is the window of the server's download statistics.
const StatsDays = 14

// activeSamples is how many readings of the busy worker count the system
// screen charts.
const activeSamples = 12

// Server is what the backend reports about itself. A part the backend did
// not answer for stays nil, with its error beside it when it failed.
type Server struct {
	Health    *client.Health
	System    *client.SystemInfo
	Wrapper   *client.WrapperStatus
	Stats     *client.Stats
	Sync      *client.LibrarySync
	Hooks     *client.Hooks
	HealthErr string
	// WrapperErr says why the wrapper's status could not be read.
	WrapperErr string
	// Read is when the last read finished.
	Read time.Time
	// Active are the last readings of System.Jobs.Active, oldest first: the
	// API keeps no history of it.
	Active []int

	reading bool
}

// WatchServer reads the server's state when the last read is older than
// ServerEvery. Views that show it call it every frame and redraw after
// ServerEvery, so it reads only while one of them is on screen.
func (s *Store) WatchServer() {
	sv := &s.Server
	if s.client == nil || sv.reading || s.now().Sub(sv.Read) < ServerEvery {
		return
	}
	sv.reading = true
	c, ctx := s.client, s.ctx
	go func() {
		var next Server
		var wg sync.WaitGroup
		read := func(fn func(context.Context)) {
			wg.Add(1)
			go func() { defer wg.Done(); fn(ctx) }()
		}
		read(func(ctx context.Context) {
			if h, err := c.Health(ctx); err == nil {
				next.Health = &h
			} else {
				next.HealthErr = err.Error()
			}
		})
		read(func(ctx context.Context) {
			if v, err := c.System(ctx); err == nil {
				next.System = &v
			}
		})
		read(func(ctx context.Context) {
			if v, err := c.WrapperStatus(ctx); err == nil {
				next.Wrapper = &v
			} else {
				next.WrapperErr = err.Error()
			}
		})
		// Backends older than these endpoints answer 404; their cards then
		// say nothing rather than fail the screen.
		read(func(ctx context.Context) {
			if v, err := c.Stats(ctx, StatsDays); err == nil {
				next.Stats = &v
			}
		})
		read(func(ctx context.Context) {
			if v, err := c.LibrarySync(ctx); err == nil {
				next.Sync = &v
			}
		})
		read(func(ctx context.Context) {
			if v, err := c.Hooks(ctx); err == nil {
				next.Hooks = &v
			}
		})
		wg.Wait()
		s.postIf(ctx, func() {
			next.Read, next.Active = s.now(), sv.Active
			if next.System != nil {
				next.Active = append(next.Active, next.System.Jobs.Active)
				if len(next.Active) > activeSamples {
					next.Active = next.Active[len(next.Active)-activeSamples:]
				}
			}
			*sv = next
		})
	}()
}

// DownloadsStorage is the free space of the backend's downloads folder,
// or nil.
func (sv *Server) DownloadsStorage() *client.Storage { return sv.storage("downloads_dir") }

// TempStorage is the free space of the backend's temporary folder, or nil.
func (sv *Server) TempStorage() *client.Storage { return sv.storage("temp_dir") }

func (sv *Server) storage(name string) *client.Storage {
	if sv.System == nil {
		return nil
	}
	for i := range sv.System.Storage {
		if sv.System.Storage[i].Name == name {
			return &sv.System.Storage[i]
		}
	}
	return nil
}
