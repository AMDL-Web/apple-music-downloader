package cli

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"amdl/internal/capability"
)

// custom returns the commands written out by hand; every other capability with
// an operation becomes a passthrough command built from the spec.
func custom() map[string]func(fs *flag.FlagSet) func(e *env, args []string) error {
	return map[string]func(fs *flag.FlagSet) func(e *env, args []string) error{
		"download":       downloadCmd,
		"jobs get":       jobsGetCmd,
		"jobs watch":     jobsWatchCmd,
		"jobs wait":      jobsWaitCmd,
		"library list":   libraryListCmd,
		"logs tail":      logsTailCmd,
		"config get":     configGetCmd,
		"config set":     configSetCmd,
		"wrapper login":  wrapperLoginCmd,
		"cli search":     cliSearchCmd,
		"cli commands":   cliCommandsCmd,
		"schema":         schemaCmd,
		"context list":   noFlags(contextList),
		"context add":    noFlags(contextAdd),
		"context use":    noFlags(contextUse),
		"context remove": noFlags(contextRemove),
		"version":        versionCmd,
		"serve":          serveCmd,
		"mcp":            mcpCmd,
	}
}

// local are the commands that never talk to a server.
var local = map[string]bool{
	"cli search": true, "cli commands": true, "schema": true, "context list": true,
	"context add": true, "context use": true, "context remove": true, "serve": true, "version": true,
}

func noFlags(run func(e *env, args []string) error) func(*flag.FlagSet) func(*env, []string) error {
	return func(*flag.FlagSet) func(*env, []string) error { return run }
}

// commands is every CLI command, in capability order.
func commands() []command {
	var out []command
	handwritten := custom()
	for _, c := range capability.All {
		if c.Command == "" {
			continue
		}
		cmd := command{Capability: c, noServer: local[c.Command]}
		if setup, ok := handwritten[c.Command]; ok {
			cmd.setup = setup
			cmd.dryRun = c.Safety != capability.Read && !cmd.noServer
		} else {
			cmd.setup = passthrough(c)
			cmd.dryRun = c.Safety != capability.Read
		}
		out = append(out, cmd)
	}
	return out
}

// flagName is the CLI spelling of a parameter or body field.
func flagName(param string) string { return strings.ReplaceAll(param, "_", "-") }

// specFlags declares one flag per parameter and body field of op that is not
// positional, typed from its schema, and returns the values they collect.
func specFlags(fs *flag.FlagSet, op capability.Operation, positional []string) map[string]any {
	values := map[string]any{}
	skip := map[string]bool{}
	for _, a := range positional {
		skip[a] = true
	}
	add := func(name, typ, desc string, enum []any) {
		if skip[name] || fs.Lookup(flagName(name)) != nil {
			return
		}
		usage := firstSentence(desc)
		if len(enum) > 0 {
			usage += fmt.Sprintf(" (%s)", joinAny(enum))
		}
		switch typ {
		case "boolean":
			fs.BoolFunc(flagName(name), usage, func(s string) error {
				b, err := strconv.ParseBool(s)
				values[name] = b
				return err
			})
		case "integer":
			fs.Func(flagName(name), usage, func(s string) error {
				n, err := strconv.Atoi(s)
				if err != nil {
					return fmt.Errorf("not a whole number: %s", s)
				}
				values[name] = n
				return nil
			})
		case "array":
			fs.Func(flagName(name), usage+"; comma-separated", func(s string) error {
				values[name] = append(asStrings(values[name]), splitList(s)...)
				return nil
			})
		default:
			fs.Func(flagName(name), usage, func(s string) error {
				values[name] = s
				return nil
			})
		}
	}
	for _, p := range op.Params {
		var enum []any
		if items, ok := p.Schema["items"].(map[string]any); ok {
			enum, _ = items["enum"].([]any)
		} else {
			enum, _ = p.Schema["enum"].([]any)
		}
		add(p.Name, p.Type(), p.Description, enum)
	}
	for _, f := range op.BodyFields() {
		typ, _ := f.Schema["type"].(string)
		enum, _ := f.Schema["enum"].([]any)
		add(f.Name, typ, f.Description, enum)
	}
	return values
}

// passthrough builds a command that sends one request from its flags and
// positional arguments and prints the answer as is.
func passthrough(c capability.Capability) func(fs *flag.FlagSet) func(e *env, args []string) error {
	return func(fs *flag.FlagSet) func(e *env, args []string) error {
		op := capability.Lookup(c.Ops[0])
		values := specFlags(fs, op, c.Args)
		return func(e *env, args []string) error {
			if len(args) > len(c.Args) {
				return usageErr("too many arguments; usage: amdl %s%s", c.Command, argSynopsis(c))
			}
			for i, a := range args {
				values[c.Args[i]] = a
			}
			req, err := op.Build(values)
			if err != nil {
				return usageErr("%v; usage: amdl %s%s", err, c.Command, argSynopsis(c))
			}
			return e.send(req, nil)
		}
	}
}

// send runs one built request, honouring --dry-run and confirmation, and
// prints the answer, or out when it is not nil.
func (e *env) send(req capability.Request, out func([]byte) error) error {
	if e.dry {
		return e.out.result(map[string]any{"method": req.Method, "url": e.c.Base() + req.Path + query(req), "body": req.Body})
	}
	if e.cmd.Safety == capability.Destructive {
		if err := e.confirm(fmt.Sprintf("amdl %s changes data that cannot be restored", e.cmd.Command)); err != nil {
			return err
		}
	}
	var body any
	if req.Body != nil {
		body = req.Body
	}
	raw, err := e.c.Call(e.ctx, req.Method, req.Path, req.Query, body)
	if err != nil {
		return err
	}
	if out != nil {
		return out(raw)
	}
	return e.out.result(raw)
}

func query(req capability.Request) string {
	if len(req.Query) == 0 {
		return ""
	}
	return "?" + req.Query.Encode()
}

func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i]
	}
	return strings.TrimSuffix(s, ".")
}

func joinAny(list []any) string {
	parts := make([]string, len(list))
	for i, v := range list {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, "|")
}

func asStrings(v any) []string {
	s, _ := v.([]string)
	return s
}
