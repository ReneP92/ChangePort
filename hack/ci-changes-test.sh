#!/usr/bin/env bash
# Checks which of CI's jobs hack/ci-changes.sh says a change needs, for a
# change of each kind.
# Usage: hack/ci-changes-test.sh
set -euo pipefail

changes="$(dirname "${BASH_SOURCE[0]}")/ci-changes.sh"
failed=0

# expect "FILES" JOB...: a change of FILES (one per line) needs exactly JOBs.
expect() {
  local files=$1 got want
  shift
  got=$(printf '%s\n' "$files" | "$changes" - 2>/dev/null | awk -F= '$2 == "true" { print $1 }' | sort | xargs)
  want=$(printf '%s\n' "$@" | sort | xargs)
  if [[ $got != "$want" ]]; then
    printf 'a change of %s needs [%s], not [%s]\n' "${files//$'\n'/, }" "$want" "$got" >&2
    failed=1
  fi
}

expect "site/index.html" site
expect "site/src/main.ts
site/tests/site.spec.ts" site
expect "site/content/docs/alerts.md" site go web e2e
expect "web/src/App.tsx" web go console
expect "terraform/modules/static-site/main.tf" terraform
expect "README.md" ""
expect "docs/PROJECT_PLAN.md" ""
expect "docs/runbook.md" go
expect "images/kafka/Dockerfile" kafka_image e2e console
expect "images/connect/Dockerfile" connect_image e2e console
expect "internal/console/messages.go" go e2e console images
expect "deploy/base/alerting/rules/postgres-sources.yaml" go e2e console images
expect "hack/cdc-test.sh" go e2e console images
expect "api/openapi.yaml" go web e2e console images
expect "hack/bun.sh" site web go console
expect "site/index.html
internal/agent/markers.go" site go e2e console images
expect ".github/workflows/ci.yml" go terraform web site e2e console kafka_image connect_image images
expect "justfile" go terraform web site e2e console kafka_image connect_image images
expect "hack/lib.sh" go terraform web site e2e console kafka_image connect_image images

all=$("$changes" --all 2>/dev/null | awk -F= '$2 == "true" { print $1 }' | sort | xargs)
want=$(printf '%s\n' go terraform web site e2e console kafka_image connect_image images | sort | xargs)
[[ $all == "$want" ]] || {
  printf -- '--all runs [%s], not every job [%s]\n' "$all" "$want" >&2
  failed=1
}

[[ $failed == 0 ]] && echo "hack/ci-changes.sh marks the jobs each kind of change needs"
exit "$failed"
