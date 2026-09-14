# Azure GPU-free control-plane smoke report

Run date: 2026-09-14 (Asia/Shanghai)  
Host: Azure `Standard_D8as_v5`, Ubuntu 24.04, East Asia  
Kubernetes: kind `v0.30.0`, Kubernetes `v1.36.4`  
Scope: scheduling control plane only; no GPU, NVML, CDI, or workload execution

## Locked targets

| Target | Source | Image |
|---|---|---|
| HAMi release | `v2.10.0` | `projecthami/hami@sha256:8c45cd84d8df3e6a8fe8c95e0141684973b5015692a0ef87588da05284f34447` |
| HAMi master | `88b118e565a9effaa27f81669da291c5508fc5e4` | locally built scheduler `sha256:08b88af4a397b21ab18ddc5adee17b8cda4da2702c9204ffa4515ae7643a9327` |
| NVIDIA DRA | `v0.5.0` | `registry.k8s.io/dra-driver-nvidia/dra-driver-nvidia-gpu@sha256:e1f104e64383ee693e982a5e6b7cf0b750023aaa5cc9b7dcc37c8e0549232933` |

## Results

- HAMi v2.10.0: real scheduler/extender selected a fake H100 and emitted a
  `1g.10gb` MIG allocation with legal placement. A `3g.40gb` reservation was
  also emitted. Raw state and logs are in `hami-release-smoke/`.
- HAMi master: the scheduler built from the exact commit ran with the current
  master Helm chart, selected `hami-master-worker`, and emitted profile
  `1g.10gb`, placement `start=0,size=1`. See
  `hami-master-smoke/master-mig-1g.json`.
- NVIDIA DRA v0.5.0 contract: kube-scheduler allocated `mig-1g10gb-0` through
  the native ResourceClaim API. While that claim existed, a whole-GPU
  `7g.80gb` request remained unallocated. After releasing 1g, the 7g claim was
  allocated to `mig-7g80gb-0`, validating shared-counter conflict and release.

HAMi's mock device plugin does not perform real MIG reconfiguration. NVIDIA's
fake ResourceSlices do not prepare a device, so its pod remains at
`ContainerCreating`. These are expected boundaries, not scheduler failures.
