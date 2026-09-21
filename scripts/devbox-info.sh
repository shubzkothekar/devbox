#!/usr/bin/env bash
# =============================================================================
# DevBox Runtime & Environment Status Inspector
# =============================================================================

echo "================================================================="
echo "                DevBox Environment Status"
echo "================================================================="

# User & OS
echo "User:       $(whoami) (UID: $(id -u), GID: $(id -g))"
echo "OS:         $(uname -s) $(uname -r) ($(uname -m))"
if [ -f /etc/os-release ]; then
    . /etc/os-release
    echo "Distro:     $PRETTY_NAME"
fi

echo "-----------------------------------------------------------------"
echo "Active Runtimes & Toolchains:"
echo "-----------------------------------------------------------------"

# Go
if command -v go >/dev/null 2>&1; then
    echo "  Go:       $(go version) [GOROOT: ${GOROOT:-/usr/local/go}]"
else
    echo "  Go:       Not Installed (INSTALL_GO=false)"
fi

# Node.js
if command -v node >/dev/null 2>&1; then
    NODE_VER=$(node --version 2>/dev/null)
    NPM_VER=$(npm --version 2>/dev/null || echo "N/A")
    PNPM_VER=$(pnpm --version 2>/dev/null || echo "N/A")
    YARN_VER=$(yarn --version 2>/dev/null || echo "N/A")
    echo "  Node.js:  $NODE_VER (npm: $NPM_VER, pnpm: $PNPM_VER, yarn: $YARN_VER)"
else
    echo "  Node.js:  Not Installed (INSTALL_NODE=false)"
fi

# Python
if command -v python3 >/dev/null 2>&1; then
    PY_VER=$(python3 --version 2>/dev/null)
    PIP_VER=$(pip3 --version 2>/dev/null | awk '{print $2}')
    echo "  Python:   $PY_VER (pip: ${PIP_VER:-N/A})"
else
    echo "  Python:   Not Installed (INSTALL_PYTHON=false)"
fi

# Bun
if command -v bun >/dev/null 2>&1; then
    echo "  Bun:      $(bun --version)"
else
    echo "  Bun:      Not Installed (INSTALL_BUN=false)"
fi

# Rust
if command -v rustc >/dev/null 2>&1; then
    echo "  Rust:     $(rustc --version) ($(cargo --version))"
else
    echo "  Rust:     Not Installed (INSTALL_RUST=false)"
fi

echo "-----------------------------------------------------------------"
echo "Git & Collaboration Tools:"
echo "-----------------------------------------------------------------"

# Git
if command -v git >/dev/null 2>&1; then
    GIT_VER=$(git --version 2>/dev/null)
    GIT_NAME_CFG=$(git config --global user.name 2>/dev/null || echo "Not configured")
    GIT_EMAIL_CFG=$(git config --global user.email 2>/dev/null || echo "Not configured")
    echo "  Git:      $GIT_VER"
    echo "            Author: $GIT_NAME_CFG <$GIT_EMAIL_CFG>"
else
    echo "  Git:      Not Installed"
fi

# GitHub CLI (gh)
if command -v gh >/dev/null 2>&1; then
    GH_VER=$(gh --version 2>/dev/null | head -n 1)
    if [ -n "$GITHUB_TOKEN" ] || [ -n "$GH_TOKEN" ]; then
        echo "  gh:       Installed ($GH_VER) [Auth: Token Configured]"
    else
        echo "  gh:       Installed ($GH_VER) [Auth: No Token (Run 'gh auth login')]"
    fi
else
    echo "  gh:       Not Installed (INSTALL_GH=false)"
fi

# GitLab CLI (glab)
if command -v glab >/dev/null 2>&1; then
    GLAB_VER=$(glab --version 2>/dev/null | head -n 1)
    if [ -n "$GITLAB_TOKEN" ] || [ -n "$GLAB_TOKEN" ]; then
        echo "  glab:     Installed ($GLAB_VER) [Auth: Token Configured]"
    else
        echo "  glab:     Installed ($GLAB_VER) [Auth: No Token (Run 'glab auth login')]"
    fi
else
    echo "  glab:     Not Installed (INSTALL_GLAB=false)"
fi

echo "-----------------------------------------------------------------"
echo "Database & Messaging CLIs:"
echo "-----------------------------------------------------------------"

# MySQL
if command -v mysql >/dev/null 2>&1; then
    echo "  MySQL:    Installed ($(mysql --version 2>/dev/null | awk '{print $1, $2, $3, $4, $5}'))"
else
    echo "  MySQL:    Not Installed (INSTALL_DB_CLI=false)"
fi

# MongoDB mongosh
if command -v mongosh >/dev/null 2>&1; then
    echo "  mongosh:  Installed ($(mongosh --version 2>/dev/null))"
else
    echo "  mongosh:  Not Installed (INSTALL_DB_CLI=false)"
fi

# Redis
if command -v redis-cli >/dev/null 2>&1; then
    echo "  Redis:    Installed ($(redis-cli --version 2>/dev/null))"
else
    echo "  Redis:    Not Installed (INSTALL_DB_CLI=false)"
fi

# NATS CLI
if command -v nats >/dev/null 2>&1; then
    echo "  NATS:     Installed ($(nats --version 2>/dev/null || echo 'nats cli'))"
else
    echo "  NATS:     Not Installed (INSTALL_NATS=false)"
fi

echo "-----------------------------------------------------------------"
echo "Network & VPN Status:"
echo "-----------------------------------------------------------------"
if ip addr show dev tun0 >/dev/null 2>&1; then
    TUN_IP=$(ip addr show dev tun0 | grep "inet " | awk '{print $2}')
    echo "  OpenVPN:  CONNECTED (tun0: $TUN_IP)"
else
    echo "  OpenVPN:  DISCONNECTED (Run 'connect-vpn' to establish connection)"
fi

echo "-----------------------------------------------------------------"
echo "Workspace Mount:"
echo "-----------------------------------------------------------------"
echo "  Path:     /workspace"
if [ -d /workspace ]; then
    echo "  Items:    $(ls -1 /workspace 2>/dev/null | tr '\n' ' ')"
fi
echo "================================================================="
