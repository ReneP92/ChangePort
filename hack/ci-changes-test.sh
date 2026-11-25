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
expect "site/content/docs/alerts.md" site go web e2e_core
expect "web/src/App.tsx" web go console e2e_core
expect "hack/kafka-with.sh" e2e_core console
expect "hack/Transaction.java" e2e_core console
expect "terraform/modules/static-site/main.tf" terraform
expect "hack/terraform.sh" terraform
expect "README.md" ""
expect "docs/PROJECT_PLAN.md" ""
expect "docs/runbook.md" go
expect "images/kafka/Dockerfile" kafka_image e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console
expect "images/connect/Dockerfile" connect_image e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console
expect "internal/console/messages.go" go e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console images
expect "deploy/base/alerting/rules/postgres-sources.yaml" go e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console images
expect "hack/cdc-test.sh" e2e_cdc
expect "api/openapi.yaml" go web e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console images
expect "hack/bun.sh" site web go console e2e_core
expect "site/index.html
internal/agent/markers.go" site go e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console images
expect "hack/chaos-test.sh" e2e_chaos
expect "hack/load.sh" e2e_failover e2e_chaos
expect "hack/alerting-test.sh" e2e_failover
expect "hack/usage-test.sh" e2e_core e2e_failover
expect "hack/cdc-recovery-test.sh" e2e_cdc
expect "hack/cdc-alerts-test.sh" e2e_alerts
expect "hack/lag-test.sh" e2e_core
expect "hack/smoke-test.sh" go e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console images
expect "hack/ui-e2e.sh" go e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console images
expect "hack/ci-changes-test.sh" ""
expect "hack/ci-changes.sh" go terraform web site e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console kafka_image connect_image images
expect ".github/workflows/ci.yml" go terraform web site e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console kafka_image connect_image images
expect "justfile" go terraform web site e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console kafka_image connect_image images
expect "hack/lib.sh" go terraform web site e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console kafka_image connect_image images

# Every script a suite of hack/e2e.sh runs marks that suite, so that a
# script one more suite comes to run is not left to the suites it ran in.
e2e="$(dirname "${BASH_SOURCE[0]}")/e2e.sh"
# scripts START END: the scripts hack/e2e.sh runs from the line matching
# START to the next matching END.
scripts() {
  awk -v start="$1" -v end="$2" '$0 ~ start { on = 1 } on { print } on && $0 ~ end { exit }' "$e2e" |
    { grep -o 'hack/[a-z-]*\.sh' || true; } | sort -u
}
for suite in core cdc failover chaos alerts; do
  if [[ $suite == core ]]; then
    ran=$(scripts '^core_suite' '^}')
  else
    ran=$(scripts "SUITE == $suite " '^fi')
  fi
  [[ -n $ran ]] || {
    echo "found no scripts of the $suite suite in hack/e2e.sh" >&2
    failed=1
  }
  for script in $ran; do
    marked=$(printf '%s\n' "$script" | "$changes" - 2>/dev/null)
    [[ $marked == *$'\n'"e2e_$suite=true"$'\n'* ]] || {
      printf 'a change of %s does not run the %s suite, which runs it (hack/e2e.sh)\n' "$script" "$suite" >&2
      failed=1
    }
  done
done

# The suites the end-to-end job's matrix runs.
suites() {
  local got
  got=$(printf '%s\n' "$1" | "$changes" - 2>/dev/null | sed -n 's/^e2e_suites=//p')
  [[ $got == "$2" ]] || {
    printf 'a change of %s runs the suites %s, not %s\n' "$1" "$2" "$got" >&2
    failed=1
  }
}
suites "hack/load.sh" '["failover","chaos"]'
suites "internal/agent/markers.go" '["core","cdc","failover","chaos","alerts"]'
suites "site/index.html" '[]'

all=$("$changes" --all 2>/dev/null | awk -F= '$2 == "true" { print $1 }' | sort | xargs)
want=$(printf '%s\n' go terraform web site e2e_core e2e_cdc e2e_failover e2e_chaos e2e_alerts console kafka_image connect_image images | sort | xargs)
[[ $all == "$want" ]] || {
  printf -- '--all runs [%s], not every job [%s]\n' "$all" "$want" >&2
  failed=1
}

[[ $failed == 0 ]] && echo "hack/ci-changes.sh marks the jobs each kind of change needs"
exit "$failed"
