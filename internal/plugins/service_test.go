package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/shubzkothekar/devbox/internal/config"
)

var lockCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func initGitRegistry(t *testing.T, files map[string]string) (dir string, commit string) {
	t.Helper()
	dir = t.TempDir()

	runFixtureGit(t, dir, "init", "-b", "main")
	runFixtureGit(t, dir, "config", "user.name", "DevBox Fixture")
	runFixtureGit(t, dir, "config", "user.email", "fixture@example.com")

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
	runFixtureGit(t, dir, "commit", "-m", "init registry fixture")
	commit = strings.TrimSpace(runFixtureGitOutput(t, dir, "rev-parse", "HEAD"))
	return dir, commit
}

func runFixtureGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	runFixtureGitOutput(t, dir, args...)
}

func runFixtureGitOutput(t *testing.T, dir string, args ...string) string {
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

func newTestProject(t *testing.T, registryURL string, ref string) string {
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

func TestInstallEnablesPluginWritesLockAndPlan(t *testing.T) {
	regDir, commit := initGitRegistry(t, map[string]string{
		"docker/plugin.yaml": "id: docker\nversion: 1.0.0\ndescription: Docker tooling\n",
	})
	projectRoot := newTestProject(t, regDir, "main")
	svc := NewService(projectRoot)

	if err := svc.Install(context.Background(), "docker"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	state, err := config.LoadProject(projectRoot)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if !state.Config.Plugins["docker"].Enabled {
		t.Fatalf("expected docker to be enabled")
	}
	if state.Lock == nil {
		t.Fatalf("expected lock file to be created")
	}
	if state.Lock.Registry.Commit != commit {
		t.Fatalf("lock commit = %q, want %q", state.Lock.Registry.Commit, commit)
	}
	if !lockCommitPattern.MatchString(state.Lock.Registry.Commit) {
		t.Fatalf("lock commit %q is not 40-hex", state.Lock.Registry.Commit)
	}

	planPath := filepath.Join(projectRoot, ".generated", "plugins", "plan.json")
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan.json: %v", err)
	}
	var plan config.ResolvedPlan
	if err := json.Unmarshal(planData, &plan); err != nil {
		t.Fatalf("unmarshal plan.json: %v", err)
	}
	if got, want := plan.PluginIDs(), []string{"docker"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan plugins = %v, want %v", got, want)
	}
}

func TestUninstallRejectsEnabledDependent(t *testing.T) {
	regDir, _ := initGitRegistry(t, map[string]string{
		"base/plugin.yaml":   "id: base\nversion: 1.0.0\n",
		"docker/plugin.yaml": "id: docker\nversion: 1.0.0\nrequires: [base]\n",
	})
	projectRoot := newTestProject(t, regDir, "main")
	svc := NewService(projectRoot)

	if err := svc.Install(context.Background(), "base"); err != nil {
		t.Fatalf("Install base: %v", err)
	}
	if err := svc.Install(context.Background(), "docker"); err != nil {
		t.Fatalf("Install docker: %v", err)
	}

	err := svc.Uninstall(context.Background(), "base")
	if err == nil {
		t.Fatal("expected error uninstalling base because docker depends on it, got nil")
	}
	if !strings.Contains(err.Error(), "docker") {
		t.Fatalf("expected error mentioning docker, got: %v", err)
	}

	state, err := config.LoadProject(projectRoot)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if !state.Config.Plugins["base"].Enabled {
		t.Fatalf("expected base to still be enabled after failed uninstall")
	}

	if err := svc.Uninstall(context.Background(), "docker"); err != nil {
		t.Fatalf("Uninstall docker: %v", err)
	}

	if err := svc.Uninstall(context.Background(), "base"); err != nil {
		t.Fatalf("Uninstall base: %v", err)
	}

	state, err = config.LoadProject(projectRoot)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if state.Config.Plugins["base"].Enabled {
		t.Fatalf("expected base to be disabled")
	}
	if state.Lock == nil {
		t.Fatalf("expected lock file to be retained")
	}
}

func TestListUsesReadOnlyAndShowsMode(t *testing.T) {
	regDir, _ := initGitRegistry(t, map[string]string{
		"docker/plugin.yaml": "id: docker\nversion: 1.2.3\ndescription: Docker container tooling\n",
	})
	projectRoot := newTestProject(t, regDir, "main")
	svc := NewService(projectRoot)
	if err := svc.Install(context.Background(), "docker"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Point registry URL to invalid network URL to guarantee no network call succeeds
	state, err := config.LoadProject(projectRoot)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	state.Config.Registry.URL = "https://invalid.example.com/nonexistent/repo.git"
	if err := config.WriteYAMLAtomic(filepath.Join(projectRoot, "devbox.plugins.yml"), state.Config); err != nil {
		t.Fatalf("WriteYAMLAtomic: %v", err)
	}

	catalog, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(catalog) != 1 || catalog[0].ID != "docker" || catalog[0].Version != "1.2.3" || catalog[0].Description != "Docker container tooling" {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}

	mode, err := svc.SourceMode(context.Background())
	if err != nil {
		t.Fatalf("SourceMode: %v", err)
	}
	if mode != "git" {
		t.Fatalf("mode = %q, want 'git'", mode)
	}

	// Now add a local override
	override := config.LocalOverride{
		Registry: config.LocalRegistry{
			Source: "path",
			Path:   regDir,
		},
	}
	if err := config.WriteYAMLAtomic(filepath.Join(projectRoot, "devbox.plugins.local.yml"), override); err != nil {
		t.Fatalf("WriteYAMLAtomic: %v", err)
	}

	mode, err = svc.SourceMode(context.Background())
	if err != nil {
		t.Fatalf("SourceMode: %v", err)
	}
	if mode != "path" {
		t.Fatalf("mode = %q, want 'path'", mode)
	}
}

func TestInstalledReturnsOnlyEnabled(t *testing.T) {
	regDir, _ := initGitRegistry(t, map[string]string{
		"docker/plugin.yaml":    "id: docker\nversion: 1.0.0\n",
		"terraform/plugin.yaml": "id: terraform\nversion: 2.0.0\n",
	})
	projectRoot := newTestProject(t, regDir, "main")
	svc := NewService(projectRoot)
	if err := svc.Install(context.Background(), "docker"); err != nil {
		t.Fatalf("Install docker: %v", err)
	}
	if err := svc.Install(context.Background(), "terraform"); err != nil {
		t.Fatalf("Install terraform: %v", err)
	}
	if err := svc.Uninstall(context.Background(), "terraform"); err != nil {
		t.Fatalf("Uninstall terraform: %v", err)
	}

	installed, err := svc.Installed(context.Background())
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if len(installed) != 1 {
		t.Fatalf("expected 1 installed plugin, got %d: %+v", len(installed), installed)
	}
	if installed[0].ID != "docker" || !installed[0].Enabled || installed[0].Version != "1.0.0" {
		t.Fatalf("unexpected installed plugin: %+v", installed[0])
	}
}

func TestUpdateReFetchesAndRewritesLock(t *testing.T) {
	regDir, firstCommit := initGitRegistry(t, map[string]string{
		"docker/plugin.yaml": "id: docker\nversion: 1.0.0\n",
	})
	projectRoot := newTestProject(t, regDir, "main")
	svc := NewService(projectRoot)
	if err := svc.Install(context.Background(), "docker"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	state, err := config.LoadProject(projectRoot)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if state.Lock.Registry.Commit != firstCommit {
		t.Fatalf("initial lock commit = %q, want %q", state.Lock.Registry.Commit, firstCommit)
	}

	// Add a new commit to the registry
	manifestPath := filepath.Join(regDir, "plugins", "docker", "plugin.yaml")
	if err := os.WriteFile(manifestPath, []byte("id: docker\nversion: 1.1.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runFixtureGit(t, regDir, "add", "-A")
	runFixtureGit(t, regDir, "commit", "-m", "bump docker to 1.1.0")
	secondCommit := strings.TrimSpace(runFixtureGitOutput(t, regDir, "rev-parse", "HEAD"))

	if err := svc.Update(context.Background(), "docker"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	state, err = config.LoadProject(projectRoot)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if state.Lock.Registry.Commit != secondCommit {
		t.Fatalf("updated lock commit = %q, want %q", state.Lock.Registry.Commit, secondCommit)
	}

	planPath := filepath.Join(projectRoot, ".generated", "plugins", "plan.json")
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan.json: %v", err)
	}
	var plan config.ResolvedPlan
	if err := json.Unmarshal(planData, &plan); err != nil {
		t.Fatalf("unmarshal plan.json: %v", err)
	}
	if got, want := plan.PluginIDs(), []string{"docker"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan plugins = %v, want %v", got, want)
	}
}

func TestResolveGeneratedUsesReadOnly(t *testing.T) {
	regDir, _ := initGitRegistry(t, map[string]string{
		"docker/plugin.yaml": "id: docker\nversion: 1.0.0\n",
	})
	projectRoot := newTestProject(t, regDir, "main")
	svc := NewService(projectRoot)
	if err := svc.Install(context.Background(), "docker"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Remove plan.json so we verify ResolveGenerated writes it
	planPath := filepath.Join(projectRoot, ".generated", "plugins", "plan.json")
	if err := os.Remove(planPath); err != nil {
		t.Fatalf("Remove plan.json: %v", err)
	}

	// Point registry URL to invalid URL to ensure read-only without network
	state, err := config.LoadProject(projectRoot)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	state.Config.Registry.URL = "https://invalid.example.com/nonexistent/repo.git"
	if err := config.WriteYAMLAtomic(filepath.Join(projectRoot, "devbox.plugins.yml"), state.Config); err != nil {
		t.Fatalf("WriteYAMLAtomic: %v", err)
	}

	plan, err := svc.ResolveGenerated(context.Background())
	if err != nil {
		t.Fatalf("ResolveGenerated: %v", err)
	}
	if got, want := plan.PluginIDs(), []string{"docker"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plan plugins = %v, want %v", got, want)
	}

	if _, err := os.Stat(planPath); err != nil {
		t.Fatalf("expected plan.json to be created: %v", err)
	}
}

func TestFindCommandLocatesCommandAndUser(t *testing.T) {
	regDir, _ := initGitRegistry(t, map[string]string{
		"docker/plugin.yaml": `id: docker
version: 1.0.0
commands:
  status:
    path: commands/status.sh
    user: devbox
  restart:
    path: commands/restart.sh
    user: root
  info:
    path: commands/info.sh
`,
		"docker/commands/status.sh":  "#!/bin/sh\necho ok\n",
		"docker/commands/restart.sh": "#!/bin/sh\necho restart\n",
		"docker/commands/info.sh":    "#!/bin/sh\necho info\n",
	})
	projectRoot := newTestProject(t, regDir, "main")
	svc := NewService(projectRoot)
	if err := svc.Install(context.Background(), "docker"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	plugin, cmd, err := svc.FindCommand(context.Background(), "docker", "status")
	if err != nil {
		t.Fatalf("FindCommand status: %v", err)
	}
	if plugin.ID != "docker" {
		t.Fatalf("plugin ID = %q, want docker", plugin.ID)
	}
	if cmd.Path != "commands/status.sh" {
		t.Fatalf("cmd.Path = %q, want commands/status.sh", cmd.Path)
	}
	if cmd.User != "devbox" {
		t.Fatalf("cmd.User = %q, want devbox", cmd.User)
	}

	_, cmdRoot, err := svc.FindCommand(context.Background(), "docker", "restart")
	if err != nil {
		t.Fatalf("FindCommand restart: %v", err)
	}
	if cmdRoot.User != "root" {
		t.Fatalf("cmdRoot.User = %q, want root", cmdRoot.User)
	}

	_, cmdDefault, err := svc.FindCommand(context.Background(), "docker", "info")
	if err != nil {
		t.Fatalf("FindCommand info: %v", err)
	}
	if cmdDefault.User != "devbox" {
		t.Fatalf("cmdDefault.User = %q, want devbox (default)", cmdDefault.User)
	}

	_, _, err = svc.FindCommand(context.Background(), "docker", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent command, got nil")
	}

	_, _, err = svc.FindCommand(context.Background(), "nonexistent", "status")
	if err == nil {
		t.Fatal("expected error for nonexistent plugin, got nil")
	}
}
