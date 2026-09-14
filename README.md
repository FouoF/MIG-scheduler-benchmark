# MIGBench

MIGBench is a reproducible, GPU-free benchmark for Dynamic MIG scheduling. It
uses a virtual-time workload engine, an H100 placement model, backend-specific
Kubernetes object adapters, and paired traces. It reports throughput, queueing,
utilization, and multiple fragmentation measures.

## Fidelity boundary

The benchmark executable is an **algorithm simulator** and labels its output as
such. The repository additionally contains GPU-free control-plane fixtures for
real scheduler contract tests. `hack/dra-smoke.sh` drives the native Kubernetes
1.36 ResourceClaim allocator against H100 placement-aware ResourceSlices. HAMi
is tested with its real scheduler/extender and mock device plugin; the mock node
agent supplies inventory and acknowledges allocations. Neither path invokes
NVML, CDI, a container runtime GPU hook, or real computation.

`mode: control-plane` remains guarded in the simulator config: control-plane
results are collected separately and must never be merged with virtual-time
numbers merely because their backend names match.

## Quick start

```sh
make test
make e2e
open work/results/report.html
```

Or run individual commands:

```sh
go run ./cmd/migbench generate -config experiment.example.yaml -out work/trace.jsonl
go run ./cmd/migbench run -config experiment.example.yaml -trace work/trace.jsonl -out work/results
go run ./cmd/migbench compare -in work/results -out work/results/report.html
go run ./cmd/migbench inspect -events work/results/hami-optimized/events.jsonl -job job-000001
```

Every run records the exact trace, config, backend lock metadata, normalized job
results, event log, and generated native Kubernetes objects. Use immutable Git
commits and image digests before publishing benchmark results.

## Control-plane smoke tests

The checked-in comparison is pinned to HAMi `v2.10.0`, HAMi master commit
`88b118e565a9effaa27f81669da291c5508fc5e4`, NVIDIA DRA `v0.5.0`, and Kubernetes
`v1.36.4`. The image digests and manifest checksums are in
`experiment.example.yaml`.

For NVIDIA DRA, create a kind cluster named `nvidia-dra` with worker
`nvidia-dra-worker`, then run:

```sh
hack/dra-smoke.sh
```

The ResourceSlice fixture enumerates all legal 1g/2g/3g/7g placements on one
H100. Shared counters make overlapping placements mutually exclusive. Pods are
expected to stop at device preparation because there is intentionally no
kubelet DRA plugin.

`cloud/azure/provision.sh` creates an isolated 8-vCPU/32-GiB Ubuntu host. Export
`AZURE_RESOURCE_GROUP` and run `cloud/azure/cleanup.sh` after copying artifacts;
the latter deletes the entire dedicated resource group.

The September 2026 Azure smoke evidence is archived as
`outputs/migbench-results.tar.gz` and includes raw objects, scheduler logs,
rendered Helm manifests, Kubernetes versions, and events for all three upstream
targets.

The revised benchmark specification is `outputs/UPDATED-TEST-PLAN.md`. It uses
resource-bound jobs (`computeCoreMS + memoryMB`) and derives duration from the
profile actually allocated. The original fixed-profile trace is retained only
as a placement regression workload.

The first 20-seed resource-bound Azure run is documented in
`outputs/RESOURCE-BOUND-AZURE-REPORT.md`; its complete evidence bundle is
`outputs/migbench-v2-azure-results.tar.gz`.

## Models and policies

Workload models are `poisson`, `burst`, `profile-skew`, and `adversarial`.
Policies are `first-fit`, `binpack`, and `spread`. The default H100-80GB profile
catalog is built in and can be overridden with explicit profiles and legal start
positions in the experiment file.

The geometry model tracks contiguous GPC slots plus framebuffer capacity. It is
appropriate for scheduler experiments, but is not a substitute for NVIDIA's
authoritative profile-placement validation on hardware.
