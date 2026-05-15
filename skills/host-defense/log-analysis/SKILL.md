---
name: log-analysis
description: 安全日志分析 — 多源日志关联分析与威胁狩猎
tags: host-defense,log,threat-hunting,siem
when-to-use: 当需要分析系统日志、Web 日志、安全设备日志进行威胁狩猎时
allowed-tools: read_file,list_files,rg,bash
user-invocable: true
argument-hint: "[log_path]"
arguments:
  - log_path
---

# 安全日志分析

## 目标
对多源日志进行关联分析，识别威胁指标，进行威胁狩猎。

## 日志源

### 系统日志
- 认证日志：`/var/log/auth.log` 或 `/var/log/secure`
- 系统日志：`/var/log/syslog` 或 `/var/log/messages`
- 内核日志：`/var/log/kern.log` 或 `dmesg`
- 审计日志：`/var/log/audit/audit.log`

### Web 服务器日志
- Apache：`/var/log/apache2/access.log`, `error.log`
- Nginx：`/var/log/nginx/access.log`, `error.log`
- Tomcat：`/var/log/tomcat*/catalina.out`

### 应用日志
- 数据库日志（MySQL/PostgreSQL slow query, error log）
- 应用错误日志
- 安全设备日志（WAF, IDS/IPS）

## 分析方法

### 1. 日志格式识别
自动检测日志格式（Apache Combined, Nginx, syslog, JSON 等）

### 2. 关键事件提取
- 失败登录：`grep -i "failed\|error\|denied\|invalid" <log>`
- 权限提升：`grep -i "sudo\|su\|privilege\|root" <log>`
- 异常状态码（Web）：`awk '{print $9}' access.log | sort | uniq -c | sort -rn`
- 攻击特征：SQL注入、XSS、路径遍历、命令注入模式

### 3. 多维关联
- **时间维度**：异常事件的时间聚合
- **IP 维度**：同一 IP 的行为轨迹
- **用户维度**：同一用户的异常操作
- **会话维度**：同一会话的完整行为链

### 4. 威胁指标识别
- 暴力破解：高频失败登录
- Web 攻击：SQL注入/XSS/路径遍历 payload
- 数据泄露：异常大量数据传输
- 横向移动：内网间异常连接

### 5. 统计分析
- 访问频率分布（正常 vs 异常基线）
- 时间分布热力图
- 来源 IP 地理分布
- URL 访问频率 Top N

## 输出要求
- 威胁事件摘要
- 可疑 IP/用户/会话列表
- 攻击模式识别结果
- 时间线和关联分析图
- 建议的响应措施
