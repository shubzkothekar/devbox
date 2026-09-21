package project

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func writeFile(t *testing.T, dir, file, content string) {
	t.Helper()
	p := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", p, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

func TestCreateClonesScaffoldAndInitializesEnv(t *testing.T) {
	scaffold := fixtureScaffoldRepository(t)
	target := filepath.Join(t.TempDir(), "api")

	result, err := Create(context.Background(), CreateRequest{
		Name:        "api",
		Destination: target,
		Ref:         "main",
		ScaffoldURL: scaffold,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Root != target {
		t.Fatalf("root = %q, want %q", result.Root, target)
	}
	if result.Ref != "main" {
		t.Fatalf("ref = %q, want %q", result.Ref, "main")
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(target, ".env")); got != "INSTALL_GO=true\n" {
		t.Fatalf(".env = %q", got)
	}

	info, err := os.Stat(filepath.Join(target, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf(".env perm = %#o, want 0600", perm)
	}
}

func TestCreateRejectsExistingDestinationWithoutChangingIt(t *testing.T) {
	target := filepath.Join(t.TempDir(), "api")
	writeFile(t, target, "keep", "sentinel")

	_, err := Create(context.Background(), CreateRequest{
		Name:        "api",
		Destination: target,
		ScaffoldURL: "unused",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want error containing 'already exists'", err)
	}
	if got := readFile(t, filepath.Join(target, "keep")); got != "sentinel" {
		t.Fatal("existing directory changed")
	}
}

func TestCreateRejectsInvalidNames(t *testing.T) {
	invalidNames := []string{
		"",
		"../escape",
		"sub/name",
		".hidden",
		"-dashstart",
		"_understart",
		"name with spaces",
		"name@symbol",
		"..",
		".",
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			_, err := Create(context.Background(), CreateRequest{
				Name:        name,
				ScaffoldURL: "unused",
			})
			if err == nil {
				t.Fatalf("expected error for invalid name %q, got nil", name)
			}
		})
	}
}

func TestCreateCleansUpTargetOnCloneFailure(t *testing.T) {
	target := filepath.Join(t.TempDir(), "failed-target")

	_, err := Create(context.Background(), CreateRequest{
		Name:        "failed-target",
		Destination: target,
		ScaffoldURL: "file:///non/existent/scaffold/path",
	})
	if err == nil {
		t.Fatal("expected clone failure error, got nil")
	}

	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target %q still exists after failure", target)
	}
}

func TestCreateDefaultDestination(t *testing.T) {
	scaffold := fixtureScaffoldRepository(t)
	tmpDir := t.TempDir()

	// Switch to temporary directory to test relative ./<name> creation
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	result, err := Create(context.Background(), CreateRequest{
		Name:        "mybox",
		ScaffoldURL: scaffold,
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := "./mybox"
	if result.Root != expected {
		t.Fatalf("root = %q, want %q", result.Root, expected)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "mybox", ".git")); err != nil {
		t.Fatalf("cloned repo missing: %v", err)
	}
}
