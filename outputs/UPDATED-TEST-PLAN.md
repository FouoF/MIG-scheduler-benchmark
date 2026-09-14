# Dynamic MIG 调度评测方案（资源需求模型版）

## 1. 评测目标

比较 HAMi release、HAMi master 和 NVIDIA DRA DynamicMIG 在相同 H100
集群、相同任务序列和相同重配置成本模型下的调度效果。

主实验不再把任务直接定义为 MIG profile，而是定义为：

- 到达时间；
- 总计算工作量（归一化核·毫秒）；
- 运行期间的显存驻留需求；
- 可选最低算力、完成期限和节点约束。

调度后，根据实际获得的 MIG profile 算力计算任务持续时间。由此同时衡量
排队、profile 选择、placement、碎片和过度分配，而不是只衡量固定时长任务能否
放入集群。

## 2. 统一任务模型

```yaml
jobID: job-000001
arrivalTimeMS: 3721
computeCoreMS: 2800000
memoryMB: 18432
minComputePercent: 25
deadlineMS: 180000
nodeSelector: {}
```

字段语义：

- `computeCoreMS`：以整张 H100 算力百分比为核数单位的工作量。例如 100%
  算力运行 28 秒等于 2,800,000 core-ms。
- `memoryMB`：任务整个执行期间必须驻留的显存，不随分配 profile 改变。
- `minComputePercent`：可选最低算力约束；为零时只要求显存可满足。
- `deadlineMS`：从到达开始计算的可选完成期限，仅用于 SLA 指标或约束实验。
- `requestedProfile`：仅在 fixed-profile 微基准中出现，主实验不设置。

实际运行时间为：

```text
runtimeMS = ceil(computeCoreMS / allocatedEffectiveComputePercent)
```

首版 `allocatedEffectiveComputePercent` 使用 profile 的线性 H100 GPC 比例。后续
用真机校准的 `speedFactor` 替换，但不得把校准 trace 混入无 GPU 基准。

## 3. Profile 能力表

每个 profile 必须显式定义：

```yaml
profiles:
- name: 1g.10gb
  memoryMB: 10240
  computePercent: 14
  placements: [0, 1, 2, 3, 4, 5, 6]
- name: 2g.20gb
  memoryMB: 20480
  computePercent: 28
  placements: [0, 2, 4]
- name: 3g.40gb
  memoryMB: 40960
  computePercent: 42
  placements: [0, 4]
- name: 7g.80gb
  memoryMB: 81920
  computePercent: 100
  placements: [0]
```

profile 列表以目标驱动/NVML 版本实际公布的 H100 能力为准。HAMi 和 DRA
必须使用同一份规范化能力表；某后端不能表示的 profile 应标记为能力不兼容，不能
算作普通调度拒绝。

## 4. 两类公平实验

### 4.1 Constraint 模式（主实验）

任务只携带显存和最低算力约束，由后端在所有可行 profile 中选择。这个模式衡量：

- profile 选择；
- placement；
- 排队和吞吐；
- 大 profile 加速与占用更多容量之间的权衡；
- MIG 重配置决策。

### 4.2 Fixed-profile 模式（解释性微基准）

公共转换器在提交前选择最小可行 profile，HAMi 和 DRA 收到完全相同的 profile
请求。这个模式隔离衡量：

- placement 策略；
- 物理碎片；
- node/GPU 选择；
- mutex 或 ResourceClaim 竞争；
- 重配置开销。

两组结果必须分别报告，不能混合聚合。若 constraint 模式差异明显但
fixed-profile 模式接近，可把差异主要归因于 profile 选择，而不是 placement。

## 5. 后端转换

### 5.1 HAMi Adapter

生成单 Pod：

```yaml
metadata:
  annotations:
    hami.io/vgpu-mode: mig
    migbench.io/compute-core-ms: "2800000"
spec:
  schedulerName: hami-scheduler
  containers:
  - resources:
      limits:
        nvidia.com/gpu: 1
        nvidia.com/gpumem: 18432
        nvidia.com/gpucores: 25
```

- `gpumem` 表示显存下限。
- `gpucores` 表示相对整卡百分比，由统一请求的 `minComputePercent` 转换。
- fixed-profile 模式增加 profile 约束；若目标 HAMi 版本不能直接表达 profile，
  Adapter 应使用与该版本匹配的请求字段，并在结果中保存转换依据。
- 从 `hami.io/vgpu-mig-allocations` 读取 profile、GPU UUID 和 placement。
- 节点 agent 在虚拟建片完成后模拟 Allocate 确认并释放 node mutex。

### 5.2 NVIDIA DRA Adapter

生成 ResourceClaim 和引用它的 Pod。constraint 模式使用 ResourceSlice 中的
`memory` 与 `multiprocessors` capacity 生成 CEL selector；fixed-profile 模式使用
`gpu.nvidia.com/profile` 精确匹配。

```yaml
exactly:
  deviceClassName: mig.nvidia.com
  selectors:
  - cel:
      expression: >-
        device.attributes['gpu.nvidia.com'].type == 'mig' &&
        device.capacity['gpu.nvidia.com'].memory >= quantity('18Gi')
```

从 `ResourceClaim.status.allocation.devices.results` 读取 pool 和 device，再关联
ResourceSlice 得到 profile、placement、显存和算力。虚拟任务结束时同时删除 Pod
与 ResourceClaim。

CEL 表达式必须通过目标 Kubernetes 版本的 server-side dry-run 契约测试，避免把
API/schema 错误计作调度失败。

## 6. 虚拟生命周期

控制面使用真实墙钟处理调度请求，工作负载生命周期使用确定性虚拟时间：

1. 到达虚拟时间后创建后端对象并启动墙钟计时。
2. 收到合法 placement 后记录调度延迟。
3. 根据布局变化加入建片、删片或整卡重排成本。
4. 成本事件结束后，任务进入 Running，并根据实际 profile 计算 `runtimeMS`。
5. 虚拟运行结束后释放任务和 placement；HAMi agent 同时完成 mutex 清理。
6. 后端 API、状态或 mutex 超时记为 backend/infrastructure error，不记作资源拒绝。

因为真实调度是墙钟、任务执行是虚拟时间，同一虚拟时间点的所有到达事件必须先
批量提交，再按稳定的 `jobID` 顺序处理结果，避免 watcher 返回顺序破坏可重复性。

## 7. Workload 生成

主 workload 联合生成计算量和显存，而不是独立生成 profile：

- 到达：Poisson、burst、周期性潮汐和 trace replay。
- 计算量：对数正态或经验分布。
- 显存：小模型、中模型、大模型和双峰分布。
- 相关性：支持计算量与显存正相关、弱相关和独立三种场景。
- SLA：可选短、中、长 deadline bucket。

必须包含以下压力序列：

1. 小显存长计算任务先到，大显存短任务后到；
2. 大显存长任务先到，小任务 burst 后到；
3. 大小显存交替，但计算量相反；
4. 多个任务集中结束，检验容量恢复和重配置风暴；
5. 同一节点并发到达，放大 HAMi mutex 竞争；
6. 总空闲显存足够但没有合法连续 placement 的请求相关碎片场景。

每组随机 workload 生成一次 JSONL 并固化。所有后端、版本和重复实验回放完全相同
的 trace。

## 8. 指标

### 8.1 主要结果

- makespan、完成数和吞吐；
- 平均、p50、p95、p99 排队时间；
- flow time（到达至完成）和 slowdown；
- deadline/SLA 完成率；
- 调度墙钟延迟和超时数。

### 8.2 资源效率

```text
core-time efficiency = Σrequested compute work / Σallocated core-time
memory-time efficiency = Σ(memory request × runtime) /
                         Σ(allocated profile memory × runtime)
```

同时报告：

- GPU、GPC、显存时间加权利用率；
- 核时和显存时间过度分配；
- 每个 profile 的选择次数和驻留时间；
- MIG 创建、删除、整卡重排次数及虚拟耗时占比。

### 8.3 碎片与竞争

- 物理碎片：空闲资源中无法组成任何合法 profile 的比例；
- 请求相关碎片：总容量足够但请求无法合法 placement 的次数和等待时间；
- stranded capacity：相对于合法 placement 枚举最大容量的损失；
- HAMi mutex 等待次数、总等待和 p95；
- DRA claim allocation 重试、pending 原因与持续时间。

## 9. 实验矩阵

每个 seed 对以下目标做隔离、全新集群的配对回放：

1. HAMi `v2.10.0`；
2. HAMi master `88b118e565a9effaa27f81669da291c5508fc5e4`；
3. NVIDIA DRA `v0.5.0`；
4. first-fit/binpack 在线基线；
5. 小规模离线 oracle 下界。

每种 workload 至少 20 个 seed；先用 50 个任务做控制面回归，再用 250/1000 个任务
做稳定性和扩展性实验。报告以 paired seed 差值和 bootstrap 95% 置信区间为主，
不只比较单次均值。

HAMi release 与 master 除 Git ref、镜像和新增策略参数外，必须使用完全相同的 chart
配置。mutex 优化实验单独改变 `node-lock-retry-timeout`，并同步 extender HTTP
timeout，不能与其他参数变化混为一个实验。

## 10. 验收标准

- 同一 seed 生成逐字节一致的资源需求 trace。
- 两后端转换后的请求在归一化显存和最低算力上等价。
- HAMi/DRA 返回的 profile 必须满足原任务约束且 placement 合法。
- 运行时间必须由实际 profile 和 `computeCoreMS` 推导，不能继续读取预生成 duration。
- 删除任务后容量完整恢复，无 mutex、claim、placement 或配额泄漏。
- fixed-profile 微基准中两后端收到相同 profile 序列。
- 基础设施错误与资源等待分别计数。
- 报告明确区分模拟结果、控制面结果和真机校准结果。

## 11. 迁移处理

旧 trace 中的 `durationMS + profile` 保留为 `profile-bound-v1`，只用于回归测试。新的
主格式版本为 `resource-bound-v2`。二者不得在同一个聚合报告中比较 makespan。

此前 250 个固定 profile/固定 duration 的结果降级为实现回归数据，不再作为 HAMi
与 NVIDIA DynamicMIG 性能结论。
