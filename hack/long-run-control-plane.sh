#!/usr/bin/env bash
set -euo pipefail

# Unattended paired control-plane benchmark. Every backend gets a fresh kind
# cluster. Existing successful summaries are skipped, so the run is resumable.
root=${MIGBENCH_ROOT:-$PWD}
bin=${MIGBENCH_BIN:-$root/migbench}
kind_config=${KIND_CONFIG:-$root/kind.yaml}
release_config=${RELEASE_CONFIG:-$root/release.yaml}
master_config=${MASTER_CONFIG:-$root/master.yaml}
dra_config=${DRA_CONFIG:-$root/dra.yaml}
release_manifest=${RELEASE_MANIFEST:-$root/manifest.yaml}
master_manifest=${MASTER_MANIFEST:-$root/master-manifest.yaml}
master_image=${MASTER_IMAGE:-hami-master:684c5f74}
out=${LONG_RUN_OUT:-$root/long-run-results}
kind_image=${KIND_IMAGE:-kindest/node:v1.36.4}
seeds=${SEEDS:-"20260914 20260915 20260916 20260917 20260918 20260919 20260920 20260921"}
distributions=${DISTRIBUTIONS:-"balanced small-heavy large-heavy"}

mkdir -p "$out"

weights() {
  case "$1" in
    balanced)    echo "35 25 25 5" ;;
    small-heavy) echo "65 22 10 3" ;;
    large-heavy) echo "15 20 45 20" ;;
    *) echo "unknown distribution $1" >&2; return 1 ;;
  esac
}

render_config() {
  local source=$1 target=$2 seed=$3 distribution=$4
  read -r w1 w2 w3 w7 <<<"$(weights "$distribution")"
  sed -E \
    -e "s/^name:.*/name: h100-long-${distribution}/" \
    -e "s/^  seed:.*/  seed: ${seed}/" \
    -e "s/^(    1g\.10gb:).*/\1 ${w1}/" \
    -e "s/^(    2g\.20gb:).*/\1 ${w2}/" \
    -e "s/^(    3g\.40gb:).*/\1 ${w3}/" \
    -e "s/^(    7g\.80gb:).*/\1 ${w7}/" \
    "$source" >"$target"
}

fresh_cluster() {
  kind delete cluster --name migbench >/dev/null 2>&1 || true
  kind create cluster --name migbench --image "$kind_image" --config "$kind_config" --wait 120s
  kubectl wait --for=condition=Ready nodes --all --timeout=120s
}

load_master_image() {
  local node
  for node in migbench-control-plane migbench-worker migbench-worker2; do
    docker save "$master_image" | docker exec -i "$node" ctr -n k8s.io images import - >/dev/null
  done
}

run_backend() {
  local backend=$1 config=$2 manifest=$3 trace=$4 run_dir=$5
  if find "$run_dir" -name summary.json -type f -print -quit 2>/dev/null | grep -q .; then
    echo "skip completed $run_dir"
    return
  fi
  fresh_cluster
  if [[ $backend == master ]]; then
    load_master_image
  fi
  if [[ -n $manifest ]]; then
    kubectl apply -f "$manifest"
    kubectl -n kube-system rollout status deploy/hami-scheduler --timeout=180s
  fi
  mkdir -p "$run_dir/cluster"
  "$bin" run -config "$config" -trace "$trace" -out "$run_dir"
  kubectl version -o yaml >"$run_dir/cluster/kubernetes-version.yaml"
  kubectl get nodes,pods,resourceclaims,resourceslices,deviceclasses -A -o yaml >"$run_dir/cluster/objects.yaml" 2>&1 || true
  kubectl get events -A -o yaml >"$run_dir/cluster/events.yaml"
  if [[ $backend != dra ]]; then
    kubectl -n kube-system logs deploy/hami-scheduler -c vgpu-scheduler-extender >"$run_dir/cluster/extender.log"
  fi
}

for distribution in $distributions; do
  for seed in $seeds; do
    pair="$out/$distribution/$seed"
    mkdir -p "$pair"
    render_config "$release_config" "$pair/release.yaml" "$seed" "$distribution"
    render_config "$master_config" "$pair/master.yaml" "$seed" "$distribution"
    render_config "$dra_config" "$pair/dra.yaml" "$seed" "$distribution"
    trace="$pair/trace.jsonl"
    if [[ ! -s $trace ]]; then
      "$bin" generate -config "$pair/release.yaml" -out "$trace"
    fi
    run_backend release "$pair/release.yaml" "$release_manifest" "$trace" "$pair/release"
    run_backend master "$pair/master.yaml" "$master_manifest" "$trace" "$pair/master"
    run_backend dra "$pair/dra.yaml" "" "$trace" "$pair/dra"
    "$bin" compare -in "$pair" -out "$pair/report.html"
  done
done

kind delete cluster --name migbench >/dev/null 2>&1 || true
"$bin" compare -in "$out" -out "$out/report.html"
tar -czf "$out.tar.gz" -C "$(dirname "$out")" "$(basename "$out")"
sha256sum "$out.tar.gz" >"$out.tar.gz.sha256"
