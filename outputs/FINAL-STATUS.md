# Control-plane benchmark status

The implementation is complete and verified with `go test ./...` and
`go vet ./...`. The final workload semantics are `computeCoreMS + memoryMB`:
memory is a hard constraint, while compute is total work rather than a minimum
instantaneous compute guarantee. Runtime is `computeCoreMS / allocated profile
compute percent`.

The common fake-H100 profile set is `1g.10gb`, `2g.20gb`, `3g.40gb`, and
`7g.80gb`. A 4g profile is intentionally absent because the tested HAMi
DynamicMIG implementation does not support it.

## Run status at requested stop

- NVIDIA DRA v0.5.0 completed the final 500-job trace: 500/500, virtual
  makespan 23,656,972 ms, mean wait 2,755,964 ms, peak GPC utilization 100%,
  backlog fraction 100%, high-load validation passed.
- HAMi v2.10.0 was interrupted on user request during its final-semantics run.
- HAMi master `6c17a8f` was not run under the final semantics before the stop.
- Earlier complete three-way results are retained as diagnostics only. They are
  not a fair final comparison because the DRA adapter treated compute as a hard
  minimum while HAMi treated it as non-guaranteed work capacity.

The Azure resource group `migbench-full-us-20260915` and VM
`migbench-full-cp` (`20.94.198.95`) remain allocated and were not cleaned up.
