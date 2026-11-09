#!/usr/bin/env bash
# Runs the console's Playwright tests against the console on kind
# (http://localhost:8088), signed in as Clerk test users: the setup creates
# this run's users, organizations and customers in the Clerk development
# instance of .env, and the teardown removes them. Run just dev-up first, with
# .env filled in (see .env.sample).
# Usage: hack/ui-e2e.sh [playwright arguments]
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ensure_kind
ensure_kubectl
cluster_exists || die "kind cluster $CLUSTER_NAME does not exist; run just dev-up first"
# The tests provision customers of their own, four Kafka clusters and a
# Connect worker at once. Next to the demo customer they outgrow a 12 GB
# Docker: the cluster ran short of memory until its API answered slowly
# enough to fail steps at random, or the kernel killed pods.
docker_memory_gib=$(($(docker info --format '{{.MemTotal}}') / 1024 / 1024 / 1024))
if ((docker_memory_gib < 15)) && kubectl get customer demo >/dev/null 2>&1; then
  die "Docker has ${docker_memory_gib} GiB of memory, too little for these tests next to the demo customer: remove it with kubectl --context $KUBE_CONTEXT delete customer demo (just dev-up brings it back), or give Docker 16 GB"
fi
config=$(console_config_status)
[[ $config == 200 ]] ||
  die "the console at http://localhost:$CONSOLE_PORT is not configured (GET /api/config answered $config); fill in .env and run just console-config"

publishable_key=$(dotenv_value CLERK_PUBLISHABLE_KEY)
[[ -n $publishable_key ]] || publishable_key=$(dotenv_value VITE_CLERK_PUBLISHABLE_KEY)
CLERK_PUBLISHABLE_KEY=$publishable_key
CLERK_SECRET_KEY=$(dotenv_value CLERK_SECRET_KEY)
CHANGEPORT_STAFF_ORG_ID=$(dotenv_value CHANGEPORT_STAFF_ORG_ID)
[[ -n $CLERK_SECRET_KEY && -n $CHANGEPORT_STAFF_ORG_ID ]] || die ".env lacks the Clerk secret key or the staff organization ID"
export CLERK_PUBLISHABLE_KEY CLERK_SECRET_KEY CHANGEPORT_STAFF_ORG_ID
# The tests create and delete customers with the pinned kubectl.
export KUBECTL=$KUBECTL_BIN KUBE_CONTEXT
export PLAYWRIGHT_BASE_URL="http://localhost:$CONSOLE_PORT"

stay_awake
hack/bun.sh install --frozen-lockfile >/dev/null
hack/bun.sh x playwright install ${CI:+--with-deps} chromium >/dev/null
hack/bun.sh run e2e "$@"
