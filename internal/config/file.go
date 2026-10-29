package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const configExampleFileName = "config.example.yaml"

// managedFileHeader tops config.yaml after the runtime config API rewrites it.
// Detailed field documentation remains in config.example.yaml because YAML
// marshaling does not preserve comments.
const managedFileHeader = `# Managed by the amdl backend: PUT /api/v1/config rewrites this file and
# does not preserve comments. Hand edits to runtime-mutable fields apply on
# POST /api/v1/config/reload or the next PUT; startup-bound fields are
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
// from undoing a valid config.yaml edit that is waiting for the next restart,
// and keeps the AMDL_* overrides (all startup keys) out of the file. A missing
// file is recreated from the defaults plus cfg's runtime values, the same
// startup values a restart would bootstrap from the example.
func saveRuntimeFields(path string, cfg Config) error {
	disk := Default()
	if exists, err := fileExists(path); err != nil {
		return err
	} else if exists {
		loaded, err := load(path, nil)
		if err != nil {
			return fmt.Errorf("reload startup-bound config fields before save: %w", err)
		}
		disk = loaded
	}
	copyRuntimeFields(&disk, cfg)
	return saveConfig(path, disk)
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
