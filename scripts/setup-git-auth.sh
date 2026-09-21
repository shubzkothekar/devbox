#!/usr/bin/env bash
set -e

echo "=== Configuring Git Identity & Authentication ==="

# 1. Configure Git Committer / Author Identity
GIT_USER_NAME="${GIT_USER_NAME:-${GIT_NAME:-${GIT_AUTHOR_NAME:-}}}"
GIT_USER_EMAIL="${GIT_USER_EMAIL:-${GIT_EMAIL:-${GIT_AUTHOR_EMAIL:-}}}"

if [ -n "$GIT_USER_NAME" ]; then
    git config --global user.name "$GIT_USER_NAME"
    echo "✅ Git user.name configured as: $GIT_USER_NAME"
else
    EXISTING_NAME=$(git config --global user.name 2>/dev/null || true)
    if [ -n "$EXISTING_NAME" ]; then
        echo "ℹ️  Git user.name already configured as: $EXISTING_NAME"
    else
        echo "Notice: GIT_USER_NAME is not set. Set GIT_USER_NAME in .env to configure your commit author name."
    fi
fi

if [ -n "$GIT_USER_EMAIL" ]; then
    git config --global user.email "$GIT_USER_EMAIL"
    echo "✅ Git user.email configured as: $GIT_USER_EMAIL"
else
    EXISTING_EMAIL=$(git config --global user.email 2>/dev/null || true)
    if [ -n "$EXISTING_EMAIL" ]; then
        echo "ℹ️  Git user.email already configured as: $EXISTING_EMAIL"
    else
        echo "Notice: GIT_USER_EMAIL is not set. Set GIT_USER_EMAIL in .env to configure your commit author email."
    fi
fi

# 2. Configure GitHub Authentication
GH_AUTH_TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
if [ -n "$GH_AUTH_TOKEN" ]; then
    echo "Found GitHub token. Configuring Git credentials and ~/.netrc for github.com..."
    # Universal GitHub authentication: allows cloning any private repo accessible by the token
    git config --global url."https://${GH_AUTH_TOKEN}@github.com/".insteadOf "https://github.com/"
    git config --global url."https://${GH_AUTH_TOKEN}@github.com/".insteadOf "git@github.com:"

    # Maintain ~/.netrc safely without wiping out other machines
    touch "$HOME/.netrc"
    if grep -q "machine github.com" "$HOME/.netrc" 2>/dev/null; then
        sed -i '/machine github.com/,+2d' "$HOME/.netrc"
    fi
    cat <<EOF >> "$HOME/.netrc"
machine github.com
login x-access-token
password ${GH_AUTH_TOKEN}
EOF
    chmod 600 "$HOME/.netrc"

    echo "✅ GitHub authentication successfully configured via Git URL rewrite and ~/.netrc"
    if command -v gh >/dev/null 2>&1; then
        echo "✅ GitHub CLI (gh) detected and token is active (GH_TOKEN/GITHUB_TOKEN)"
    fi
else
    echo "Notice: GITHUB_TOKEN is not set. If accessing private repositories, set GITHUB_TOKEN in your .env."
fi

# 3. Configure GitLab Authentication
GL_AUTH_TOKEN="${GITLAB_TOKEN:-${GLAB_TOKEN:-${GL_TOKEN:-}}}"
if [ -n "$GL_AUTH_TOKEN" ]; then
    GL_HOST="${GITLAB_HOST:-gitlab.com}"
    echo "Found GitLab token. Configuring Git credentials and ~/.netrc for ${GL_HOST}..."
    git config --global url."https://oauth2:${GL_AUTH_TOKEN}@${GL_HOST}/".insteadOf "https://${GL_HOST}/"
    git config --global url."https://oauth2:${GL_AUTH_TOKEN}@${GL_HOST}/".insteadOf "git@${GL_HOST}:"

    touch "$HOME/.netrc"
    if grep -q "machine ${GL_HOST}" "$HOME/.netrc" 2>/dev/null; then
        sed -i "/machine ${GL_HOST}/,+2d" "$HOME/.netrc"
    fi
    cat <<EOF >> "$HOME/.netrc"
machine ${GL_HOST}
login oauth2
password ${GL_AUTH_TOKEN}
EOF
    chmod 600 "$HOME/.netrc"

    echo "✅ GitLab authentication successfully configured for ${GL_HOST}"
    if command -v glab >/dev/null 2>&1; then
        echo "✅ GitLab CLI (glab) detected and token is active (GLAB_TOKEN/GITLAB_TOKEN)"
    fi
fi

# 4. Configure Go private module environment if GOPRIVATE is specified
if [ -n "$GOPRIVATE" ]; then
    export GOPRIVATE="$GOPRIVATE"
    export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
    export GOSUMDB="off"
    echo "GOPRIVATE: $GOPRIVATE"
    echo "GOPROXY: $GOPROXY"
fi
