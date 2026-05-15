---
name: intrusion-detection
description: 入侵检测分析 — 日志分析与异常行为识别
tags: host-defense,ids,log-analysis,forensics
when-to-use: 当怀疑主机被入侵、需要分析日志和异常行为时
allowed-tools: read_file,list_files,rg,bash
user-invocable: true
---

# 入侵检测分析

## 目标
通过日志分析、进程审查和系统状态检查，识别入侵痕迹并重建攻击时间线。

## 分析流程

### 1. 登录与认证分析
- 认证日志：`cat /var/log/auth.log` 或 `/var/log/secure`
- 失败登录统计：`grep "Failed password" /var/log/auth.log | awk '{print $(NF-3)}' | sort | uniq -c | sort -rn`
- 成功登录来源：`grep "Accepted" /var/log/auth.log`
- 当前登录用户：`w` 和 `last`
- SSH 暴力破解检测：短时间内大量失败登录

### 2. 进程异常检查
- 当前进程树：`ps auxf`
- 高 CPU/内存进程：`top -bn1 | head -20`
- 隐藏进程检测：比对 `/proc` 和 `ps` 输出
- 异常进程特征：随机名称、从 /tmp 或 /dev/shm 运行、异常父进程

### 3. 网络连接审查
- 活跃连接：`ss -antp` 或 `netstat -antp`
- 外连连接：关注非标准端口的外连
- DNS 查询：`cat /var/log/syslog | grep -i dns` 或检查 DNS 缓存
- 已建立的反向 Shell 连接特征

### 4. 文件系统检查
- 最近修改的文件：`find / -mtime -1 -type f 2>/dev/null`
- 隐藏文件和目录：`find / -name ".*" -type f 2>/dev/null`
- 可疑位置的可执行文件：`find /tmp /dev/shm /var/tmp -type f -executable 2>/dev/null`
- 关键系统文件完整性：`rpm -Va` 或 `debsums -c`
- Webshell 特征：`grep -r "eval\|base64_decode\|system\|exec\|passthru" /var/www/ 2>/dev/null`

### 5. 持久化检查
- Crontab：`crontab -l` 和 `ls -la /etc/cron.*`
- 系统服务：`systemctl list-unit-files --state=enabled`
- 启动项：`ls /etc/init.d/` 和 `/etc/rc.local`
- Shell 配置：检查 `.bashrc`, `.bash_profile`, `.profile` 是否被修改
- SSH authorized_keys：检查所有用户的 `.ssh/authorized_keys`
- LD_PRELOAD：`echo $LD_PRELOAD` 和 `cat /etc/ld.so.preload`

### 6. 时间线重建
按时间排序整合所有异常事件，构建攻击时间线：
1. 初始入侵时间点
2. 权限提升
3. 横向移动
4. 数据窃取/破坏
5. 持久化植入

## 输出要求
- 攻击时间线
- 入侵指标（IOC）列表
- 受影响的账户和系统
- 建议的处置措施
