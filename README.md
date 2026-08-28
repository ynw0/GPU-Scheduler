# GPU Scheduler

GPU Scheduler 是面向单台 Linux AI 训练服务器的 Docker 资源调度平台。它统一管理 Docker Image 与 Docker Compose 训练任务，提供 CPU、GPU、内存和 NUMA 约束、排队、配额、实时监控、审计与状态对账。

## 文档

- [设计目标](DESIGN_GOALS.md)
- [系统架构](ARCHITECTURE.md)
- [实施计划](IMPLEMENTATION_PLAN.md)
- [API 规范](api/openapi.yaml)
- [Linux 部署](DEPLOYMENT.md)
- [运维手册](OPERATIONS.md)
- [安全模型](SECURITY.md)

## 技术基线

- Linux 裸机、cgroup v2
- Docker Engine、Docker Compose 2.30+
- NVIDIA Driver、NVIDIA Container Toolkit、NVML
- Go 1.26.7
- React 19.2、TypeScript、Vite 8.2、Node.js 24 LTS
- SQLite WAL

项目不提供 Windows 生产运行、cgroup v1、MIG、GPU 共享、任务抢占和多节点兼容分支。

## 进程

- `gpu-scheduler-controller`：非特权 Web/API、调度、数据库、认证和通知。
- `gpu-scheduler-agent`：Linux 特权服务，访问 Docker、Compose、cgroup、系统拓扑和 NVML。
- Caddy：提供局域网 HTTPS。

Controller 与 Agent 只通过本机 Unix Socket 通信，Agent 不监听 TCP。
