package service

import (
	"encoding/xml"
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

func TestPlist(t *testing.T) {
	var doc struct {
		Dict struct {
			Keys    []string `xml:"key"`
			Strings []string `xml:"string"`
			Array   []string `xml:"array>string"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal([]byte(plist(spec)), &doc); err != nil {
		t.Fatal(err)
	}
	want := append([]string{spec.Exe}, spec.Args...)
	if strings.Join(doc.Dict.Array, "|") != strings.Join(want, "|") {
		t.Errorf("ProgramArguments = %q, want %q", doc.Dict.Array, want)
	}
	if doc.Dict.Strings[0] != Label || doc.Dict.Strings[1] != spec.Log {
		t.Errorf("Label, StandardErrorPath = %q", doc.Dict.Strings)
	}
	for _, k := range []string{"RunAtLoad", "KeepAlive", "StandardErrorPath"} {
		if !strings.Contains(strings.Join(doc.Dict.Keys, " "), k) {
			t.Errorf("no %s key", k)
		}
	}
}

func TestUnit(t *testing.T) {
	u := unit(Spec{Exe: "/home/x/go/bin/amdl", Args: []string{"serve", "--listen", "127.0.0.1:1"}})
	if !strings.Contains(u, "\nExecStart=\"/home/x/go/bin/amdl\" \"serve\" \"--listen\" \"127.0.0.1:1\"\n") {
		t.Errorf("unit:\n%s", u)
	}
	if !strings.Contains(u, "WantedBy=default.target") || !strings.Contains(u, "Restart=on-failure") {
		t.Errorf("unit:\n%s", u)
	}
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
