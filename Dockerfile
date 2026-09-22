# =============================================================================
# DevBox Multi-Runtime Development Container
# Base: Ubuntu 24.04 LTS (Optimized for Apple Silicon / ARM64 & x86_64)
# Features: Configurable Runtimes (Go, Node.js, Python, Bun, Rust), OpenVPN, OpenSSH Server, DB CLI Tools
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

# --- Runtime Selection Flags & Versions ---
ARG INSTALL_GO=true
ARG GO_VERSION=1.24.5
ARG GOPRIVATE=""

ARG INSTALL_NODE=true
ARG NODE_VERSION=22

ARG INSTALL_PYTHON=true

ARG INSTALL_BUN=false

ARG INSTALL_RUST=false

ARG INSTALL_DB_CLI=true

ARG INSTALL_NATS=true

ARG INSTALL_GH=true

ARG INSTALL_GLAB=true
ARG GLAB_VERSION=1.118.0

# 1. Install foundational system utilities, networking & build tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    apt-transport-https \
    bash-completion \
    build-essential \
    ca-certificates \
    curl \
    dnsutils \
    git \
    gnupg \
    htop \
    iproute2 \
    iptables \
    iputils-ping \
    jq \
    less \
    lsb-release \
    make \
    nano \
    net-tools \
    openssh-server \
    openvpn \
    pkg-config \
    procps \
    resolvconf \
    sudo \
    tar \
    tcpdump \
    tmux \
    traceroute \
    tree \
    tzdata \
    unzip \
    vim \
    wget \
    zsh \
    && rm -rf /var/lib/apt/lists/*

# 1.1 Plugin System: Install CLI and execute pre-resolved build hooks
COPY --from=devbox-builder /bin/devbox /usr/local/bin/devbox
COPY .generated/plugins/plan.json /opt/devbox/plugins/plan.json
COPY .generated/plugins /opt/devbox/plugins
COPY scripts/run-plugin-builds /usr/local/bin/run-plugin-builds
RUN chmod +x /usr/local/bin/run-plugin-builds && /usr/local/bin/run-plugin-builds

# 2. Golang Toolchain (Optional: controlled by INSTALL_GO)
RUN if [ "$INSTALL_GO" = "true" ]; then \
        ARCH=$(dpkg --print-architecture) && \
        echo "Installing Go ${GO_VERSION} (${ARCH})..." && \
        curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -o /tmp/go.tar.gz && \
        tar -C /usr/local -xzf /tmp/go.tar.gz && \
        rm /tmp/go.tar.gz; \
    else \
        echo "Skipping Go installation (INSTALL_GO=${INSTALL_GO})"; \
    fi

# 3. Node.js LTS & Package Managers (Optional: controlled by INSTALL_NODE)
RUN if [ "$INSTALL_NODE" = "true" ]; then \
        echo "Installing Node.js ${NODE_VERSION}.x LTS, pnpm, and yarn..." && \
        mkdir -p /etc/apt/keyrings && \
        curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg && \
        echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_${NODE_VERSION}.x nodistro main" > /etc/apt/sources.list.d/nodesource.list && \
        apt-get update && \
        apt-get install -y --no-install-recommends nodejs && \
        npm install -g pnpm yarn && \
        rm -rf /var/lib/apt/lists/*; \
    else \
        echo "Skipping Node.js installation (INSTALL_NODE=${INSTALL_NODE})"; \
    fi

# 4. Python 3, pip, venv & pipx (Optional: controlled by INSTALL_PYTHON)
RUN if [ "$INSTALL_PYTHON" = "true" ]; then \
        echo "Installing Python 3, pip, venv, and pipx..." && \
        apt-get update && \
        apt-get install -y --no-install-recommends \
            python3 \
            python3-pip \
            python3-venv \
            python3-dev \
            pipx \
        && rm -rf /var/lib/apt/lists/*; \
    else \
        echo "Skipping Python installation (INSTALL_PYTHON=${INSTALL_PYTHON})"; \
    fi

# 5. Database & Cache CLI Tools: MySQL Client, MongoDB Shell, Redis (Optional: controlled by INSTALL_DB_CLI)
RUN if [ "$INSTALL_DB_CLI" = "true" ]; then \
        echo "Installing Database CLI tools (MySQL, MongoDB mongosh, Redis)..." && \
        curl -fsSL https://www.mongodb.org/static/pgp/server-8.0.asc | gpg --dearmor -o /usr/share/keyrings/mongodb-server-8.0.gpg && \
        echo "deb [ arch=amd64,arm64 signed-by=/usr/share/keyrings/mongodb-server-8.0.gpg ] https://repo.mongodb.org/apt/ubuntu noble/mongodb-org/8.0 multiverse" > /etc/apt/sources.list.d/mongodb-org-8.0.list && \
        apt-get update && \
        apt-get install -y --no-install-recommends \
            default-mysql-client \
            mongodb-mongosh \
            redis-tools \
        && rm -rf /var/lib/apt/lists/*; \
    else \
        echo "Skipping Database CLI tools (INSTALL_DB_CLI=${INSTALL_DB_CLI})"; \
    fi

# 6. NATS CLI (Optional: controlled by INSTALL_NATS)
RUN if [ "$INSTALL_NATS" = "true" ]; then \
        echo "Installing NATS CLI..." && \
        curl -sf https://binaries.nats.dev/nats-io/natscli/nats@latest | PREFIX=/usr/local/bin sh; \
    else \
        echo "Skipping NATS CLI (INSTALL_NATS=${INSTALL_NATS})"; \
    fi

# 7. Bun Runtime (Optional: controlled by INSTALL_BUN)
RUN if [ "$INSTALL_BUN" = "true" ]; then \
        echo "Installing Bun runtime..." && \
        curl -fsSL https://bun.sh/install | BUN_INSTALL=/usr/local bash && \
        chmod 755 /usr/local/bin/bun; \
    else \
        echo "Skipping Bun installation (INSTALL_BUN=${INSTALL_BUN})"; \
    fi

# 8. Rust Toolchain (Optional: controlled by INSTALL_RUST)
RUN if [ "$INSTALL_RUST" = "true" ]; then \
        echo "Installing Rust toolchain (cargo, rustc)..." && \
        export RUSTUP_HOME=/usr/local/rustup && \
        export CARGO_HOME=/usr/local/cargo && \
        curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable --profile minimal --no-modify-path && \
        chmod -R a+rwX /usr/local/rustup /usr/local/cargo; \
    else \
        echo "Skipping Rust installation (INSTALL_RUST=${INSTALL_RUST})"; \
    fi

# 9. GitHub CLI (gh) (Optional: controlled by INSTALL_GH)
RUN if [ "$INSTALL_GH" = "true" ]; then \
        ARCH=$(dpkg --print-architecture) && \
        echo "Installing GitHub CLI (gh) for ${ARCH}..." && \
        mkdir -p -m 755 /etc/apt/keyrings && \
        curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg && \
        chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && \
        echo "deb [arch=${ARCH} signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list && \
        apt-get update && \
        apt-get install -y --no-install-recommends gh && \
        rm -rf /var/lib/apt/lists/*; \
    else \
        echo "Skipping GitHub CLI installation (INSTALL_GH=${INSTALL_GH})"; \
    fi

# 10. GitLab CLI (glab) (Optional: controlled by INSTALL_GLAB)
RUN if [ "$INSTALL_GLAB" = "true" ]; then \
        ARCH=$(dpkg --print-architecture) && \
        echo "Installing GitLab CLI (glab ${GLAB_VERSION}) for ${ARCH}..." && \
        curl -fsSL "https://gitlab.com/api/v4/projects/34675721/packages/generic/glab/${GLAB_VERSION}/glab_${GLAB_VERSION}_linux_${ARCH}.deb" -o /tmp/glab.deb && \
        (dpkg -i /tmp/glab.deb || (apt-get update && apt-get install -y -f && rm -rf /var/lib/apt/lists/*)) && \
        rm -f /tmp/glab.deb; \
    else \
        echo "Skipping GitLab CLI installation (INSTALL_GLAB=${INSTALL_GLAB})"; \
    fi

# 11. Configure OpenSSH Server
RUN mkdir -p /var/run/sshd && \
    sed -i 's/#PermitRootLogin prohibit-password/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PasswordAuthentication yes/PasswordAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PubkeyAuthentication yes/PubkeyAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's@session\s*required\s*pam_loginuid.so@session optional pam_loginuid.so@g' /etc/pam.d/sshd && \
    echo "AcceptEnv GITHUB_TOKEN GH_TOKEN GITLAB_TOKEN GLAB_TOKEN GITLAB_HOST GIT_USER_NAME GIT_USER_EMAIL GOPRIVATE GOPROXY" >> /etc/ssh/sshd_config

# 12. Create development user and set up passwordless sudo
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

# 13. Create SSH and workspace directories
RUN mkdir -p /home/${USERNAME}/.ssh /workspace && \
    chown -R ${USERNAME}: /home/${USERNAME}/.ssh /workspace && \
    chmod 700 /home/${USERNAME}/.ssh

# 14. Helper Scripts, Environment Setup & Shell Profile
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
