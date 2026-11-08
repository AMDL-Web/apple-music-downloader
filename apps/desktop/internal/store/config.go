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
	// backend's reason for each key whose last change it refused, and
	// Refused that change's value, so a field shows the value its reason is
	// about rather than the saved one. Both stay until the key changes again
	// or ForgetRefusal drops them.
	Saving  map[string]bool
	Failed  map[string]string
	Refused map[string]any
	// Reloading is set while the backend reads its config.yaml again, from
	// ReloadSince; ReloadErr says why that file could not be used, and stays
	// until the next reload answers.
	Reloading   bool
	ReloadSince time.Time
	ReloadErr   string
	// PendingRestart names the keys saved with a value the backend applies
	// when it starts again (the response's pending_restart).
	PendingRestart []string
	// Restarting is set from a restart request until the configuration is
	// read again. Values stay on screen meanwhile, so nothing moves; the
	// reads that fail while the backend is down are retried every
	// restartPoll, for restartWithin, without an Err.
	Restarting bool

	reading bool
	// stale asks WatchConfig for a read though Values are there, from
	// restartSince.
	stale        bool
	restartSince time.Time
	// tried is when the last read started: the next waits ServerEvery,
	// whether it failed or not, as the server's state does, so a change made
	// elsewhere (the console, the CLI) shows within it.
	tried time.Time
	// edits counts the answers that replaced Values with a newer
	// configuration than a read sent before them can hold.
	edits int
	// pending holds the values on their way, which Value shows until the
	// backend answers, and sent when each left.
	pending map[string]any
	sent    map[string]time.Time
}

// SavingFor is how long key's change has been on its way, and whether one
// is.
func (cf *Config) SavingFor(key string, now time.Time) (time.Duration, bool) {
	if !cf.Saving[key] {
		return 0, false
	}
	return now.Sub(cf.sent[key]), true
}

// configAnswer is the ConfigResponse schema.
type configAnswer struct {
	Config         map[string]any `json:"config"`
	Persisted      bool           `json:"persisted"`
	ReloadError    string         `json:"reload_error"`
	PendingRestart []string       `json:"pending_restart"`
}

// ForgetRefusal drops the backend's refusal of key's last change, its
// reason and its value, as editing or reverting the field does.
func (cf *Config) ForgetRefusal(key string) {
	delete(cf.Failed, key)
	delete(cf.Refused, key)
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

// How often the configuration is read while a restart is under way, and for
// how long, as the console waits for the server to come back.
const (
	restartPoll   = time.Second
	restartWithin = 90 * time.Second
)

// WatchConfig reads the configuration when it has not been read on this
// connection, the last read is older than ServerEvery, or a restart made it
// stale. Views that show it call it every frame, so a change made by
// another client shows while one is on screen.
func (s *Store) WatchConfig() {
	cf := &s.Config
	if s.client == nil || cf.reading ||
		(cf.Values != nil && !cf.stale && s.now().Sub(cf.tried) < ServerEvery) ||
		(cf.Values == nil && cf.Err != "" && s.now().Sub(cf.tried) < ServerEvery) ||
		(cf.stale && s.now().Sub(cf.tried) < restartPoll) {
		return
	}
	cf.reading, cf.tried = true, s.now()
	c, ctx, edits := s.client, s.ctx, cf.edits
	go func() {
		var ans configAnswer
		raw, err := c.Call(ctx, http.MethodGet, "/api/v1/config", nil, nil)
		if err == nil {
			err = json.Unmarshal(raw, &ans)
		}
		s.postIf(ctx, func() {
			cf.reading = false
			if err != nil {
				if cf.stale && s.now().Sub(cf.restartSince) < restartWithin {
					return
				}
				cf.Err, cf.stale, cf.Restarting = Reason(err), false, false
				return
			}
			cf.Err, cf.Restarting, cf.stale = "", false, false
			// A change answered while this read was out is newer than it.
			if edits != cf.edits {
				return
			}
			cf.Values, cf.Persisted, cf.PendingRestart = ans.Config, ans.Persisted, ans.PendingRestart
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
		cf.Saving, cf.Failed, cf.Refused, cf.pending, cf.sent = map[string]bool{}, map[string]string{}, map[string]any{}, map[string]any{}, map[string]time.Time{}
	}
	cf.Saving[key], cf.pending[key], cf.sent[key] = true, v, s.now()
	cf.ForgetRefusal(key)
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
			delete(cf.sent, key)
			if err != nil {
				cf.Failed[key], cf.Refused[key] = Reason(err), v
				return
			}
			cf.Values, cf.Persisted, cf.PendingRestart = ans.Config, ans.Persisted, ans.PendingRestart
			cf.edits++
			// Folders and switches show in the server's state, such as
			// the free space of a new downloads folder.
			s.refreshServer()
			if s.AutoRestart && len(ans.PendingRestart) > 0 {
				s.RestartServer()
			}
		})
	}()
}

// ReloadConfig makes the backend read its config.yaml again.
func (s *Store) ReloadConfig() {
	cf := &s.Config
	if s.client == nil || cf.Reloading {
		return
	}
	cf.Reloading, cf.ReloadSince = true, s.now()
	c, ctx := s.client, s.ctx
	go func() {
		var ans configAnswer
		raw, err := c.Call(ctx, http.MethodPost, "/api/v1/config/reload", nil, nil)
		if err == nil {
			err = json.Unmarshal(raw, &ans)
		}
		s.postIf(ctx, func() {
			cf.Reloading, cf.ReloadErr = false, ""
			switch {
			case err != nil:
				cf.ReloadErr = Reason(err)
			case ans.ReloadError != "":
				cf.ReloadErr = ans.ReloadError
			default:
				cf.Values, cf.Persisted, cf.PendingRestart = ans.Config, ans.Persisted, ans.PendingRestart
				cf.edits++
				s.refreshServer()
			}
		})
	}()
}

// RestartServer asks the backend to stop and start again in place (POST
// /api/v1/system/restart), which applies PendingRestart. The configuration
// is read again once the backend answers.
func (s *Store) RestartServer() {
	cf := &s.Config
	if s.client == nil || cf.Restarting {
		return
	}
	cf.Restarting = true
	c, ctx := s.client, s.ctx
	go func() {
		_, err := c.Call(ctx, http.MethodPost, "/api/v1/system/restart", nil, nil)
		s.postIf(ctx, func() {
			if err != nil {
				cf.Restarting = false
				s.setNotice("The server did not restart: "+Reason(err), true)
				return
			}
			// WatchConfig reads again; until the backend answers, its reads
			// fail and retry, and the last Values stay on screen.
			cf.stale, cf.restartSince = true, s.now()
			s.refreshServer()
		})
	}()
}

// ResetLibrarySync clears the library sync's anchor of account ("" for the
// default account): its next check records the library as it is then and
// queues nothing.
func (s *Store) ResetLibrarySync(account string) {
	if s.client == nil {
		return
	}
	c, ctx := s.client, s.ctx
	go func() {
		_, err := c.ResetLibrarySync(ctx, account)
		s.postIf(ctx, func() {
			if err != nil {
				s.setNotice("Library sync did not start over: "+Reason(err), true)
				return
			}
			s.setNotice("Library sync starts over from now", false)
			s.refreshServer()
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
