# Linux 部署

## 前置条件

- Linux cgroup v2。
- Docker Engine 与 Docker Compose 2.30+。
- NVIDIA Driver、NVIDIA Container Toolkit、NVML。
- Go 1.26.7、Node.js 24 LTS、pnpm 11、C 编译工具链和 SQLite CLI。
- Caddy 与可解析的内部域名。

## 安装

```bash
./deploy/build-linux.sh
sudo ./deploy/install-linux.sh
sudoedit /etc/gpu-scheduler/config.toml
sudo install -m 0644 deploy/Caddyfile.example /etc/caddy/conf.d/gpu-scheduler.caddy
sudo systemctl reload caddy
sudo systemctl enable --now gpu-scheduler-agent gpu-scheduler-controller gpu-scheduler-backup.timer
sudo /opt/gpu-scheduler/bin/gpu-scheduler-admin -config /etc/gpu-scheduler/config.toml -username admin -email admin@example.com
```

客户端必须信任 Caddy internal CA。平台不提供生产 HTTP 登录模式。

## 目录权限

- `/etc/gpu-scheduler/master.key`：`root:gpu-scheduler 0640`，仅 Controller 服务组可读。
- `/etc/gpu-scheduler/config.toml`：`root:gpu-scheduler 0640`。
- `/run/gpu-scheduler/agent.sock`：`root:gpu-scheduler 0660`。
- `/var/lib/gpu-scheduler`：`gpu-scheduler:gpu-scheduler 0750`。

## 升级

先完成数据库备份，再停止 Controller、替换三个二进制，先启动 Agent、再启动 Controller。数据库 migration 只向前执行；升级失败时恢复旧二进制和升级前备份，不修改用户 Compose 来源。
