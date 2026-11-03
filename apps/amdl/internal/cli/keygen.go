package cli

import (
	"flag"

	"amdl/internal/wrapper"
)

// wrapperKeygenCmd prints a strong random key for wrapper.token, to set on
// the wrapper's gateway and on the backend (amdl config set wrapper.token=…).
// It contacts no server.
func wrapperKeygenCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl wrapper keygen")
		}
		key, err := wrapper.GenerateKey()
		if err != nil {
			return err
		}
		return e.out.result(map[string]string{"key": key})
	}
}
