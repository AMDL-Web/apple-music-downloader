package cli

import (
	"encoding/json"
	"flag"
	"net/http"
	"time"

	"amdl/internal/capability"
	"amdl/internal/domain"
)

// checkPoll is how often library check --wait asks the server how far the
// check is.
const checkPoll = time.Second

// libraryCheckCmd starts a disk check. The server runs it in the background;
// with --wait the command follows it until it is finished and prints that
// check, so a script needs no polling of its own.
func libraryCheckCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	op := capability.Lookup("startLibraryCheck")
	values := specFlags(fs, op, nil)
	wait := fs.Bool("wait", false, "wait until the check is finished and print it")
	timeout := fs.Duration("timeout", 10*time.Minute, "how long --wait waits, as a duration such as 90s or 20m (default 10m); 0 only looks once")
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl library check [--kind albums|playlists --id <id>] [--force] [--wait]")
		}
		req, err := op.Build(values)
		if err != nil {
			return usageErr("%v", err)
		}
		if !*wait || e.dry {
			return e.send(req, nil)
		}
		var body any
		if req.Body != nil {
			body = req.Body
		}
		raw, err := e.c.Call(e.ctx, req.Method, req.Path, req.Query, body)
		if err != nil {
			return err
		}
		var started struct {
			Check *domain.CheckRun `json:"check"`
		}
		if err := json.Unmarshal(raw, &started); err != nil {
			return err
		}
		check := started.Check
		deadline := time.Now().Add(*timeout)
		for check != nil && check.FinishedAt == nil {
			if !time.Now().Before(deadline) {
				_ = e.out.result(map[string]any{"check": check}) // the error below is the outcome either way
				return &cliError{Code: "wait_timeout", exit: exitWaitTimeout,
					Message: "the disk check is still running", Hint: "amdl library check-status"}
			}
			select {
			case <-e.ctx.Done():
				return e.ctx.Err()
			case <-time.After(checkPoll):
			}
			raw, err := e.c.Call(e.ctx, http.MethodGet, "/api/v1/library/check", nil, nil)
			if err != nil {
				return err
			}
			var now struct {
				Check *domain.CheckRun `json:"check"`
			}
			if err := json.Unmarshal(raw, &now); err != nil {
				return err
			}
			if now.Check != nil {
				check = now.Check
			}
		}
		out := map[string]any{"check": check}
		if check != nil && check.AbortedReason == "too_many_missing" {
			out["hint"] = "more than half of the checked files are missing, so nothing was marked (an unmounted share looks like this); " +
				"if they are really gone, run it again with --force: " + checkForceCommand(values)
		}
		return e.out.result(out)
	}
}

// checkForceCommand is the command line that repeats a check with --force,
// keeping the record it was limited to.
func checkForceCommand(values map[string]any) string {
	line := "amdl library check"
	if kind, ok := values["kind"].(string); ok {
		line += " --kind " + kind
	}
	if id, ok := values["id"].(string); ok {
		line += " --id " + id
	}
	return line + " --force --wait"
}
