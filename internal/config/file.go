package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

const configExampleFileName = "config.example.yaml"

// managedFileHeader tops config.yaml after the runtime config API rewrites it.
// Detailed field documentation remains in config.example.yaml because YAML
// marshaling does not preserve comments.
const managedFileHeader = `# Managed by the amdl backend: PUT /api/v1/config rewrites this file and
# does not preserve comments. Hand edits to runtime-mutable fields apply on
# SIGHUP, POST /api/v1/config/reload or the next PUT; startup-bound fields are
# read only when the process starts. Complete field documentation lives in
# config.example.yaml.
`

// EnsureFile creates configPath from config.example.yaml in the same
// directory when it does not exist yet, and reports whether it did. The
// example is copied verbatim (comments included) after it passes Load without
// AMDL_* overlays, so environment settings are never baked into the file.
func EnsureFile(configPath string) (created bool, err error) {
	exists, err := fileExists(configPath)
	if err != nil || exists {
		return false, err
	}
	if err := bootstrapConfig(configPath); err != nil {
		return false, err
	}
	return true, nil
}

func bootstrapConfig(configPath string) error {
	examplePath := filepath.Join(filepath.Dir(configPath), configExampleFileName)
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		return fmt.Errorf("config file %s does not exist and bootstrapping from %s failed: %w", configPath, examplePath, err)
	}
	if _, err := load(examplePath, nil); err != nil {
		return fmt.Errorf("config file %s does not exist and bootstrapping from %s failed: %w", configPath, examplePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(configPath, raw, 0o600)
}

// saveConfig writes the complete configuration to path, owner-only because
// the file may hold a media-user-token.
func saveConfig(path string, cfg Config) error {
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, append([]byte(managedFileHeader), out...), 0o600)
}

// saveRuntimeFields rewrites the combined file with cfg's runtime values while
// retaining startup-bound values currently on disk. This prevents an API PUT
// from undoing a valid config.yaml edit that is waiting for the next restart.
// Every key an AMDL_* variable in environ pins keeps the file's own value
// instead of cfg's effective one: the overlay is re-applied on every load, so
// writing it would pin the environment value even after the variable is unset.
func saveRuntimeFields(path string, cfg Config, environ []string) error {
	disk := cfg
	fileValues := Default()
	if exists, err := fileExists(path); err != nil {
		return err
	} else if exists {
		loaded, err := load(path, nil)
		if err != nil {
			return fmt.Errorf("reload startup-bound config fields before save: %w", err)
		}
		fileValues = loaded
		disk = loaded
		copyRuntimeFields(&disk, cfg)
	}
	restorePinnedFields(&disk, fileValues, environ)
	return saveConfig(path, disk)
}

// restorePinnedFields puts fileValues back into every field of disk whose
// AMDL_* variable is set in environ. fileValues is the file's content (or the
// defaults a missing file falls back to) with no environment overlay applied,
// so an env-pinned key is saved as what the file itself says.
func restorePinnedFields(disk *Config, fileValues Config, environ []string) {
	set := make(map[string]bool, len(environ))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		set[name] = true
	}
	diskValue := reflect.ValueOf(disk).Elem()
	fileValue := reflect.ValueOf(fileValues)
	for _, field := range envFields() {
		if set[field.name] {
			diskValue.FieldByIndex(field.index).Set(fileValue.FieldByIndex(field.index))
		}
	}
}

// writeFileAtomic writes data via a random temporary file and rename, so a
// crash cannot leave a truncated config and a pre-planted fixed-name symlink
// cannot redirect the write.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else {
		return false, err
	}
}
