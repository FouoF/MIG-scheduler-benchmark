#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
if [[ ! -x "$root/bin/migbench" ]]; then
  make -C "$root" build
fi

events=${MIGBENCH_EVENTS:-"$root/work/dra-agent-events.jsonl"}
mkdir -p "$(dirname "$events")"
"$root/bin/migbench" agent \
  -backend nvidia-dra \
  -node "${MIGBENCH_NODE:-nvidia-dra-worker}" \
  -gpus "${MIGBENCH_GPUS:-1}" \
  -events "$events" &
agent_pid=$!
cleanup() {
  kill "$agent_pid" 2>/dev/null || true
  wait "$agent_pid" 2>/dev/null || true
}
trap cleanup EXIT

kubectl apply -f "$root/deploy/nvidia-dra/smoke-claims.yaml"

for _ in $(seq 1 60); do
  device=$(kubectl get resourceclaim resource-job-a -o jsonpath='{.status.allocation.devices.results[0].device}' 2>/dev/null || true)
  node=$(kubectl get pod resource-job-a -o jsonpath='{.spec.nodeName}' 2>/dev/null || true)
  if [[ -n "$device" && -n "$node" ]]; then
    printf 'DRA allocation passed: node=%s device=%s\n' "$node" "$device"
    for _ in $(seq 1 20); do
      if grep -q '"type":"allocated"' "$events" 2>/dev/null; then
        printf 'Fake-agent validation passed: events=%s\n' "$events"
        exit 0
      fi
      sleep 0.1
    done
    printf 'allocation was not observed by fake agent\n' >&2
    break
  fi
  sleep 1
done

kubectl describe pod resource-job-a >&2 || true
kubectl get resourceclaim resource-job-a -o yaml >&2 || true
exit 1
