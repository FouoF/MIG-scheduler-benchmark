#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
kubectl apply --server-side -f "$root/deploy/nvidia-dra/fake-h100-kind.yaml"
kubectl apply -f "$root/deploy/nvidia-dra/smoke-claims.yaml"

for _ in $(seq 1 60); do
  device=$(kubectl get resourceclaim resource-job-a -o jsonpath='{.status.allocation.devices.results[0].device}' 2>/dev/null || true)
  node=$(kubectl get pod resource-job-a -o jsonpath='{.spec.nodeName}' 2>/dev/null || true)
  if [[ -n "$device" && -n "$node" ]]; then
    printf 'DRA allocation passed: node=%s device=%s\n' "$node" "$device"
    exit 0
  fi
  sleep 1
done

kubectl describe pod resource-job-a >&2 || true
kubectl get resourceclaim resource-job-a -o yaml >&2 || true
exit 1
