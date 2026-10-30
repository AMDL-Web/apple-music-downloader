package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"amdl/internal/client"
)

// Exit statuses, by class of error; the error object on stderr carries the
// exact code. docs/cli.md documents the table.
const (
	exitOK          = 0
	exitInternal    = 1
	exitInput       = 2
	exitNotFound    = 3
	exitConflict    = 4
	exitUnavailable = 5
	exitAuth        = 6
	exitJobFailed   = 7
	exitWaitTimeout = 8
	exitConfirm     = 10
	exitInterrupted = 130
)

// cliError is a failure the CLI reports itself, in the API's error shape plus
// a hint naming the command to run next.
type cliError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
	Hint    string          `json:"hint,omitempty"`
	exit    int
}

func (e *cliError) Error() string { return e.Message }

func usageErr(format string, a ...any) error {
	return &cliError{Code: "usage_error", Message: fmt.Sprintf(format, a...), exit: exitInput}
}

// restartWithWrapper is the one way the CLI tells a person how to see and
// change the wrapper a backend uses: wrapper.address is a startup key, so a
// change is a restart.
const restartWithWrapper = "amdl system get shows the address in use as startup.wrapper_address; to change it, " +
	"restart the backend with --wrapper <address> (amdl service install or amdl serve --local), " +
	"or for a backend amdl does not run, edit wrapper.address in the file at startup.config_path and restart it"

// exitFor maps an API error code to its exit status.
func exitFor(code string, status int) int {
	switch code {
	case "invalid_request", "invalid_json", "request_too_large", "invalid_overrides",
		"config_key_not_mutable", "invalid_config", "no_urls_accepted", "usage_error":
		return exitInput
	case "route_not_found", "job_not_found", "login_not_found", "account_not_found", "catalog_not_found":
		return exitNotFound
	case "job_active", "job_not_retryable", "job_not_cancellable", "job_finalizing", "duplicate_active",
		"already_logged_in", "login_pending", "method_not_allowed":
		return exitConflict
	case "upstream_error", "queue_full", "wrapper_unavailable", "feature_unavailable",
		"upstream_timeout", "server_unreachable", "server_not_configured":
		return exitUnavailable
	case "authentication_failed", "login_unsupported":
		return exitAuth
	case "internal_error", "store_error":
		return exitInternal
	}
	switch {
	case status >= 500:
		return exitUnavailable
	case status >= 400:
		return exitInput
	}
	return exitInternal
}

// asCLIError turns any error into the object written to stderr.
func asCLIError(err error) *cliError {
	var ce *cliError
	if errors.As(err, &ce) {
		if ce.exit == 0 {
			ce.exit = exitFor(ce.Code, 0)
		}
		return ce
	}
	var api *client.APIError
	if errors.As(err, &api) {
		code := api.Code
		if code == "" {
			code = fmt.Sprintf("http_%d", api.Status)
		}
		msg := api.Message
		if msg == "" {
			msg = api.Error()
		}
		ce := &cliError{Code: code, Message: msg, Details: api.Details, exit: exitFor(code, api.Status)}
		switch code {
		case "config_key_not_mutable":
			ce.Hint = "a startup key: edit the file at startup.config_path (amdl system get) and restart the backend"
			if strings.Contains(string(api.Details), "wrapper.address") {
				ce.Hint = restartWithWrapper
			}
		case "wrapper_unavailable":
			ce.Hint = "the backend cannot reach its wrapper; start the wrapper, or point the backend at another: " + restartWithWrapper
		}
		return ce
	}
	var netErr *url.Error
	if errors.As(err, &netErr) || errors.Is(err, client.ErrWatchdog) {
		return &cliError{Code: "server_unreachable", Message: err.Error(), exit: exitUnavailable,
			Hint: "check the server address: amdl context list"}
	}
	return &cliError{Code: "internal_error", Message: err.Error(), exit: exitInternal}
}
