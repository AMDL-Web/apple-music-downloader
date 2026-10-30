package client

// Problem says what keeps the wrapper from decrypting and the amdl command
// that helps; both are empty while it serves an account. A wrapper amdl runs
// is judged by its own mode first: while it boots, logs in, is stopped or
// failed, its lite cannot answer for itself.
func (w WrapperStatus) Problem() (what, fix string) {
	if m := w.Managed; m != nil {
		switch m.Mode {
		case "serving":
		case "starting":
			return "the local wrapper is starting", "wait a minute, then amdl wrapper status"
		case "login":
			return "the local wrapper is logging an account in", "finish the login, or wait for it to end"
		case "stopped":
			return "the local wrapper is stopped", "amdl wrapper start, if amdl wrapper stop turned it off"
		default:
			what = "the local wrapper failed"
			if m.LastError != "" {
				what += ": " + m.LastError
			}
			return what, "amdl wrapper logs"
		}
	}
	switch {
	case !w.Ready:
		return "the wrapper is not ready", "it may still be starting: amdl wrapper status"
	case !w.LoggedIn && w.Managed != nil:
		return "no Apple ID is signed in", "amdl wrapper login"
	case !w.LoggedIn:
		return "no Apple ID is signed in", "amdl wrapper login, or the wrapper's own --login"
	}
	return "", ""
}
