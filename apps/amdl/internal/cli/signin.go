package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"amdl/internal/capability"
)

// TerminalSecret reads a line from the terminal f without echoing it. An
// interrupt gives up the read and puts the terminal back as it was.
func TerminalSecret(f *os.File) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		fd := f.Fd()
		state, err := term.GetState(fd)
		if err != nil {
			return "", err
		}
		type read struct {
			b   []byte
			err error
		}
		done := make(chan read, 1)
		go func() {
			b, err := term.ReadPassword(fd)
			done <- read{b, err}
		}()
		select {
		case r := <-done:
			return string(r.b), r.err
		case <-ctx.Done():
			// The read stays blocked until the process exits, which it is
			// about to; only the echo it turned off needs restoring.
			_ = term.Restore(fd, state) // nothing better to do on failure
			return "", ctx.Err()
		}
	}
}

// ask prompts on stderr and reads one line from the terminal on stdin; a
// secret one is not echoed.
func (e *env) ask(prompt string, secret bool) (string, error) {
	fmt.Fprint(e.Stderr, prompt)
	if !secret {
		type read struct {
			line string
			err  error
		}
		done := make(chan read, 1)
		go func() {
			line, err := bufio.NewReader(e.Stdin).ReadString('\n')
			done <- read{line, err}
		}()
		select {
		case r := <-done:
			if r.err != nil && r.line == "" {
				return "", usageErr("nothing was entered")
			}
			return strings.TrimSpace(r.line), nil
		case <-e.ctx.Done():
			// Like TerminalSecret: the read stays blocked until the process
			// exits, which an interrupt is about to do.
			return "", e.ctx.Err()
		}
	}
	if e.ReadSecret == nil {
		return "", usageErr("this terminal cannot be read without echoing")
	}
	s, err := e.ReadSecret(e.ctx)
	fmt.Fprintln(e.Stderr) // the Enter that ended it was not echoed either
	return s, err
}

// firstLine reads the first line of stdin, keeping everything but its line
// ending: spaces can be part of a password.
func (e *env) firstLine(what string) (string, error) {
	line, err := bufio.NewReader(e.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", usageErr("no %s on stdin", what)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

const loginUsage = "usage: amdl wrapper login [--username <apple id>] in a terminal, or amdl wrapper login --username <apple id> --password-stdin"

func wrapperLoginCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	username := fs.String("username", "", "the Apple ID; a terminal asks for it when it is missing")
	stdin := fs.Bool("password-stdin", false, "read the password from the first line of stdin; without it a terminal asks for the password without echoing it, and a pipe is refused")
	return func(e *env, args []string) error {
		ask := e.InTTY && !*stdin
		user := strings.TrimSpace(*username)
		if len(args) > 0 || (!ask && (user == "" || !*stdin)) {
			return usageErr(loginUsage)
		}
		if e.dry {
			if user == "" {
				user = "(asked)"
			}
			from := "(from stdin)"
			if ask {
				from = "(asked)"
			}
			return e.send(loginRequest(user, from), nil)
		}
		var password string
		var err error
		if ask {
			if user == "" {
				if user, err = e.ask("Apple ID: ", false); err != nil {
					return err
				}
			}
			password, err = e.ask("Password: ", true)
		} else {
			password, err = e.firstLine("password")
		}
		if err != nil {
			return err
		}
		if user == "" || password == "" {
			return usageErr("an Apple ID and a password are both needed")
		}
		return e.send(loginRequest(user, password), func(raw []byte) error {
			var r struct {
				Status  string `json:"status"`
				LoginID string `json:"login_id"`
			}
			if json.Unmarshal(raw, &r) != nil || r.Status != "needs_2fa" {
				return e.out.result(json.RawMessage(raw))
			}
			if !e.InTTY {
				e.note("Apple sent a two-step code; within 60 seconds (4 minutes for a wrapper amdl runs), give it to amdl wrapper 2fa %s on stdin or at its prompt", r.LoginID)
				return e.out.result(json.RawMessage(raw))
			}
			code, err := e.twoStepCode("Apple sent a two-step code; enter it within 60 seconds (4 minutes for a wrapper amdl runs): ")
			if err != nil {
				return err
			}
			return e.send(twoStepRequest(r.LoginID, code), nil)
		})
	}
}

func loginRequest(user, password string) capability.Request {
	return capability.Request{Method: http.MethodPost, Path: "/api/v1/wrapper/login",
		Body: map[string]any{"username": user, "password": password}}
}

func twoStepRequest(id, code string) capability.Request {
	req, err := capability.Lookup("submitWrapperTwoStepCode").Build(map[string]any{"login_id": id, "two_step_code": code})
	if err != nil {
		panic(err) // both values are present, so only a changed spec gets here
	}
	return req
}

// twoStepCode reads a two-step code: at a prompt that does not echo it in a
// terminal, else from the first line of stdin.
func (e *env) twoStepCode(prompt string) (string, error) {
	var code string
	var err error
	if e.InTTY {
		code, err = e.ask(prompt, true)
	} else {
		code, err = e.firstLine("two-step code")
	}
	if err != nil {
		return "", err
	}
	if code = strings.TrimSpace(code); code == "" {
		return "", usageErr("no two-step code was given")
	}
	return code, nil
}

func wrapperTwoStepCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) != 1 {
			return usageErr("usage: amdl wrapper 2fa <login_id>, with the code typed at its prompt or on the first line of stdin")
		}
		if e.dry {
			from := "(from stdin)"
			if e.InTTY {
				from = "(asked)"
			}
			return e.send(twoStepRequest(args[0], from), nil)
		}
		code, err := e.twoStepCode("Two-step code: ")
		if err != nil {
			return err
		}
		return e.send(twoStepRequest(args[0], code), nil)
	}
}
