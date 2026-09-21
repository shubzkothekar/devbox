#!/usr/bin/env bash
# =============================================================================
# DevBox Environment Configuration
# Sourced by /etc/profile, ~/.bashrc, and ~/.zshrc
# =============================================================================

# System-wide and user PATH additions
export PATH="/usr/local/cargo/bin:/usr/local/go/bin:$HOME/go/bin:$HOME/.local/bin:$HOME/.cargo/bin:$PATH"

# Go configuration
if [ -d "/usr/local/go" ]; then
    export GOROOT=/usr/local/go
    export GOPATH="$HOME/go"
    export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
    export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"
fi

if [ -n "$GOPRIVATE" ]; then
    export GOPRIVATE="$GOPRIVATE"
fi

# Rust configuration
if [ -d "/usr/local/cargo" ]; then
    export RUSTUP_HOME=/usr/local/rustup
    export CARGO_HOME=/usr/local/cargo
fi

# Git committer / author environment variables
if [ -n "$GIT_USER_NAME" ]; then
    export GIT_AUTHOR_NAME="$GIT_USER_NAME"
    export GIT_COMMITTER_NAME="$GIT_USER_NAME"
    if [ "$(git config --global user.name 2>/dev/null)" != "$GIT_USER_NAME" ]; then
        git config --global user.name "$GIT_USER_NAME" 2>/dev/null || true
    fi
fi

if [ -n "$GIT_USER_EMAIL" ]; then
    export GIT_AUTHOR_EMAIL="$GIT_USER_EMAIL"
    export GIT_COMMITTER_EMAIL="$GIT_USER_EMAIL"
    if [ "$(git config --global user.email 2>/dev/null)" != "$GIT_USER_EMAIL" ]; then
        git config --global user.email "$GIT_USER_EMAIL" 2>/dev/null || true
    fi
fi

# GitHub CLI authentication
if [ -n "$GITHUB_TOKEN" ] && [ -z "$GH_TOKEN" ]; then
    export GH_TOKEN="$GITHUB_TOKEN"
elif [ -n "$GH_TOKEN" ] && [ -z "$GITHUB_TOKEN" ]; then
    export GITHUB_TOKEN="$GH_TOKEN"
fi

# GitLab CLI authentication
if [ -n "$GITLAB_TOKEN" ] && [ -z "$GLAB_TOKEN" ]; then
    export GLAB_TOKEN="$GITLAB_TOKEN"
elif [ -n "$GLAB_TOKEN" ] && [ -z "$GITLAB_TOKEN" ]; then
    export GITLAB_TOKEN="$GLAB_TOKEN"
fi
