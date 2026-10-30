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

// getConfig returns the runtime-changeable part of the in-memory config
// snapshot (mutable download fields, logging level and access log, simulate,
// library sync, and the runtime catalog keys). Startup-bound fields are
// omitted: clients cannot change them through this API. It never reads the
// file; POST /config/reload does.
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
	s.writeConfigResponse(w, s.currentConfig(), reloadErr)
}

// writeConfigResponse answers GET, PUT and POST /config/reload alike: the
// ConfigResponse schema for cfg, with catalog.media_user_token redacted.
func (s *Server) writeConfigResponse(w http.ResponseWriter, cfg config.Config, reloadErr error) {
	view, err := config.MutableView(cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternalError, "render config: "+err.Error())
		return
	}
	if catalog, ok := view["catalog"].(map[string]any); ok {
		if _, ok := catalog["media_user_token"]; ok {
			catalog["media_user_token"] = redactToken(cfg.Catalog.MediaUserToken)
		}
	}
	resp := map[string]any{
		"config":    view,
		"persisted": s.cfg != nil && s.cfg.Persistent(),
	}
	if reloadErr != nil {
		resp["reload_error"] = reloadErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// updateConfig merges the request body onto the current runtime config:
// omitted fields keep their current values, present fields (including whole
// sections) are replaced. The merged result must pass full config validation,
// and fields consumed only at startup (server, database, logging outputs,
// wrapper, tools, catalog client token and request limits, and process-wide
// download worker/media pools) are rejected — changing them at runtime would
// silently do nothing. Accepted changes apply to new requests and newly
// started jobs immediately and are written back to the combined config file,
// so they survive restarts.
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
		// A client that writes back the config it read sends the redacted
		// token; that leaves the saved one as it is.
		if strings.HasPrefix(merged.Catalog.MediaUserToken, redactedTokenPrefix) {
			merged.Catalog.MediaUserToken = current.Catalog.MediaUserToken
		}
		if locked := config.RuntimeLockedChanges(current, merged); len(locked) > 0 {
			err := errors.New("fields can only be changed in the config file and require a restart: " + strings.Join(locked, ", "))
			reject = func(w http.ResponseWriter) {
				writeErrorDetails(w, http.StatusUnprocessableEntity, CodeConfigKeyNotMutable, err.Error(), map[string]any{"keys": locked})
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

// redactedTokenPrefix starts the form a config response shows a set
// media-user-token in: the prefix and the token's last four characters,
// enough to tell two tokens apart without the credential reaching a client
// or an agent's context.
const redactedTokenPrefix = "..."

func redactToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) < 16 {
		return redactedTokenPrefix
	}
	return redactedTokenPrefix + token[len(token)-4:]
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
