# {project} 主机安全评估报告

> 本报告是各子报告的**无损超集**。下方各汇总表仅作索引/概览，明细以详细发现节（§4-§7）与附录（§15）为准；不得用汇总表替代明细。子报告中的主机/端口/服务清单、扫描与命令输出、配置片段必须逐条原样保留。

## 0. 发现记录与派生（模板专属）

> 本模板在 SKILL 通用机制之上定义自己的发现记录 schema / status / 去重键与派生命令，并规定 fid 打在哪里。**按严重度组织，无入口点概念、不产 `entry-points.jsonl`。**

### 0.1 发现记录 schema（jsonl，每行一条）

```json
{"id","title","severity","cve","host","port_service","component_version","status","confidence","source_report","description"}
```

- **status 取值与桶映射**：`confirmed` → 进正文（已确认，必附 POC/检测证据）；`needs_review` → 进正文（待复核，归"待人工复核项汇总"）；`false_positive` → 进排除项；`not_vulnerable` → 仅作覆盖证明；`superseded` → 忽略。
- **去重键** = `[cve, host, port_service]`；三者任一不同即各自独立成行，禁止折叠（同一 CVE 命中多台主机/多个服务，逐条列出）。
- `id` 带源前缀、全局唯一。

### 0.2 派生命令（SKILL step 1 执行，产出 3 件机器产物）

```bash
mkdir -p shared/coverage-ledger/findings
merged=$(find shared/coverage-ledger/findings -name '*.jsonl' -exec cat {} +)
bad=$(printf '%s' "$merged" | jq -s 'map(select((.cve==null or .cve=="") and (.host==null or .host=="") and (.port_service==null or .port_service==""))) | length')
[ "${bad:-0}" = "0" ] || echo "FAIL: 有 $bad 条 jsonl 去重键(cve,host,port_service)全空，必须补全再跑"
incl=$(printf '%s' "$merged" | jq -s 'unique_by([.cve,.host,.port_service]) | map(select(.status=="confirmed" or .status=="needs_review"))')
{
  echo "# Findings Index（jq 机械派生，禁止手写）"; echo
  echo "| id | title | severity | cve | host | port_service | status | source_report |"
  echo "|----|-------|----------|-----|------|--------------|--------|---------------|"
  printf '%s' "$incl" | jq -r '.[] | "| \(.id) | \(.title) | \(.severity) | \(.cve) | \(.host) | \(.port_service) | \(.status) | \(.source_report) |"'
} > shared/findings-index.md
printf '%s' "$incl"   | jq -r '.[].id'                                  | sort -u > shared/coverage-ledger/index-ids.txt
printf '%s' "$merged" | jq -r 'select(.status=="false_positive") | .id' | sort -u > shared/coverage-ledger/exclude-ids.txt
```

### 0.3 fid 锚点位置

每条进入正文的发现行末附 `<!-- fid:<该发现 jsonl 的 id> -->`——§4-§7 每张按严重度组织的 `### CRIT-xx` 发现卡片标题行、§8 配置基线检查表每一行、§11 误报与排除项表每一行（核到 `exclude-ids.txt`）。

## 1. 评估概览

| 项目 | 内容 |
|------|------|
| 评估对象 | {主机范围 / IP 段 / 资产组} |
| 评估类型 | 主机安全评估 |
| 评估日期 | {date} |
| 操作系统 | {OS 类型及版本} |
| 核心服务 | {关键服务列表} |

## 2. 检测范围

| 维度 | 覆盖状态 | 说明 |
|------|---------|------|
| 端口与服务暴露 | ✅/⚠️/❌ | {说明} |
| 系统补丁与漏洞 | ✅/⚠️/❌ | {说明} |
| 账户与权限配置 | ✅/⚠️/❌ | {说明} |
| 服务配置安全 | ✅/⚠️/❌ | {说明} |
| 日志与审计配置 | ✅/⚠️/❌ | {说明} |
| 网络安全配置 | ✅/⚠️/❌ | {说明} |

## 3. 风险统计

| 严重度 | Confirmed | Needs Review | 合计 |
|--------|-----------|-------------|------|
| CRITICAL | N | N | N |
| HIGH | N | N | N |
| MEDIUM | N | N | N |
| LOW | N | N | N |
| **合计** | **N** | **N** | **N** |

## 4. CRITICAL 级发现

### CRIT-01: {漏洞类型} — {受影响主机/服务} <!-- fid:{该发现 jsonl 的 id} -->

| 属性 | 值 |
|------|-----|
| 漏洞类型 | CVE-xxxx-xxxx / CWE-xxx {名称} |
| 严重度 | CRITICAL |
| 主机 | {IP / hostname} |
| 端口/服务 | {port/protocol — service name} |
| 组件版本 | {component version} |
| 验证状态 | confirmed |
| 置信度 | high / medium / low |

**描述**: {漏洞的技术描述}

**检测证据**:

```
{命令输出 / 配置片段 / 扫描结果}
```

**POC**:

```http
GET /vulnerable-endpoint HTTP/1.1
Host: {TARGET}:{PORT}
```

或

```python
import socket

TARGET = "{TARGET}"
PORT = {PORT}
# minimal reproduction script
```

**影响**: {攻击者可以做什么，横向移动风险}

**修复建议**:
1. {措施 1}
2. {措施 2}

---

（更多 CRITICAL 级发现...）

## 5. HIGH 级发现

（同 §4 格式）

## 6. MEDIUM 级发现

（同 §4 格式）

## 7. LOW 级发现

（同 §4 格式）

## 8. 配置基线检查

| 检查项 | 严重度 | 当前状态 | 期望状态 | 修复建议 |
|--------|-------|---------|---------|---------|
| {item} | {sev} | {current} | {expected} | {fix} | <!-- fid:{该发现 jsonl 的 id} --> |

## 9. 修复建议汇总

| 优先级 | 数量 | 说明 |
|--------|------|------|
| **P0 — 立即修复** | N | {概述} |
| **P1 — 高优先级** | N | {概述} |
| **P2 — 中优先级** | N | {概述} |
| **P3 — 低优先级** | N | {概述} |

## 10. 待人工复核项汇总

> 以下发现已执行分析但因特定原因无法确认。每项附注具体原因和建议的排查方向。

| 编号 | 漏洞类型 | 严重度 | 位置 | 原因 | 排查建议 |
|------|---------|-------|------|------|---------|
| NR-01 | {type} | {sev} | {location} | {为什么无法确认} | {建议如何排查} |

## 11. 误报与排除项

> 以下项在初步检测中被标记为可疑，经分析确认为误报。

| 编号 | 初始判定 | 排除原因 |
|------|---------|---------|
| FP-01 | {初始判定} | {为什么排除} | <!-- fid:{该 false_positive 发现 jsonl 的 id} --> |

## 12. 已验证安全的维度

> 以下维度在本次评估范围内已检测，未发现漏洞。

| 维度 | 验证方法 | 结论 |
|------|---------|------|
| {dimension} | {method} | 未发现漏洞 |

## 13. 评估局限性

> 以下维度因前置条件不足、环境限制或时间约束未能完整覆盖。必须并入各子报告中的环境/前置条件/网络可达性限制说明，逐条保留。

| 维度 | 限制原因 | 影响评估 | 补充建议 |
|------|---------|---------|---------|
| {dimension} | {reason} | {impact} | {what to do next} |

## 14. 结论

### 整体风险评级

{总体安全水位评估：一段话描述}

### 核心发现

1. {最关键的 2-3 个发现，一句话概括}

### 后续建议

1. {优先级最高的后续行动}

## 15. 附录：详细枚举与原始证据

> 容纳子报告中的大块明细（主机/端口/服务清单、扫描结果、命令输出、配置片段等）。逐条原样保留，禁止抽样或概括。正文相关发现引用此处对应小节。

### 15.1 主机 / 端口 / 服务清单

| # | 主机 | 端口/协议 | 服务 | 组件版本 | 备注 |
|---|------|----------|------|---------|------|
| 1 | {ip} | {port/proto} | {service} | {version} | {note} |

### 15.2 扫描 / 命令原始输出

```
{原样保留子报告中的扫描结果或命令输出}
```

### 15.3 配置片段与其他原始证据

```
{原样保留配置片段}
```

## 16. 附录：源报告覆盖表

> 对账依据为 `shared/coverage-ledger/findings/*.jsonl`（jq 去重后的发现真值源）及其派生视图 `shared/findings-index.md`：逐份源报告登记其贡献的发现是否全部纳入本报告。
> "部分纳入"必须注明未纳入条目去向（落入 §11 误报与排除项 / §13 评估局限性）。

| 源报告文件 | 贡献的发现编号 | 是否全部纳入 | 未纳入项说明 |
|-----------|--------------|------------|------------|
| {file path} | {发现编号，跨节引用用复合 ID} | 是 / 部分 | {若部分，注明未纳入条目及去向；全部纳入填"—"} |
