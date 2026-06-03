---
name: <技能唯一名，稳定不轻易改>
description: <检测/解决什么风险；触发条件一句话，与同类可区分>
when-to-use: <何时触发/适用信号，与 description 正交>
tags: <逗号分隔，如 auth,idor,access-control>
# 下列字段按需保留，无需要就删除；不要新增解析器不认的字段。
# enabled: true          # 默认启用，停用才写 false
# user-invocable: true   # 仅作子技能、不希望用户直呼时写 false
# agent: all             # 跨 agent 通用即 all/省略
# context: inline        # 默认 inline；走子代理才 fork
# allowed-tools: bash,read_file   # 仅 fork 或需限定工具时
# arguments: [target_url]         # 需要入参时，配合正文 $target_url
# argument-hint: "[target_url]"
# mcp: syntaxflow                 # 仅依赖 MCP 工具时
# version: "1.0"
---

# <技能标题>

## 目标
<一句话说明本技能要确认什么风险真实成立>

## 适用场景
- <典型接口/参数/业务形态>

## 前置条件与安全边界
- 仅在授权环境验证，禁止破坏性改写生产数据。
- 单接口请求预算：<给出上限，避免过度探测>。
- 单轮只改一个变量，避免多变量干扰。

## 检测步骤
1. <建立基线>
2. **向量①<名称>**：<手法>
3. **向量②<名称>**：<正交手法，如置空/污染/编码绕过>
4. <写操作回读 / 复验>

## 闭环验证要求（必须遵守）
通用闭环口径见同根目录 `common/closure-verification.md`（技能表 path 列同一抽取根下，需要时 read_file 读取）。核心：结论须形成「输入 → 处理 → 真实危害 → 可复核证据」完整证据链；仅凭中间信号最多判 `suspected`，<填本漏洞「证明什么才算 confirmed」>。

## 判定标准

| 现象 | 判定 |
|------|------|
| <证明真实危害> | confirmed |
| <仅中间信号> | suspected |
| <防护生效/无泄露> | not vulnerable |

## 误报控制
- <本漏洞特有的误判来源与排除方式>

## 最小 PoC 输出模板
- 目标接口：`<METHOD> <URL>`
- 对照请求：`<基线>` vs `<攻击>`
- 关键证据：`<真实危害字段/副作用>`
- 结论：`confirmed | suspected | not vulnerable`

## 修复建议
- <服务端根因修复，非仅前端/配置层缓解>
