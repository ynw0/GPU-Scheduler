# 设计目标

## 产品定位

GPU Scheduler 是单机 AI 训练资源调度平台，不是通用 Docker 管理面板。平台必须持续准确回答：当前谁占用了什么、还有什么可以分配、新任务应该分到哪里、数据库记录是否与机器实际状态一致。

## 成功标准

1. 用户可以从本机镜像或管理员登记的 Compose 来源创建训练任务。
2. 用户可以自动分配或手动点选 GPU、物理核、SMT 线程、内存和 NUMA 节点。
3. 同一 CPU/GPU 不会被两个受管任务同时预约。
4. Strict NUMA 保证 CPU 和内存节点位于所选 GPU 的本地 NUMA 集合。
5. 页面明确区分 Requested、Allocated、Effective、Usage 和训练阶段。
6. 平台能发现非受管容器、宿主 GPU 进程和外部修改，并从可用资源中扣除。
7. 任务支持队列、团队与用户配额、历史、日志、指标、通知和审计。
8. Controller 或 Agent 重启后能通过实际状态对账恢复，不误删无法确认归属的容器。

## 核心不变量

- 资源预约以数据库事务和唯一约束为最终并发边界。
- GPU 使用 UUID 作为稳定标识，GPU index 仅用于界面显示。
- Docker DeviceRequests 是 GPU 隔离手段；`CUDA_VISIBLE_DEVICES` 不是安全控制。
- Compose 最终模型必须先规范化、再由平台替换资源字段、最后验证实际 HostConfig。
- 普通用户不能通过命令、Compose、挂载、设备或 capability 绕过平台。
- 容器 RUNNING 不等于正在训练；训练阶段只接受显式心跳上报。
- 不把宿主 CPU 线程热力图错误描述为容器逐线程利用率。

## 用户和权限

- `Viewer`：查看脱敏后的资源与任务状态。
- `User`：创建和管理自己的任务。
- `TeamOperator`：管理团队任务、P0 优先级和团队配额范围。
- `Admin`：系统、用户、Compose 来源、资源池和强制操作。
- `Auditor`：只读审计。

采用本地账号、Argon2id、Secure/HttpOnly/SameSite Session Cookie 和 CSRF 校验。权限同时受角色、任务所有权和团队范围约束。

## 资源策略

- GPU 整卡独占，不支持 MIG 和共享整卡。
- CPU 默认按完整物理核选择，高级模式允许拆分 SMT sibling。
- 训练服务使用独占 cpuset；辅助服务必须显式进入共享 CPU 池并设置硬限制。
- NUMA 使用 Strict Locality；跨 NUMA 多 GPU 任务只允许使用对应节点集合。
- 系统保留 CPU、故障资源、非受管容器和宿主 GPU 进程不可分配。

## 队列和配额

- 优先级为 P0、P1、P2、P3，默认 P2。
- P0 仅 TeamOperator/Admin，P1 起 User 可用。
- 不抢占运行任务，不跨优先级补位。
- 同优先级允许可运行的小任务补位；任务最多被绕过 5 次，之后成为该优先级队列屏障。
- 团队和用户分别设置 GPU、逻辑 CPU 与内存配额，任务必须同时满足两层约束。

## 非目标

- 通用 Docker GUI、镜像构建或仓库拉取。
- Web Terminal、文件管理器和原始 Docker 命令执行。
- Windows 生产运行、Docker Desktop、cgroup v2、Docker systemd cgroup driver 和非目标机兼容。
- Kubernetes、Slurm、Nomad、多节点、高可用和任务抢占。
- MIG、GPU 超卖和未经管控的 Compose。
