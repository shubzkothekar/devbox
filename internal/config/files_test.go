package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile(%s): %v", path, err)
	}
}

func TestLoadProjectReadsStrictPluginConfiguration(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "devbox.plugins.yml"), `registry:
  source: git
  url: https://example.test/devbox-registry.git
  ref: v1.0.0
plugins:
  docker:
    enabled: true
    options:
      compose: true
`)

	state, err := LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Config.Plugins["docker"].Options["compose"]; got != true {
		t.Fatalf("compose = %#v, want true", got)
	}
}

func TestLoadProjectRejectsUnknownConfigurationFields(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "devbox.plugins.yml"), `registry:
  source: git
  url: https://example.test/registry.git
  ref: main
unexpected: value
`)

	_, err := LoadProject(root)
	if err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("error = %v, want unknown-field error", err)
	}
}
