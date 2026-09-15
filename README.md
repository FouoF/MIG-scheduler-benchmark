# MIGBench

MIGBench is a reproducible, GPU-free benchmark for Dynamic MIG scheduling. It
uses a virtual-time workload engine, an H100 placement model, backend-specific
Kubernetes object adapters, and paired traces. It reports throughput, queueing,
utilization, and multiple fragmentation measures.

## Fidelity boundary

The `run` command is an **algorithm simulator** and labels its output as such.
The `agent` command is a scheduler-facing fake node agent: it publishes H100
inventory to the Kubernetes API, observes decisions made by the real scheduler,
validates their geometry, and acknowledges valid HAMi node locks. It never
chooses a placement. `hack/dra-smoke.sh` drives the native Kubernetes 1.36
ResourceClaim allocator against the agent's placement-aware ResourceSlices.
Neither backend invokes NVML, CDI, a container runtime GPU hook, or real
computation.

`mode: control-plane` uses the real Kubernetes scheduling path described below.
The CLI rejects attempts to merge control-plane and algorithm-simulator output
in one run.

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

# Run one agent per simulated node (four H100s on this node).
go run ./cmd/migbench agent -backend nvidia-dra -node worker-0 -gpus 4 \
  -events work/worker-0-events.jsonl
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

The fake agent enumerates all legal 1g/2g/3g/7g placements for every simulated
H100. Per-GPU shared counters make overlapping placements mutually exclusive
without coupling separate physical GPUs. Pods are expected to stop at device
preparation because there is intentionally no kubelet DRA plugin; allocation in
the ResourceClaim is the scheduler decision being measured.

For HAMi, run the same executable with `-backend hami`. It publishes the
`hami.io/node-nvidia-register` topology understood by Dynamic MIG and patches
the node's aggregate extended-resource capacity. Once the real HAMi scheduler
writes `hami.io/vgpu-mig-allocations`, the agent validates the exact
profile/start/size snapshot. Only then does it remove `hami.io/mutex.lock`.
Malformed, overlapping, or impossible decisions terminate the run as backend
errors instead of being counted as workload rejection.

This agent intentionally implements the scheduler contract, not the kubelet
device-plugin gRPC or DRA NodePrepare contract. That keeps Pod runtime and image
pull latency out of a scheduling-only benchmark.

## Real control-plane trace replay

Set a backend's `mode` to `control-plane`, list the exact worker node names in
`parameters.nodes`, and run the normal command against an already deployed
backend:

```sh
go run ./cmd/migbench generate \
  -config experiment.control-plane.example.yaml -out work/control-trace.jsonl
go run ./cmd/migbench run \
  -config experiment.control-plane.example.yaml \
  -trace work/control-trace.jsonl -out work/control-results
```

The runner starts one in-process fake agent per listed node, submits the native
Pod or ResourceClaim+Pod objects, and records placements chosen by the real
scheduler. Workload arrival, `computeCoreMS / allocatedCompute`, and completion
use virtual time. API scheduling latency remains wall-clock data. At virtual
completion the runner deletes the Pod and, for DRA, its ResourceClaim, which
unblocks queued work without waiting for the simulated compute duration in real
time.

Run each control-plane backend in a fresh cluster and use the same trace file.
Do not put release and master into the same control-plane invocation: backend
deployment and Kubernetes state are deliberately outside the runner's mutation
scope. `settleMS` is the post-change quiet window used to distinguish a stable
pending queue from scheduler processing; keep it identical in paired runs.
On infrastructure failure, partial events, job results, native objects, backend
lock metadata, and `infrastructure-error.txt` remain in the result directory.

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

The default experiment is now a saturated fragmentation run: 56 prefill jobs
occupy all GPC slots, compute duration has a 600-second median, and results are
accepted only when peak GPC allocation reaches 99% and the pending queue is
non-empty for at least 90% of the steady-state window. Validation results are
in `outputs/SATURATED-FRAGMENTATION-REPORT.md`.

## Models and policies

Workload models are `poisson`, `burst`, `profile-skew`, and `adversarial`.
Policies are `first-fit`, `binpack`, and `spread`. The default H100-80GB profile
catalog is built in and can be overridden with explicit profiles and legal start
positions in the experiment file.

The geometry model tracks contiguous GPC slots plus framebuffer capacity. It is
appropriate for scheduler experiments, but is not a substitute for NVIDIA's
authoritative profile-placement validation on hardware.
