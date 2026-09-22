# =============================================================================
# DevBox Multi-Runtime Development Container
# Base: Ubuntu 24.04 LTS (Optimized for Apple Silicon / ARM64 & x86_64)
# Features: DevBox Plugin System (Runtimes & CLIs), OpenVPN, OpenSSH Server
# =============================================================================

FROM golang:1.24 AS devbox-builder
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/devbox ./cmd/devbox

FROM ubuntu:24.04

# Prevent interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC

# --- User & Permissions Build Arguments ---
ARG USERNAME=devbox
ARG USER_UID=501
ARG USER_GID=501

# --- Runtime Configuration (passed through to plugin build hooks) ---
ARG GOPRIVATE=""

# 1. Plugin System: Install CLI and execute pre-resolved build hooks
# Runtimes and CLI tools declared in devbox.plugins.yml are installed by
# their respective plugin build.sh hooks per .generated/plugins/plan.json.
COPY --from=devbox-builder /bin/devbox /usr/local/bin/devbox
COPY .generated/plugins/plan.json /opt/devbox/plugins/plan.json
COPY .generated/plugins /opt/devbox/plugins
COPY scripts/run-plugin-builds /usr/local/bin/run-plugin-builds
RUN chmod +x /usr/local/bin/run-plugin-builds && /usr/local/bin/run-plugin-builds

# 2. Configure OpenSSH Server
RUN mkdir -p /var/run/sshd && \
    sed -i 's/#PermitRootLogin prohibit-password/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PasswordAuthentication yes/PasswordAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PubkeyAuthentication yes/PubkeyAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's@session\s*required\s*pam_loginuid.so@session optional pam_loginuid.so@g' /etc/pam.d/sshd && \
    echo "AcceptEnv GITHUB_TOKEN GH_TOKEN GITLAB_TOKEN GLAB_TOKEN GITLAB_HOST GIT_USER_NAME GIT_USER_EMAIL GOPRIVATE GOPROXY" >> /etc/ssh/sshd_config

# 3. Create development user and set up passwordless sudo
RUN if getent group ${USER_GID} >/dev/null; then \
        EXISTING_GROUP=$(getent group ${USER_GID} | cut -d: -f1); \
        useradd -m -s /bin/bash -u ${USER_UID} -g ${EXISTING_GROUP} ${USERNAME}; \
    else \
        groupadd -g ${USER_GID} ${USERNAME} && \
        useradd -m -s /bin/bash -u ${USER_UID} -g ${USERNAME} ${USERNAME}; \
    fi && \
    echo "${USERNAME}:${USERNAME}" | chpasswd && \
    echo "${USERNAME} ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/${USERNAME} && \
    chmod 0440 /etc/sudoers.d/${USERNAME}

# 4. Create SSH and workspace directories
RUN mkdir -p /home/${USERNAME}/.ssh /workspace && \
    chown -R ${USERNAME}: /home/${USERNAME}/.ssh /workspace && \
    chmod 700 /home/${USERNAME}/.ssh

# 5. Helper Scripts, Environment Setup & Shell Profile
COPY scripts/devbox-env.sh /etc/profile.d/devbox.sh
COPY scripts/devbox-info.sh /usr/local/bin/devbox-info
COPY scripts/connect-vpn.sh /usr/local/bin/connect-vpn
COPY scripts/setup-git-auth.sh /usr/local/bin/setup-git-auth
COPY scripts/run-plugin-starts /usr/local/bin/run-plugin-starts
COPY scripts/devbox-plugin /usr/local/bin/devbox-plugin
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

RUN chmod 644 /etc/profile.d/devbox.sh && \
    chmod +x /usr/local/bin/devbox-info \
             /usr/local/bin/connect-vpn \
             /usr/local/bin/setup-git-auth \
             /usr/local/bin/run-plugin-starts \
             /usr/local/bin/devbox-plugin \
             /usr/local/bin/entrypoint.sh && \
    echo '[ -f /etc/profile.d/devbox.sh ] && . /etc/profile.d/devbox.sh' >> /home/${USERNAME}/.bashrc && \
    echo 'cd /workspace 2>/dev/null || true' >> /home/${USERNAME}/.bashrc && \
    echo '[ -f /etc/profile.d/devbox.sh ] && . /etc/profile.d/devbox.sh' >> /home/${USERNAME}/.zshrc && \
    echo 'cd /workspace 2>/dev/null || true' >> /home/${USERNAME}/.zshrc && \
    chown ${USERNAME}: /home/${USERNAME}/.bashrc /home/${USERNAME}/.zshrc

# Global environment defaults
ENV GOROOT=/usr/local/go
ENV GOPATH=/home/${USERNAME}/go
ENV RUSTUP_HOME=/usr/local/rustup
ENV CARGO_HOME=/usr/local/cargo
ENV PATH=/usr/local/cargo/bin:/usr/local/go/bin:/home/${USERNAME}/go/bin:/home/${USERNAME}/.local/bin:/usr/local/bin:${PATH}
ENV GOPRIVATE=${GOPRIVATE}
ENV GOPROXY=https://proxy.golang.org,direct
ENV GOTOOLCHAIN=auto

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["/usr/sbin/sshd", "-D", "-e"]
