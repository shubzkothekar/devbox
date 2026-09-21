#!/usr/bin/env bash
set -e

# =============================================================================
# Container Entrypoint
# =============================================================================

# 1. Resolve target non-root development user
TARGET_USER="${DEV_USERNAME:-devbox}"
if ! id "$TARGET_USER" >/dev/null 2>&1; then
    TARGET_USER=$(id -un 501 2>/dev/null || id -un 1000 2>/dev/null || echo "devbox")
fi
USER_HOME=$(eval echo "~$TARGET_USER")
USER_SSH_DIR="$USER_HOME/.ssh"

# 2. Ensure /dev/net/tun exists for OpenVPN
if [ ! -c /dev/net/tun ]; then
    mkdir -p /dev/net
    mknod /dev/net/tun c 10 200
    chmod 600 /dev/net/tun
fi

# 3. Ensure SSH host keys are generated
ssh-keygen -A > /dev/null 2>&1 || true

# 4. Setup SSH directory and permissions for development user
mkdir -p "$USER_SSH_DIR"

# If an authorized_keys file was mounted directly or a public key was mounted to /tmp/authorized_keys
if [ -f "/tmp/authorized_keys" ]; then
    cat /tmp/authorized_keys >> "$USER_SSH_DIR/authorized_keys"
    # Deduplicate entries in authorized_keys
    sort -u "$USER_SSH_DIR/authorized_keys" -o "$USER_SSH_DIR/authorized_keys" 2>/dev/null || true
fi

if [ -f "$USER_SSH_DIR/authorized_keys" ]; then
    chmod 700 "$USER_SSH_DIR"
    chmod 600 "$USER_SSH_DIR/authorized_keys"
    chown -R "${TARGET_USER}:" "$USER_SSH_DIR"
fi

# 5. Populate /etc/environment so SSH sessions inherit container environment
cat <<EOF > /etc/environment
PATH="${PATH}"
DEV_USERNAME="${TARGET_USER}"
GIT_USER_NAME="${GIT_USER_NAME:-${GIT_NAME:-}}"
GIT_USER_EMAIL="${GIT_USER_EMAIL:-${GIT_EMAIL:-}}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
GH_TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
GITLAB_TOKEN="${GITLAB_TOKEN:-}"
GLAB_TOKEN="${GLAB_TOKEN:-${GITLAB_TOKEN:-}}"
GITLAB_HOST="${GITLAB_HOST:-gitlab.com}"
GOPRIVATE="${GOPRIVATE:-}"
GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"
EOF
chmod 644 /etc/environment

# 6. Configure Git identity and authentication for development user
su - "$TARGET_USER" -c "
    export GIT_USER_NAME=\"${GIT_USER_NAME:-${GIT_NAME:-}}\"
    export GIT_USER_EMAIL=\"${GIT_USER_EMAIL:-${GIT_EMAIL:-}}\"
    export GITHUB_TOKEN=\"${GITHUB_TOKEN:-}\"
    export GH_TOKEN=\"${GH_TOKEN:-${GITHUB_TOKEN:-}}\"
    export GITLAB_TOKEN=\"${GITLAB_TOKEN:-}\"
    export GLAB_TOKEN=\"${GLAB_TOKEN:-${GITLAB_TOKEN:-}}\"
    export GITLAB_HOST=\"${GITLAB_HOST:-gitlab.com}\"
    export GOPRIVATE=\"${GOPRIVATE:-}\"
    export GOPROXY=\"${GOPROXY:-https://proxy.golang.org,direct}\"
    /usr/local/bin/setup-git-auth
" || echo "Notice: setup-git-auth completed with non-fatal status."

# 7. Auto-connect VPN if requested
if [ "$AUTO_CONNECT_VPN" = "true" ]; then
    echo "AUTO_CONNECT_VPN=true: attempting to establish VPN connection..."
    /usr/local/bin/connect-vpn || echo "VPN auto-connect failed. You can connect manually with 'connect-vpn'."
fi

# 8. Execute requested command
# If running default sshd command, run directly. Otherwise start ssh as a daemon first.
if [[ "$*" == *"/usr/sbin/sshd"* ]]; then
    exec "$@"
else
    service ssh start
    exec "$@"
fi
