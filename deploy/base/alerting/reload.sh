# shellcheck shell=sh
# Keeps Alertmanager's running configuration current: every 10 seconds it
# copies the configuration pick.sh picks, if it changed, and has Alertmanager
# reload it. Kubernetes updates the mounted ConfigMap and Secret within a
# minute of their change. A configuration Alertmanager refuses leaves the
# last one running, and says so here.
# shellcheck source=pick.sh
. /scripts/pick.sh
while true; do
  source=$(pick)
  if ! cmp -s "$source" /active/alertmanager.yaml; then
    cp "$source" /active/alertmanager.yaml
    if wget -q -O - --post-data= http://127.0.0.1:9093/-/reload >/dev/null; then
      echo "reloaded $source"
    else
      echo "Alertmanager refused $source"
    fi
  fi
  sleep 10
done
