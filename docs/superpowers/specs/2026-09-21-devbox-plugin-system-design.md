# DevBox Plugin System Design

## Goal

Allow DevBox users to adapt a development container through version-controlled plugins maintained in a separate `devbox-registry` repository. A plugin may customize the image build, container startup, developer-invoked commands, and additive Dev Container settings. Version 1 supports a pinned Git registry for reproducible use and a local-path registry override for plugin development.

## Scope

- Plugin selection and its canonical Git registry source are declared in committed `devbox.plugins.yml`.
- `devbox.plugins.lock.yml` records the exact resolved registry commit used for reproducible builds.
- An ignored `devbox.plugins.local.yml` can replace the Git source with a local registry path during plugin development.
- Plugins can install/configure image dependencies during Docker build.
- Plugins can perform idempotent setup at container startup.
- The `devbox plugin` CLI can list registry plugins, show installed plugins, install, uninstall, update, and dispatch plugin commands.
- Plugins can contribute additive, structured Dev Container configuration.
- A resolver validates configuration and generates a deterministic build/runtime plan.

Version 1 excludes multiple registries, arbitrary source URLs per plugin, remote code execution outside the configured registry, arbitrary patches to tracked `.devcontainer/devcontainer.base.json`, and automatic plugin execution outside declared lifecycle hooks.

## Architecture

```text
devbox.plugins.yml + devbox.plugins.lock.yml
                  |                    ^
                  v                    |
     configured devbox-registry         | install/update resolves Git commit
                  |
                  v
          plugin resolver
  source selection -> discovery -> validation -> dependency ordering -> merge validation
                  |
                  v
          .generated/plugins/
          .devcontainer/devcontainer.json
                  |
                  +--> Docker image build hooks
                  +--> container startup hooks
                  +--> devbox plugin CLI
```

The resolver is the trust boundary. It materializes exactly one configured registry source, validates plugin identity and requested options, resolves dependencies, rejects conflicts, and writes generated files consumed by Docker, startup, and Dev Container integration. Normal Git-backed builds use only the exact commit in `devbox.plugins.lock.yml`; Docker and runtime scripts execute the generated resolved plan rather than independently accessing the registry.

## Repository Layout

```text
# DevBox repository
devbox.plugins.yml           # configured Git registry and enabled plugin options
devbox.plugins.lock.yml      # resolved Git commit; version-controlled
devbox.plugins.local.yml     # ignored local-path registry override
scripts/
  resolve-plugins
  sync-plugin-registry
  run-plugin-builds
  run-plugin-starts
  devbox-plugin
.generated/
  registry/                  # materialized Git registry snapshot; ignored by Git
  plugins/                   # normalized resolved plugin plan; ignored by Git
.devcontainer/
  devcontainer.base.json     # DevBox-owned, version-controlled base configuration
  devcontainer.json          # generated final configuration; ignored by Git

# Separate devbox-registry repository
plugins/
  <plugin-id>/
    plugin.yaml
    build.sh                 # optional
    start.sh                 # optional
    commands/
      <command>.sh           # optional
```

The concrete implementation language for the CLI and resolver should use project-standard tooling and avoid a new runtime dependency where practical. The resolver must run before `docker compose build` and before Dev Container tooling consumes generated configuration.

## Registry Sources and User Configuration

`devbox.plugins.yml` is version controlled. It declares one canonical Git registry and enabled plugin options:

```yaml
registry:
  source: git
  url: https://github.com/<org>/devbox-registry.git
  ref: v1.0.0
plugins:
  docker:
    enabled: true
    options:
      compose: true
  terraform:
    enabled: false
```

`devbox.plugins.lock.yml` is version controlled and records the immutable registry commit resolved from `url` and `ref`:

```yaml
registry:
  url: https://github.com/<org>/devbox-registry.git
  ref: v1.0.0
  commit: <40-character-git-commit>
```

`devbox.plugins.local.yml` is ignored and supports registry development without publishing changes:

```yaml
registry:
  source: path
  path: ../devbox-registry
```

A local override replaces the configured Git source only for that checkout. It is explicitly reported as non-reproducible development mode and is never written to the lock file. Without an override, builds require a matching lock file and materialize only its recorded commit. Only declared, enabled plugins participate in a build or runtime plan. Unknown plugin IDs, undeclared options, invalid values, stale locks, or duplicate declarations fail resolution.

## Plugin Manifest

Each `devbox-registry/plugins/<plugin-id>/plugin.yaml` declares the plugin contract:

```yaml
id: docker
name: Docker tooling
version: 1.0.0
description: Installs Docker CLI tooling and optional Compose support.
requires: []
conflicts: []
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
  mounts:
    - source=docker-sock,target=/var/run/docker.sock,type=bind
  containerEnv:
    DOCKER_HOST: unix:///var/run/docker.sock
  postCreateCommands:
    - docker version
```

Manifest constraints:

- `id` is unique and matches the plugin directory name.
- `requires` and `conflicts` reference plugin IDs.
- Options have explicit types, defaults, and enum/range restrictions when applicable.
- Hook and command paths are relative to the plugin directory, are regular files, and cannot escape that directory.
- A command declares whether it runs as `devbox` (default) or root.
- All hooks must be non-interactive.

## Lifecycle

### Resolution

`resolve-plugins` performs the following in a stable order:

1. Select `devbox.plugins.local.yml` when present; otherwise require the configured Git registry and matching lock file.
2. Materialize the local registry path or the lock-file Git commit under `.generated/registry/`.
3. Discover `.generated/registry/plugins/*/plugin.yaml`.
4. Validate manifests and the user configuration.
5. Apply option defaults and validate supplied values.
6. Ensure enabled dependencies are enabled and detect conflicts/cycles.
7. Topologically sort enabled plugins; plugins unrelated by dependency sort lexicographically by ID.
8. Merge Dev Container contributions and reject collisions.
9. Emit the normalized resolved plugin plan, copied hook sources or explicit safe references, and generated Dev Container configuration.

### Image Build

Docker invokes the generated build plan after foundational dependencies are installed. Build hooks run as root, in resolved order, with validated options exposed through a documented namespaced environment format such as `DEVBOX_PLUGIN_DOCKER_COMPOSE=true`. A build-hook failure fails the image build. Disabled plugins are not copied into or executed from the build plan.

### Container Startup

The entrypoint invokes generated startup hooks after mounts and standard DevBox environment initialization. Hooks run in resolved order and must be idempotent. A plugin completion marker is written only after successful completion. A failure is reported with the plugin ID and exits startup with a non-zero status; it does not create a completion marker.

### Registry and Developer Commands

The `devbox plugin` CLI owns registry synchronization and plugin configuration changes:

```text
devbox plugin list
devbox plugin installed
devbox plugin install <id>
devbox plugin uninstall <id>
devbox plugin update [<id>]
devbox plugin <plugin-id> <command> [args...]
```

- `list` displays the catalog at the locked Git commit, or the active local path in development mode.
- `installed` displays enabled plugin configuration, resolved versions, dependency state, and the source mode.
- `install` fetches the configured Git `ref`, resolves and validates its immutable commit, validates the requested plugin and dependency closure, enables the requested plugin, and atomically writes `devbox.plugins.yml` and `devbox.plugins.lock.yml`. In local-path mode it validates and enables from the local registry without writing a lock.
- `uninstall` removes or disables the requested plugin configuration after ensuring no enabled plugin requires it. It retains the current Git lock because the registry source has not changed.
- `update` is the only command that advances the Git registry pin. It fetches the configured ref, resolves a new commit, validates all enabled plugins, then atomically writes the lock. With a plugin ID, it additionally verifies that plugin remains present and compatible after the refresh.
- `devbox plugin <plugin-id> <command> [args...]` dispatches only commands declared by an enabled plugin. Commands run as `devbox` unless the manifest explicitly requests root. The dispatcher validates the plugin and command before execution and passes only validated plugin options plus user-supplied command arguments.

Git registry operations may use the network only through explicit `install` or `update`; resolution, image builds, startup, command dispatch, `list`, and `installed` use the local materialized locked snapshot. If the snapshot is absent or does not match the lock, they fail with an instruction to run `devbox plugin install` or `devbox plugin update`.

## Dev Container Contributions

Plugins may contribute only additive, typed fields through the `devcontainer` manifest section. Initial supported fields are:

- `extensions`
- `mounts`
- `containerEnv`
- `postCreateCommands`
- `forwardPorts`

Because Dev Container tooling accepts a single JSON configuration rather than an include/merge directive, DevBox stores the tracked base at `.devcontainer/devcontainer.base.json`. The resolver merges enabled plugin contributions into generated `.devcontainer/devcontainer.json`, which is the file consumed by Dev Container tooling and is ignored by Git. Plugins never directly rewrite or patch the tracked base file.

Merge rules are explicit:

- Lists are deduplicated while preserving dependency-resolved plugin order.
- Environment variables with the same key and different values conflict.
- Mounts targeting the same destination with different definitions conflict.
- Duplicate ports are deduplicated; incompatible port metadata conflicts.
- Lifecycle commands append in resolved order.
- Unsupported or arbitrary Dev Container fields fail resolution.

## Security and Reliability

- Version 1 supports exactly one registry source per DevBox checkout: a pinned Git registry or an ignored local-path development override.
- Git sources may be fetched only by explicit `install` or `update`; all build and runtime operations use the materialized lock-file commit.
- A normal build rejects a missing, stale, or mismatched lock file; local-path mode is visibly marked non-reproducible.
- Resolver path validation prevents traversal and symlink escapes outside the materialized registry and each plugin directory.
- Plugin scripts are executed only if referenced by a validated manifest and included in the generated plan.
- Plugin option values are passed as data, not interpolated into generated shell code.
- Resolver and hook logs prefix output with the plugin ID and lifecycle stage.

## Failure Handling

Resolution fails before a Docker or Dev Container operation for an absent, stale, or mismatched Git lock; inaccessible registry snapshot; unknown plugin IDs/options; duplicate IDs; invalid option values; missing dependencies; conflicts; dependency cycles; unsafe paths; unsupported contributions; and merge conflicts.

`install` and `update` leave configuration and locks unchanged if fetch, commit resolution, catalog validation, dependency resolution, or atomic write fails. Build-hook failures stop the Docker image build. Startup-hook failures are visible and do not record successful state. Command dispatch returns a clear error for disabled plugins, unknown commands, or unavailable generated state.

## Testing

Unit tests cover:

- Git and local-path source selection, lock validation, and snapshot materialization;
- plugin discovery and manifest identity validation;
- option defaults and schema validation;
- dependency ordering, missing dependencies, conflicts, and cycles;
- hook/command path safety;
- `list`, `installed`, `install`, `uninstall`, and `update` atomic configuration/lock behavior;
- Dev Container merge, deduplication, and conflict rejection;
- generated-plan determinism.

Integration tests use a fixture registry to verify that Git installs resolve and lock immutable commits, updates advance only when explicitly requested, local-path overrides bypass locking and report development mode, enabled build hooks execute, disabled hooks do not execute, startup hooks remain idempotent across restarts, command dispatch respects declared users, and generated Dev Container contributions are valid and deterministic.

## Acceptance Criteria

A user can install a plugin from the separately maintained `devbox-registry`, which fetches the configured Git ref, records its immutable commit in `devbox.plugins.lock.yml`, validates the plugin, and enables it in `devbox.plugins.yml`. Builds reproduce the locked catalog without network access. A local-path override supports registry development and is marked non-reproducible. Installed plugins can be listed, uninstalled safely, explicitly updated, perform idempotent startup setup, expose declared commands, and add supported Dev Container configuration without modifying the tracked base `.devcontainer/devcontainer.base.json`. Invalid configuration fails before build or startup with an actionable error.
