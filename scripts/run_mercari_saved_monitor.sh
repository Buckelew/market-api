#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

export ENV_FILE="${ENV_FILE:-${SCRIPT_DIR}/mercari_saved_monitor.env}"

cd "${REPO_DIR}"
exec go run ./cmd/mercari-saved-monitor
