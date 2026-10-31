#!/usr/bin/env bash
# Gives an EKS environment's Alertmanager its receivers' secrets: writes the
# Secret metrics/alertmanager-receivers from the git-ignored .env at the
# repository root (variables in .env.sample, named after the environment, so
# that dev never pages through prod's receivers): the Slack webhook URL, the
# PagerDuty routing key and the dead man's switch URL of the Watchdog.
# Alertmanager reads them each time it sends, so nothing restarts. kind needs
# none: its alerts go to its own alert receiver.
# Usage: hack/alerting-config.sh dev|prod (prod also needs CONFIRM=prod)
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

env=${1:?usage: hack/alerting-config.sh dev|prod}
case "$env" in
  dev | prod) KUBE_CONTEXT=changeport-$env ;;
  *) die "unknown environment $env; use dev or prod" ;;
esac
[[ $env != prod || ${CONFIRM:-} == prod ]] || die "configuring prod's alerting needs CONFIRM=prod"
[[ -f "$ROOT/.env" ]] || die "no .env; see .env.sample"
ensure_kubectl

prefix="ALERTMANAGER_$(tr "[:lower:]" "[:upper:]" <<<"$env")"
slack=$(dotenv_value "${prefix}_SLACK_API_URL")
pagerduty=$(dotenv_value "${prefix}_PAGERDUTY_ROUTING_KEY")
watchdog=$(dotenv_value "${prefix}_WATCHDOG_URL")
[[ $slack == https://hooks.slack.com/* ]] || die ".env has no Slack webhook URL for $env (${prefix}_SLACK_API_URL=https://hooks.slack.com/...)"
[[ -n $pagerduty ]] || die ".env has no PagerDuty routing key for $env (${prefix}_PAGERDUTY_ROUTING_KEY)"
[[ $watchdog == https://* ]] || die ".env has no dead man's switch URL for $env (${prefix}_WATCHDOG_URL=https://...)"

# The secrets go through files, not command lines, which other users can see.
work=$(umask 077 && mktemp -d)
trap 'rm -rf "$work"' EXIT
printf '%s' "$slack" >"$work/slack-api-url"
printf '%s' "$pagerduty" >"$work/pagerduty-routing-key"
printf '%s' "$watchdog" >"$work/watchdog-url"
# Server-side apply keeps no copy of the data in an annotation.
kubectl -n metrics create secret generic alertmanager-receivers --dry-run=client -o yaml \
  --from-file="$work/slack-api-url" --from-file="$work/pagerduty-routing-key" --from-file="$work/watchdog-url" |
  kubectl apply --server-side --field-manager=changeport-alerting-config -f - >/dev/null
log "Alertmanager of $env sends to its receivers; the Watchdog reaches the dead man's switch within a minute or two"
