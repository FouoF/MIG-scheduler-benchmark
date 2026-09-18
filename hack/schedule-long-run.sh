#!/usr/bin/env bash
set -euo pipefail

root=${MIGBENCH_ROOT:-$PWD}
start_at=${START_AT:-"today 22:00"}
unit=${SYSTEMD_UNIT:-migbench-long-run}
kubeconfig=${KUBECONFIG:-$HOME/.kube/config}

sudo systemd-run \
  --unit="$unit" \
  --on-calendar="$start_at" \
  --working-directory="$root" \
  --property=StandardOutput=append:"$root/long-run.log" \
  --property=StandardError=append:"$root/long-run.log" \
  /usr/bin/env MIGBENCH_ROOT="$root" KUBECONFIG="$kubeconfig" bash "$root/hack/long-run-control-plane.sh"

echo "scheduled $unit for $start_at"
echo "status: systemctl status $unit"
echo "logs:   journalctl -u $unit -f"
