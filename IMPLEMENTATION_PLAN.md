# 实施计划

## 技术基线

- Go 1.26.7
- React 19.2、TypeScript、Vite 8.2、Node.js 24 LTS
- Ant Design、ECharts
- SQLite WAL
- 固定 Ubuntu 20.04、Docker 24.0.5/API 1.43、cgroup v1/cgroupfs、Docker Compose 2.24.5、NVIDIA NVML
- OpenAPI 3.1 作为公开接口唯一来源

## 里程碑

### 1. 仓库与基础设施

- 初始化 Git、Go workspace、React 工程、OpenAPI、SQLite migration、配置系统和 Controller/Agent 骨架。
- 建立 Windows 开发与 Linux 构建边界、systemd 和 Caddy 配置。

### 2. 资源发现与调度

- 实现 CPU、物理核、SMT、Socket、NUMA、内存和 GPU 拓扑模型。
- 实现资源池、系统保留核、自动分配、手动验证和 Strict NUMA。
- 使用事务和 allocation 唯一约束解决并发冲突。

### 3. Image 与 Compose

- 接入 Docker Engine、事件流、本机镜像、结构化 Image 启动和生命周期。
- 实现 Compose 规范化、安全检查、effective spec 和 create-update-inspect-start。

### 4. 监控、对账与 UI

- 实现 cgroup、Docker、NVML 指标、SSE、历史聚合、日志和告警。
- 实现资源地图、新建任务、队列、详情、模板、通知、配额、Compose 来源和审计页面。
- 实现 unmanaged、drifted、orphaned 和宿主 GPU 进程检测。

### 5. 生产能力

- 实现本地认证、RBAC、团队与用户配额、优先级队列、训练心跳、站内通知、SMTP、Webhook 和审计。
- 实现备份、恢复、安装、升级和运行手册。

### 6. 目标机 v1 收敛

- Agent 启动时验证 Docker 24.0.5/API 1.43、cgroup v1/cgroupfs、所需控制器、Compose 2.24.5、NVML 和全局 Swap 关闭状态。
- Docker Client 强制使用配置的 Unix Socket，不读取 `DOCKER_HOST`，目标机关闭未经认证的 TCP 2375。
- 删除无效的容器级 Swap 限制声明，内存限制只表达目标机真正能执行的能力。
- 新增 cgroup v1 路径解析与容器 CPU、内存、OOM、cpuset、blkio 指标；Job Metrics 不再用宿主 CPU 快照冒充容器 Usage。
- 在 Ubuntu 20.04/glibc 2.31 环境生成 Agent 生产制品，CI 只验证同一目标基线。

## 公开 API

```text
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET  /api/v1/me
GET  /api/v1/topology
GET  /api/v1/resources/availability
GET  /api/v1/images
GET  /api/v1/unmanaged-containers
POST /api/v1/job-plans
POST /api/v1/jobs
GET  /api/v1/jobs
GET  /api/v1/jobs/{id}
POST /api/v1/jobs/{id}/actions/{start|stop|cancel|retry}
POST /api/v1/jobs/{id}/heartbeat
GET  /api/v1/jobs/{id}/metrics
GET  /api/v1/jobs/{id}/logs/stream
GET  /api/v1/events/stream
GET/POST/PATCH /api/v1/templates
GET/POST/PATCH /api/v1/teams
GET/POST/PATCH /api/v1/quotas
GET/POST/PATCH /api/v1/compose-sources
GET/POST/PATCH /api/v1/resource-pools
GET /api/v1/audit-events
GET/PATCH /api/v1/notifications
```

创建任务要求 `Idempotency-Key`；异步操作返回 202，资源冲突返回 409，校验错误返回 422。状态、指标、日志和通知使用 SSE。

## Git 策略

每个里程碑完成后独立提交，禁止把整个系统堆积为单次提交。提交前检查 `git diff`，只纳入当前里程碑文件。

## 测试与验收

开发期间编写测试但不执行。实现完成后先征得用户同意，再运行：

- 拓扑、Strict NUMA、自动分配、配额、队列和状态机单元测试。
- Image、Compose、安全限制、失败清理和并发预约集成测试。
- Docker 外部修改、Agent 重启、事件丢失和资源漂移对账测试。
- cgroup、NVML、指标聚合、SSE、日志和告警测试。
- RBAC、CSRF、会话、secret、邮件、Webhook、备份与恢复测试。
- 目标 Linux 服务器完整验收。
