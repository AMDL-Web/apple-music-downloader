package config

import (
	"reflect"
	"testing"
)

// No variable overrides a key: the four that once did, and every other
// AMDL_* variable except the file paths and the client's server, are named by
// UnreadEnv so main can warn, and Load leaves the file's values alone.
func TestEnvironmentSetsNoKey(t *testing.T) {
	t.Setenv("AMDL_SERVER_LISTEN", ":19090")
	t.Setenv("AMDL_WRAPPER_ADDRESS", "http://wrapper:12340")
	t.Setenv("AMDL_DATABASE_PATH", "/srv/amdl/amdl.db")
	t.Setenv("AMDL_WRAPPER_DATA_DIR", "/wrapper-data")
	path := writeConfig(t, "wrapper:\n  address: \"http://10.0.0.1:12340\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	def := Default()
	if cfg.Server.Listen != def.Server.Listen || cfg.Wrapper.Address != "http://10.0.0.1:12340" ||
		cfg.Database.Path != def.Database.Path || cfg.Wrapper.DataDir != "" {
		t.Fatalf("an environment variable reached the config: %+v %+v %+v", cfg.Server, cfg.Wrapper, cfg.Database)
	}
}

func TestUnreadEnvNamesEveryKeyVariable(t *testing.T) {
	environ := []string{
		"AMDL_SIMULATE_ENABLED=true",
		"AMDL_SERVER_LISTEN=:19090",
		"AMDL_WRAPPER_ADDRESS=http://wrapper:12340",
		"AMDL_SIMULATE_ENABLED=true",
		"AMDL_CONFIG=/etc/amdl/config.yaml",
		"AMDL_HOOKS_CONFIG=/etc/amdl/hooks.yaml",
		"AMDL_URL=http://nas:18080",
		"HOME=/root",
	}
	want := []string{"AMDL_SERVER_LISTEN", "AMDL_SIMULATE_ENABLED", "AMDL_WRAPPER_ADDRESS"}
	if got := UnreadEnv(environ); !reflect.DeepEqual(got, want) {
		t.Fatalf("UnreadEnv = %v, want %v", got, want)
	}
	if got := UnreadEnv([]string{"AMDL_URL=http://nas:18080", "PATH=/bin"}); len(got) != 0 {
		t.Fatalf("UnreadEnv = %v, want none", got)
	}
}
