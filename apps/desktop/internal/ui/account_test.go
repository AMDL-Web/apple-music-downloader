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
		return &client.WrapperStatus{Regions: []string{}, LoginSupported: true,
			Managed: &client.ManagedWrapper{Mode: mode, Accel: "hvf", LastError: lastErr}}
	}
	for _, tc := range []struct{ mode, lastErr, word, next string }{
		{"starting", "", "Starting", ""},
		{"login", "", "Logging in", ""},
		{"stopped", "", "Stopped", "amdl wrapper start"},
		{"failed", "qemu exited: status 1", "Failed", "qemu exited: status 1"},
	} {
		st := accountState(down(tc.mode, tc.lastErr), "")
		if st.word != tc.word || !strings.Contains(st.note, tc.next) || st.signIn || st.problem == "" {
			t.Errorf("mode %s: %+v; want %q with %q, no sign-in", tc.mode, st, tc.word, tc.next)
		}
	}
	dl := down("starting", "")
	dl.Managed.Download = &client.WrapperDownload{DoneBytes: 12_000_000, TotalBytes: 48_000_000}
	if st := accountState(dl, ""); st.word != "Downloading" || st.note != "wrapper-lite · 12.0 / 48.0 MB" || st.signIn {
		t.Errorf("while the host downloads the payload: %+v", st)
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
	lite := &client.WrapperStatus{Ready: true, Regions: []string{}}
	if st := accountState(lite, ""); st.signIn || !strings.Contains(st.note, "wrapper's own machine") {
		t.Errorf("wrapper-lite cannot log in over the API: %+v", st)
	}
	if k := wrapperKind(serving); k != "local wrapper" {
		t.Errorf("kind %q; want local wrapper", k)
	}
}
