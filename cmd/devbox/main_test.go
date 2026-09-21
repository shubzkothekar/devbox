package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubzkothekar/devbox/internal/config"
)

func runFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, stderr.String())
	}
	return stdout.String()
}

func initTestRegistry(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	runFixtureGit(t, dir, "init", "-b", "main")
	runFixtureGit(t, dir, "config", "user.name", "DevBox Test")
	runFixtureGit(t, dir, "config", "user.email", "test@example.com")

	for rel, content := range files {
		path := filepath.Join(dir, "plugins", rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	runFixtureGit(t, dir, "add", "-A")
	runFixtureGit(t, dir, "commit", "-m", "init test registry")
	return dir
}

func newCLIProject(t *testing.T, registryURL string, ref string) string {
	t.Helper()
	root := t.TempDir()
	cfg := config.ProjectConfig{
		Registry: config.RegistryConfig{
			Source: "git",
			URL:    registryURL,
			Ref:    ref,
		},
		Plugins: map[string]config.PluginSelection{},
	}
	if err := config.WriteYAMLAtomic(filepath.Join(root, "devbox.plugins.yml"), cfg); err != nil {
		t.Fatalf("WriteYAMLAtomic: %v", err)
	}
	return root
}

func TestCLISyntaxValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "no args",
			args:    []string{},
			wantErr: "usage: devbox create <container-name> | devbox plugin <command>",
		},
		{
			name:    "create missing container name",
			args:    []string{"create"},
			wantErr: "usage: devbox create <container-name>",
		},
		{
			name:    "create missing destination value",
			args:    []string{"create", "my-app", "--destination"},
			wantErr: "usage: devbox create <container-name>",
		},
		{
			name:    "create missing ref value",
			args:    []string{"create", "my-app", "--ref"},
			wantErr: "usage: devbox create <container-name>",
		},
		{
			name:    "create unknown flag",
			args:    []string{"create", "my-app", "--unknown"},
			wantErr: `unknown flag "--unknown"`,
		},
		{
			name:    "create extra positional args",
			args:    []string{"create", "my-app", "extra"},
			wantErr: "usage: devbox create <container-name>",
		},
		{
			name:    "create flag after extra positional args",
			args:    []string{"create", "my-app", "extra", "--ref", "main"},
			wantErr: "usage: devbox create <container-name>",
		},
		{
			name:    "unsupported top command",
			args:    []string{"unknown"},
			wantErr: `unsupported command "unknown"`,
		},
		{
			name:    "plugin missing subcommand",
			args:    []string{"plugin"},
			wantErr: "usage: devbox plugin <command>",
		},
		{
			name:    "plugin missing project root value",
			args:    []string{"plugin", "resolve", "--project-root"},
			wantErr: "usage: devbox plugin resolve [--project-root <path>]",
		},
		{
			name:    "plugin list extra args",
			args:    []string{"plugin", "list", "extra"},
			wantErr: "usage: devbox plugin list",
		},
		{
			name:    "plugin installed extra args",
			args:    []string{"plugin", "installed", "extra"},
			wantErr: "usage: devbox plugin installed",
		},
		{
			name:    "plugin install missing id",
			args:    []string{"plugin", "install"},
			wantErr: "usage: devbox plugin install <plugin-id>",
		},
		{
			name:    "plugin install extra args",
			args:    []string{"plugin", "install", "p1", "p2"},
			wantErr: "usage: devbox plugin install <plugin-id>",
		},
		{
			name:    "plugin uninstall missing id",
			args:    []string{"plugin", "uninstall"},
			wantErr: "usage: devbox plugin uninstall <plugin-id>",
		},
		{
			name:    "plugin uninstall extra args",
			args:    []string{"plugin", "uninstall", "p1", "p2"},
			wantErr: "usage: devbox plugin uninstall <plugin-id>",
		},
		{
			name:    "plugin update extra args",
			args:    []string{"plugin", "update", "p1", "p2"},
			wantErr: "usage: devbox plugin update [plugin-id]",
		},
		{
			name:    "plugin resolve extra args",
			args:    []string{"plugin", "resolve", "extra"},
			wantErr: "usage: devbox plugin resolve [--project-root <path>]",
		},
		{
			name:    "plugin dispatch missing command",
			args:    []string{"plugin", "some-plugin"},
			wantErr: "usage: devbox plugin <plugin-id> <command> [args...]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tc.args, &stdout, &stderr)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %q, want containing %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestCLIPluginLifecycleCommands(t *testing.T) {
	regDir := initTestRegistry(t, map[string]string{
		"docker/plugin.yaml": `id: docker
version: 1.0.0
description: Docker container runtime
commands:
  status:
    path: commands/status.sh
`,
		"docker/commands/status.sh": "#!/bin/sh\necho ok\n",
	})
	projectRoot := newCLIProject(t, regDir, "main")

	// 1. devbox plugin install docker
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"plugin", "install", "docker", "--project-root=" + projectRoot}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("plugin install docker: %v", err)
		}
		if !strings.Contains(stdout.String(), `Installed plugin "docker"`) {
			t.Fatalf("unexpected install output: %s", stdout.String())
		}
	}

	// 2. devbox plugin list --project-root <root>
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"plugin", "list", "--project-root", projectRoot}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("plugin list: %v", err)
		}
		out := stdout.String()
		if !strings.Contains(out, "Registry source mode: git") {
			t.Fatalf("expected git source mode, got: %s", out)
		}
		if !strings.Contains(out, "docker (1.0.0) - Docker container runtime") {
			t.Fatalf("expected docker catalog entry, got: %s", out)
		}
	}

	// Verify plan and lock files exist
	planFile := filepath.Join(projectRoot, ".generated", "plugins", "plan.json")
	if _, err := os.Stat(planFile); err != nil {
		t.Fatalf("expected plan.json to exist: %v", err)
	}
	lockFile := filepath.Join(projectRoot, "devbox.plugins.lock.yml")
	if _, err := os.Stat(lockFile); err != nil {
		t.Fatalf("expected lock file to exist: %v", err)
	}

	// 4. devbox plugin installed (after install)
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"plugin", "installed", "--project-root", projectRoot}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("plugin installed: %v", err)
		}
		out := stdout.String()
		if !strings.Contains(out, "docker (1.0.0)") {
			t.Fatalf("expected docker in installed output, got: %s", out)
		}
	}

	// 5. devbox plugin resolve
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"plugin", "resolve", "--project-root", projectRoot}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("plugin resolve: %v", err)
		}
		if !strings.Contains(stdout.String(), "Resolved 1 plugin(s)") {
			t.Fatalf("unexpected resolve output: %s", stdout.String())
		}
	}

	// 6. devbox plugin update docker
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"plugin", "update", "docker", "--project-root", projectRoot}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("plugin update docker: %v", err)
		}
		if !strings.Contains(stdout.String(), `Updated plugin "docker"`) {
			t.Fatalf("unexpected update output: %s", stdout.String())
		}
	}

	// 7. devbox plugin update (all)
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"plugin", "update", "--project-root", projectRoot}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("plugin update all: %v", err)
		}
		if !strings.Contains(stdout.String(), "Updated all plugins") {
			t.Fatalf("unexpected update all output: %s", stdout.String())
		}
	}

	// 8. devbox plugin docker status (command dispatch)
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"plugin", "docker", "status", "--project-root", projectRoot}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("plugin docker status: %v", err)
		}
	}

	// 9. devbox plugin docker nonexistent (command dispatch failure)
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"plugin", "docker", "nonexistent", "--project-root", projectRoot}, &stdout, &stderr)
		if err == nil {
			t.Fatal("expected error for nonexistent command, got nil")
		}
	}

	// 10. devbox plugin uninstall docker
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"plugin", "uninstall", "docker", "--project-root", projectRoot}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("plugin uninstall docker: %v", err)
		}
		if !strings.Contains(stdout.String(), `Uninstalled plugin "docker"`) {
			t.Fatalf("unexpected uninstall output: %s", stdout.String())
		}
	}

	// 11. devbox plugin installed (after uninstall)
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"plugin", "installed", "--project-root", projectRoot}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("plugin installed: %v", err)
		}
		if strings.Contains(stdout.String(), "docker") {
			t.Fatalf("expected docker to be uninstalled, got: %s", stdout.String())
		}
	}
}

func TestCLIPluginLocalOverride(t *testing.T) {
	localRegDir := t.TempDir()
	dockerDir := filepath.Join(localRegDir, "plugins", "docker")
	if err := os.MkdirAll(dockerDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := "id: docker\nversion: 2.0.0\ndescription: Local Docker\n"
	if err := os.WriteFile(filepath.Join(dockerDir, "plugin.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	projectRoot := newCLIProject(t, "https://example.test/unused.git", "main")
	override := config.LocalOverride{
		Registry: config.LocalRegistry{
			Source: "path",
			Path:   localRegDir,
		},
	}
	if err := config.WriteYAMLAtomic(filepath.Join(projectRoot, "devbox.plugins.local.yml"), override); err != nil {
		t.Fatalf("WriteYAMLAtomic: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{"plugin", "list", "--project-root", projectRoot}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("plugin list: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Registry source mode: path") {
		t.Fatalf("expected path source mode, got: %s", out)
	}
	if !strings.Contains(out, "docker (2.0.0) - Local Docker") {
		t.Fatalf("expected local docker catalog entry, got: %s", out)
	}
}

func fixtureScaffoldRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runFixtureGit(t, dir, "init", "-b", "main")
	runFixtureGit(t, dir, "config", "user.name", "DevBox Test")
	runFixtureGit(t, dir, "config", "user.email", "test@example.com")

	envExample := filepath.Join(dir, ".env.example")
	if err := os.WriteFile(envExample, []byte("INSTALL_GO=true\n"), 0644); err != nil {
		t.Fatalf("WriteFile(.env.example): %v", err)
	}

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# Scaffold\n"), 0644); err != nil {
		t.Fatalf("WriteFile(README.md): %v", err)
	}

	runFixtureGit(t, dir, "add", "-A")
	runFixtureGit(t, dir, "commit", "-m", "initial scaffold commit")
	return dir
}

func TestCLICreate(t *testing.T) {
	scaffold := fixtureScaffoldRepository(t)
	t.Setenv("DEVBOX_SCAFFOLD_URL", scaffold)

	// 1. Successful create with explicit destination and ref
	targetDir := filepath.Join(t.TempDir(), "custom-api")
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"create", "custom-api", "--destination", targetDir, "--ref", "main"}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
		expectedOut := fmt.Sprintf("Created DevBox project: %s\nNext:\n  cd %s\n  devbox plugin install <plugin-id>\n", targetDir, targetDir)
		if stdout.String() != expectedOut {
			t.Fatalf("stdout = %q, want %q", stdout.String(), expectedOut)
		}

		if _, err := os.Stat(filepath.Join(targetDir, ".git")); err != nil {
			t.Fatalf(".git missing: %v", err)
		}
		envPath := filepath.Join(targetDir, ".env")
		data, err := os.ReadFile(envPath)
		if err != nil {
			t.Fatalf(".env missing: %v", err)
		}
		if string(data) != "INSTALL_GO=true\n" {
			t.Fatalf(".env content = %q", string(data))
		}
		info, err := os.Stat(envPath)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Fatalf(".env perm = %#o, want 0600", perm)
		}
	}

	// 2. Reject existing destination
	{
		var stdout, stderr bytes.Buffer
		err := run([]string{"create", "custom-api", "--destination", targetDir}, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("expected already exists error, got %v", err)
		}
	}

	// 3. Successful create with default destination
	{
		tmpDir := t.TempDir()
		origDir, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chdir(origDir) }()

		var stdout, stderr bytes.Buffer
		err = run([]string{"create", "default-api"}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("create with default dest failed: %v", err)
		}
		expectedOut := "Created DevBox project: ./default-api\nNext:\n  cd ./default-api\n  devbox plugin install <plugin-id>\n"
		if stdout.String() != expectedOut {
			t.Fatalf("stdout = %q, want %q", stdout.String(), expectedOut)
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "default-api", ".git")); err != nil {
			t.Fatalf("cloned .git missing: %v", err)
		}
	}
}

