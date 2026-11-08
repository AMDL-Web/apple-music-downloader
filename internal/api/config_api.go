package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"amdl/internal/config"
)

// getConfig returns the keys clients can change: the runtime keys as they
// run, and the restart keys as saved in config.yaml, which apply on the next
// start; pending_restart names the ones that differ from the running values.
// The file-only keys (server.listen, database.path, tools.ffmpeg) are
// omitted. A hand edit of a runtime key shows after POST /config/reload.
func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	s.writeConfigView(w, nil)
}

// reloadConfig re-reads the config file and applies its runtime-mutable
// fields, then answers like GET /config. A file that cannot be read or does
// not validate leaves the previous snapshot in place; the response is still
// 200 and reload_error says why, because the request itself succeeded.
func (s *Server) reloadConfig(w http.ResponseWriter, r *http.Request) {
	reloadErr := s.cfg.Reload()
	s.applyLoggingConfig()
	s.writeConfigView(w, reloadErr)
}

func (s *Server) writeConfigView(w http.ResponseWriter, reloadErr error) {
	s.writeConfigResponse(w, s.savedConfig(), reloadErr)
}

// savedConfig is the live snapshot with the restart and file keys as saved
// in config.yaml (config.Store.Saved).
func (s *Server) savedConfig() config.Config {
	if s.cfg == nil {
		return config.Config{}
	}
	return s.cfg.Saved()
}

// pendingRestart names the keys whose saved value waits for a restart; never
// nil, so it encodes as an array.
func (s *Server) pendingRestart(saved config.Config) []string {
	if s.cfg == nil {
		return []string{}
	}
	if keys := s.cfg.PendingRestart(saved); keys != nil {
		return keys
	}
	return []string{}
}

// writeConfigResponse answers GET, PUT and POST /config/reload alike: the
// ConfigResponse schema for cfg, the configuration as saved, with
// wrapper.token hidden.
func (s *Server) writeConfigResponse(w http.ResponseWriter, cfg config.Config, reloadErr error) {
	view, err := config.MutableView(cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternalError, "render config: "+err.Error())
		return
	}
	if wrapperView, ok := view["wrapper"].(map[string]any); ok {
		if _, ok := wrapperView["token"]; ok {
			wrapperView["token"] = hideSecret(cfg.Wrapper.Token)
		}
	}
	resp := map[string]any{
		"config":          view,
		"persisted":       s.cfg != nil && s.cfg.Persistent(),
		"pending_restart": s.pendingRestart(cfg),
	}
	if reloadErr != nil {
		resp["reload_error"] = reloadErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// updateConfig merges the request body onto the saved config: omitted fields
// keep their current values, present fields (including whole sections) are
// replaced. The merged result must pass full config validation, the same
// Validate a start runs, so a saved value always loads. The file-only keys
// (server.listen, database.path, tools.ffmpeg) are rejected, and so is
// wrapper.address while this process runs the wrapper itself. Accepted
// changes are written back to the combined config file; runtime keys apply to
// new requests and newly started jobs at once, restart keys on the next start
// (pending_restart in the response).
func (s *Server) updateConfig(w http.ResponseWriter, r *http.Request) {
	var body json.RawMessage
	if !decodeJSONBody(w, r, &body, false) {
		return
	}
	// Decode, merge, and validate inside the store's atomic update, so two
	// concurrent PUTs can never merge onto the same stale snapshot and
	// silently drop each other's changes. reject carries a request-level
	// failure out of the closure. UpdateAndSave re-reads the file before it
	// calls the closure, so an error with mutated still false is the file
	// itself, not this request.
	var reject func(http.ResponseWriter)
	mutated := false
	updated, err := s.cfg.UpdateAndSave(func(current config.Config) (config.Config, error) {
		mutated = true
		merged := current
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&merged); err != nil {
			reject = func(w http.ResponseWriter) {
				writeError(w, http.StatusBadRequest, jsonErrorCode(err), describeJSONError(err))
			}
			return config.Config{}, err
		}
		// A client that writes back the config it read sends the hidden
		// token; that leaves the saved one as it is.
		if strings.HasPrefix(merged.Wrapper.Token, redactedTokenPrefix) {
			merged.Wrapper.Token = current.Wrapper.Token
		}
		if locked := config.RuntimeLockedChanges(current, merged); len(locked) > 0 {
			err := errors.New("fields can only be changed in the config file and require a restart: " + strings.Join(locked, ", "))
			reject = func(w http.ResponseWriter) {
				writeErrorDetails(w, http.StatusUnprocessableEntity, CodeConfigKeyNotMutable, err.Error(), map[string]any{"keys": locked})
			}
			return config.Config{}, err
		}
		if s.wrapperManaged && merged.Wrapper.Address != current.Wrapper.Address {
			err := errors.New("wrapper.address is set by the wrapper this machine runs; after amdl wrapper uninstall, an address can be set once the backend's process starts again")
			reject = func(w http.ResponseWriter) {
				writeErrorDetails(w, http.StatusUnprocessableEntity, CodeConfigKeyNotMutable, err.Error(),
					map[string]any{"keys": []string{"wrapper.address"}, "reason": "managed_wrapper"})
			}
			return config.Config{}, err
		}
		if err := merged.Validate(); err != nil {
			reject = func(w http.ResponseWriter) {
				writeError(w, http.StatusUnprocessableEntity, CodeInvalidConfig, err.Error())
			}
			return config.Config{}, err
		}
		return merged, nil
	})
	switch {
	case reject != nil:
		reject(w)
		return
	case err != nil && !mutated && !isFileAccessError(err):
		writeError(w, http.StatusUnprocessableEntity, CodeInvalidConfig, "the config file no longer validates; fix it, then retry: "+err.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, CodeInternalError, "persist config: "+err.Error())
		return
	}
	// Another concurrent update may have committed after this one returned;
	// apply the store's final snapshot so an older request cannot restore its
	// logging level after a newer write.
	s.applyLoggingConfig()
	s.writeConfigResponse(w, updated, nil)
}

// redactedTokenPrefix is how a config response shows a set wrapper.token.
const redactedTokenPrefix = "..."

// hideSecret is how a config response shows a write-only secret: "..." when
// set, with none of its characters.
func hideSecret(secret string) string {
	if secret == "" {
		return ""
	}
	return redactedTokenPrefix
}

// isFileAccessError reports an error reading or writing the config file
// itself (permissions, I/O), as opposed to its content not validating.
func isFileAccessError(err error) bool {
	var pathErr *fs.PathError
	return errors.As(err, &pathErr)
}

func (s *Server) applyLoggingConfig() {
	if s.logSystem == nil {
		return
	}
	// The level was validated with the config, so SetLevel cannot fail here.
	_ = s.logSystem.FollowLevel(func() string { return s.currentConfig().Logging.Level })
}
