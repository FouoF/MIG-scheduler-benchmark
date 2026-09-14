# Fake agent implementation and control-plane verification

Date: 2026-09-14

## Outcome

`migbench agent` now implements the GPU-free scheduler contract for HAMi
Dynamic MIG and Kubernetes/NVIDIA DRA. It publishes simulated H100 topology,
observes placements made by the real scheduler, rejects illegal or overlapping
placements, records allocation/release events, and acknowledges HAMi's node
mutex only after the lock owner has a newly observed valid allocation.

The agent does not implement NVML, CDI, kubelet device-plugin gRPC, or DRA
NodePrepare. Pods may therefore remain at the runtime preparation boundary;
the benchmark measures the control-plane allocation decision.

## Verified environment

- Azure CPU-only VM: `Standard_D8as_v5`; no GPU attached.
- kind `v0.30.0`.
- Kubernetes `v1.36.4`.
- HAMi master source snapshot: `88b118e565a9effaa27f81669da291c5508fc5e4`.
- NVIDIA path: native Kubernetes DRA allocator using `resource.k8s.io/v1`.

## Results

NVIDIA DRA selected `gpu0-mig-2g20gb-0` for a request needing 18 GiB and at
least 20 multiprocessors. The fake agent normalized that to
`2g.20gb/start=0/size=2`, accepted it, and later emitted a release after the
ResourceClaim was deleted.

HAMi master selected these two simultaneous placements on one simulated H100:

- `2g.20gb/start=0/size=2`
- `3g.40gb/start=4/size=3`

The second placement demonstrates that the real master scheduler preserved a
legal three-slice region beside the existing two-slice allocation. The agent
validated both, removed `hami.io/mutex.lock` for the matching new Pod only, and
emitted two release events after the Pods were deleted.

Evidence:

- `fake-agent-hami-master-events.jsonl` — SHA-256
  `47d116af63b38937ab0f2cc0010351d8c05b392d8fd55a1e287e1bc37b908d60`
- `fake-agent-nvidia-dra-events.jsonl` — SHA-256
  `b2a245e528bdc97fb1489c77bf97c72932e5d241ee378e1e75cfa63a855aa7a2`

## Automated checks

`go test ./...` covers H100 legal starts, overlap rejection, atomic allocation
snapshot replacement, multi-GPU HAMi inventory, per-GPU DRA shared counters,
26 placement candidates for two H100s, and exact HAMi mutex-owner matching.

The DRA smoke script now starts `migbench agent` instead of applying the old
static ResourceSlice fixture and requires an agent-side `allocated` event before
passing.
