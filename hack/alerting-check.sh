#!/usr/bin/env bash
# Validates the alerting configuration with the tools of the pinned images:
# vmalert parses every rule of deploy/base/alerting/rules (expressions and
# annotation templates), and amtool checks Alertmanager's configuration, of
# the EKS environments and of kind.
# Usage: hack/alerting-check.sh
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

image() { sed -n "s|^ *image: \(.*$1.*\)|\1|p" "$ROOT/deploy/base/alerting/alerting.yaml" | head -1; }
vmalert=$(image vmalert)
alertmanager=$(image alertmanager)
[[ -n $vmalert && -n $alertmanager ]] || die "cannot find the images in deploy/base/alerting/alerting.yaml"

output=$(mktemp)
trap 'rm -f "$output"' EXIT
docker run --rm -v "$ROOT/deploy/base/alerting/rules:/rules:ro" "$vmalert" -dryRun -rule='/rules/*.yaml' >"$output" 2>&1 ||
  die "vmalert refuses the rules:"$'\n'"$(grep -E 'error|fatal' "$output")"
log "vmalert parses every rule of deploy/base/alerting/rules"
for config in deploy/base/alerting/alertmanager.yaml deploy/overlays/kind/alertmanager.yaml; do
  docker run --rm -v "$ROOT/$(dirname "$config"):/config:ro" --entrypoint amtool "$alertmanager" \
    check-config "/config/$(basename "$config")" >/dev/null || die "amtool refuses $config"
done
log "amtool accepts Alertmanager's configuration of the EKS environments and of kind"
