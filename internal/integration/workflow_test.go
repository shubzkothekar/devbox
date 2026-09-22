package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shubzkothekar/devbox/internal/config"
)

var (
	devboxBin string
	repoRoot  string
)

func TestMain(m *testing.M) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller failed")
	}
	repoRoot = filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../.."))

	binDir, err := os.MkdirTemp("", "devbox-integration-bin-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(binDir)

	devboxBin = filepath.Join(binDir, "devbox")
	cmd := exec.Command("go", "build", "-o", devboxBin, "./cmd/devbox")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build devbox binary: %v\n%s", err, string(out)))
	}

	_ = os.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	code := m.Run()
	os.Exit(code)
}

type envOption struct {
	key   string
	value string
}

func withEnv(key, value string) envOption {
	return envOption{key: key, value: value}
}

func runFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		perm := info.Mode().Perm()
		if strings.HasSuffix(path, ".sh") {
			perm = 0755
		}
		return os.WriteFile(target, data, perm)
	})
}

func commitFixtureScaffold(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runFixtureGit(t, dir, "init", "-b", "main")
	runFixtureGit(t, dir, "config", "user.name", "DevBox Test")
	runFixtureGit(t, dir, "config", "user.email", "test@example.com")

	scaffoldSrc := filepath.Join(repoRoot, "testdata", "scaffold")
	if err := copyDir(scaffoldSrc, dir); err != nil {
		t.Fatalf("copy scaffold: %v", err)
	}

	scriptsSrc := filepath.Join(repoRoot, "scripts")
	scriptsDst := filepath.Join(dir, "scripts")
	if err := copyDir(scriptsSrc, scriptsDst); err != nil {
		t.Fatalf("copy scripts: %v", err)
	}

	runFixtureGit(t, dir, "add", "-A")
	runFixtureGit(t, dir, "commit", "-m", "initial scaffold")
	return dir
}

func commitFixtureRegistry(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()

	runFixtureGit(t, dir, "init", "-b", "main")
	runFixtureGit(t, dir, "config", "user.name", "DevBox Test")
	runFixtureGit(t, dir, "config", "user.email", "test@example.com")

	registrySrc := filepath.Join(repoRoot, "testdata", "registry")
	if err := copyDir(registrySrc, dir); err != nil {
		t.Fatalf("copy registry: %v", err)
	}

	runFixtureGit(t, dir, "add", "-A")
	runFixtureGit(t, dir, "commit", "-m", "initial registry")
	commit := runFixtureGit(t, dir, "rev-parse", "HEAD")
	return dir, commit
}

func parseArgsAndEnvs(args []any) ([]string, []string) {
	var cmdArgs []string
	var envs []string
	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			cmdArgs = append(cmdArgs, v)
		case []string:
			cmdArgs = append(cmdArgs, v...)
		case envOption:
			envs = append(envs, v.key+"="+v.value)
		default:
			panic(fmt.Sprintf("unsupported argument type %T", arg))
		}
	}
	return cmdArgs, envs
}

func runDevbox(t *testing.T, args ...any) string {
	t.Helper()
	return runDevboxAt(t, repoRoot, args...)
}

func runDevboxAt(t *testing.T, dir string, args ...any) string {
	t.Helper()
	cmdArgs, envs := parseArgsAndEnvs(args)
	cmd := exec.Command(devboxBin, cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), envs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("devbox %v in %s failed: %v\nstdout:\n%s\nstderr:\n%s", cmdArgs, dir, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func runDevboxAtExpectingError(t *testing.T, dir string, wantErrSubstr string, args ...any) string {
	t.Helper()
	cmdArgs, envs := parseArgsAndEnvs(args)
	cmd := exec.Command(devboxBin, cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), envs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatalf("devbox %v in %s: expected error containing %q, got success\nstdout:\n%s", cmdArgs, dir, wantErrSubstr, stdout.String())
	}
	combined := stdout.String() + "\n" + stderr.String()
	if !strings.Contains(combined, wantErrSubstr) {
		t.Fatalf("devbox %v in %s: error %v did not contain %q\noutput:\n%s", cmdArgs, dir, err, wantErrSubstr, combined)
	}
	return combined
}

func patchRegistryURL(t *testing.T, target, registryURL string) {
	t.Helper()
	cfgPath := filepath.Join(target, "devbox.plugins.yml")
	state, err := config.LoadProject(target)
	if err != nil {
		t.Fatalf("LoadProject(%s): %v", target, err)
	}
	state.Config.Registry.URL = registryURL
	if err := config.WriteYAMLAtomic(cfgPath, state.Config); err != nil {
		t.Fatalf("WriteYAMLAtomic(%s): %v", cfgPath, err)
	}
}

func assertLockCommit(t *testing.T, target, expectedCommit string) {
	t.Helper()
	state, err := config.LoadProject(target)
	if err != nil {
		t.Fatalf("LoadProject(%s): %v", target, err)
	}
	if state.Lock == nil {
		t.Fatalf("assertLockCommit: lock file missing in %s", target)
	}
	if state.Lock.Registry.Commit != expectedCommit {
		t.Fatalf("assertLockCommit: lock commit = %q, want %q", state.Lock.Registry.Commit, expectedCommit)
	}
}

func assertPlanIDs(t *testing.T, target string, expectedIDs []string) {
	t.Helper()
	planPath := filepath.Join(target, ".generated", "plugins", "plan.json")
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", planPath, err)
	}
	var plan config.ResolvedPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("Unmarshal plan.json: %v", err)
	}
	var gotIDs []string
	for _, p := range plan.Plugins {
		gotIDs = append(gotIDs, p.ID)
	}
	if len(gotIDs) != len(expectedIDs) {
		t.Fatalf("assertPlanIDs: got %v, want %v", gotIDs, expectedIDs)
	}
	for i := range gotIDs {
		if gotIDs[i] != expectedIDs[i] {
			t.Fatalf("assertPlanIDs: at index %d got %q, want %q (all: %v)", i, gotIDs[i], expectedIDs[i], gotIDs)
		}
	}
}

func assertGeneratedDevContainerExtension(t *testing.T, target, extensionID string) {
	t.Helper()
	devcontainerPath := filepath.Join(target, ".devcontainer", "devcontainer.json")
	data, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", devcontainerPath, err)
	}
	var parsed struct {
		Customizations struct {
			VSCode struct {
				Extensions []string `json:"extensions"`
			} `json:"vscode"`
		} `json:"customizations"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal devcontainer.json: %v", err)
	}
	for _, ext := range parsed.Customizations.VSCode.Extensions {
		if ext == extensionID {
			return
		}
	}
	t.Fatalf("assertGeneratedDevContainerExtension: extension %q not found in %v", extensionID, parsed.Customizations.VSCode.Extensions)
}

func TestCreateThenInstallThenResolveUsesPinnedRegistry(t *testing.T) {
	scaffoldURL := commitFixtureScaffold(t)
	registryURL, firstCommit := commitFixtureRegistry(t)
	target := filepath.Join(t.TempDir(), "container")

	runDevbox(t, "create", "container", "--destination", target, "--ref", "main", withEnv("DEVBOX_SCAFFOLD_URL", scaffoldURL))
	patchRegistryURL(t, target, registryURL)
	runDevboxAt(t, target, "plugin", "install", "docker")

	assertLockCommit(t, target, firstCommit)
	assertPlanIDs(t, target, []string{"base", "docker"})
	assertGeneratedDevContainerExtension(t, target, "ms-azuretools.vscode-docker")
}

func TestRegistryUpdateIgnoresNewerCommitsUntilUpdate(t *testing.T) {
	scaffoldURL := commitFixtureScaffold(t)
	registryURL, firstCommit := commitFixtureRegistry(t)
	target := filepath.Join(t.TempDir(), "container")

	runDevbox(t, "create", "container", "--destination", target, "--ref", "main", withEnv("DEVBOX_SCAFFOLD_URL", scaffoldURL))
	patchRegistryURL(t, target, registryURL)
	runDevboxAt(t, target, "plugin", "install", "docker")

	assertLockCommit(t, target, firstCommit)

	// Add a new commit to the registry fixture modifying docker plugin description
	dockerManifestPath := filepath.Join(registryURL, "plugins", "docker", "plugin.yaml")
	manifestContent, err := os.ReadFile(dockerManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	updatedManifest := strings.Replace(string(manifestContent), "version: 1.0.0", "version: 1.1.0", 1)
	if err := os.WriteFile(dockerManifestPath, []byte(updatedManifest), 0644); err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, registryURL, "add", "-A")
	runFixtureGit(t, registryURL, "commit", "-m", "update docker version to 1.1.0")
	secondCommit := runFixtureGit(t, registryURL, "rev-parse", "HEAD")
	if firstCommit == secondCommit {
		t.Fatalf("expected new commit hash, got %s", secondCommit)
	}

	// devbox plugin resolve should ignore the newer commit and retain firstCommit
	runDevboxAt(t, target, "plugin", "resolve")
	assertLockCommit(t, target, firstCommit)

	// Check installed report and generated plugin manifest still have version 1.0.0
	installedBefore := runDevboxAt(t, target, "plugin", "installed")
	if !strings.Contains(installedBefore, "docker (1.0.0)") {
		t.Fatalf("expected installed output to show docker (1.0.0) before update, got:\n%s", installedBefore)
	}
	manifestBefore, err := os.ReadFile(filepath.Join(target, ".generated", "plugins", "docker", "plugin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifestBefore), "version: 1.0.0") {
		t.Fatalf("expected docker manifest version 1.0.0 before update, got:\n%s", string(manifestBefore))
	}

	// devbox plugin update docker advances lock to secondCommit
	runDevboxAt(t, target, "plugin", "update", "docker")
	assertLockCommit(t, target, secondCommit)

	// Installed report and generated plugin manifest should now have version 1.1.0
	installedAfter := runDevboxAt(t, target, "plugin", "installed")
	if !strings.Contains(installedAfter, "docker (1.1.0)") {
		t.Fatalf("expected installed output to show docker (1.1.0) after update, got:\n%s", installedAfter)
	}
	manifestAfter, err := os.ReadFile(filepath.Join(target, ".generated", "plugins", "docker", "plugin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifestAfter), "version: 1.1.0") {
		t.Fatalf("expected docker manifest version 1.1.0 after update, got:\n%s", string(manifestAfter))
	}
}

func TestLocalOverrideLeavesLockUntouched(t *testing.T) {
	scaffoldURL := commitFixtureScaffold(t)
	registryURL, firstCommit := commitFixtureRegistry(t)
	target := filepath.Join(t.TempDir(), "container")

	runDevbox(t, "create", "container", "--destination", target, "--ref", "main", withEnv("DEVBOX_SCAFFOLD_URL", scaffoldURL))
	patchRegistryURL(t, target, registryURL)
	runDevboxAt(t, target, "plugin", "install", "docker")
	assertLockCommit(t, target, firstCommit)

	// Create local override directory with customized plugin
	localDir := t.TempDir()
	if err := copyDir(filepath.Join(repoRoot, "testdata", "registry"), localDir); err != nil {
		t.Fatal(err)
	}
	dockerManifestPath := filepath.Join(localDir, "plugins", "docker", "plugin.yaml")
	manifestContent, err := os.ReadFile(dockerManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	updatedManifest := strings.Replace(string(manifestContent), "version: 1.0.0", "version: 9.9.9-local", 1)
	if err := os.WriteFile(dockerManifestPath, []byte(updatedManifest), 0644); err != nil {
		t.Fatal(err)
	}

	overrideYAML := fmt.Sprintf("registry:\n  source: path\n  path: %s\n", localDir)
	if err := os.WriteFile(filepath.Join(target, "devbox.plugins.local.yml"), []byte(overrideYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Resolve with local override
	runDevboxAt(t, target, "plugin", "resolve")

	// Lock file must remain untouched with firstCommit
	assertLockCommit(t, target, firstCommit)

	// Installed report should reflect local version
	installedLocal := runDevboxAt(t, target, "plugin", "installed")
	if !strings.Contains(installedLocal, "docker (9.9.9-local)") {
		t.Fatalf("expected installed output to show docker (9.9.9-local) with local override, got:\n%s", installedLocal)
	}

	// Plan should reflect local source mode and local directory
	planData, err := os.ReadFile(filepath.Join(target, ".generated", "plugins", "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan config.ResolvedPlan
	if err := json.Unmarshal(planData, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Source.Mode != "path" {
		t.Fatalf("expected plan source mode to be 'path', got %q", plan.Source.Mode)
	}

	// Verify plugin list reports path source mode
	listOut := runDevboxAt(t, target, "plugin", "list")
	if !strings.Contains(listOut, "Registry source mode: path") {
		t.Fatalf("expected list output to mention 'Registry source mode: path', got:\n%s", listOut)
	}
}

func TestStartupHookIdempotency(t *testing.T) {
	scaffoldURL := commitFixtureScaffold(t)
	registryURL, _ := commitFixtureRegistry(t)
	target := filepath.Join(t.TempDir(), "container")

	runDevbox(t, "create", "container", "--destination", target, "--ref", "main", withEnv("DEVBOX_SCAFFOLD_URL", scaffoldURL))
	patchRegistryURL(t, target, registryURL)
	runDevboxAt(t, target, "plugin", "install", "docker")

	testLog := filepath.Join(t.TempDir(), "test.log")
	markerDir := filepath.Join(t.TempDir(), "markers")
	planPath := filepath.Join(target, ".generated", "plugins", "plan.json")

	runStartsScript := filepath.Join(target, "scripts", "run-plugin-starts")
	runHook := func() string {
		cmd := exec.Command("bash", runStartsScript)
		cmd.Dir = target
		cmd.Env = append(os.Environ(),
			"DEVBOX_TEST_LOG="+testLog,
			"DEVBOX_PLUGIN_MARKER_DIR="+markerDir,
			"DEVBOX_PLUGIN_PLAN="+planPath,
		)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("run-plugin-starts failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
		}
		return stdout.String()
	}

	// First execution: hook should run and write to log
	out1 := runHook()
	if !strings.Contains(out1, "[plugin:docker start] starting") {
		t.Fatalf("expected first run output to indicate start, got:\n%s", out1)
	}

	markerFile := filepath.Join(markerDir, "docker.started")
	if _, err := os.Stat(markerFile); err != nil {
		t.Fatalf("expected marker file %s to exist: %v", markerFile, err)
	}

	logBytes, err := os.ReadFile(testLog)
	if err != nil {
		t.Fatalf("read testLog: %v", err)
	}
	if !strings.Contains(string(logBytes), "docker:start") {
		t.Fatalf("expected testLog to contain 'docker:start', got:\n%s", string(logBytes))
	}
	linesCount1 := len(strings.Split(strings.TrimSpace(string(logBytes)), "\n"))

	// Second execution: hook must be skipped due to existing marker
	out2 := runHook()
	if strings.Contains(out2, "[plugin:docker start] starting") {
		t.Fatalf("expected second run to skip start hook, got:\n%s", out2)
	}

	logBytes2, err := os.ReadFile(testLog)
	if err != nil {
		t.Fatalf("read testLog: %v", err)
	}
	linesCount2 := len(strings.Split(strings.TrimSpace(string(logBytes2)), "\n"))
	if linesCount2 != linesCount1 {
		t.Fatalf("testLog was modified on second run (%d lines vs %d lines)", linesCount2, linesCount1)
	}
}

func TestCommandDispatchAndUndeclaredRejection(t *testing.T) {
	scaffoldURL := commitFixtureScaffold(t)
	registryURL, _ := commitFixtureRegistry(t)
	target := filepath.Join(t.TempDir(), "container")

	runDevbox(t, "create", "container", "--destination", target, "--ref", "main", withEnv("DEVBOX_SCAFFOLD_URL", scaffoldURL))
	patchRegistryURL(t, target, registryURL)
	runDevboxAt(t, target, "plugin", "install", "docker")

	testLog := filepath.Join(t.TempDir(), "test.log")

	// 1. Valid command execution via `devbox plugin exec docker status`
	runDevboxAt(t, target, "plugin", "exec", "docker", "status", withEnv("DEVBOX_TEST_LOG", testLog))

	logBytes, err := os.ReadFile(testLog)
	if err != nil {
		t.Fatalf("read testLog: %v", err)
	}
	if !strings.Contains(string(logBytes), "docker:status:user=") {
		t.Fatalf("expected testLog to contain 'docker:status:user=', got:\n%s", string(logBytes))
	}

	// 2. Shorthand command execution via `devbox plugin docker status`
	runDevboxAt(t, target, "plugin", "docker", "status", withEnv("DEVBOX_TEST_LOG", testLog))

	logBytes2, err := os.ReadFile(testLog)
	if err != nil {
		t.Fatalf("read testLog: %v", err)
	}
	if strings.Count(string(logBytes2), "docker:status:user=") != 2 {
		t.Fatalf("expected 2 status executions in testLog, got:\n%s", string(logBytes2))
	}

	// 3. Undeclared command rejection
	runDevboxAtExpectingError(t, target, `plugin "docker" has no command "undeclared"`, "plugin", "exec", "docker", "undeclared")
}
