package plugins

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shubzkothekar/devbox/internal/config"
	"github.com/shubzkothekar/devbox/internal/registry"
)

// fixtureSource creates a temporary directory laid out as a materialized
// registry snapshot (i.e. the shape produced by registry.Materialize) and
// writes files under its "plugins/" directory. files keys are paths
// relative to a single plugin's directory, e.g. "docker/plugin.yaml".
func fixtureSource(t *testing.T, files map[string]string) registry.Source {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, "plugins", rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}
	return registry.Source{Root: root, Mode: "git"}
}

func TestResolveOrdersDependenciesThenIDs(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"base/plugin.yaml": `id: base
version: 1.0.0
`,
		"docker/plugin.yaml": `id: docker
version: 1.0.0
requires: [base]
`,
		"alpha/plugin.yaml": `id: alpha
version: 1.0.0
`,
	})
	plan, err := Resolve(source, map[string]config.PluginSelection{
		"docker": {Enabled: true}, "base": {Enabled: true}, "alpha": {Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plan.PluginIDs(), []string{"alpha", "base", "docker"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if plan.Version != 1 {
		t.Fatalf("Version = %d, want 1", plan.Version)
	}
	if plan.Source.Root != source.Root || plan.Source.Mode != source.Mode {
		t.Fatalf("Source = %+v, want %+v", plan.Source, source)
	}
}

func TestResolveRejectsEscapingHookPath(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"bad/plugin.yaml": "id: bad\nversion: 1.0.0\nhooks:\n  build: ../escape.sh\n",
	})
	_, err := Resolve(source, map[string]config.PluginSelection{"bad": {Enabled: true}})
	if err == nil || !strings.Contains(err.Error(), "escapes plugin directory") {
		t.Fatalf("error = %v, want path safety error", err)
	}
}

func TestResolveRejectsUnknownSelectedPlugin(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"base/plugin.yaml": "id: base\nversion: 1.0.0\n",
	})
	_, err := Resolve(source, map[string]config.PluginSelection{"missing": {Enabled: true}})
	if err == nil || !strings.Contains(err.Error(), "unknown plugin") {
		t.Fatalf("error = %v, want unknown plugin error", err)
	}
}

func TestResolveRejectsMissingRequiredDependency(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"base/plugin.yaml": "id: base\nversion: 1.0.0\n",
		"docker/plugin.yaml": `id: docker
version: 1.0.0
requires: [base]
`,
	})
	_, err := Resolve(source, map[string]config.PluginSelection{
		"docker": {Enabled: true},
	})
	if err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("error = %v, want missing dependency error", err)
	}
}

func TestResolveRejectsConflictingPlugins(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"a/plugin.yaml": `id: a
version: 1.0.0
conflicts: [b]
`,
		"b/plugin.yaml": "id: b\nversion: 1.0.0\n",
	})
	_, err := Resolve(source, map[string]config.PluginSelection{
		"a": {Enabled: true}, "b": {Enabled: true},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("error = %v, want conflict error", err)
	}
}

func TestResolveRejectsDependencyCycle(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"a/plugin.yaml": "id: a\nversion: 1.0.0\nrequires: [b]\n",
		"b/plugin.yaml": "id: b\nversion: 1.0.0\nrequires: [a]\n",
	})
	_, err := Resolve(source, map[string]config.PluginSelection{
		"a": {Enabled: true}, "b": {Enabled: true},
	})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %v, want cycle error", err)
	}
}

func TestResolveRejectsManifestIDMismatch(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"base/plugin.yaml": "id: wrong-id\nversion: 1.0.0\n",
	})
	_, err := Resolve(source, map[string]config.PluginSelection{"base": {Enabled: true}})
	if err == nil || !strings.Contains(err.Error(), "wrong-id") {
		t.Fatalf("error = %v, want manifest id mismatch error", err)
	}
}

func TestResolveRejectsUnknownOption(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"base/plugin.yaml": "id: base\nversion: 1.0.0\n",
	})
	_, err := Resolve(source, map[string]config.PluginSelection{
		"base": {Enabled: true, Options: map[string]any{"nope": true}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("error = %v, want unknown option error", err)
	}
}

func TestResolveRejectsWrongOptionType(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"base/plugin.yaml": `id: base
version: 1.0.0
options:
  compose:
    type: boolean
    default: true
`,
	})
	_, err := Resolve(source, map[string]config.PluginSelection{
		"base": {Enabled: true, Options: map[string]any{"compose": "yes"}},
	})
	if err == nil || !strings.Contains(err.Error(), "compose") {
		t.Fatalf("error = %v, want option type error", err)
	}
}

func TestResolveAppliesOptionDefaultsAndOverrides(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"base/plugin.yaml": `id: base
version: 1.0.0
options:
  compose:
    type: boolean
    default: true
  name:
    type: string
    default: base
`,
	})
	plan, err := Resolve(source, map[string]config.PluginSelection{
		"base": {Enabled: true, Options: map[string]any{"name": "custom"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plugins) != 1 {
		t.Fatalf("Plugins = %v, want 1 entry", plan.Plugins)
	}
	got := plan.Plugins[0].Options
	if got["compose"] != true {
		t.Fatalf("compose = %#v, want true (default)", got["compose"])
	}
	if got["name"] != "custom" {
		t.Fatalf("name = %#v, want %q (override)", got["name"], "custom")
	}
}

func TestResolveIgnoresDisabledPlugins(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"base/plugin.yaml":      "id: base\nversion: 1.0.0\n",
		"terraform/plugin.yaml": "id: terraform\nversion: 1.0.0\n",
	})
	plan, err := Resolve(source, map[string]config.PluginSelection{
		"base":      {Enabled: true},
		"terraform": {Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plan.PluginIDs(), []string{"base"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestResolveResolvesValidHookAndCommandPaths(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"base/plugin.yaml": `id: base
version: 1.0.0
hooks:
  build: build.sh
  start: start.sh
commands:
  status:
    path: commands/status.sh
    user: devbox
`,
		"base/build.sh":           "#!/bin/sh\n",
		"base/start.sh":           "#!/bin/sh\n",
		"base/commands/status.sh": "#!/bin/sh\n",
	})
	plan, err := Resolve(source, map[string]config.PluginSelection{"base": {Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	plugin := plan.Plugins[0]
	if plugin.Build != "build.sh" {
		t.Fatalf("Build = %q, want %q", plugin.Build, "build.sh")
	}
	if plugin.Start != "start.sh" {
		t.Fatalf("Start = %q, want %q", plugin.Start, "start.sh")
	}
	cmd, ok := plugin.Commands["status"]
	if !ok {
		t.Fatalf("Commands = %v, want key %q", plugin.Commands, "status")
	}
	if cmd.Path != filepath.Join("commands", "status.sh") || cmd.User != "devbox" {
		t.Fatalf("cmd = %+v, want path %q user %q", cmd, filepath.Join("commands", "status.sh"), "devbox")
	}
}

func TestResolveRejectsMissingHookFile(t *testing.T) {
	source := fixtureSource(t, map[string]string{
		"base/plugin.yaml": "id: base\nversion: 1.0.0\nhooks:\n  build: missing.sh\n",
	})
	_, err := Resolve(source, map[string]config.PluginSelection{"base": {Enabled: true}})
	if err == nil {
		t.Fatal("error = nil, want error for missing hook file")
	}
}
