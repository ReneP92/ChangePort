#!/usr/bin/env bash
# Installs Playwright's Chromium for the web app or, with BUN_DIR=site, the
# landing site, and on CI the system packages it needs, trying again when a
# download or a package mirror fails, as Ubuntu's did for a whole CI run.
# Usage: [BUN_DIR=site] hack/playwright-install.sh
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

retry_network hack/bun.sh x playwright install ${CI:+--with-deps} chromium >/dev/null
