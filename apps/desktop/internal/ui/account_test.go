package ui

import (
	"strings"
	"testing"

	"amdl/internal/client"
)

func TestAccountSaysWhyTheLocalWrapperCannotDecrypt(t *testing.T) {
	// While its guest boots, logs in, is stopped or failed, a wrapper amdl
	// runs answers not ready and signed out (internal/wrapperhost Service).
	down := func(mode, lastErr string) *client.WrapperStatus {
		return &client.WrapperStatus{Regions: []string{}, Accounts: []string{}, LoginSupported: true,
			Managed: &client.ManagedWrapper{Mode: mode, Accel: "hvf", LastError: lastErr}}
	}
	for _, tc := range []struct{ mode, lastErr, word, next string }{
		{"starting", "", "Starting", "15 seconds"},
		{"login", "", "Logging in", "half a minute"},
		{"stopped", "", "Stopped", "amdl wrapper start"},
		{"failed", "qemu exited: status 1", "Failed", "qemu exited: status 1"},
	} {
		st := accountState(down(tc.mode, tc.lastErr), "")
		if st.word != tc.word || !strings.Contains(st.note, tc.next) || st.signIn || st.problem == "" {
			t.Errorf("mode %s: %+v; want %q with %q, no sign-in", tc.mode, st, tc.word, tc.next)
		}
	}
	serving := down("serving", "")
	serving.Ready = true
	if st := accountState(serving, ""); st.word != "Signed out" || !st.signIn {
		t.Errorf("a serving guest with no account: %+v; want the sign-in", st)
	}
	serving.LoggedIn, serving.Regions = true, []string{"us"}
	if st := accountState(serving, ""); st.word != "Signed in" || st.problem != "" {
		t.Errorf("a signed-in guest: %+v", st)
	}
	lite := &client.WrapperStatus{Ready: true, Regions: []string{}, Accounts: []string{}}
	if st := accountState(lite, ""); st.signIn || !strings.Contains(st.note, "wrapper's own machine") {
		t.Errorf("wrapper-lite cannot log in over the API: %+v", st)
	}
	if k := wrapperKind(serving); k != "local wrapper" {
		t.Errorf("kind %q; want local wrapper", k)
	}
}
