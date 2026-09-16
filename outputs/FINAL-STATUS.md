# Final control-plane benchmark report

## Scope

- Kubernetes 1.36.4 on one CPU-only Azure VM.
- Two worker nodes, four simulated H100 GPUs per node, 56 total GPC slots.
- Real HAMi scheduler/extender or real kube-scheduler DRA allocation path.
- Fake agents publish inventory and validate placement; no NVML, CDI, GPU, or container execution is involved.
- One shared, replayable 500-job trace (`seed=20260914`). The first 56 1g jobs prefill every GPC slot. Median nominal work duration is 600 seconds.
- Workload is `computeCoreMS + memoryMB`. Memory is a hard constraint; compute is total work, not a minimum instantaneous-compute guarantee. Runtime is `computeCoreMS / allocated-profile-compute-percent`.
- Common profile set: 1g.10gb, 2g.20gb, 3g.40gb, 7g.80gb. 4g is excluded because the tested HAMi DynamicMIG implementation does not support it.

All three runs completed 500/500 jobs, reached 100% peak GPC utilization, had a 100% backlogged measurement window, and passed high-load validation.

## Results

| Backend | Makespan | Throughput/h | Mean wait | P95 wait | GPC util. | Physical frag. | Request frag. |
|---|---:|---:|---:|---:|---:|---:|---:|
| HAMi release v2.10.0 | 23,847,259 ms | 75.48 | 2,945,291 ms | 12,337,395 ms | 91.46% | 4.40% | 260 |
| HAMi master 6c17a8f | 23,835,780 ms | 75.52 | 2,874,508 ms | 11,986,859 ms | 89.80% | 7.55% | 272 |
| NVIDIA DRA v0.5.0 | 23,656,972 ms | 76.09 | 2,755,964 ms | 13,219,745 ms | 92.54% | 6.18% | 252 |

The allocation profile counts are identical for every backend: 220×1g, 119×2g, 127×3g, and 34×7g.

## Interpretation

HAMi release and master do not make identical decisions: 484 of 500 normalized placement signatures differ. Nevertheless, their end-to-end outcome on this trace is nearly tied. Master reduces makespan by only 11,479 ms (0.048%) and raises throughput by 0.048%. Its mean wait is 2.40% lower and P95 wait is 2.84% lower, but its average physical fragmentation is 3.15 percentage points higher, request-fragmentation count rises from 260 to 272, and GPC utilization is 1.66 points lower. This run therefore does not show that master reduces fragmentation; it shows different placements with almost no net makespan gain.

NVIDIA DRA has the best makespan in this single trace: 0.80% below HAMi release and 0.75% below HAMi master. Its mean wait is 6.43% below release, although its P95 wait is higher than both HAMi variants. It also has the fewest request-fragmentation incidents (252). The advantage is small enough that a single seed is not sufficient for a general superiority claim.

Wall-clock scheduling latency is separated from virtual time. Mean decision latency was about 0.166 s for DRA, 0.759 s for HAMi release, and 0.755 s for HAMi master. Long virtual runtimes prevented this difference from directly dominating makespan, but scheduler ordering can still affect which pending job gets a newly freed placement.

`strandedCapacity` is zero for all three runs under the four-profile common geometry. Physical and request-related fragmentation remain the useful explanatory measures for this experiment.

## Limitations

This is one paired seed, so no seed-level confidence interval can be computed. The next statistically meaningful step is to run at least 10–20 paired seeds with the same version locks and report paired bootstrap intervals. Results do not cover real MIG creation latency, NVML, CDI, kubelet device preparation, GPU compute performance, or failure recovery.

## Reproducibility

- Runner SHA-256: `34a73bb56a012e4bd1b2e40a4477565513a7d8fa75511c939e23dc2f2f1bebaa`
- Trace SHA-256: `2b99d1602442570ec4fb2f99c6e89a69df3d4ba0b6128c9b96174dda57404b16`
- Evidence archive SHA-256: `0dc5cae327975825d2e516af26ed6f377a106be7731a50c6c89db46ad26ea47c`

The original run input with its temporary master lock placeholders is retained inside the evidence archive. `resolved-experiment.yaml`, the master image inspect output, rendered Helm manifests, and upstream Git commit resolve the actual deployed artifacts without modifying the raw run records.

The Azure resource group `migbench-full-us-20260915` and VM `migbench-full-cp` (`20.94.198.95`) remain allocated and have not been cleaned up.
