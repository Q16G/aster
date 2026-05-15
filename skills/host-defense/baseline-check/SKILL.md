---
name: baseline-check
description: 安全基线检查 — 主机安全配置审计（CIS Benchmark）
tags: host-defense,baseline,hardening,cis
when-to-use: 当需要检查主机安全配置是否符合安全基线标准时
allowed-tools: read_file,list_files,rg,bash
user-invocable: true
---

# 安全基线检查

## 目标
对目标主机进行全面的安全配置审计，参照 CIS Benchmark 标准评估安全加固状态。

## 检查项目

### 1. 系统信息收集
- `uname -a` — 内核版本
- `cat /etc/os-release` — 发行版信息
- `uptime` — 运行时间
- `last reboot` — 重启历史

### 2. 用户与认证
- 检查空密码账户：`awk -F: '($2 == "") {print $1}' /etc/shadow`
- 检查 UID=0 的非 root 账户：`awk -F: '($3 == 0) {print $1}' /etc/passwd`
- 审查 sudoers 配置：`cat /etc/sudoers` 和 `/etc/sudoers.d/`
- 密码策略：`cat /etc/login.defs`（PASS_MAX_DAYS, PASS_MIN_LEN 等）
- 检查 SUID/SGID 文件：`find / -perm -4000 -o -perm -2000 -type f 2>/dev/null`

### 3. 网络安全
- 开放端口：`ss -tlnp` 或 `netstat -tlnp`
- 防火墙状态：`iptables -L -n` 或 `ufw status` 或 `firewall-cmd --list-all`
- 网络转发：`sysctl net.ipv4.ip_forward`
- ICMP 重定向：`sysctl net.ipv4.conf.all.accept_redirects`

### 4. SSH 加固
- 检查 `/etc/ssh/sshd_config`：
  - PermitRootLogin (应为 no)
  - PasswordAuthentication (推荐 no)
  - PermitEmptyPasswords (应为 no)
  - MaxAuthTries (应 ≤ 4)
  - Protocol (应为 2)
  - X11Forwarding (应为 no)

### 5. 服务安全
- 列出运行中服务：`systemctl list-units --type=service --state=running`
- 检查不必要的服务（telnet, rsh, rexec, finger 等）
- 检查 cron 任务：`crontab -l` 和 `/etc/cron.*`

### 6. 文件系统
- 世界可写文件：`find / -xdev -type f -perm -0002 2>/dev/null`
- 无主文件：`find / -xdev -nouser -o -nogroup 2>/dev/null`
- /tmp 挂载选项（noexec, nosuid）
- 关键目录权限（/etc/passwd 644, /etc/shadow 600）

### 7. 日志与审计
- auditd 状态：`systemctl status auditd`
- 系统日志配置：`ls /var/log/`
- 日志轮转：`cat /etc/logrotate.conf`

### 8. 更新状态
- 可用更新：`yum check-update` / `apt list --upgradable`
- 安全更新：检查是否有未安装的安全补丁

## 输出要求
- 按检查类别的合规/不合规统计
- 每项检查结果：状态（通过/警告/失败）、当前值、建议值、修复命令
- 总体安全评分和优先修复建议
