#!/usr/bin/env bash
# Runs the pinned Bun in web/, or in BUN_DIR such as site/, downloading it
# into bin/ first if needed, so no Bun, Node or other package manager has to
# be installed.
# Usage: [BUN_DIR=site] hack/bun.sh <bun arguments>, such as hack/bun.sh run lint
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ensure_bun
cd "${BUN_DIR:-web}"
exec "$BUN_BIN" "$@"
