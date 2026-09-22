#!/usr/bin/env bash
set -euo pipefail
echo "docker:status:user=$(id -un 2>/dev/null || whoami)" >> "${DEVBOX_TEST_LOG:?}"
