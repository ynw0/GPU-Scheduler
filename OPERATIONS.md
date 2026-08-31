# 运维手册

## 日常检查

```bash
systemctl status gpu-scheduler-agent gpu-scheduler-controller
journalctl -u gpu-scheduler-agent -u gpu-scheduler-controller --since today
systemctl list-timers gpu-scheduler-backup.timer
docker info --format '{{.ServerVersion}} {{.CgroupVersion}} {{.CgroupDriver}} {{.SwapLimit}}'
docker compose version
grep -E 'SwapTotal' /proc/meminfo
```

Agent 离线时 Controller 保持只读降级状态，不创建新资源预约。非受管无限制容器会阻塞相应资源分配。

固定基线必须显示 Docker `24.0.5 1 cgroupfs false`、Compose `v2.24.5` 和 `SwapTotal: 0 kB`。Docker 不得监听未经认证的 TCP 2375。

## 恢复数据库

1. 停止 Controller。
2. 保留当前数据库副本。
3. 将选定备份复制为 `/var/lib/gpu-scheduler/scheduler.db`。
4. 修复 `gpu-scheduler:gpu-scheduler 0640` 权限。
5. 启动 Controller，等待 Agent reconciliation 完成。
6. 检查 `DRIFTED`、`ORPHANED` 和非受管容器，不自动删除来源不明的对象。

主密钥必须独立备份。只有数据库而没有对应主密钥时，已保存的 secret 无法恢复。

## 告警基线

- Agent 心跳中断：立即告警。
- OOM、GPU 健康异常和资源漂移：立即告警。
- GPU 利用率低于 10% 且 CPU 高于 85% 持续 10 分钟：数据管线瓶颈告警。
- 检测到 Swap 被启用、磁盘使用率超过 90%、备份失败：运维告警。
