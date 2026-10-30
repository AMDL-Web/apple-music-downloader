package client

import (
	"strings"
	"testing"
)

func TestWrapperProblem(t *testing.T) {
	managed := func(mode, last string) WrapperStatus {
		return WrapperStatus{Managed: &ManagedWrapper{Mode: mode, LastError: last}}
	}
	for _, c := range []struct {
		w         WrapperStatus
		what, fix string
	}{
		{WrapperStatus{Ready: true, Status: true}, "", ""},
		{WrapperStatus{Ready: true, Status: true, Managed: &ManagedWrapper{Mode: "serving"}}, "", ""},
		{WrapperStatus{Ready: true}, "no Apple ID", "--login"},
		{WrapperStatus{Ready: true, Managed: &ManagedWrapper{Mode: "serving"}}, "no Apple ID", "amdl wrapper login"},
		{managed("starting", ""), "starting", "amdl wrapper status"},
		{managed("login", ""), "logging", "finish the login"},
		{managed("stopped", ""), "stopped", "amdl wrapper start"},
		{managed("failed", "the wrapper did not answer within 3m0s"), "did not answer within 3m0s", "amdl wrapper logs"},
		{WrapperStatus{}, "not ready", "amdl wrapper status"},
	} {
		what, fix := c.w.Problem()
		if (c.what == "") != (what == "") || !strings.Contains(what, c.what) || !strings.Contains(fix, c.fix) {
			t.Errorf("Problem(%+v) = %q, %q; want %q, %q", c.w, what, fix, c.what, c.fix)
		}
	}
}
