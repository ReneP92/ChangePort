# shellcheck shell=sh
# The configuration Alertmanager runs: the one the provisioner wrote, with
# the customers' routes, or the base until it has. Sourced by the init
# container and reload.sh.
pick() {
  if [ -s /config/generated/alertmanager.yaml ]; then
    echo /config/generated/alertmanager.yaml
  else
    echo /config/base/alertmanager.yaml
  fi
}
