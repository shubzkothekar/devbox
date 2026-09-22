# AGENTS.md — AI Agent Guidelines for DevBox

This document provides operational instructions, architectural guidelines, and safety invariants for autonomous AI coding agents working in the `devbox` repository.

---

## 1. Repository Overview

DevBox is a dual-layer project:
1. **Host-Side Go CLI (`cmd/devbox`)**: Bootstraps new project scaffolds (`devbox create`), resolves declarative plugin dependencies, materializes pinned Git registry snapshots, merges Dev Container configurations, and executes lifecycle hooks and commands.
2. **Container Runtime Scaffold**: An Ubuntu 24.04-based container environment optimized for macOS (via Colima/Docker) featuring multi-language runtimes (Go, Node.js, Python, Bun, Rust), internal OpenVPN termination, SSH server, and an offline-first plugin execution model.

---

## 2. Directory Structure & Key Components

```
devbox/
├── .devcontainer/
│   ├── devcontainer.base.json   # Base Dev Container template (tracked in Git)
│   └── devcontainer.json        # Merged Dev Container configuration (gitignored, generated)
├── .generated/                  # Materialized plugin payloads & plan.json (gitignored)
│   └── plugins/
│       ├── <plugin-id>/         # Materialized plugin payloads for image builds
│       └── plan.json            # Deterministic, JSON-serialized resolution plan
├── cmd/
│   └── devbox/                  # Host DevBox CLI entry point (main.go)
├── internal/                    # Internal Go packages
│   ├── config/                  # Strict YAML/JSON configurations, lockfile, atomic I/O
│   ├── devcontainer/            # Deterministic Dev Container merge engine
│   ├── integration/             # Hermetic end-to-end integration test suite
│   ├── plugins/                 # Dependency resolution, catalog discovery, lifecycle service
│   ├── project/                 # Scaffold clone & project bootstrap engine
│   └── registry/                # Pinned Git & local path materialization engine
├── scripts/                     # Shell adapters and lifecycle helpers
│   ├── devbox-plugin            # In-container command dispatcher wrapper
│   ├── resolve-plugins          # Offline plugin plan resolution adapter
│   ├── run-plugin-builds        # Docker build-time plugin hook runner
│   ├── run-plugin-starts        # Container startup plugin hook runner (with markers)
│   ├── connect-vpn.sh           # OpenVPN connection helper
│   ├── devbox-env.sh            # Container environment initialization
│   ├── devbox-info.sh           # Runtime diagnostics inspector
│   └── setup-git-auth.sh        # Git authentication configuration
├── testdata/                    # Realistic test fixtures for registry and scaffold
├── devbox.plugins.yml           # Declarative plugin selections (tracked in Git)
├── devbox.plugins.lock.yml      # Pinned Git commit lockfile (tracked in Git)
├── devbox.plugins.local.yml     # Local registry path override (gitignored)
├── Dockerfile                   # Multi-stage image build with plugin hooks
├── docker-compose.yml           # Container service orchestration
└── entrypoint.sh                # Container initialization with plugin start hooks
```

---

## 3. Essential Commands & Verification Workflows

Always execute verification commands before completing tasks or proposing changes:

### Build
```bash
# Compile the host devbox CLI
go build -o devbox ./cmd/devbox

# Install to GOPATH/bin
go install ./cmd/devbox
```

### Test
```bash
# Run all Go unit and integration tests with zero caching
go test ./... -count=1

# Run only the hermetic integration tests
go test ./internal/integration -v -count=1

# Run tests for a specific package
go test ./internal/plugins -v -count=1
go test ./internal/devcontainer -v -count=1
go test ./internal/project -v -count=1
go test ./internal/registry -v -count=1
```

### Shell & Formatting Checks
```bash
# Validate shell script syntax
bash -n entrypoint.sh scripts/*

# Check for trailing whitespace, blank lines at EOF, and formatting issues
git diff --check
```

---

## 4. Architectural Invariants & Safety Rules

When modifying or extending this codebase, adhere strictly to these non-negotiable architectural rules:

### A. The Offline Registry Boundary
- **Network Access Rule**: Only `devbox plugin install` and `devbox plugin update` may perform network Git fetches.
- **Offline Operations**: `devbox plugin list`, `devbox plugin installed`, `devbox plugin resolve`, `scripts/resolve-plugins`, Docker image builds (`scripts/run-plugin-builds`), and container startups (`scripts/run-plugin-starts`) must run **entirely offline** against the locked, materialized snapshot in `.generated/registry` or `.generated/plugins`.

### B. Strict Decoding & Validation
- Always use strict decoding (`KnownFields(true)`) when parsing user configuration files (`devbox.plugins.yml`, `devbox.plugins.lock.yml`, `devbox.plugins.local.yml`) and plugin manifests (`plugin.yaml`). Reject unknown fields immediately to prevent silent typos.
- Lock commit hashes must be validated as 40-character hexadecimal strings (`^[0-9a-f]{40}$`).

### C. Atomic File Writes
- Never write configuration files, lockfiles, plan files, or Dev Container JSON directly to their target paths.
- Always use atomic write functions (`config.WriteYAMLAtomic`, `config.WriteJSONAtomic`, `config.WriteFileAtomic`) which write to a temporary file in the same directory and perform an atomic rename.

### D. Path Traversal & Escape Prevention
- Never trust relative paths specified in plugin manifests (`hooks.build`, `hooks.start`, `commands.<name>.path`).
- Paths must be relative, regular files, located strictly within the plugin directory. Reject paths that escape the plugin directory via `..` or symlinks.
- In `devbox create`, project names must match `^[A-Za-z0-9][A-Za-z0-9._-]*$` and contain no path separators (`filepath.Base(name) == name`).

### E. Dev Container Configuration Separation
- `.devcontainer/devcontainer.base.json` is the **only** tracked Dev Container file in Git.
- `.devcontainer/devcontainer.json` is **generated and gitignored**. Never commit generated Dev Container output to Git.
- Merging must be deterministic: extensions deduplicated in resolved dependency order, container environment variables merged (rejecting key collisions with conflicting values), and mounts deduplicated.

### F. Local Overrides Independence
- `devbox.plugins.local.yml` is for local plugin development and is gitignored.
- When an override is present (`source: path`), resolution bypasses Git locks and uses the local directory.
- Local overrides **must never read, require, or mutate** `devbox.plugins.lock.yml`.

### G. Hook Idempotency & Execution
- Startup hooks (`hooks.start`) run via `scripts/run-plugin-starts` during container boot.
- They must write a marker file to `/var/lib/devbox/plugins/<id>.started` (or `$DEVBOX_PLUGIN_MARKER_DIR/<id>.started`) and skip execution if the marker already exists.
- Plugin options are exported to hooks and commands as environment variables formatted as: `DEVBOX_PLUGIN_<UPPERCASE_ID>_<UPPERCASE_OPTION>`.

---

## 5. Testing Discipline

- **No Network in Tests**: Integration and unit tests must never connect to external network hosts. Use temporary local Git repositories (`git init -b main`, local paths as clone URLs) for testing scaffold creation, registry materialization, and updates.
- **Hermetic Test Directories**: Every test must operate within `t.TempDir()`. Never leave temporary artifacts or mutate shared fixtures.
- **Fail with Context**: When subcommands or scripts fail in tests, capture and print both `stdout` and `stderr` to make root causes immediately diagnosable.
