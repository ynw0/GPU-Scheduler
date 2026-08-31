# 安全模型

- Docker 只监听 `/var/run/docker.sock`，该 socket 只对 root Agent 可见；禁止启用未经 TLS 认证的 TCP 2375，Controller 不加入 docker 组。
- Agent 不监听 TCP，只提供 Unix Socket 上的结构化接口，并再次验证挂载和 Compose。
- 普通用户禁止 privileged、Docker API socket、宿主根目录、未授权设备、`SYS_ADMIN`、host PID/IPC/cgroup 和 host network。
- Image 和 Compose 只使用本机现有镜像，不拉取、不构建。
- Compose 来源与宿主挂载根目录由管理员登记，路径在解析 symlink 后验证。
- 本地密码采用 Argon2id；连续五次失败锁定 15 分钟。
- Session Cookie 使用 Secure、HttpOnly、SameSite Strict；写操作要求 CSRF token。
- Secret 主密钥不进入数据库、日志或 Git。
- Webhook 使用 HMAC-SHA256；SMTP 使用 TLS 1.2 以上的隐式 TLS 连接。
- 所有资源、权限、启动、停止和强制操作进入审计事件。

安全边界面向受信内部用户。若需要对恶意多租户开放，应改用 Kubernetes/虚拟机级隔离，而不是扩大本平台权限模型。
