# Real control-plane trace runner

Date: 2026-09-15

## Implemented

The normal `migbench run` command now supports `mode: control-plane` for HAMi
and NVIDIA DRA. Each run:

1. requires an explicit, ordered node list;
2. starts one in-process fake H100 agent for each node;
3. converts each resource-bound trace entry into native backend objects;
4. submits arrivals according to virtual time;
5. reads placements selected by the real scheduler;
6. validates the complete MIG geometry snapshot;
7. derives runtime as `computeCoreMS / allocatedComputePercent`;
8. deletes the Pod and ResourceClaim at virtual completion; and
9. writes the existing summary, jobs, events, native objects, and backend lock
   formats used by the report pipeline.

Wall-clock scheduler latency is recorded separately. Simulated and
control-plane results cannot be mixed in one invocation, and multiple real
backends must be run separately against fresh clusters.

## Azure CPU-only verification

- kind `v0.30.0`
- Kubernetes `v1.36.4`
- one worker and one simulated H100
- native `resource.k8s.io/v1` allocation path
- eight resource-bound jobs

Seven prefill jobs were allocated to all seven `1g.10gb` placements at virtual
time zero. Peak GPC utilization reached 100%. A later `7g.80gb` request stayed
queued until all seven small allocations had been released, then occupied the
whole virtual GPU. All eight jobs completed, all Pods and ResourceClaims were
deleted, and no GPU was present on the Azure VM.

This short smoke trace intentionally had no backlog before its last arrival, so
`highLoadValid=false` because backlog fraction was 0%. It verifies allocation,
full occupancy, queueing, virtual completion, and cleanup; it is not presented
as a comparative scheduling result.

During verification, an adversarial workload-generation bug was found: the
adversarial phase override ran after prefill selection, turning the last three
of seven prefill jobs into whole-GPU requests. Prefill now has strict precedence
and a regression test verifies all seven H100 GPC slots are requested.

Evidence bundle: `control-runner-dra-azure.tar.gz`, SHA-256
`21b8f76b43e83960ec5bd39f9c3052017a1ac3821b5e6a6b51edf1bda1271420`.
