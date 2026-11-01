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
# does not preserve comments. Hand edits to runtime keys apply on
# POST /api/v1/config/reload or the next PUT; the other keys are read only
# when the backend starts. Complete field documentation lives in
# config.example.yaml.
`

// EnsureFile creates configPath from example when it does not exist yet,
// with each seed's key set to its value; the rest of the example, comments
// included, is written verbatim. When the file exists, the seeds do not
// apply, except that a loopback server.listen is rewritten to a server.listen
// seed that is not loopback (reconcileSeeds); Bootstrap reports that and the
// seeds the file differs from. Callers pass configs.Example.
func EnsureFile(configPath string, example []byte, seeds ...Seed) (Bootstrap, error) {
	exists, err := fileExists(configPath)
	if err != nil {
		return Bootstrap{}, err
	}
	if exists {
		if len(seeds) == 0 {
			return Bootstrap{}, nil
		}
		raw, err := os.ReadFile(configPath)
		if err != nil {
			return Bootstrap{}, err
		}
		out, b, err := reconcileSeeds(configPath, raw, seeds)
		if err != nil || out == nil {
			return b, err
		}
		return b, writeFileAtomic(configPath, out, 0o600)
	}
	if _, err := loadBytes(configExampleFileName, example); err != nil {
		return Bootstrap{}, fmt.Errorf("config file %s does not exist and the example to create it from is invalid: %w", configPath, err)
	}
	content := example
	if len(seeds) > 0 {
		if content, err = applySeeds(configExampleFileName, example, seeds); err != nil {
			return Bootstrap{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return Bootstrap{}, err
	}
	if err := writeFileAtomic(configPath, content, 0o600); err != nil {
		return Bootstrap{}, err
	}
	return Bootstrap{Created: true}, nil
}

// FromExample parses example as EnsureFile would write it: where a client
// that writes its own config.yaml starts, so a new install gets the
// example's values (such as its {SongNumber} album layout) rather than
// Default's, which keep older installs' layout. Callers pass configs.Example.
func FromExample(example []byte) (Config, error) {
	return loadBytes(configExampleFileName, example)
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

// saveEditableFields rewrites the combined file with cfg's runtime and
// restart values while retaining the file-class values on disk, so an API PUT
// never undoes a config.yaml edit of server.listen, database.path or
// tools.ffmpeg that waits for the next restart. A missing file is recreated
// from the defaults plus cfg's runtime and restart values.
func saveEditableFields(path string, cfg Config) error {
	disk := Default()
	if exists, err := fileExists(path); err != nil {
		return err
	} else if exists {
		loaded, err := Load(path)
		if err != nil {
			return fmt.Errorf("reload file-only config fields before save: %w", err)
		}
		disk = loaded
	}
	copyFields(&disk, cfg, isEditable)
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
