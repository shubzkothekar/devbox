# DevBox Project and Plugin System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a host-side `devbox` CLI that creates independent DevBox projects and manages reproducible registry-backed plugins across Docker build, container startup, command dispatch, and generated Dev Container configuration.

**Architecture:** Implement the host CLI as a statically built Go executable so it can safely parse and rewrite the YAML configuration files without requiring a host interpreter. The CLI materializes one registry snapshot, validates manifests and enabled-plugin topology, then writes a normalized plan consumed by shell lifecycle adapters in the scaffold. Plugins remain external to the scaffold in a pinned Git registry; Docker and startup execute only the generated plan and never fetch Git content.

**Tech Stack:** Go 1.24 (`cmd/devbox`, standard library, `gopkg.in/yaml.v3`), Bash, Docker Compose, Dev Container JSON, Go `testing` package.

## Global Constraints

- Version 1 supports exactly one configured Git registry, or one ignored local-path override, per DevBox project.
- Git registry network access is allowed only from explicit `devbox plugin install` and `devbox plugin update` operations.
- Git-backed resolution, Docker build, startup, plugin command dispatch, `list`, and `installed` must use the locally materialized commit named by `devbox.plugins.lock.yml`.
- `devbox create <container-name>` must reject unsafe names and existing/non-empty destinations, preserve cloned Git metadata, create `.env` from `.env.example` only when absent, and never install plugins, build, or start containers.
- Plugin hook and command paths must be regular files within their plugin directory; reject traversal and symlink escapes.
- Build hooks run as root; startup and developer commands run as `devbox` unless a manifest command explicitly declares root.
- Plugin option values are data; do not interpolate option values into generated shell source.
- Dev Container contributions are limited to `extensions`, `mounts`, `containerEnv`, `postCreateCommands`, and `forwardPorts` and must merge deterministically.
- Use atomic writes for `devbox.plugins.yml`, `devbox.plugins.lock.yml`, generated JSON, and generated plans.
- Do not add non-Go runtime dependencies to the host machine. `gopkg.in/yaml.v3` is compiled into the `devbox` executable.

---

## File Structure

| Path | Responsibility |
| --- | --- |
| `go.mod`, `go.sum` | Build the distributable host-side CLI with a compiled YAML parser. |
| `cmd/devbox/main.go` | Parse top-level commands, print usage, map typed errors to actionable stderr output. |
| `internal/project/create.go` | Validate names/targets, clone a scaffold at a ref, initialize `.env`, and clean only a newly-created failed target. |
| `internal/project/create_test.go` | Fixture-based project-creation behavior and cleanup tests. |
| `internal/config/types.go` | YAML document structs for plugin configuration, lock state, local overrides, manifests, and generated plan. |
| `internal/config/files.go` | Strict YAML decoding, validation, and atomic YAML/JSON writes. |
| `internal/config/files_test.go` | Configuration decode, schema, option, and atomic-write tests. |
| `internal/registry/materialize.go` | Resolve Git refs, clone/fetch snapshots only for install/update, and validate cached commit snapshots. |
| `internal/registry/materialize_test.go` | Local Git fixture coverage for locks, snapshots, and local overrides. |
| `internal/plugins/resolve.go` | Discover manifests, validate paths/options, calculate dependency order, and build generated hook/command plan. |
| `internal/plugins/resolve_test.go` | Manifest, dependency, conflict, cycle, and path-safety tests. |
| `internal/plugins/service.go` | Implement list/installed/install/uninstall/update and command lookup using configuration, registry, and resolver packages. |
| `internal/plugins/service_test.go` | Atomic configuration mutation and user-facing plugin-operation tests. |
| `internal/devcontainer/merge.go` | Validate and deterministically merge supported plugin contributions into generated Dev Container JSON. |
| `internal/devcontainer/merge_test.go` | List, environment, mount, and port conflict/deduplication tests. |
| `scripts/resolve-plugins` | Run the installed CLI resolver in the scaffold before Docker/Dev Container consumption. |
| `scripts/run-plugin-builds` | Execute only generated build hooks in resolved order as root. |
| `scripts/run-plugin-starts` | Execute only generated idempotent startup hooks with completion markers. |
| `scripts/devbox-plugin` | In-container dispatcher that loads generated command metadata and uses `sudo` only for manifest-declared root commands. |
| `Dockerfile` | Copy resolver/adapter scripts and execute generated build hooks after baseline dependencies. |
| `entrypoint.sh` | Invoke generated startup hooks after existing environment setup and before the requested process. |
| `docker-compose.yml` | Make generated plugin plan available during image build and runtime. |
| `.devcontainer/devcontainer.base.json` | Version-controlled base Dev Container settings. |
| `.devcontainer/devcontainer.json` | Ignored generated Dev Container settings consumed by tools. |
| `devbox.plugins.yml` | Version-controlled initial registry declaration and no enabled plugins. |
| `devbox.plugins.lock.yml` | Version-controlled lock format, initially absent registry commit until first install/update. |
| `devbox.plugins.local.yml` | Ignored documented local development override template; do not commit a user override. |
| `.gitignore` | Exclude generated registry/plan, generated Dev Container config, local override, and `.env`. |
| `README.md` | Document CLI installation, project creation, plugin lifecycle, local registry development, and generated files. |
| `testdata/scaffold/`, `testdata/registry/` | Minimal Git-backed fixtures for end-to-end Go integration tests. |

### Task 1: Bootstrap the Host CLI and Typed Configuration Contract

**Files:**
- Create: `go.mod`
- Create: `cmd/devbox/main.go`
- Create: `internal/config/types.go`
- Create: `internal/config/files.go`
- Create: `internal/config/files_test.go`

**Interfaces:**
- Produces `config.ProjectConfig`, `config.RegistryLock`, `config.LocalOverride`, `config.Manifest`, `config.ResolvedPlan`, and `config.LoadProject(root string) (ProjectState, error)`.
- Produces `config.WriteYAMLAtomic(path string, value any) error` and `config.WriteJSONAtomic(path string, value any) error` for all later mutation tasks.
- Consumes no existing code; every later Go package imports only the public `internal/config` types it needs.

- [ ] **Step 1: Create a Go module that embeds YAML support in the CLI binary**

Create `go.mod`:

```go
module github.com/<org>/devbox

go 1.24.0

require gopkg.in/yaml.v3 v3.0.1
```

Run: `go mod tidy`
Expected: `go.sum` is created and `go list ./...` exits 0.

- [ ] **Step 2: Write failing configuration tests for valid project state and rejected unknown fields**

Create `internal/config/files_test.go`:

```go
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
```

- [ ] **Step 3: Run the configuration tests to verify they fail**

Run: `go test ./internal/config -run 'TestLoadProject' -count=1`
Expected: FAIL because `LoadProject` does not exist.

- [ ] **Step 4: Implement strict configuration structures and atomic persistence**

Create `internal/config/types.go` with the stable public contract:

```go
type ProjectConfig struct {
    Registry RegistryConfig            `yaml:"registry"`
    Plugins  map[string]PluginSelection `yaml:"plugins"`
}

type RegistryConfig struct {
    Source string `yaml:"source"`
    URL    string `yaml:"url"`
    Ref    string `yaml:"ref"`
}

type PluginSelection struct {
    Enabled bool           `yaml:"enabled"`
    Options map[string]any `yaml:"options,omitempty"`
}

type RegistryLock struct {
    Registry LockedRegistry `yaml:"registry"`
}

type LockedRegistry struct {
    URL    string `yaml:"url"`
    Ref    string `yaml:"ref"`
    Commit string `yaml:"commit"`
}

type LocalOverride struct {
    Registry LocalRegistry `yaml:"registry"`
}

type LocalRegistry struct {
    Source string `yaml:"source"`
    Path   string `yaml:"path"`
}

type ProjectState struct {
    Root     string
    Config   ProjectConfig
    Lock     *RegistryLock
    Override *LocalOverride
}
```

Create `internal/config/files.go` so `LoadProject` uses `yaml.NewDecoder`, calls `KnownFields(true)`, validates `registry.source == "git"`, non-empty Git URL/ref, and checks a loaded lock commit with `^[0-9a-f]{40}$`. Implement `WriteYAMLAtomic` and `WriteJSONAtomic` by creating a same-directory temporary file, writing with mode `0644`, closing it, then `os.Rename` to the target.

- [ ] **Step 5: Add the command entrypoint with consistent errors**

Create `cmd/devbox/main.go`:

```go
func main() {
    if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
        fmt.Fprintln(os.Stderr, "devbox:", err)
        os.Exit(1)
    }
}

func run(args []string, stdout, stderr io.Writer) error {
    if len(args) == 0 {
        return errors.New("usage: devbox create <container-name> | devbox plugin <command>")
    }
    return fmt.Errorf("unsupported command %q", args[0])
}
```

Keep the dispatcher minimal in this task; later tasks replace the unsupported branches with concrete services.

- [ ] **Step 6: Run configuration and compilation checks**

Run: `go test ./internal/config -count=1 && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 7: Commit the CLI foundation**

```bash
git add go.mod go.sum cmd/devbox/main.go internal/config
git commit -m "feat: bootstrap devbox CLI configuration"
```

### Task 2: Materialize Registries and Resolve a Safe Plugin Plan

**Files:**
- Create: `internal/registry/materialize.go`
- Create: `internal/registry/materialize_test.go`
- Create: `internal/plugins/resolve.go`
- Create: `internal/plugins/resolve_test.go`
- Modify: `internal/config/types.go`

**Interfaces:**
- Consumes `config.ProjectState` from Task 1.
- Produces `registry.Materialize(ctx context.Context, state config.ProjectState, mode registry.Mode) (registry.Source, error)` where `Mode` is `ReadOnly`, `Install`, or `Update`.
- Produces `plugins.Resolve(source registry.Source, selections map[string]config.PluginSelection) (config.ResolvedPlan, error)`.
- `config.ResolvedPlan` includes ordered plugins and only validated relative hook/command references; Tasks 3–5 rely on it.

- [ ] **Step 1: Write failing resolver tests covering ordering and unsafe paths**

Create `internal/plugins/resolve_test.go`:

```go
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
    if err != nil { t.Fatal(err) }
    if got, want := plan.PluginIDs(), []string{"alpha", "base", "docker"}; !reflect.DeepEqual(got, want) {
        t.Fatalf("order = %v, want %v", got, want)
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
```

- [ ] **Step 2: Run resolver tests to verify they fail**

Run: `go test ./internal/plugins -run 'TestResolve' -count=1`
Expected: FAIL because `Resolve` and fixture helpers do not exist.

- [ ] **Step 3: Implement registry materialization modes without background network access**

Create `internal/registry/materialize.go` with:

```go
type Mode uint8
const (
    ReadOnly Mode = iota
    Install
    Update
)

type Source struct {
    Root string
    Mode string // "git" or "path"
}

func Materialize(ctx context.Context, state config.ProjectState, mode Mode) (Source, error)
```

Behavior:

1. If `state.Override != nil`, canonicalize the configured local path with `filepath.EvalSymlinks`, require a directory, and return `Source{Root: path, Mode: "path"}` without reading or writing a lock.
2. For `ReadOnly`, require a valid lock and `state.Root/.generated/registry/.git`; verify `git -C <snapshot> rev-parse HEAD` equals the locked commit. Return an error instructing the user to run `devbox plugin install` or `devbox plugin update` if absent or mismatched.
3. For `Install` and `Update`, resolve `registry.url` plus `registry.ref` using a temporary Git clone, obtain `git rev-parse HEAD`, validate the 40-character SHA, replace only `.generated/registry` atomically, and let the caller write the lock after full catalog validation succeeds.
4. Execute Git commands through `exec.CommandContext`; capture stderr and include the lifecycle action in errors without exposing credentials.

- [ ] **Step 4: Implement strict manifest validation and normalized plans**

Add the following types to `internal/config/types.go`:

```go
type Manifest struct {
    ID          string                    `yaml:"id"`
    Version     string                    `yaml:"version"`
    Requires    []string                  `yaml:"requires"`
    Conflicts   []string                  `yaml:"conflicts"`
    Options     map[string]OptionSchema   `yaml:"options"`
    Hooks       HookPaths                 `yaml:"hooks"`
    Commands    map[string]CommandSchema  `yaml:"commands"`
    DevContainer DevContainerContribution `yaml:"devcontainer"`
}

type OptionSchema struct {
    Type    string `yaml:"type"`
    Default any    `yaml:"default"`
}

type HookPaths struct { Build string `yaml:"build"`; Start string `yaml:"start"` }
type CommandSchema struct { Path string `yaml:"path"`; User string `yaml:"user"` }
```

In `internal/plugins/resolve.go`, discover only `plugins/*/plugin.yaml`; require a manifest `id` matching the directory name; validate enabled selection IDs, option types, missing required dependencies, conflicts, and cycles. Sort unrelated dependency nodes lexicographically. Use `filepath.Rel(pluginRoot, candidate)` and `os.Lstat`/`filepath.EvalSymlinks` to reject absolute paths, non-regular files, `..` escapes, and symlink targets outside `pluginRoot`.

Add `config.ResolvedPlan` with this JSON-safe representation:

```go
type ResolvedPlan struct {
    Version int              `json:"version"`
    Source  ResolvedSource   `json:"source"`
    Plugins []ResolvedPlugin `json:"plugins"`
}

type ResolvedPlugin struct {
    ID       string         `json:"id"`
    Root     string         `json:"root"`
    Options  map[string]any `json:"options"`
    Build    string         `json:"build,omitempty"`
    Start    string         `json:"start,omitempty"`
    Commands map[string]ResolvedCommand `json:"commands,omitempty"`
}
```

- [ ] **Step 5: Add local Git fixture tests for materialization and stale snapshots**

Create tests that initialize a temporary bare Git repository, make a commit containing `plugins/example/plugin.yaml`, and assert:

```go
func TestMaterializeReadOnlyRejectsMissingSnapshot(t *testing.T) { /* lock exists; .generated/registry absent */ }
func TestMaterializeInstallWritesSnapshotAtLockedCommit(t *testing.T) { /* SHA equals fixture commit */ }
func TestMaterializeLocalOverrideDoesNotRequireLock(t *testing.T) { /* Source.Mode == "path" */ }
```

Use `git init`, `git add`, and `git commit` through a test helper configured with fixture author identity. Never access a network in tests.

- [ ] **Step 6: Run resolver and registry tests**

Run: `go test ./internal/registry ./internal/plugins -count=1`
Expected: PASS.

- [ ] **Step 7: Commit registry and resolver support**

```bash
git add internal/config/types.go internal/registry internal/plugins
git commit -m "feat: resolve pinned plugin registries"
```

### Task 3: Implement Plugin Configuration Commands and Generated Plan Output

**Files:**
- Create: `internal/plugins/service.go`
- Create: `internal/plugins/service_test.go`
- Modify: `cmd/devbox/main.go`
- Modify: `internal/config/files.go`
- Create: `scripts/resolve-plugins`
- Modify: `.gitignore`

**Interfaces:**
- Consumes `registry.Materialize` and `plugins.Resolve` from Task 2.
- Produces `plugins.Service.List`, `Installed`, `Install`, `Uninstall`, `Update`, `ResolveGenerated`, and `FindCommand`.
- Produces `<project>/.generated/plugins/plan.json`, which Tasks 4–5 consume.

- [ ] **Step 1: Write failing service tests for atomic install and dependent uninstall rejection**

Create `internal/plugins/service_test.go`:

```go
func TestInstallEnablesPluginWritesLockAndPlan(t *testing.T) {
    project, registryURL := fixtureProjectAndRegistry(t)
    service := NewService(project)

    if err := service.Install(context.Background(), "docker"); err != nil { t.Fatal(err) }

    state, err := config.LoadProject(project)
    if err != nil { t.Fatal(err) }
    if !state.Config.Plugins["docker"].Enabled { t.Fatal("docker is not enabled") }
    if state.Lock == nil || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(state.Lock.Registry.Commit) {
        t.Fatalf("lock = %#v", state.Lock)
    }
    if _, err := os.Stat(filepath.Join(project, ".generated/plugins/plan.json")); err != nil { t.Fatal(err) }
    _ = registryURL
}

func TestUninstallRejectsEnabledDependent(t *testing.T) {
    project, _ := fixtureProjectWithEnabled(t, "base", "docker")
    err := NewService(project).Uninstall(context.Background(), "base")
    if err == nil || !strings.Contains(err.Error(), "required by enabled plugin docker") {
        t.Fatalf("error = %v", err)
    }
}
```

- [ ] **Step 2: Run the service tests to verify they fail**

Run: `go test ./internal/plugins -run 'Test(Install|Uninstall)' -count=1`
Expected: FAIL because `Service` is not implemented.

- [ ] **Step 3: Implement explicit install, uninstall, update, list, and installed operations**

Create `internal/plugins/service.go`:

```go
type Service struct { Root string }
func NewService(root string) Service
func (s Service) List(ctx context.Context) ([]CatalogPlugin, error)
func (s Service) Installed(ctx context.Context) ([]InstalledPlugin, error)
func (s Service) Install(ctx context.Context, id string) error
func (s Service) Uninstall(ctx context.Context, id string) error
func (s Service) Update(ctx context.Context, id string) error
func (s Service) ResolveGenerated(ctx context.Context) (config.ResolvedPlan, error)
func (s Service) FindCommand(ctx context.Context, pluginID, command string) (config.ResolvedPlugin, config.ResolvedCommand, error)
```

Implement exact behaviors:

- `List` and `Installed` use `registry.ReadOnly` unless a local override exists; print source mode alongside catalog data.
- `Install` uses `registry.Install`, validates the entire requested plugin set plus the candidate, resolves it, then atomically persists config, lock (Git mode only), and `.generated/plugins/plan.json` only after every validation succeeds.
- `Uninstall` rejects a plugin required by any enabled manifest; otherwise disables/removes that selection, retains the Git lock, and rewrites `plan.json` from the same snapshot.
- `Update` uses `registry.Update`, resolves all enabled plugins, writes a new lock only after the complete plan succeeds, and with non-empty `id` verifies that ID remains enabled and compatible.
- `ResolveGenerated` uses read-only materialization and `config.WriteJSONAtomic` at `.generated/plugins/plan.json`.

Do not permit ordinary resolve/list/installed/dispatch paths to perform Git fetches.

- [ ] **Step 4: Connect `devbox plugin` subcommands to the service**

Replace `run` in `cmd/devbox/main.go` with dispatch behavior equivalent to:

```go
case "plugin":
    root, err := os.Getwd()
    if err != nil { return err }
    service := plugins.NewService(root)
    switch args[1] {
    case "list": return printCatalog(stdout, service.List(ctx))
    case "installed": return printInstalled(stdout, service.Installed(ctx))
    case "install": return service.Install(ctx, requireOneArg(args[2:], "plugin ID"))
    case "uninstall": return service.Uninstall(ctx, requireOneArg(args[2:], "plugin ID"))
    case "update": return service.Update(ctx, optionalOneArg(args[2:]))
    default: return dispatchPluginCommand(ctx, args[1:], stdout, service)
    }
```

Make each missing argument error name the valid command syntax. Route plugin command dispatch metadata through a later `exec` implementation; this task only resolves and validates command identity.

- [ ] **Step 5: Add the scaffold resolver adapter and generated-file ignores**

Create executable `scripts/resolve-plugins`:

```bash
#!/usr/bin/env bash
set -euo pipefail

project_root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
exec devbox plugin resolve --project-root "$project_root"
```

Add `.generated/`, `.devcontainer/devcontainer.json`, and `devbox.plugins.local.yml` to `.gitignore`, preserving the tracked `.devcontainer/devcontainer.base.json` and `.env.example` exceptions.

- [ ] **Step 6: Run service tests and full Go suite**

Run: `go test ./internal/plugins ./cmd/devbox -count=1 && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 7: Commit plugin lifecycle commands**

```bash
git add cmd/devbox internal/plugins internal/config scripts/resolve-plugins .gitignore
git commit -m "feat: manage reproducible DevBox plugins"
```

### Task 4: Implement `devbox create` Project Bootstrap

**Files:**
- Create: `internal/project/create.go`
- Create: `internal/project/create_test.go`
- Modify: `cmd/devbox/main.go`
- Modify: `README.md`

**Interfaces:**
- Produces `project.Create(ctx context.Context, request project.CreateRequest) (project.CreateResult, error)`.
- `CreateRequest` fields: `Name string`, `Destination string`, `Ref string`, `ScaffoldURL string`.
- The command dispatcher calls this interface only for `devbox create`; plugin operations remain rooted in existing projects.

- [ ] **Step 1: Write failing create tests for destination safety, Git preservation, and `.env` initialization**

Create `internal/project/create_test.go`:

```go
func TestCreateClonesScaffoldAndInitializesEnv(t *testing.T) {
    scaffold := fixtureScaffoldRepository(t)
    target := filepath.Join(t.TempDir(), "api")

    result, err := Create(context.Background(), CreateRequest{
        Name: "api", Destination: target, Ref: "main", ScaffoldURL: scaffold,
    })
    if err != nil { t.Fatal(err) }
    if result.Root != target { t.Fatalf("root = %q", result.Root) }
    if _, err := os.Stat(filepath.Join(target, ".git")); err != nil { t.Fatal(err) }
    if got := readFile(t, filepath.Join(target, ".env")); got != "INSTALL_GO=true\n" {
        t.Fatalf(".env = %q", got)
    }
}

func TestCreateRejectsExistingDestinationWithoutChangingIt(t *testing.T) {
    target := filepath.Join(t.TempDir(), "api")
    writeFile(t, target, "keep", "sentinel")
    err := Create(context.Background(), CreateRequest{Name: "api", Destination: target, ScaffoldURL: "unused"})
    if err == nil || !strings.Contains(err.Error(), "already exists") { t.Fatalf("error = %v", err) }
    if got := readFile(t, filepath.Join(target, "keep")); got != "sentinel" { t.Fatal("existing directory changed") }
}
```

Add a test for rejected basename `../escape` and a fixture clone failure that asserts the newly-created target does not remain.

- [ ] **Step 2: Run project tests to verify they fail**

Run: `go test ./internal/project -count=1`
Expected: FAIL because the project package does not exist.

- [ ] **Step 3: Implement deterministic and non-destructive project creation**

Create `internal/project/create.go`:

```go
type CreateRequest struct {
    Name        string
    Destination string
    Ref         string
    ScaffoldURL string
}

type CreateResult struct {
    Root string
    Ref  string
}

func Create(ctx context.Context, request CreateRequest) (CreateResult, error)
```

Implementation requirements:

1. Accept `Name` only if it equals `filepath.Base(Name)`, is non-empty, and matches `^[A-Za-z0-9][A-Za-z0-9._-]*$`.
2. Derive `Destination` as `./<Name>` when empty; reject an existing path, including an empty directory, rather than replacing it.
3. Clone `ScaffoldURL` with `git clone --branch <Ref> --single-branch` when `Ref` is present, otherwise clone the documented stable default. Do not use `--depth` so the created project retains normal Git history and the checked-out ref.
4. After clone, copy `.env.example` to `.env` only when `.env.example` exists and `.env` does not. Preserve permissions with `io.Copy` to a newly created file mode `0600`.
5. On any clone or initialization failure, remove only `Destination` if this invocation created it; never delete an existing target.

- [ ] **Step 4: Add CLI flags and post-create next steps**

In `cmd/devbox/main.go`, parse:

```text
devbox create <container-name> [--destination <path>] [--ref <git-ref>]
```

Read `DEVBOX_SCAFFOLD_URL` for testable/distribution-time scaffold selection; otherwise use a compile-time documented default constant. On success write exactly:

```text
Created DevBox project: <absolute-or-user-supplied destination>
Next:
  cd <destination>
  devbox plugin install <plugin-id>
```

Reject flags after an unknown positional argument and do not call any plugin, Docker, or Compose service during this branch.

- [ ] **Step 5: Document installation and project creation**

Add a README section before the current quick start:

```markdown
## Create a DevBox Project

Install the host-side `devbox` CLI, then create a new scaffold checkout:

```bash
devbox create my-api --ref v1.0.0
cd my-api
devbox plugin install docker
```

`create` preserves the scaffold repository's Git history, initializes `.env` from `.env.example`, and does not build or start a container. Use `--destination /absolute/or/relative/path` to choose a target other than `./my-api`.
```

- [ ] **Step 6: Run project tests and the entire Go suite**

Run: `go test ./internal/project -count=1 && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 7: Commit project creation support**

```bash
git add cmd/devbox internal/project README.md
git commit -m "feat: create DevBox scaffold projects"
```

### Task 5: Generate Dev Container Configuration and Wire Lifecycle Hooks

**Files:**
- Create: `internal/devcontainer/merge.go`
- Create: `internal/devcontainer/merge_test.go`
- Create: `.devcontainer/devcontainer.base.json`
- Modify: `.devcontainer/devcontainer.json`
- Create: `scripts/run-plugin-builds`
- Create: `scripts/run-plugin-starts`
- Create: `scripts/devbox-plugin`
- Modify: `Dockerfile`
- Modify: `entrypoint.sh`
- Modify: `docker-compose.yml`
- Modify: `internal/plugins/service.go`
- Modify: `cmd/devbox/main.go`

**Interfaces:**
- Consumes `config.ResolvedPlan` produced by Task 3.
- Produces `devcontainer.Merge(base []byte, plan config.ResolvedPlan) ([]byte, error)`.
- Generates `.devcontainer/devcontainer.json` and `.generated/plugins/plan.json` before tooling runs.
- Shell adapters consume `plan.json`; they do not parse manifests or access the registry.

- [ ] **Step 1: Write failing Dev Container merge tests for additive fields and conflicts**

Create `internal/devcontainer/merge_test.go`:

```go
func TestMergeDeduplicatesExtensionsInResolvedOrder(t *testing.T) {
    base := []byte(`{"name":"DevBox","customizations":{"vscode":{"extensions":["base.ext"]}}}`)
    plan := config.ResolvedPlan{Plugins: []config.ResolvedPlugin{
        {ID: "alpha", DevContainer: config.DevContainerContribution{Extensions: []string{"base.ext", "alpha.ext"}}},
        {ID: "beta", DevContainer: config.DevContainerContribution{Extensions: []string{"beta.ext", "alpha.ext"}}},
    }}
    got, err := Merge(base, plan)
    if err != nil { t.Fatal(err) }
    if !strings.Contains(string(got), `"extensions":["base.ext","alpha.ext","beta.ext"]`) { t.Fatalf("%s", got) }
}

func TestMergeRejectsConflictingContainerEnvironment(t *testing.T) {
    base := []byte(`{"containerEnv":{"API_URL":"https://one.test"}}`)
    plan := config.ResolvedPlan{Plugins: []config.ResolvedPlugin{{ID: "two", DevContainer: config.DevContainerContribution{ContainerEnv: map[string]string{"API_URL":"https://two.test"}}}}}
    _, err := Merge(base, plan)
    if err == nil || !strings.Contains(err.Error(), "API_URL") { t.Fatalf("error = %v", err) }
}
```

- [ ] **Step 2: Run merge tests to verify they fail**

Run: `go test ./internal/devcontainer -count=1`
Expected: FAIL because the package and `Merge` function do not exist.

- [ ] **Step 3: Implement deterministic supported-field merging**

Create `internal/devcontainer/merge.go` with:

```go
func Merge(base []byte, plan config.ResolvedPlan) ([]byte, error)
```

Unmarshal base into typed structs rather than arbitrary `map[string]any`. Preserve existing required scaffold fields (`name`, `dockerComposeFile`, `service`, `workspaceFolder`, `shutdownAction`, `remoteUser`, customizations). Merge only manifest contributions:

- `extensions`: append after base list and deduplicate in resolved plugin order.
- `mounts`: deduplicate identical strings; reject different mount definitions with the same parsed `target=`.
- `containerEnv`: accept identical values; reject a changed value for an existing key.
- `postCreateCommands`: append in resolved order.
- `forwardPorts`: deduplicate equal numeric ports; reject mismatched metadata if port objects are introduced.

Marshal stable, indented JSON and write it using `config.WriteJSONAtomic`.

- [ ] **Step 4: Make resolution generate Dev Container output**

Extend `plugins.Service.ResolveGenerated` to load `.devcontainer/devcontainer.base.json`, call `devcontainer.Merge`, and atomically write `.devcontainer/devcontainer.json`. Add a `devbox plugin resolve --project-root <path>` internal command used by `scripts/resolve-plugins`; it must use `registry.ReadOnly` and fail with an install/update instruction when the snapshot is unavailable.

Move the current tracked `.devcontainer/devcontainer.json` contents unchanged to `.devcontainer/devcontainer.base.json`. Leave a generated `.devcontainer/devcontainer.json` out of Git and document it as generated.

- [ ] **Step 5: Write build and startup adapters that execute only generated hooks**

Create executable `scripts/run-plugin-builds`:

```bash
#!/usr/bin/env bash
set -euo pipefail

plan=${DEVBOX_PLUGIN_PLAN:-/opt/devbox/plugins/plan.json}
[ -f "$plan" ] || exit 0
jq -c '.plugins[] | select(.build != null and .build != "")' "$plan" | while IFS= read -r plugin; do
  id=$(jq -r '.id' <<<"$plugin")
  root=$(jq -r '.root' <<<"$plugin")
  hook=$(jq -r '.build' <<<"$plugin")
  echo "[plugin:$id build] starting"
  "$root/$hook"
done
```

Before each hook invocation, export each option as `DEVBOX_PLUGIN_<UPPERCASE_PLUGIN_ID>_<UPPERCASE_OPTION>` using a JSON-to-environment helper implemented by the Go CLI, not shell interpolation. Replace the inline `jq` option processing above with `devbox plugin hook-env --plugin-json "$plugin" -- "$root/$hook"` so the CLI invokes the hook with validated environment values.

Create executable `scripts/run-plugin-starts` with identical plan lookup and option export, but write markers under `/var/lib/devbox/plugins/<id>.started` only after a successful hook. Skip an existing marker and prefix all output with `[plugin:<id> start]`.

Create executable `scripts/devbox-plugin` that calls `devbox plugin exec <plugin-id> <command> -- "$@"`; the Go CLI loads `plan.json`, validates the declared command, and executes it as `devbox` or via `sudo -n` only when `user: root` appears in `ResolvedCommand`.

- [ ] **Step 6: Integrate adapters with Docker and runtime without registry access**

Modify `Dockerfile` after foundational dependencies and before optional runtime installs:

```dockerfile
COPY .generated/plugins/plan.json /opt/devbox/plugins/plan.json
COPY .generated/plugins /opt/devbox/plugins
COPY scripts/run-plugin-builds /usr/local/bin/run-plugin-builds
RUN chmod +x /usr/local/bin/run-plugin-builds && /usr/local/bin/run-plugin-builds
```

Guard the Docker build with a pre-build requirement: `scripts/resolve-plugins` must have created `.generated/plugins/plan.json`; Docker must fail with a clear missing-plan error rather than fetching Git.

Modify `entrypoint.sh` after existing environment/VPN initialization and before step 8:

```bash
# 8. Run validated plugin startup hooks before the requested process
/usr/local/bin/run-plugin-starts

# 9. Execute requested command
```

Modify `docker-compose.yml` to mount or copy the generated plan at runtime and expose only generated plugin command dispatch; do not mount the entire registry writable. Add no new automatic network capability.

- [ ] **Step 7: Run Go tests and shell syntax validation**

Run:

```bash
go test ./... -count=1
bash -n scripts/resolve-plugins scripts/run-plugin-builds scripts/run-plugin-starts scripts/devbox-plugin entrypoint.sh
```

Expected: both commands exit 0.

- [ ] **Step 8: Commit generated Dev Container and lifecycle integration**

```bash
git add internal/devcontainer internal/plugins cmd/devbox .devcontainer scripts Dockerfile entrypoint.sh docker-compose.yml .gitignore
git commit -m "feat: run generated DevBox plugin lifecycle"
```

### Task 6: Add End-to-End Fixtures, Documentation, and Release Verification

**Files:**
- Create: `testdata/scaffold/.env.example`
- Create: `testdata/scaffold/devbox.plugins.yml`
- Create: `testdata/scaffold/.devcontainer/devcontainer.base.json`
- Create: `testdata/registry/plugins/base/plugin.yaml`
- Create: `testdata/registry/plugins/docker/plugin.yaml`
- Create: `testdata/registry/plugins/docker/build.sh`
- Create: `testdata/registry/plugins/docker/start.sh`
- Create: `testdata/registry/plugins/docker/commands/status.sh`
- Modify: `README.md`
- Modify: `.gitignore`

**Interfaces:**
- Consumes all public CLI commands and generated-file contracts from Tasks 1–5.
- Produces a no-network local integration suite demonstrating the complete expected workflow.

- [ ] **Step 1: Create a fixture registry with lifecycle and Dev Container contributions**

Create `testdata/registry/plugins/docker/plugin.yaml`:

```yaml
id: docker
name: Docker tooling
version: 1.0.0
requires:
  - base
options:
  compose:
    type: boolean
    default: true
hooks:
  build: build.sh
  start: start.sh
commands:
  status:
    path: commands/status.sh
    user: devbox
devcontainer:
  extensions:
    - ms-azuretools.vscode-docker
  containerEnv:
    DOCKER_HOST: unix:///var/run/docker.sock
  postCreateCommands:
    - docker version
```

Make `build.sh`, `start.sh`, and `commands/status.sh` executable fixture scripts that append a unique marker to `${DEVBOX_TEST_LOG:?}`. The `base` manifest has no hooks and provides only dependency coverage.

- [ ] **Step 2: Write a failing end-to-end workflow test**

Add `internal/integration/workflow_test.go`:

```go
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
```

Add tests that a later registry commit is ignored by `plugin resolve` until `plugin update`, local overrides leave the lock untouched, startup markers prevent a second hook run, and `plugin exec docker status` rejects an undeclared command.

- [ ] **Step 3: Run integration tests to verify they fail before missing helper coverage is added**

Run: `go test ./internal/integration -count=1`
Expected: FAIL until fixture helpers and all task interfaces are connected.

- [ ] **Step 4: Implement fixture helpers and make full integration coverage pass**

Use temporary local Git repositories; each test commits its scaffold and registry fixture, configures local Git author identity, and executes the compiled CLI through `go run ./cmd/devbox`. Capture stdout/stderr and fail tests with the complete command output. Do not use a network address or a shared filesystem fixture mutated by another test.

- [ ] **Step 5: Complete README command reference**

Document all supported commands exactly:

```text
devbox create <container-name> [--destination <path>] [--ref <git-ref>]
devbox plugin list
devbox plugin installed
devbox plugin install <id>
devbox plugin uninstall <id>
devbox plugin update [<id>]
devbox plugin <plugin-id> <command> [args...]
```

Explain version-controlled `devbox.plugins.yml` and `devbox.plugins.lock.yml`, ignored `devbox.plugins.local.yml`, `.generated/` and generated `.devcontainer/devcontainer.json`, non-reproducible local development mode, and the rule that only `install`/`update` access a Git registry.

- [ ] **Step 6: Run the complete verification suite**

Run:

```bash
go test ./... -count=1
bash -n entrypoint.sh scripts/*.sh
git diff --check
git status --short
```

Expected: all tests pass, all scripts parse, no whitespace errors, and status shows only intended tracked changes before committing.

- [ ] **Step 7: Commit fixtures, documentation, and verification coverage**

```bash
git add testdata internal/integration README.md .gitignore
git commit -m "test: verify DevBox plugin workflow"
```

## Plan Self-Review

- **Spec coverage:** Task 4 implements the host-side `create` workflow, destination/name safeguards, scaffold ref, preserved Git metadata, `.env` setup, cleanup, and no implicit plugin/build/container actions. Tasks 2–3 implement pinned Git and local-path sources, lock behavior, validated catalog configuration, and all requested plugin CLI actions. Task 5 implements build/start/command lifecycle and generated Dev Container merging. Task 6 verifies the end-to-end workflow, explicit update behavior, local override, idempotent startup, and command safety.
- **Placeholder scan:** No `TBD`, `TODO`, deferred implementation markers, or unnamed files remain. Placeholder `<org>` and `<plugin-id>` appear only where the specification deliberately represents distribution-time URL and user-provided command values; the implementation defines a configurable scaffold URL constant/environment seam.
- **Type consistency:** `config.ProjectState` flows into `registry.Materialize`; `registry.Source` flows into `plugins.Resolve`; `config.ResolvedPlan` flows into both plan persistence and `devcontainer.Merge`; all task interfaces use these exact names.
