package cli

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"amdl/internal/capability"
)

// accountsSetCmd is amdl accounts set: the spec's body fields as flags that
// take on or off, and --default for position 0.
func accountsSetCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	op := capability.Lookup("updateAccount")
	values := map[string]any{}
	switchFlag := func(name, field, usage string) {
		fs.Func(name, usage+" (on|off)", func(s string) error {
			on, err := parseSwitch(s)
			if err != nil {
				return err
			}
			values[field] = on
			return nil
		})
	}
	switchFlag("library-sync", "library_sync", "poll the account's library for new albums to download")
	switchFlag("apple-library", "apple_library", "keep a local copy of the account's Apple Music library")
	makeDefault := fs.Bool("default", false, "make it the default account, position 0")
	fs.Func("position", "move it to this place in the list; 0 is the default", func(s string) error {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return fmt.Errorf("not a position: %s", s)
		}
		values["position"] = n
		return nil
	})
	return func(e *env, args []string) error {
		if len(args) != 1 {
			return usageErr("usage: amdl accounts set <account_id> [--library-sync on|off] [--apple-library on|off] [--default]")
		}
		if *makeDefault {
			if _, ok := values["position"]; ok {
				return usageErr("--default and --position both set the position; give one")
			}
			values["position"] = 0
		}
		if len(values) == 0 {
			return usageErr("nothing to change; give --library-sync, --apple-library or --default")
		}
		values["account_id"] = args[0]
		if err := e.c.ResolveAccounts(e.ctx, values); err != nil {
			return err
		}
		req, err := op.Build(values)
		if err != nil {
			return usageErr("%v", err)
		}
		return e.send(req, nil)
	}
}

// parseSwitch reads on/off and the spellings people use for them.
func parseSwitch(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("%q is not on or off", s)
}

// wrapperLogoutCmd is amdl wrapper logout: the spec's body takes exactly one
// of an Apple ID and an account id, which the spec's schema cannot say.
func wrapperLogoutCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	op := capability.Lookup("logoutWrapper")
	values := specFlags(fs, op, nil)
	const usage = "usage: amdl wrapper logout --username <apple_id> | --account-id <id, Apple ID or region>"
	return func(e *env, args []string) error {
		if len(args) > 0 || (values["username"] == nil) == (values["account_id"] == nil) {
			return usageErr("give exactly one of --username and --account-id; %s", usage)
		}
		if err := e.c.ResolveAccounts(e.ctx, values); err != nil {
			return err
		}
		req, err := op.Build(values)
		if err != nil {
			return usageErr("%v; %s", err, usage)
		}
		return e.send(req, nil)
	}
}
