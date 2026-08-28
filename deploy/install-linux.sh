#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if ! getent group gpu-scheduler >/dev/null; then groupadd --system gpu-scheduler; fi
if ! id gpu-scheduler >/dev/null 2>&1; then
  useradd --system --gid gpu-scheduler --home-dir /var/lib/gpu-scheduler --shell /usr/sbin/nologin gpu-scheduler
fi

install -d -m 0755 /opt/gpu-scheduler/bin
install -d -m 0750 -o gpu-scheduler -g gpu-scheduler /var/lib/gpu-scheduler
install -d -m 0750 -o gpu-scheduler -g gpu-scheduler /var/lib/gpu-scheduler/backups
install -d -m 0750 -o root -g gpu-scheduler /etc/gpu-scheduler

install -m 0755 "$repo_root/bin/gpu-scheduler-controller" /opt/gpu-scheduler/bin/
install -m 0755 "$repo_root/bin/gpu-scheduler-agent" /opt/gpu-scheduler/bin/
install -m 0755 "$repo_root/bin/gpu-scheduler-admin" /opt/gpu-scheduler/bin/
install -m 0644 "$repo_root/deploy/systemd/gpu-scheduler-controller.service" /etc/systemd/system/
install -m 0644 "$repo_root/deploy/systemd/gpu-scheduler-agent.service" /etc/systemd/system/
install -m 0644 "$repo_root/deploy/systemd/gpu-scheduler-backup.service" /etc/systemd/system/
install -m 0644 "$repo_root/deploy/systemd/gpu-scheduler-backup.timer" /etc/systemd/system/
install -m 0755 "$repo_root/deploy/backup.sh" /opt/gpu-scheduler/bin/gpu-scheduler-backup

if [[ ! -f /etc/gpu-scheduler/config.toml ]]; then
  install -m 0640 -o root -g gpu-scheduler "$repo_root/config.example.toml" /etc/gpu-scheduler/config.toml
fi
if [[ ! -f /etc/gpu-scheduler/master.key ]]; then
  umask 0177
  openssl rand -out /etc/gpu-scheduler/master.key 32
  chown root:gpu-scheduler /etc/gpu-scheduler/master.key
  chmod 0640 /etc/gpu-scheduler/master.key
fi

systemctl daemon-reload
echo "Installation complete. Configure /etc/gpu-scheduler/config.toml, install the Caddy site, then start the services."
