package api

import "net/http"

// restartSystem answers 202 and then asks the process to stop and start
// again (Deps.Restart), which applies the config keys saved for a restart
// (pending_restart). The running jobs are interrupted the way a shutdown
// interrupts them and resume after the start. The response is written before
// the signal, and the shutdown drains this request, so the client gets it.
func (s *Server) restartSystem(w http.ResponseWriter, r *http.Request) {
	if s.restart == nil {
		writeError(w, http.StatusServiceUnavailable, CodeFeatureUnavailable, "this backend cannot restart itself; restart its process")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"restarting": true})
	s.restart()
}
