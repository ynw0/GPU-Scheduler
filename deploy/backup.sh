#!/usr/bin/env bash
set -euo pipefail

database="/var/lib/gpu-scheduler/scheduler.db"
backup_dir="/var/lib/gpu-scheduler/backups"
retention="14"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
target="$backup_dir/scheduler-$timestamp.db"

install -d -m 0750 -o gpu-scheduler -g gpu-scheduler "$backup_dir"
sqlite3 "$database" ".backup '$target'"
chown gpu-scheduler:gpu-scheduler "$target"
chmod 0640 "$target"

mapfile -t old_backups < <(find "$backup_dir" -maxdepth 1 -type f -name 'scheduler-*.db' -printf '%T@ %p\n' | sort -rn | tail -n "+$((retention + 1))" | cut -d' ' -f2-)
for backup in "${old_backups[@]}"; do
  [[ "$backup" == "$backup_dir"/* ]] || exit 1
  rm -- "$backup"
done

