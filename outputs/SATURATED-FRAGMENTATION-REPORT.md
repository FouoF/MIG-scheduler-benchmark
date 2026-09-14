# Saturated fragmentation workload validation

Configuration: 2 nodes × 4 simulated H100 GPUs, 56 total GPC slots, 500 jobs,
56-job prefill, 600-second median compute duration, 15-second mean arrival
interval, and a measurement window that excludes warmup and final drain.

Across 20 paired seeds, every backend reached 100% peak GPC allocation. The
minimum fraction of measurement time with a non-empty pending queue was 99.4%,
so every run passed the configured 99% peak and 90% backlog thresholds.

| Policy model | Mean GPC utilization | Mean memory utilization | Physical fragmentation | Stranded capacity | Request-fragmented jobs |
|---|---:|---:|---:|---:|---:|
| first-fit | 94.4% | 92.4% | 3.60% | 6.92% | 250.8 |
| HAMi release model | 94.5% | 92.8% | 3.63% | 9.15% | 275.6 |
| HAMi master model | 94.5% | 92.8% | 3.63% | 9.15% | 275.6 |
| NVIDIA DRA model | 93.6% | 92.0% | 3.39% | 8.59% | 264.2 |

Utilization and fragmentation are integrated only across the steady-state
measurement window. Lower-than-100% average utilization cannot be attributed
to insufficient offered load because the queue remained non-empty; it is
therefore attributable to placement/profile constraints in this model.

These values compare simulator policies, not upstream scheduler
implementations. HAMi release and master remain identical until the real
control-plane trace runner measures mutex behavior.
