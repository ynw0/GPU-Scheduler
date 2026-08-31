# 目标运行平台

GPU Scheduler 只面向当前固定的单台 A800 训练服务器，不提供跨发行版、跨 cgroup 版本或跨 Docker 版本兼容。

## 固定基线

| 组件 | 固定值 |
| --- | --- |
| 操作系统 | Ubuntu 20.04，glibc 2.31 |
| 内核 | 5.4.0-216-generic |
| Docker Engine | 24.0.5，API 1.43 |
| Docker cgroup | version 1，driver `cgroupfs` |
| Docker Compose | v2.24.5 |
| NVIDIA Container Toolkit | 1.14.3 |
| NVML | `libnvidia-ml.so.1` 可动态加载 |

宿主机存在 `/sys/fs/cgroup/unified` 目录，但 `cgroup.controllers` 为空。该布局仍按 Docker cgroup v1 处理；Agent 以 Docker Info 返回的 `CgroupVersion=1` 和 `CgroupDriver=cgroupfs` 为运行依据。

## 启动契约

Agent 启动时必须一次性验证：

1. Docker 只能通过配置的 Unix Socket 访问。
2. Docker Engine、API、cgroup version 和 cgroup driver 与固定基线完全一致。
3. `cpu,cpuacct`、`cpuset`、`memory`、`blkio` 控制器已挂载。
4. Docker Compose 版本严格等于 v2.24.5。
5. Docker 支持 cpuset 与 memory limit，NVML 可以初始化。
6. 读取宿主机 Swap 总量和 Docker Swap limit 能力。

前五项任一条件不满足时 Agent 拒绝启动。Swap 可以正常启用；能力差异只影响每任务 Swap 限制，不影响 Agent 启动。

## Swap 语义

- Docker `SwapLimit=false` 时，平台不写 `MemorySwap` 或 Compose `memswap_limit`，并提示“不支持每任务 Swap 限制”。容器继续共享宿主机现有 Swap。
- Docker `SwapLimit=true` 时，平台仍不禁用 Swap，由 Docker 按其默认策略使用和限制宿主 Swap。
- 平台始终监控宿主 Swap 总量与用量，但不把宿主共享 Swap 记为某个 Job 的专属资源。

## cgroup v1 职责

资源计划仍由 Scheduler 计算，资源限制仍由 Docker 写入 cgroup v1。Agent 的 cgroup 代码只负责读取和验证实际状态：

- `cpuacct.usage`、`cpuacct.usage_percpu`：容器 CPU 总量与逐逻辑 CPU 用量。
- `memory.usage_in_bytes`、`memory.limit_in_bytes`、`memory.failcnt`、`memory.oom_control`：内存与 OOM 状态。
- `cpuset.cpus`、`cpuset.mems`：实际 CPU 与 NUMA 内存节点。
- `blkio.throttle.io_service_bytes_recursive`：容器块设备 I/O 计数。

容器控制器路径由 Docker Inspect 的 init PID 和 `/proc/<pid>/cgroup` 确定，不根据容器 ID 猜测目录。

## 明确不支持

- cgroup v2 和 Docker `systemd` cgroup driver。
- Ubuntu 22.04/24.04、Docker Desktop、Windows 生产运行。
- 其他 Docker Engine、API、Compose 或 NVIDIA Container Toolkit 版本。
- 每 Job PSI、MIG、GPU 共享、抢占和多节点。

Agent 使用 CGO 链接 NVML。生产 Agent 必须在 Ubuntu 20.04 或等价 glibc 2.31 构建环境生成，不能使用更高版本 glibc 的通用 Linux 构建产物。
