#!/usr/bin/env bash
set -euo pipefail
echo "docker:build:compose=${DEVBOX_PLUGIN_DOCKER_COMPOSE:-}" >> "${DEVBOX_TEST_LOG:?}"
