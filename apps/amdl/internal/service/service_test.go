package service

import (
	"encoding/xml"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

var spec = Spec{
	Exe:  "/Users/a b/bin/amdl",
	Args: []string{"serve", "--local", "--background", "--listen", "127.0.0.1:52011"},
	Log:  "/Users/a b/Library/Application Support/AMDL/server/logs/server.log",
	User: `DESKTOP\alice`,
}

var update = flag.Bool("update", false, "rewrite testdata/ with the service definitions generated now")

// golden compares got with testdata/name, or writes it there with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -update writes it)", err)
	}
	if got != string(want) {
		t.Errorf("%s changed; go test ./apps/amdl/internal/service -update rewrites it. got:\n%s", path, got)
	}
}

// The launchd agent: amdl with its arguments, started at login, restarted
// when it exits, logging to the server log; paths with spaces stay one
// argument.
func TestPlist(t *testing.T) {
	golden(t, "amdl.plist", plist(spec))
}

// The systemd user unit: ExecStart quotes each argument, restart on failure,
// started with the user's session.
func TestUnit(t *testing.T) {
	golden(t, "amdl.service", unit(Spec{Exe: "/home/x/go/bin/amdl", Args: []string{"serve", "--listen", "127.0.0.1:1"}}))
	// systemd.service(5): "%%" is a literal percent and "$$" a literal dollar.
	if got := systemdQuote(`50%$HOME"\`); got != `"50%%$$HOME\"\\"` {
		t.Errorf("systemdQuote = %s", got)
	}
}

func TestWinQuote(t *testing.T) {
	// The CommandLineToArgvW rules: backslashes are literal unless they
	// precede a quote; quotes inside an argument are escaped.
	for in, want := range map[string]string{
		"serve":            "serve",
		"":                 `""`,
		`C:\Program Files`: `"C:\Program Files"`,
		`a"b`:              `"a\"b"`,
		`a\"b`:             `"a\\\"b"`,
		`C:\dir with\`:     `"C:\dir with\\"`,
	} {
		if got := winQuote(in); got != want {
			t.Errorf("winQuote(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestTaskXML(t *testing.T) {
	at := time.Date(2026, 10, 30, 14, 30, 5, 0, time.Local)
	raw := taskXML(Spec{Exe: `C:\Users\alice\amdl.exe`, Args: []string{"serve", "--local", "--listen", "127.0.0.1:52011"}, User: spec.User}, at)
	if raw[0] != 0xff || raw[1] != 0xfe || len(raw)%2 != 0 {
		t.Fatalf("not UTF-16LE with a byte order mark")
	}
	u := make([]uint16, 0, len(raw)/2-1)
	for i := 2; i < len(raw); i += 2 {
		u = append(u, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	golden(t, "amdl-task.xml", string(utf16.Decode(u)))
	text := strings.Replace(string(utf16.Decode(u)), `encoding="UTF-16"`, `encoding="UTF-8"`, 1)
	var task struct {
		Trigger       string `xml:"Triggers>LogonTrigger>UserId"`
		Start         string `xml:"Triggers>TimeTrigger>StartBoundary"`
		Every         string `xml:"Triggers>TimeTrigger>Repetition>Interval"`
		LogonType     string `xml:"Principals>Principal>LogonType"`
		RunLevel      string `xml:"Principals>Principal>RunLevel"`
		Multiple      string `xml:"Settings>MultipleInstancesPolicy"`
		Limit         string `xml:"Settings>ExecutionTimeLimit"`
		Batteries     string `xml:"Settings>DisallowStartIfOnBatteries"`
		StopBatteries string `xml:"Settings>StopIfGoingOnBatteries"`
		Command       string `xml:"Actions>Exec>Command"`
		Arguments     string `xml:"Actions>Exec>Arguments"`
	}
	if err := xml.Unmarshal([]byte(text), &task); err != nil {
		t.Fatal(err)
	}
	if task.Trigger != spec.User || task.LogonType != "InteractiveToken" || task.RunLevel != "LeastPrivilege" {
		t.Errorf("principal = %+v", task)
	}
	if task.Start != "2026-10-30T14:30:05" || task.Every != "PT5M" {
		t.Errorf("time trigger = %q every %q", task.Start, task.Every)
	}
	// Task Scheduler's defaults stop a task after 72 hours and on battery.
	if task.Limit != "PT0S" || task.Batteries != "false" || task.StopBatteries != "false" || task.Multiple != "IgnoreNew" {
		t.Errorf("settings = %+v", task)
	}
	if task.Command != `C:\Users\alice\amdl.exe` || task.Arguments != "serve --local --listen 127.0.0.1:52011" {
		t.Errorf("action = %q %q", task.Command, task.Arguments)
	}
}

func TestDisabled(t *testing.T) {
	// schtasks /Query /XML after /Change /DISABLE: the triggers stay enabled.
	def := "<Task><Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers>" +
		"<Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><Enabled>false</Enabled></Settings></Task>"
	if !disabled(def) || disabled(strings.Replace(def, "<Enabled>false", "<Enabled>true", 1)) {
		t.Error("disabled misreads the settings")
	}
	if disabled("<Task><Triggers><TimeTrigger><Enabled>false</Enabled></TimeTrigger></Triggers><Settings></Settings></Task>") {
		t.Error("a disabled trigger is not a disabled task")
	}
}
