package config

import (
	"slices"
	"strings"
)

// No environment variable sets a config key: config.yaml is the one source,
// changed through PUT /api/v1/config or by hand. Three AMDL_* variables are
// read, none of them a key: AMDL_CONFIG and AMDL_HOOKS_CONFIG (the two file
// paths, read in main) and AMDL_URL, the server the amdl client commands talk
// to, which a shell that also runs amdl serve may well have set.
var envPaths = []string{"AMDL_CONFIG", "AMDL_HOOKS_CONFIG", "AMDL_URL"}

// UnreadEnv returns the AMDL_* variables set in environ that the backend does
// not read, sorted. They are left over from releases that let a variable
// override a key (AMDL_SERVER_LISTEN, AMDL_WRAPPER_ADDRESS and others); main
// logs a warning for each so an install notices that the setting no longer
// applies and moves it into config.yaml.
func UnreadEnv(environ []string) []string {
	var unread []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, "AMDL_") || slices.Contains(envPaths, name) {
			continue
		}
		unread = append(unread, name)
	}
	slices.Sort(unread)
	return slices.Compact(unread)
}
