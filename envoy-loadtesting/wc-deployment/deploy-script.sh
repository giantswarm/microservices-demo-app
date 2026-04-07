#!/usr/bin/env bash
set -euo pipefail

# Prefer the top-level deploy.sh for full orchestration.
# This script is a convenience wrapper for WC-only deployment.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."

exec "${ROOT_DIR}/deploy.sh" wc
