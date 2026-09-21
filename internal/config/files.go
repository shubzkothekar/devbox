package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

const (
	projectConfigFile = "devbox.plugins.yml"
	registryLockFile  = "devbox.plugins.lock.yml"
	localOverrideFile = "devbox.plugins.local.yml"
)

var lockCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// LoadProject reads and strictly validates a DevBox project's plugin
// configuration rooted at root. It requires devbox.plugins.yml and
// optionally loads devbox.plugins.lock.yml and devbox.plugins.local.yml
// when present.
func LoadProject(root string) (ProjectState, error) {
	state := ProjectState{Root: root}

	configPath := filepath.Join(root, projectConfigFile)
	if err := decodeStrictYAMLFile(configPath, &state.Config); err != nil {
		return ProjectState{}, fmt.Errorf("load %s: %w", projectConfigFile, err)
	}
	if err := validateProjectConfig(state.Config); err != nil {
		return ProjectState{}, fmt.Errorf("validate %s: %w", projectConfigFile, err)
	}

	lockPath := filepath.Join(root, registryLockFile)
	if exists, err := fileExists(lockPath); err != nil {
		return ProjectState{}, err
	} else if exists {
		var lock RegistryLock
		if err := decodeStrictYAMLFile(lockPath, &lock); err != nil {
			return ProjectState{}, fmt.Errorf("load %s: %w", registryLockFile, err)
		}
		if !lockCommitPattern.MatchString(lock.Registry.Commit) {
			return ProjectState{}, fmt.Errorf("validate %s: registry.commit %q is not a 40-character git commit hash", registryLockFile, lock.Registry.Commit)
		}
		state.Lock = &lock
	}

	overridePath := filepath.Join(root, localOverrideFile)
	if exists, err := fileExists(overridePath); err != nil {
		return ProjectState{}, err
	} else if exists {
		var override LocalOverride
		if err := decodeStrictYAMLFile(overridePath, &override); err != nil {
			return ProjectState{}, fmt.Errorf("load %s: %w", localOverrideFile, err)
		}
		state.Override = &override
	}

	return state, nil
}

func validateProjectConfig(cfg ProjectConfig) error {
	if cfg.Registry.Source != "git" {
		return fmt.Errorf("registry.source must be %q, got %q", "git", cfg.Registry.Source)
	}
	if cfg.Registry.URL == "" {
		return fmt.Errorf("registry.url must not be empty")
	}
	if cfg.Registry.Ref == "" {
		return fmt.Errorf("registry.ref must not be empty")
	}
	return nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// decodeStrictYAMLFile decodes the YAML document at path into out, rejecting
// unknown fields.
func decodeStrictYAMLFile(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err := decoder.Decode(out); err != nil {
		return err
	}
	return nil
}

// WriteYAMLAtomic marshals value as YAML and writes it to path atomically:
// it creates a temporary file in the same directory, writes the content
// with mode 0644, closes it, then renames it over path.
func WriteYAMLAtomic(path string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal yaml for %s: %w", path, err)
	}
	return writeFileAtomic(path, data)
}

// WriteJSONAtomic marshals value as JSON and writes it to path atomically:
// it creates a temporary file in the same directory, writes the content
// with mode 0644, closes it, then renames it over path.
func WriteJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json for %s: %w", path, err)
	}
	return writeFileAtomic(path, data)
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file to %s: %w", path, err)
	}
	return nil
}
