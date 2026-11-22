#!/usr/bin/env bash
# Says which of CI's jobs a pull request needs (.github/workflows/ci.yml),
# from the files it changes, so that a change to the landing site does not
# wait for the end-to-end suites on kind. Each file marks the jobs whose
# results it can change; a file this does not know marks the platform's,
# which run nearly everything. Each end-to-end suite on kind counts as a job
# of its own: a change to one suite's scripts runs that suite alone. Pushes
# to main and runs started by hand run every job (--all), whatever changed.
#
# Usage: hack/ci-changes.sh [base]   the files changed since base (default
#                                    HEAD^1: the merge commit GitHub checks
#                                    out for a pull request has the base
#                                    branch as its first parent)
#        hack/ci-changes.sh - <files the files on stdin, one per line
#        hack/ci-changes.sh --all    every job
#
# Prints job=true or job=false, one per line, then e2e_suites, the suites
# to run as a JSON list for the end-to-end job's matrix, which CI appends to
# $GITHUB_OUTPUT; and which file marked each job, on stderr.
set -euo pipefail

suites=(e2e_core e2e_cdc e2e_failover e2e_chaos)
jobs=(go terraform web site "${suites[@]}" console kafka_image connect_image images)
declare -A need why
for job in "${jobs[@]}"; do need[$job]=false; done

# mark FILE JOB...: FILE needs each JOB.
mark() {
  local file=$1 job
  shift
  for job in "$@"; do
    need[$job]=true
    why[$job]=${why[$job]:-$file}
  done
}

case ${1:-} in
--all) files=--all ;;
-) files=$(cat) ;;
*) files=$(git diff --name-only "${1:-HEAD^1}" HEAD) ;;
esac

while IFS= read -r file; do
  [[ -n $file ]] || continue
  case $file in
  # Every job runs on these: the workflow and its actions, what decides
  # which jobs run, the recipes, and the tools hack/lib.sh pins.
  --all | .github/* | hack/ci-changes.sh | justfile | hack/lib.sh) mark "$file" "${jobs[@]}" ;;
  # The shell job, which runs for every change, runs its test.
  hack/ci-changes-test.sh) ;;
  # The customer docs: Go's tests check the alerts' links into them, the web
  # app's tests and just generate read the examples in them, and the core
  # suite runs the connection guide's.
  site/content/docs/*) mark "$file" site go web e2e_core ;;
  site/*) mark "$file" site ;;
  # The console's web app, built into the console's image; just generate,
  # which the Go job checks, writes its API types and examples.
  web/*) mark "$file" web go console ;;
  # The API the console serves and the web app's types are generated from.
  api/openapi.yaml) mark "$file" web go "${suites[@]}" console images ;;
  # The Bun and the Playwright browser the site, the web app and the core
  # suite's console test run on.
  hack/bun.sh | hack/playwright-install.sh) mark "$file" site web go console e2e_core ;;
  terraform/*) mark "$file" terraform ;;
  # The Kafka and Connect images run in every cluster on kind.
  images/kafka/* | hack/kafka-image-test.sh) mark "$file" kafka_image "${suites[@]}" console ;;
  images/connect/* | hack/connect-image-test.sh) mark "$file" connect_image "${suites[@]}" console ;;
  # The scripts of one suite or two (hack/e2e.sh), and what only they use.
  hack/smoke-test.sh) mark "$file" go "${suites[@]}" console images ;;
  hack/*-test.sh | hack/kafka-with.sh | hack/Transaction.java | hack/host-dns.sh | hack/load.sh)
    case ${file#hack/} in
    chaos-test.sh) mark "$file" e2e_chaos ;;
    failover-test.sh | alerting-test.sh) mark "$file" e2e_failover ;;
    load.sh) mark "$file" e2e_failover e2e_chaos ;;
    usage-test.sh) mark "$file" e2e_core e2e_failover ;;
    cdc-test.sh | cdc-recovery-test.sh) mark "$file" e2e_cdc ;;
    metrics-test.sh | deletion-protection-test.sh | agent-test.sh | lag-test.sh | kafka-with.sh | Transaction.java | \
      client-telemetry-test.sh | console-access-test.sh | console-test.sh | tiered-storage-test.sh | replay-test.sh | \
      host-access-test.sh | host-dns.sh | connecting-guide-test.sh | postgres-test.sh | cdc-runtime-test.sh)
      mark "$file" e2e_core
      ;;
    *) mark "$file" go "${suites[@]}" console images ;;
    esac
    ;;
  # The runbook and the timeline, whose sections the alerts link, which Go's
  # tests check.
  docs/runbook.md | docs/timeline.md) mark "$file" go ;;
  # The other docs and notes, which only the text checks read; they run for
  # every change.
  docs/* | *.md) ;;
  # The platform: Go, the manifests, the scripts, and the images built from
  # Go.
  *) mark "$file" go "${suites[@]}" console images ;;
  esac
done <<<"$files"

run=()
for suite in "${suites[@]}"; do
  [[ ${need[$suite]} == false ]] || run+=("\"${suite#e2e_}\"")
done

for job in "${jobs[@]}"; do
  echo "$job=${need[$job]}"
  if [[ ${need[$job]} == true ]]; then
    echo "$job: needed for ${why[$job]}" >&2
  else
    echo "$job: not needed" >&2
  fi
done
echo "e2e_suites=[$(IFS=,; echo "${run[*]}")]"
