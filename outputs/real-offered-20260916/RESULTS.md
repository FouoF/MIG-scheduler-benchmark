# Real control-plane offered-load benchmark

Date: 2026-09-16

## Scope

- Kubernetes v1.36.4 on a CPU-only Azure VM.
- Two kind worker nodes, four simulated H100 GPUs per node, 56 GPC slots total.
- Real HAMi scheduler/extender and real kube-scheduler DRA allocation path.
- Fake node agents publish inventory and validate placement; no NVML, CDI,
  kubelet GPU plugin, or GPU hardware is used.
- One shared 500-job resource-bound trace (`seed=20260914`) generated from an
  empty cluster with `targetOfferedLoad=1.40` and no prefill jobs.
- Common profiles: `1g.10gb`, `2g.20gb`, `3g.40gb`, and `7g.80gb`.

## Version locks

- HAMi release: `v2.10.0`.
- HAMi master: `684c5f74f6a6b9118d08ea281670220aa49a0ab4`.
- NVIDIA DRA: `v0.5.0`.
- Runner commit: `72a3905`.

Current HAMi master requires `--device-plugin-namespace` and
`--device-plugin-service-account` for `/refit` authentication. The master
manifest supplies `kube-system` and `hami-scheduler`; its checksum and locally
built image digest are included in the evidence archive.

## Results

| Backend | Makespan (ms) | Throughput/h | Mean wait (ms) | P95 wait (ms) | P99 wait (ms) | GPC util. | Physical frag. | Stranded | Request frag. | Mean scheduling |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| HAMi release v2.10.0 | 29,199,026 | 61.6459 | 1,996,134 | 10,601,688 | 13,867,992 | 86.86% | 3.21% | 3.02% | 252 | 537.7 ms |
| HAMi master 684c5f74 | 29,282,173 | 61.4708 | 1,910,064 | 10,337,852 | 13,342,334 | 87.12% | 4.21% | 1.90% | 277 | 505.8 ms |
| NVIDIA DRA v0.5.0 | 29,195,586 | 61.6532 | 2,119,875 | 12,714,828 | 15,141,259 | 85.32% | 5.47% | 5.30% | 261 | 138.8 ms |

All runs completed 500/500 jobs, reached 100% peak GPC utilization, had no
timeouts, and passed the average-utilization/backlog high-load validation.
Backlogged measurement time was 89.3% for release, 90.0% for master, and 89.2%
for DRA.

## Interpretation

HAMi master and release are not making the same decisions: 446 of 500
normalized node/GPU placements differ. Master lowers mean wait by 4.31%, P95 by
2.49%, and P99 by 3.79%. It also raises GPC utilization by 0.26 percentage
points and lowers stranded capacity by 1.12 points. Those improvements do not
reduce the single-seed makespan: master is 0.285% slower, while physical
fragmentation is 1.00 point higher and request-fragmentation incidents increase
from 252 to 277.

NVIDIA DRA has a makespan only 3,440 ms (0.012%) below HAMi release, which is a
tie at this experiment's resolution. DRA has much lower wall-clock scheduling
latency, but mean wait is 6.20% higher and P95 is 19.93% higher than release.
It also has lower GPC utilization and higher physical and stranded
fragmentation. Long virtual task runtimes keep wall-clock API latency from
directly dominating makespan.

The effect is profile dependent. Relative to release, master substantially
improves 1g, 3g, and 7g mean waits, especially 7g (about 0.90 million ms), but
roughly doubles 2g mean wait. This redistribution explains why aggregate queue
latency improves without a corresponding makespan improvement.

This is one paired seed, so it is evidence of behavior for this trace, not a
general ranking. None of the runs records a whole-GPU reconfiguration event;
the result measures real online placement and control-plane allocation, not
the cost of an NVML-backed MIG teardown/rebuild. Run 10-20 paired seeds before
claiming a statistically stable improvement.

## Evidence

- Archive SHA-256:
  `281b47c343d06623856f38d928ceca1967ee9d6697358858889c03572a0704db`
- The archive contains trace, per-job results, event logs, generated Kubernetes
  objects, scheduler logs, cluster snapshots, manifests, image inspection, and
  version/checksum locks.
- Azure resource group `migbench-real-offered-20260916` is intentionally still
  allocated pending review.
