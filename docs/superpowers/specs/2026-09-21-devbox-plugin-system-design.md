# DevBox Plugin System Design

## Goal

Allow DevBox users to adapt a development container through local, version-controlled plugins. A plugin may customize the image build, container startup, developer-invoked commands, and additive Dev Container settings. Version 1 discovers plugins only from the repository's `plugins/` directory.

## Scope

- Plugin selection is declared in committed `devbox.plugins.yml`.
- Plugins can install/configure image dependencies during Docker build.
- Plugins can perform idempotent setup at container startup.
- Plugins can expose explicit developer commands through `devbox plugin`.
- Plugins can contribute additive, structured Dev Container configuration.
- A resolver validates configuration and generates a deterministic build/runtime plan.

Version 1 excludes remote registries, URL-based plugins, arbitrary source downloads by the plugin manager, arbitrary patches to tracked `.devcontainer/devcontainer.json`, and automatic plugin execution outside declared lifecycle hooks.

## Architecture

```text
devbox.plugins.yml + plugins/*/plugin.yaml
                  |
                  v
          plugin resolver
  discovery -> validation -> dependency ordering -> merge validation
                  |
                  v
          .generated/plugins/
          .devcontainer/devcontainer.json
                  |
                  +--> Docker image build hooks
                  +--> container startup hooks
                  +--> devbox plugin command dispatcher
```

The resolver is the trust boundary. It discovers only local manifests, validates their identity and requested options, resolves dependencies, rejects conflicts, and materializes generated files consumed by Docker, startup, and Dev Container integration. Docker and runtime scripts execute the generated resolved plan rather than independently discovering plugin files.

## Repository Layout

```text
devbox.plugins.yml
plugins/
  <plugin-id>/
    plugin.yaml
    build.sh                 # optional
    start.sh                 # optional
    commands/
      <command>.sh           # optional
scripts/
  resolve-plugins
  run-plugin-builds
  run-plugin-starts
  devbox-plugin
.generated/
  plugins/                   # resolver output; ignored by Git
.devcontainer/
  devcontainer.base.json     # DevBox-owned, version-controlled base configuration
  devcontainer.json          # generated final configuration; ignored by Git
```

The concrete implementation language for `resolve-plugins` should use project-standard tooling and avoid a new runtime dependency where practical. The resolver must be runnable before `docker compose build` and before Dev Container tooling consumes generated configuration.

## User Configuration

`devbox.plugins.yml` is version controlled and contains the enabled state and plugin-scoped options:

```yaml
plugins:
  docker:
    enabled: true
    options:
      compose: true
  terraform:
    enabled: false
```

Only declared, enabled plugins participate in a build or runtime plan. Unknown plugin IDs, undeclared options, invalid values, or duplicate declarations fail resolution.

## Plugin Manifest

Each `plugins/<plugin-id>/plugin.yaml` declares the plugin contract:

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

1. Discover `plugins/*/plugin.yaml`.
2. Validate manifests and the user configuration.
3. Apply option defaults and validate supplied values.
4. Ensure enabled dependencies are enabled and detect conflicts/cycles.
5. Topologically sort enabled plugins; plugins unrelated by dependency sort lexicographically by ID.
6. Merge Dev Container contributions and reject collisions.
7. Emit the normalized resolved plugin plan, copied hook sources or explicit safe references, and generated Dev Container configuration.

### Image Build

Docker invokes the generated build plan after foundational dependencies are installed. Build hooks run as root, in resolved order, with validated options exposed through a documented namespaced environment format such as `DEVBOX_PLUGIN_DOCKER_COMPOSE=true`. A build-hook failure fails the image build. Disabled plugins are not copied into or executed from the build plan.

### Container Startup

The entrypoint invokes generated startup hooks after mounts and standard DevBox environment initialization. Hooks run in resolved order and must be idempotent. A plugin completion marker is written only after successful completion. A failure is reported with the plugin ID and exits startup with a non-zero status; it does not create a completion marker.

### Developer Commands

`devbox plugin <plugin-id> <command> [args...]` dispatches only commands declared by an enabled plugin. Commands run as `devbox` unless the manifest explicitly requests root. The dispatcher validates the plugin and command before execution and passes only validated plugin options plus user-supplied command arguments.

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

- Version 1 has one local discovery root: `plugins/`.
- No remote registry lookups, plugin downloads, or URL references are supported.
- Resolver path validation prevents traversal and symlink escapes outside the plugin directory.
- Plugin scripts are executed only if referenced by a validated manifest and included in the generated plan.
- Plugin option values are passed as data, not interpolated into generated shell code.
- Resolver and hook logs prefix output with the plugin ID and lifecycle stage.

## Failure Handling

Resolution fails before a Docker or Dev Container operation for unknown plugin IDs/options, duplicate IDs, invalid option values, missing dependencies, conflicts, dependency cycles, unsafe paths, unsupported contributions, and merge conflicts.

Build-hook failures stop the Docker image build. Startup-hook failures are visible and do not record successful state. Command dispatch returns a clear error for disabled plugins, unknown commands, or unavailable generated state.

## Testing

Unit tests cover:

- plugin discovery and manifest identity validation;
- option defaults and schema validation;
- dependency ordering, missing dependencies, conflicts, and cycles;
- hook/command path safety;
- Dev Container merge, deduplication, and conflict rejection;
- generated-plan determinism.

Integration tests use fixture plugins to verify that enabled build hooks execute, disabled hooks do not execute, startup hooks remain idempotent across restarts, command dispatch respects declared users, and generated Dev Container contributions are valid and deterministic.

## Acceptance Criteria

A user can add a local plugin directory and its manifest, enable it through `devbox.plugins.yml`, and rebuild DevBox to apply its validated build behavior. The same plugin can perform idempotent startup setup, expose declared commands, and add supported Dev Container configuration without modifying the tracked base `.devcontainer/devcontainer.base.json`. Invalid configuration fails before build or startup with an actionable error.
