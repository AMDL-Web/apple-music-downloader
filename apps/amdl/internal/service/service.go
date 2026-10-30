// Package service registers amdl serve --local with the OS as a per-user
// background service that starts at login: a launchd LaunchAgent on macOS, a
// systemd --user unit on Linux and a logon task in Task Scheduler on Windows,
// where a real service would need an administrator. None of them touches the
// backend's data; the backend itself (internal/local) owns that.
package service

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"

	"amdl/internal/sysproc"
)

// Name is the unit and task name; Label is the launchd label.
const (
	Name  = "amdl"
	Label = "io.github.amdl-web.amdl"
)

// Spec is the command the service runs.
type Spec struct {
	// Exe is the amdl executable, an absolute path.
	Exe string
	// Args are its arguments: serve --local --background --listen <addr>.
	Args []string
	// Log receives what the process writes to stderr before amdl opens its
	// own log (launchd only; systemd keeps it in the journal).
	Log string
	// User is the account whose logon starts the task (Windows only).
	User string
}

// State is what the OS's service manager says about the service.
type State struct {
	// Manager is launchd, systemd or Task Scheduler.
	Manager string `json:"manager"`
	// Definition is the plist, the unit file or the task's name.
	Definition string `json:"definition"`
	Installed  bool   `json:"installed"`
	// Status is the manager's own word for it, such as active or Running.
	Status string `json:"status,omitempty"`
}

// ErrUnsupported means this OS has no service manager amdl knows.
var ErrUnsupported = errors.New("amdl service supports macOS, Linux with systemd, and Windows")

// runTimeout bounds each call to launchctl, systemctl or schtasks.
const runTimeout = 30 * time.Second

// run runs a service manager command and returns its output.
var run = func(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	sysproc.NoConsole(cmd)
	raw, err := cmd.CombinedOutput()
	out := decode(raw)
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return out, nil
}

// decode turns a service manager's output into text: UTF-8 everywhere but
// Windows, where schtasks writes the system's OEM code page.
var decode = func(b []byte) string { return string(b) }

func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s)) // a strings.Builder does not fail
	return b.String()
}

// plist is the LaunchAgent: started at login, restarted after a crash but not
// after a clean exit, so launchctl bootout stops it.
func plist(s Spec) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range append([]string{s.Exe}, s.Args...) {
		b.WriteString("\t\t<string>" + esc(a) + "</string>\n")
	}
	b.WriteString(`	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>StandardErrorPath</key>
	<string>` + esc(s.Log) + `</string>
</dict>
</plist>
`)
	return b.String()
}

// unit is the systemd --user unit, started with the user's session.
func unit(s Spec) string {
	args := make([]string, 0, len(s.Args)+1)
	for _, a := range append([]string{s.Exe}, s.Args...) {
		args = append(args, systemdQuote(a))
	}
	return `[Unit]
Description=AMDL backend (amdl serve --local)
After=network-online.target

[Service]
ExecStart=` + strings.Join(args, " ") + `
Restart=on-failure
RestartSec=10
TimeoutStopSec=30

[Install]
WantedBy=default.target
`
}

// systemdQuote quotes one ExecStart word: systemd expands % specifiers and $
// variables even inside quotes.
func systemdQuote(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$").Replace(s)
	return `"` + s + `"`
}

// taskXML is the Task Scheduler definition, UTF-16 as schtasks /XML wants it:
// started at the user's logon with their normal rights, and without the
// defaults that would stop it on battery or after three days. Task Scheduler
// does not restart a task whose process dies (RestartOnFailure covers failing
// to start), so a trigger from at on repeats every five minutes and, the task
// being single-instance, starts the backend again only when it is gone.
func taskXML(s Spec, at time.Time) []byte {
	args := make([]string, len(s.Args))
	for i, a := range s.Args {
		args[i] = winQuote(a)
	}
	doc := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>AMDL backend (amdl serve --local), registered by amdl service install</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + esc(s.User) + `</UserId>
    </LogonTrigger>
    <TimeTrigger>
      <Enabled>true</Enabled>
      <StartBoundary>` + at.Format("2006-01-02T15:04:05") + `</StartBoundary>
      <Repetition>
        <Interval>PT5M</Interval>
        <StopAtDurationEnd>false</StopAtDurationEnd>
      </Repetition>
    </TimeTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + esc(s.User) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
    </IdleSettings>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + esc(s.Exe) + `</Command>
      <Arguments>` + esc(strings.Join(args, " ")) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
	doc = strings.ReplaceAll(doc, "\n", "\r\n")
	u := utf16.Encode([]rune(doc))
	out := make([]byte, 2, 2+2*len(u))
	out[0], out[1] = 0xff, 0xfe // little-endian byte order mark
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

// winQuote quotes one argument the way Windows programs split a command line.
func winQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range s {
		switch r {
		case '\\':
			slashes++
			continue
		case '"':
			b.WriteString(strings.Repeat(`\`, 2*slashes+1))
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
		}
		slashes = 0
		b.WriteRune(r)
	}
	b.WriteString(strings.Repeat(`\`, 2*slashes))
	b.WriteByte('"')
	return b.String()
}
