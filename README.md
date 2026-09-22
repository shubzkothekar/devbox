# DevBox — Multi-Runtime Development Container

A high-performance, modular Linux development container designed for macOS via **Colima**. Features configurable runtimes (**Go**, **Node.js**, **Python**, **Bun**, **Rust**), isolated container-level **OpenVPN**, built-in **OpenSSH server**, database CLIs, and direct integration with **Zed**, **VS Code**, and terminal workflows.

---

## 📋 Table of Contents

- [Overview & Architecture](#-overview--architecture)
- [Prerequisites](#-prerequisites)
- [Create a DevBox Project](#create-a-devbox-project)
- [Plugin CLI Reference & Lifecycle](#plugin-cli-reference--lifecycle)
  - [Command Reference](#command-reference)
  - [Configuration Files & Reproducibility](#configuration-files--reproducibility)
  - [Lifecycle & Execution Model](#lifecycle--execution-model)
- [Quick Start](#-quick-start)
- [Runtime Configuration via `.env`](#-runtime-configuration-via-env)
  - [Available Runtime Flags](#available-runtime-flags)
  - [Example Presets](#example-presets)
  - [Applying Runtime Changes](#applying-runtime-changes)
- [Connecting to DevBox](#-connecting-to-devbox)
  - [1. Terminal SSH (Recommended)](#1-terminal-ssh-recommended)
  - [2. Zed Editor](#2-zed-editor)
  - [3. VS Code](#3-vs-code)
  - [4. Docker Exec Fallback](#4-docker-exec-fallback)
- [Built-In Helper Tools](#-built-in-helper-tools)
- [Workspace & Multi-Project Layout](#-workspace--multi-project-layout)
- [Git & Private Module Authentication](#-git--private-module-authentication)
- [OpenVPN Integration](#-openvpn-integration)
- [Port Reference](#-port-reference)
- [Lifecycle & Maintenance Commands](#-lifecycle--maintenance-commands)

---

## 🎯 Overview & Architecture

DevBox provides a clean, reproducible development environment without polluting your host macOS system:

- **Configurable Runtimes**: Toggle Go, Node.js, Python, Bun, Rust, Database CLIs, and NATS CLI via `.env` flags before building. Install only what your projects need.
- **Apple Silicon Optimized**: Runs on Ubuntu 24.04 LTS with Colima using Apple's native Virtualization framework (`vz`) and `virtiofs` for high-speed file sharing.
- **Isolated OpenVPN Networking**: VPN connections terminate inside the container using Linux TUN/TAP (`/dev/net/tun`), providing internal VPC and database access without routing or disrupting your host Mac internet traffic.
- **No Permission Mismatches**: Matches your macOS user UID/GID (default `501:501`), avoiding permission conflicts on bind-mounted directories.
- **Passwordless SSH**: Automatically imports your host SSH public key (`~/.ssh/id_ed25519.pub`) on container launch.
- **Clean Decoupling**: The container scaffold acts as a standalone repository; your active project repositories reside inside `./workspace/` and are excluded from the container template's git tracking.

---

## 💻 Prerequisites

Ensure Homebrew, Colima, Docker CLI, and Docker Compose are installed on your Mac:

```bash
# Install Colima and Docker CLI tools via Homebrew
brew install colima docker docker-compose

# Ensure you have an SSH ed25519 key generated on your Mac
[ -f ~/.ssh/id_ed25519.pub ] || ssh-keygen -t ed25519 -C "devbox"
```

---

## Create a DevBox Project

Install the host-side `devbox` CLI, then create a new scaffold checkout:

```bash
devbox create my-api --ref v1.0.0
cd my-api
devbox plugin install docker
```

`create` preserves the scaffold repository's Git history, initializes `.env` from `.env.example`, and does not build or start a container. Use `--destination /absolute/or/relative/path` to choose a target other than `./my-api`.

---

## Plugin CLI Reference & Lifecycle

DevBox includes a reproducible, modular plugin system driven by declarative manifests.

### Command Reference

```text
devbox create <container-name> [--destination <path>] [--ref <git-ref>]
devbox plugin list
devbox plugin installed
devbox plugin install <id>
devbox plugin uninstall <id>
devbox plugin update [<id>]
devbox plugin <plugin-id> <command> [args...]
```

- **`devbox create <container-name> [--destination <path>] [--ref <git-ref>]`**:
  Clones the scaffold repository, preserves complete Git history, initializes `.env` from `.env.example` with `0600` permissions, and refuses to overwrite an existing directory.
- **`devbox plugin list`**:
  Lists all available plugins from the materialized registry catalog along with their versions and descriptions. Runs offline using `registry.ReadOnly`.
- **`devbox plugin installed`**:
  Lists all enabled plugins in the active project along with resolved catalog versions. Runs offline using `registry.ReadOnly`.
- **`devbox plugin install <id>`**:
  Connects to the configured Git registry, materializes a snapshot, enables `<id>` and its required dependencies in `devbox.plugins.yml`, pins the resolved 40-character Git commit in `devbox.plugins.lock.yml`, writes `.generated/plugins/plan.json`, and updates `.devcontainer/devcontainer.json`.
- **`devbox plugin uninstall <id>`**:
  Disables `<id>` in `devbox.plugins.yml`, updates `.generated/plugins/plan.json` and `.devcontainer/devcontainer.json`, while retaining the locked Git commit. Rejects uninstallation if another enabled plugin depends on `<id>`.
- **`devbox plugin update [<id>]`**:
  Re-fetches the configured Git registry ref, advances `devbox.plugins.lock.yml` to the latest commit, re-resolves all enabled plugins, and regenerates `.generated/plugins/plan.json` and `.devcontainer/devcontainer.json`.
- **`devbox plugin <plugin-id> <command> [args...]`**:
  Dispatches a declared plugin command. Validates that the command is declared in the plugin manifest, exports configuration options as environment variables (`DEVBOX_PLUGIN_<PLUGIN>_<OPTION>`), and executes the script as the declared user (or via `sudo -n` when `user: root`). Also accessible via `devbox plugin exec <plugin-id> <command> [args...]` or the container-level `devbox-plugin` wrapper.

### Configuration Files & Reproducibility

- **`devbox.plugins.yml`** *(version-controlled)*:
  Declares the project's canonical Git registry source and enabled plugins with optional parameter overrides.
- **`devbox.plugins.lock.yml`** *(version-controlled)*:
  Pins the exact 40-character Git commit hash of the registry. Ensures reproducible builds across team members and CI environments.
- **`devbox.plugins.local.yml`** *(git-ignored)*:
  Overrides the Git registry with a local file path (`source: path`, `path: /path/to/registry`) for plugin authoring and testing. In local development mode, lock files are neither read nor mutated, providing a non-reproducible override for active plugin iteration.
- **`.generated/`** *(git-ignored)*:
  Contains materialized plugin directories and `.generated/plugins/plan.json`. Generated files are strictly derived from the locked registry and project configuration.
- **`.devcontainer/devcontainer.json`** *(git-ignored)*:
  The active VS Code Dev Container configuration. Automatically merged from the tracked `.devcontainer/devcontainer.base.json` and all enabled plugin contributions (`extensions`, `containerEnv`, `mounts`, `postCreateCommands`, `forwardPorts`).

### Lifecycle & Execution Model

- **Registry Network Boundary**:
  Only `devbox plugin install` and `devbox plugin update` connect to the network to fetch Git refs. All other commands (`list`, `installed`, `resolve`, container builds, and runtime hooks) operate offline against the pinned, materialized snapshot.
- **Build Hooks (`hooks.build`)**:
  Executed during Docker image builds via `scripts/run-plugin-builds`. Runs before container launch with plugin options passed as validated environment variables.
- **Startup Hooks (`hooks.start`)**:
  Executed during container startup via `scripts/run-plugin-starts` in `entrypoint.sh`. Writes an idempotent marker (`/var/lib/devbox/plugins/<id>.started`) upon successful completion to ensure hooks run exactly once per container lifecycle.
- **Command Dispatch (`commands.<name>`)**:
  Declared CLI utilities invoked on demand inside or outside the container, ensuring scoped execution and permission enforcement.

---

## 🚀 Quick Start

### Step 1: Start Colima

Launch Colima with the recommended flags for Apple Silicon:

```bash
colima start --cpu 4 --memory 8 --disk 60 --vm-type vz --mount-type virtiofs
```

> **Tip:** You can adjust `--cpu` and `--memory` to match your machine's hardware specifications.

### Step 2: Configure Environment Variables

```bash
cd devbox-container
cp .env.example .env
```

Edit `.env` to enable your desired runtimes and supply your GitHub token:

```ini
# Enable desired runtimes
INSTALL_GO=true
INSTALL_NODE=true
INSTALL_PYTHON=true
INSTALL_BUN=false
INSTALL_RUST=false

# GitHub token for cloning private repos and pulling private Go modules
GITHUB_TOKEN=ghp_your_token_here
```

### Step 3: Build & Start Container

```bash
docker compose up -d --build
```

### Step 4: Verify Container Status

```bash
# Check running container
docker compose ps

# Inspect active runtimes inside the container
ssh -p 2222 devbox@localhost devbox-info
```

---

## ⚙️ Runtime Configuration via `.env`

All toolchains and utilities are controlled through build-time arguments mapped directly to variables in `.env`. Set any runtime to `false` to skip its installation and produce a smaller, faster container image.

### Available Runtime Flags

| Environment Variable | Default | Description | Valid Values / Example |
| :--- | :--- | :--- | :--- |
| `INSTALL_GO` | `true` | Installs Golang compiler and standard tools | `true`, `false` |
| `GO_VERSION` | `1.24.5` | Golang version to download and install | `1.24.5`, `1.23.6` |
| `INSTALL_NODE` | `true` | Installs Node.js LTS, `npm`, `pnpm`, and `yarn` | `true`, `false` |
| `NODE_VERSION` | `22` | NodeSource major version | `22`, `20`, `18` |
| `INSTALL_PYTHON` | `true` | Installs Python 3, `pip`, `venv`, dev headers, and `pipx` | `true`, `false` |
| `INSTALL_BUN` | `false` | Installs Bun runtime and bundler (`bun`, `bunx`) | `true`, `false` |
| `INSTALL_RUST` | `false` | Installs Rust toolchain (`rustup`, `rustc`, `cargo`) | `true`, `false` |
| `INSTALL_DB_CLI` | `true` | Installs `default-mysql-client`, `mongodb-mongosh`, and `redis-tools` | `true`, `false` |
| `INSTALL_NATS` | `true` | Installs official NATS CLI (`nats`) | `true`, `false` |
| `INSTALL_GH` | `true` | Installs official GitHub CLI (`gh`) | `true`, `false` |
| `INSTALL_GLAB` | `true` | Installs official GitLab CLI (`glab`) | `true`, `false` |
| `GLAB_VERSION` | `1.118.0` | GitLab CLI release version for deb package | `1.118.0` |

### System & Authentication Flags

| Environment Variable | Default | Description |
| :--- | :--- | :--- |
| `DEV_USERNAME` | `devbox` | Container non-root user name |
| `DEV_UID` | `501` | Host UID mapping (run `id -u` on Mac) |
| `DEV_GID` | `501` | Host GID mapping (run `id -g` on Mac) |
| `SSH_PORT` | `2222` | Host port mapped to container SSH port 22 |
| `SSH_PUBKEY_PATH` | `~/.ssh/id_ed25519.pub` | Host public SSH key mounted for login |
| `GIT_USER_NAME` | `""` | Configures `git config --global user.name` and author name |
| `GIT_USER_EMAIL` | `""` | Configures `git config --global user.email` and author email |
| `GITHUB_TOKEN` / `GH_TOKEN` | `""` | GitHub PAT for private git repos, private Go modules, and `gh` CLI |
| `GITLAB_TOKEN` / `GLAB_TOKEN` | `""` | GitLab token for private GitLab repositories and `glab` CLI |
| `GITLAB_HOST` | `gitlab.com` | Self-hosted or SaaS GitLab host domain |
| `GIT_ORG` | `""` | Default GitHub organization (e.g. `your-org`) |
| `GOPRIVATE` | `""` | Private module glob patterns for Go (e.g. `github.com/your-org/*`) |
| `GOPROXY` | `https://proxy.golang.org,direct` | Go module download proxy |
| `GOTOOLCHAIN` | `auto` | Automatic Go toolchain switching |
| `AUTO_CONNECT_VPN` | `false` | Automatically connect to OpenVPN on container startup |

---

### Example Presets

#### 1. Full-Stack Polyglot (Go + Node.js + Python + DB Tools)
```ini
INSTALL_GO=true
GO_VERSION=1.24.5
INSTALL_NODE=true
NODE_VERSION=22
INSTALL_PYTHON=true
INSTALL_BUN=false
INSTALL_RUST=false
INSTALL_DB_CLI=true
INSTALL_NATS=true
```

#### 2. Modern Frontend & Fast JavaScript (Node.js + Bun)
```ini
INSTALL_GO=false
INSTALL_NODE=true
NODE_VERSION=22
INSTALL_PYTHON=false
INSTALL_BUN=true
INSTALL_RUST=false
INSTALL_DB_CLI=false
INSTALL_NATS=false
```

#### 3. Systems Programming (Go + Rust)
```ini
INSTALL_GO=true
GO_VERSION=1.24.5
INSTALL_NODE=false
INSTALL_PYTHON=false
INSTALL_BUN=false
INSTALL_RUST=true
INSTALL_DB_CLI=true
INSTALL_NATS=true
```

#### 4. Python Data & Automation
```ini
INSTALL_GO=false
INSTALL_NODE=false
INSTALL_PYTHON=true
INSTALL_BUN=false
INSTALL_RUST=false
INSTALL_DB_CLI=true
INSTALL_NATS=false
```

---

### Applying Runtime Changes

Whenever you modify any `INSTALL_*` or `*_VERSION` flags in `.env`, rebuild the container image:

```bash
docker compose up -d --build
```

Docker layer caching will reuse cached base layers and re-run only the modified runtime installation steps.

---

## 🔑 Connecting to DevBox

### 1. Terminal SSH (Recommended)

Add a host entry to `~/.ssh/config` on your macOS host:

```sshconfig
Host devbox
    HostName 127.0.0.1
    Port 2222
    User devbox
    IdentityFile ~/.ssh/id_ed25519
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
```

Now you can connect instantly from any terminal window:

```bash
ssh devbox
```

Or connect directly without modifying SSH config:

```bash
ssh -p 2222 devbox@localhost
```

*(Default password fallback is `devbox` with passwordless `sudo` privileges).*

---

### 2. Zed Editor

Zed supports seamless remote editing over SSH. Once your `~/.ssh/config` includes `Host devbox`, open your workspace:

```bash
# Open the entire workspace
zed ssh://devbox/workspace

# Or open a specific project directory
zed ssh://devbox/workspace/my-project
```

Inside Zed, integrated terminals open directly into `/workspace` with all configured runtimes ready.

---

### 3. VS Code

You can connect to DevBox using either of two methods:

#### Option A: Remote - SSH Extension
1. Install the **Remote - SSH** extension (`ms-vscode-remote.remote-ssh`).
2. Press `Cmd + Shift + P` -> **Remote-SSH: Connect to Host...** -> choose `devbox`.
3. Open Folder: `/workspace`.

#### Option B: Dev Containers Extension
1. Install the **Dev Containers** extension (`ms-vscode-remote.remote-containers`).
2. Open the project root folder in VS Code.
3. Click the bottom-left green button or press `Cmd + Shift + P` -> **Dev Containers: Reopen in Container**.

---

### 4. Docker Exec Fallback

If SSH is unavailable, connect directly via the Docker CLI:

```bash
docker exec -it -u devbox devbox bash
```

---

## 🧰 Built-In Helper Tools

DevBox includes customized helper scripts accessible from anywhere in the container:

### `devbox-info`
Displays an overview of all active runtimes, installed versions, database CLIs, OpenVPN status, and workspace mounts:

```bash
devbox-info
```

### `connect-vpn [path-to-ovpn]`
Connects to OpenVPN in the background, checks TUN interface creation, and verifies DNS and routes. Automatically searches for `.ovpn` files in `/workspace/vpn`.

```bash
connect-vpn
```

### `setup-git-auth`
Configures Git URL rewriting (`url.https://TOKEN@github.com/.insteadOf`) and populates `~/.netrc` using your `GITHUB_TOKEN`. Automatically executed on container startup if `GITHUB_TOKEN` is set.

```bash
setup-git-auth
```

---

## 📁 Workspace & Multi-Project Layout

The host directory `./workspace` is mounted directly into the container at `/workspace`:

```
devbox-container/
├── .env.example          # Template configuration
├── .env                  # Active configuration (ignored by git)
├── .gitignore            # Ignores .env, vpn/*.ovpn, and workspace/*
├── Dockerfile            # Container image definition with configurable runtimes
├── docker-compose.yml    # Multi-runtime service orchestration
├── entrypoint.sh         # Container initialization script
├── scripts/
│   ├── devbox-env.sh     # System-wide PATH and environment setup
│   ├── devbox-info.sh    # devbox-info command
│   ├── connect-vpn.sh    # connect-vpn command
│   └── setup-git-auth.sh # setup-git-auth command
├── vpn/                  # OpenVPN profiles and auth files (ignored by git)
│   ├── .gitkeep
│   └── README.md
└── workspace/            # Project repositories root (ignored by git)
    ├── .gitkeep
    ├── README.md
    ├── Engage/           # e.g., Go microservices + React frontend
    └── CRM/              # e.g., Node.js TypeScript services
```

### Working with Repositories Inside Workspace

Clone your independent project repositories directly into `workspace/`:

```bash
cd workspace/
git clone git@github.com:my-org/my-service.git
```

Each subfolder in `workspace/` maintains its own independent Git history and remotes. The root DevBox repository tracks only the container infrastructure.

---

## 🔐 Git & Private Module Authentication

### Git Identity (Name & Email)

Set your Git committer name and email in `.env`:

```ini
GIT_USER_NAME="Your Name"
GIT_USER_EMAIL="you@example.com"
```

On container boot or whenever you open an interactive shell / SSH session, DevBox automatically:
- Configures `git config --global user.name` and `git config --global user.email`.
- Exports `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, and `GIT_COMMITTER_EMAIL`.
- Ensures all commits made via terminal, VS Code, or Zed carry the proper identity.

### GitHub Personal Access Token (PAT) & `gh` CLI

When working with private repositories, private Go/Node dependencies, or the GitHub CLI:

1. Create a GitHub Personal Access Token with `repo` scope (or fine-grained token with read access to code and packages).
2. Set it in `.env`:
   ```ini
   GITHUB_TOKEN=ghp_yourPersonalAccessTokenHere
   ```
3. When the container boots, `entrypoint.sh` and `devbox-env.sh` automatically:
   - Configures Git URL rewriting so `git clone https://github.com/...` and `git clone git@github.com:...` use your token transparently.
   - Generates `~/.netrc` with GitHub credentials so tools like `go get` and `curl` authenticate without interactive prompts.
   - Exports `GH_TOKEN`, enabling the official `gh` CLI immediately with zero extra login steps.

### GitLab Token & `glab` CLI

If using GitLab (cloud or self-hosted):

```ini
GITLAB_TOKEN=glpat-yourPersonalAccessTokenHere
GITLAB_HOST=gitlab.com
```

DevBox automatically:
- Rewrites GitLab HTTPS and SSH URLs to use your OAuth/PAT token.
- Adds credentials to `~/.netrc` for seamless cloning.
- Exports `GLAB_TOKEN`, enabling the official `glab` CLI immediately.

### Go Private Modules

`GOPRIVATE` is exported system-wide based on your `.env` setting:

```ini
GOPRIVATE=github.com/your-org/*
GOPROXY=https://proxy.golang.org,direct
GOTOOLCHAIN=auto
```

This prevents Go from leaking private module paths to public proxies and forces direct fetching using your authenticated Git configuration.

---

## 🛡️ OpenVPN Integration

DevBox isolates your VPN connection inside the container. This means you can access internal databases, staging VPCs, and internal services without routing your macOS host traffic through the VPN.

### Setup Instructions

1. **Place your `.ovpn` file** into `./vpn/`:
   ```bash
   cp /path/to/my-company.ovpn ./vpn/
   ```

2. **(Optional) Automated Credentials**:
   If your VPN requires a username and password, create `./vpn/auth.txt`:
   ```
   my_vpn_username
   my_vpn_password
   ```
   The `connect-vpn` script will automatically detect and pass `--auth-user-pass /workspace/vpn/auth.txt`.

3. **Connect**:
   Inside the container (via SSH or editor terminal):
   ```bash
   connect-vpn
   ```

4. **Verify Connectivity**:
   ```bash
   # Check tun0 interface
   ip a show tun0

   # Ping internal resources
   ping 10.x.x.x
   ```

5. **Disconnect**:
   ```bash
   sudo killall openvpn
   ```

6. **Auto-Connect on Boot**:
   Set `AUTO_CONNECT_VPN=true` in your `.env` to connect automatically when the container starts.

---

## 🔌 Port Reference

| Category | Service / Tool | Container Port | Host Port | Configuration |
| :--- | :--- | :--- | :--- | :--- |
| **System** | **SSH Server** | `22` | `2222` | `${SSH_PORT:-2222}` in `.env` |
| **Engage** | Vite Dev Server | `5173` | `5173` | `http://localhost:5173` |
| **Engage** | `auth-service` | `3100` | `3100` | Go Fiber API |
| **Engage** | `api-backend` | `3150` | `3150` | Go Fiber API |
| **Engage** | `cronjob-service` | `3300` | `3300` | Go Fiber API |
| **Engage** | `ai-flow` | `3530` | `3530` | Go Fiber API |
| **Engage** | `api-ingestion-service` | `3720` | `3720` | Go Fiber API |
| **Engage** | `api-processing-service` | `3740` | `3740` | Go Fiber API |
| **CRM** | Frontend | `3005` | `3005` | `http://localhost:3005` |
| **CRM** | `user-service` | `3400` | `3400` | Node.js Express API |
| **CRM** | `webhook-service` | `3600` | `3600` | Node.js Express API |
| **CRM** | `leadbot-backend` | `3800` | `3800` | Node.js Express API |
| **CRM** | `workflow-automation-service` | `3900` | `3900` | Node.js Express API |
| **CRM** | `integration-service` | `4002` | `4002` | Node.js Express API |
| **CRM** | `webhook-sender` | `4003` | `4003` | Node.js Express API |
| **CRM** | `backend-service` | `3300` | `3300` | Node.js Express API |

*(To expose additional ports, add them under `ports:` in `docker-compose.yml` or use a `docker-compose.override.yml` file).*

---

## 🛠️ Lifecycle & Maintenance Commands

### Start / Stop Container
```bash
cd devbox-container

# Start in background
docker compose up -d

# Stop container
docker compose stop

# Stop and remove container
docker compose down
```

### View Logs
```bash
# Follow container logs
docker compose logs -f

# Follow OpenVPN logs inside the container
ssh devbox "tail -f /var/log/openvpn.log"
```

### Clean Rebuild
To rebuild from scratch without using Docker build cache:
```bash
docker compose build --no-cache
docker compose up -d
```

### Clear Go Module Cache Volume
```bash
docker compose down -v
```
