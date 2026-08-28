# 系统架构

## 组件

```text
Browser
  │ HTTPS / REST / SSE
  ▼
Caddy
  │ loopback HTTP
  ▼
gpu-scheduler-controller (unprivileged)
  ├── Auth / RBAC / CSRF
  ├── Job API / Queue / Quota
  ├── Scheduler / Reservations
  ├── SQLite WAL
  ├── Audit / Notifications
  └── Embedded React assets
          │ HTTP/JSON over Unix Socket
          ▼
gpu-scheduler-agent (privileged, Linux only)
  ├── Docker Engine API / events
  ├── docker compose
  ├── cgroup v2
  ├── /proc and /sys
  ├── NVML
  ├── Metrics collector
  └── Runtime reconciler
```

Controller 不持有 Docker socket。Agent 只接受狭窄的结构化操作，并对请求进行二次安全校验。

## 模块边界

- `domain`：任务、资源、拓扑、分配、配额、队列和状态机类型。
- `scheduler`：自动分配、手动验证、Strict NUMA、配额和并发预约。
- `inventory`：CPU、NUMA、GPU 和 Docker 实际状态。
- `executor`：Image 与 Compose 生命周期。
- `monitor`：指标、聚合、告警和训练心跳。
- `reconcile`：期望状态与实际状态对账。
- `auth`：密码、会话、CSRF、RBAC 和 secret 加密。
- `store`：SQLite schema、事务和数据保留。
- `api`：OpenAPI Controller 接口与内部 Agent 接口。

## Image 执行

Image 任务使用结构化字段创建 Docker Engine `ContainerCreate` 请求。资源由平台写入 HostConfig，禁止执行用户提供的原始 `docker run` 字符串。

## Compose 执行

```text
registered source
    ↓ docker compose config --format json
normalized model
    ↓ security validation and resource replacement
immutable effective compose
    ↓ docker compose create
created containers
    ↓ Engine Update(CpusetMems) + Inspect verification
docker compose start
```

任一服务未分类或安全校验失败均禁止创建。部分创建失败时只清理当前 attempt 创建且带平台标签的对象。

## 调度数据流

1. `POST /job-plans` 根据当前快照返回候选资源，但不预约。
2. `POST /jobs` 在 SQLite immediate transaction 中重新计算。
3. 写入 Job、Attempt 和 RESERVED allocations。
4. Controller 调用 Agent 创建运行时对象。
5. Agent inspect 验证后，allocation 转为 ACTIVE。
6. 创建失败则 Job 进入 FAILED，释放本次 RESERVED allocation。
7. Agent 事件流和 5 秒 reconciliation 持续校验 actual state。

## 指标

- 2 秒采集和 SSE 实时推送。
- 10 秒原始快照，保留 7 天。
- 1 分钟聚合，保留 90 天。
- `/proc/stat` 提供宿主逻辑 CPU；cgroup 提供容器 CPU、内存、Swap、OOM、I/O；NVML 提供 GPU 指标与进程。

## 安全

- Agent Unix Socket 权限 `0660`，仅 Controller 服务组可访问。
- 禁止 privileged、Docker socket、宿主根挂载、未授权 devices、`SYS_ADMIN`、host PID/IPC/cgroup 和 `use_api_socket`。
- Compose 宿主路径必须位于管理员登记的 allowlist。
- Secret 使用 `/etc/gpu-scheduler/master.key` 加密，文件权限 `0600`。
- 所有启动、停止、强制终止、权限、资源和 Compose 变更写入审计日志。

## 生产目录

```text
/etc/gpu-scheduler/config.toml
/etc/gpu-scheduler/master.key
/run/gpu-scheduler/agent.sock
/var/lib/gpu-scheduler/scheduler.db
/var/lib/gpu-scheduler/effective-specs/
/var/lib/gpu-scheduler/backups/
```

