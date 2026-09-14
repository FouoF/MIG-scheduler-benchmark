# Resource-bound v2 Azure run

Run date: 2026-09-14  
Source commit: `eb8330872ad50a18f0e8862eb7a6299674d73ea7`  
Host: Azure `Standard_D8as_v5` (8 vCPU, 32 GiB, no GPU), East Asia  
Control plane: kind `v0.30.0`, Kubernetes `v1.36.4`

## Workload

- Format: `resource-bound-v2`
- 20 paired seeds: 20260914 through 20260933
- 250 jobs per seed and backend
- Each job contains arrival time, `computeCoreMS`, `memoryMB`, and
  `minComputePercent`; no profile or fixed duration is stored in the trace.
- Runtime is derived from the profile allocated by the simulator.

## Twenty-seed simulator aggregate

| Backend/policy model | Seeds | Mean makespan (ms) | Mean wait (ms) | Mean throughput (/virtual hour) |
|---|---:|---:|---:|---:|
| first-fit | 20 | 1,294,629.6 | 26,139.1 | 715.13 |
| HAMi v2.10.0 model | 20 | 1,295,884.4 | 26,223.6 | 715.60 |
| HAMi master model | 20 | 1,295,884.4 | 26,223.6 | 715.60 |
| NVIDIA DRA v0.5.0 model | 20 | 1,313,569.7 | 34,834.5 | 702.03 |

These are virtual-time policy-model results executed on the Azure VM. HAMi
release and master are equal because they currently use the same modeled
binpack policy; this table does not measure the master mutex implementation.

## Real control-plane adapter checks

The same normalized request was submitted to all three real scheduling paths:

```text
computeCoreMS=2,800,000
memoryMB=18,432
minComputePercent=20
```

- HAMi v2.10.0 selected `2g.20gb`, placement `start=0,size=2`.
- HAMi master `88b118e` selected `2g.20gb`, placement `start=0,size=2`.
- Kubernetes DRA selected `mig-2g20gb-0` from the fake NVIDIA H100
  ResourceSlice using capacity-based CEL selectors.

This proves request conversion and placement selection. It is a scheduler
control-plane test, not GPU execution. The DRA selector initially exposed an
API contract bug (`Quantity >= Quantity` is invalid CEL); the checked-in version
uses `Quantity.compareTo(...) >= 0` and passed server-side validation.

Raw traces, all 80 per-seed summaries, ResourceClaim/ResourceSlice objects,
HAMi Pods, and scheduler logs are stored in `migbench-v2-azure-results.tar.gz`.
Archive SHA-256:

```text
8e3f5b76629214cdb1d1bf708c1187e6a8b4f5b0740358e7b69d91d0be6d74c9
```
