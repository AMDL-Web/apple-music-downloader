package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"amdl/internal/client"
)

// Config is the backend's runtime configuration (GET /api/v1/config), which
// Settings changes one key at a time.
type Config struct {
	// Values are its sections by name, as JSON decodes them: numbers are
	// float64 and lists []any. Nil until the first read.
	Values map[string]any
	// Persisted means the backend writes changes to its config.yaml.
	Persisted bool
	// Err says why the last read failed.
	Err string
	// Saving names the keys whose change is on its way; Failed holds the
	// backend's reason for each key whose last change it refused.
	Saving map[string]bool
	Failed map[string]string
	// Reloading is set while the backend reads its config.yaml again, and
	// ReloadErr says why that file could not be used.
	Reloading bool
	ReloadErr string

	reading bool
	// tried is when the last read started: after a failed one the next
	// waits ServerEvery, as the server's state does.
	tried time.Time
	// pending holds the values on their way, which Value shows until the
	// backend answers.
	pending map[string]any
}

// configAnswer is the ConfigResponse schema.
type configAnswer struct {
	Config      map[string]any `json:"config"`
	Persisted   bool           `json:"persisted"`
	ReloadError string         `json:"reload_error"`
}

// Value is key's value, such as "download.cover_size", or nil: the one on
// its way while the backend has not answered.
func (c *Config) Value(key string) any {
	if v, ok := c.pending[key]; ok {
		return v
	}
	section, leaf, ok := strings.Cut(key, ".")
	if !ok {
		return nil
	}
	m, _ := c.Values[section].(map[string]any)
	return m[leaf]
}

// WatchConfig reads the configuration when it has not been read on this
// connection, or the last read failed.
func (s *Store) WatchConfig() {
	cf := &s.Config
	if s.client == nil || cf.reading || (cf.Values != nil && cf.Err == "") ||
		(cf.Err != "" && s.now().Sub(cf.tried) < ServerEvery) {
		return
	}
	cf.reading, cf.tried = true, s.now()
	c, ctx := s.client, s.ctx
	go func() {
		var ans configAnswer
		raw, err := c.Call(ctx, http.MethodGet, "/api/v1/config", nil, nil)
		if err == nil {
			err = json.Unmarshal(raw, &ans)
		}
		s.postIf(ctx, func() {
			cf.reading = false
			if err != nil {
				cf.Err = Reason(err)
				return
			}
			cf.Values, cf.Persisted, cf.Err = ans.Config, ans.Persisted, ""
		})
	}()
}

// SetConfig sends key's new value, alone: PUT /api/v1/config merges it into
// the rest. The backend answers with the whole configuration, which replaces
// Values; when it refuses, Values keep the old value and Failed says why.
func (s *Store) SetConfig(key string, v any) {
	cf := &s.Config
	section, leaf, ok := strings.Cut(key, ".")
	if s.client == nil || !ok {
		return
	}
	if cf.Saving == nil {
		cf.Saving, cf.Failed, cf.pending = map[string]bool{}, map[string]string{}, map[string]any{}
	}
	cf.Saving[key], cf.pending[key] = true, v
	delete(cf.Failed, key)
	c, ctx := s.client, s.ctx
	go func() {
		var ans configAnswer
		raw, err := c.Call(ctx, http.MethodPut, "/api/v1/config", nil, map[string]any{section: map[string]any{leaf: v}})
		if err == nil {
			err = json.Unmarshal(raw, &ans)
		}
		s.postIf(ctx, func() {
			delete(cf.Saving, key)
			delete(cf.pending, key)
			if err != nil {
				cf.Failed[key] = Reason(err)
				return
			}
			cf.Values, cf.Persisted = ans.Config, ans.Persisted
			// Folders and switches show in the server's state, such as
			// the free space of a new downloads folder.
			s.Server.Read = time.Time{}
		})
	}()
}

// ReloadConfig makes the backend read its config.yaml again.
func (s *Store) ReloadConfig() {
	cf := &s.Config
	if s.client == nil || cf.Reloading {
		return
	}
	cf.Reloading, cf.ReloadErr = true, ""
	c, ctx := s.client, s.ctx
	go func() {
		var ans configAnswer
		raw, err := c.Call(ctx, http.MethodPost, "/api/v1/config/reload", nil, nil)
		if err == nil {
			err = json.Unmarshal(raw, &ans)
		}
		s.postIf(ctx, func() {
			cf.Reloading = false
			switch {
			case err != nil:
				cf.ReloadErr = Reason(err)
			case ans.ReloadError != "":
				cf.ReloadErr = ans.ReloadError
			default:
				cf.Values, cf.Persisted = ans.Config, ans.Persisted
				s.Server.Read = time.Time{}
			}
		})
	}()
}

// ResetLibrarySync clears the library sync's anchor: its next check records
// the library as it is then and queues nothing.
func (s *Store) ResetLibrarySync() {
	if s.client == nil {
		return
	}
	c, ctx := s.client, s.ctx
	go func() {
		_, err := c.Call(ctx, http.MethodPost, "/api/v1/library-sync/reset", nil, nil)
		s.postIf(ctx, func() {
			if err != nil {
				s.setNotice("Library sync did not start over: "+Reason(err), true)
				return
			}
			s.setNotice("Library sync starts over from now", false)
			s.Server.Read = time.Time{}
		})
	}()
}

// Reason is an error in the backend's words: its message, or the error.
func Reason(err error) string {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.Message != "" {
		return apiErr.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the backend did not answer in time"
	}
	return err.Error()
}
