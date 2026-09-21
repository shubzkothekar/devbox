package registry

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubzkothekar/devbox/internal/config"
)

// initFixtureRepo initializes a local Git repository at a temporary
// directory containing a single commit with plugins/example/plugin.yaml,
// using a fixture author identity so the test never depends on the host's
// global Git configuration or network access. It returns the repository
// directory and the resulting commit SHA.
func initFixtureRepo(t *testing.T) (dir string, commit string) {
	t.Helper()
	dir = t.TempDir()

	runFixtureGit(t, dir, "init", "-b", "main")
	runFixtureGit(t, dir, "config", "user.name", "DevBox Fixture")
	runFixtureGit(t, dir, "config", "user.email", "fixture@example.com")

	manifestPath := filepath.Join(dir, "plugins", "example", "plugin.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("id: example\nversion: 1.0.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	runFixtureGit(t, dir, "add", "-A")
	runFixtureGit(t, dir, "commit", "-m", "add example plugin")

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

func newProjectState(t *testing.T, root string) config.ProjectState {
	t.Helper()
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return config.ProjectState{
		Root: root,
		Config: config.ProjectConfig{
			Registry: config.RegistryConfig{Source: "git", URL: "unused", Ref: "main"},
		},
	}
}

func TestMaterializeReadOnlyRejectsMissingSnapshot(t *testing.T) {
	_, commit := initFixtureRepo(t)

	projectRoot := t.TempDir()
	state := newProjectState(t, projectRoot)
	state.Lock = &config.RegistryLock{
		Registry: config.LockedRegistry{URL: "unused", Ref: "main", Commit: commit},
	}

	_, err := Materialize(context.Background(), state, ReadOnly)
	if err == nil {
		t.Fatal("error = nil, want error because .generated/registry snapshot is absent")
	}
	if !strings.Contains(err.Error(), "devbox plugin install") && !strings.Contains(err.Error(), "devbox plugin update") {
		t.Fatalf("error = %v, want guidance to run install/update", err)
	}
}

func TestMaterializeReadOnlyRejectsStaleSnapshot(t *testing.T) {
	fixtureRepo, firstCommit := initFixtureRepo(t)

	projectRoot := t.TempDir()
	state := newProjectState(t, projectRoot)
	state.Config.Registry.URL = fixtureRepo
	state.Config.Registry.Ref = "main"

	// Materialize a snapshot at firstCommit.
	if _, err := Materialize(context.Background(), state, Install); err != nil {
		t.Fatalf("Materialize(Install): %v", err)
	}

	// Advance the fixture registry so the lock's commit no longer matches
	// the already-materialized snapshot.
	extraPath := filepath.Join(fixtureRepo, "plugins", "example", "extra.txt")
	if err := os.WriteFile(extraPath, []byte("extra\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runFixtureGit(t, fixtureRepo, "add", "-A")
	runFixtureGit(t, fixtureRepo, "commit", "-m", "add extra file")
	secondCommit := strings.TrimSpace(runFixtureGitOutput(t, fixtureRepo, "rev-parse", "HEAD"))
	if secondCommit == firstCommit {
		t.Fatal("expected second commit to differ from first")
	}

	state.Lock = &config.RegistryLock{
		Registry: config.LockedRegistry{URL: fixtureRepo, Ref: "main", Commit: secondCommit},
	}

	_, err := Materialize(context.Background(), state, ReadOnly)
	if err == nil {
		t.Fatal("error = nil, want error because snapshot commit does not match locked commit")
	}
	if !strings.Contains(err.Error(), "devbox plugin update") {
		t.Fatalf("error = %v, want guidance to run devbox plugin update", err)
	}
}

func TestMaterializeInstallWritesSnapshotAtLockedCommit(t *testing.T) {
	fixtureRepo, commit := initFixtureRepo(t)

	projectRoot := t.TempDir()
	state := newProjectState(t, projectRoot)
	state.Config.Registry.URL = fixtureRepo
	state.Config.Registry.Ref = "main"

	source, err := Materialize(context.Background(), state, Install)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if source.Mode != "git" {
		t.Fatalf("Mode = %q, want %q", source.Mode, "git")
	}

	wantRoot := filepath.Join(projectRoot, ".generated", "registry")
	if source.Root != wantRoot {
		t.Fatalf("Root = %q, want %q", source.Root, wantRoot)
	}

	head := strings.TrimSpace(runFixtureGitOutput(t, source.Root, "rev-parse", "HEAD"))
	if head != commit {
		t.Fatalf("snapshot HEAD = %q, want %q", head, commit)
	}

	manifestPath := filepath.Join(source.Root, "plugins", "example", "plugin.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("snapshot missing manifest: %v", err)
	}

	// Now that Install has produced a snapshot, ReadOnly should succeed
	// against a matching lock without any further network/clone access.
	state.Lock = &config.RegistryLock{
		Registry: config.LockedRegistry{URL: fixtureRepo, Ref: "main", Commit: commit},
	}
	readOnlySource, err := Materialize(context.Background(), state, ReadOnly)
	if err != nil {
		t.Fatalf("Materialize(ReadOnly) after install: %v", err)
	}
	if readOnlySource.Root != wantRoot || readOnlySource.Mode != "git" {
		t.Fatalf("ReadOnly source = %+v, want root %q mode %q", readOnlySource, wantRoot, "git")
	}
}

func TestMaterializeUpdateReplacesExistingSnapshot(t *testing.T) {
	fixtureRepo, firstCommit := initFixtureRepo(t)

	projectRoot := t.TempDir()
	state := newProjectState(t, projectRoot)
	state.Config.Registry.URL = fixtureRepo
	state.Config.Registry.Ref = "main"

	if _, err := Materialize(context.Background(), state, Install); err != nil {
		t.Fatalf("initial Materialize(Install): %v", err)
	}

	// Advance the fixture registry with a second commit.
	extraPath := filepath.Join(fixtureRepo, "plugins", "example", "extra.txt")
	if err := os.WriteFile(extraPath, []byte("extra\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runFixtureGit(t, fixtureRepo, "add", "-A")
	runFixtureGit(t, fixtureRepo, "commit", "-m", "add extra file")
	secondCommit := strings.TrimSpace(runFixtureGitOutput(t, fixtureRepo, "rev-parse", "HEAD"))
	if secondCommit == firstCommit {
		t.Fatal("expected second commit to differ from first")
	}

	source, err := Materialize(context.Background(), state, Update)
	if err != nil {
		t.Fatalf("Materialize(Update): %v", err)
	}

	head := strings.TrimSpace(runFixtureGitOutput(t, source.Root, "rev-parse", "HEAD"))
	if head != secondCommit {
		t.Fatalf("snapshot HEAD after update = %q, want %q", head, secondCommit)
	}
}

func TestMaterializeLocalOverrideDoesNotRequireLock(t *testing.T) {
	overrideDir := t.TempDir()
	manifestPath := filepath.Join(overrideDir, "plugins", "example", "plugin.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("id: example\nversion: 1.0.0\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	projectRoot := t.TempDir()
	state := newProjectState(t, projectRoot)
	state.Override = &config.LocalOverride{
		Registry: config.LocalRegistry{Source: "path", Path: overrideDir},
	}
	// No lock file set at all; local override must not require one.

	source, err := Materialize(context.Background(), state, ReadOnly)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if source.Mode != "path" {
		t.Fatalf("Mode = %q, want %q", source.Mode, "path")
	}
	resolvedOverride, err := filepath.EvalSymlinks(overrideDir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if source.Root != resolvedOverride {
		t.Fatalf("Root = %q, want %q", source.Root, resolvedOverride)
	}
}

func TestMaterializeRejectsNonDirectoryOverride(t *testing.T) {
	projectRoot := t.TempDir()
	filePath := filepath.Join(projectRoot, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	state := newProjectState(t, projectRoot)
	state.Override = &config.LocalOverride{
		Registry: config.LocalRegistry{Source: "path", Path: filePath},
	}

	_, err := Materialize(context.Background(), state, ReadOnly)
	if err == nil {
		t.Fatal("error = nil, want error for non-directory override path")
	}
}
