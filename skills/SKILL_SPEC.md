# SKILL.md 编写规范（sastx）

> 本文件是 `skills/` 目录下所有 SKILL.md 的编写权威规范。新增或改造 skill 前必读。
> 立项依据见飞书计划文档（内部记录），核心原则对齐 [Anthropic Claude Code Skills 官方规范](https://code.claude.com/docs/en/skills) 与 [anthropics/skills 官方 repo](https://github.com/anthropics/skills) 中的 `skill-creator/SKILL.md`。

## 一、目录与文件结构

```
skills/
├── <category>/                  # 一级分类：code-audit / pentest / host-defense / common / ctf / vuln-repro
│   └── <skill-name>/             # 二级目录：skill 名（kebab-case），目录名即 skill 标识
│       ├── SKILL.md              # 必需：skill 入口
│       ├── references/           # 可选：案例库、详细规则、模板
│       ├── scripts/              # 可选：辅助脚本（lint、转换、模板生成）
│       └── assets/               # 可选：静态资源（图、payload 样本）
└── common/                       # 共享口径（如 closure-verification.md），不是独立 skill
```

**二级目录结构是硬约束**：`skills/skill_extractor.go` 按 `category/skill-name/SKILL.md` 路径抽取，扁平化或三级嵌套不被识别。

## 二、Frontmatter 规范

字段命名统一 **kebab-case**（小写 + 连字符）。Anthropic 官方混用 `when_to_use` 是历史遗留，本项目内部一致性优先，统一用 `when-to-use`。

### 必填字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `name` | string | skill 标识，等于目录名，kebab-case |
| `description` | string | "做什么 + 何时用"，写**触发线索**而非纯定义；和 `when-to-use` 合计建议 ≤ 1536 字符（Anthropic 上限） |
| `tags` | csv | 用于路由匹配的关键词，逗号分隔（如 `code-audit,csp,xss`） |
| `when-to-use` | string | 触发条件短语；附加在 description 后，共用上限 |
| `allowed-tools` | csv | 允许调用的工具集（如 `bash,read_file,list_files,rg,list_skills`） |
| `user-invocable` | bool | 是否允许用户用 `/<name>` 直接触发；子 skill 设 `false`，父/独立 skill 通常 `true` |

### 可选字段

| 字段 | 何时填 | 示例 |
|---|---|---|
| `argument-hint` | skill 接收 CLI 参数时 | `"[target_path]"` |
| `arguments` | 同上，列出位置参数名 | `- target_path` |
| `mcp` | 调用 MCP 工具时（如 dataflow-analysis、yak）| 见现有 mcp 类 skill |
| `disable-model-invocation` | 仅人工触发、禁止模型自动调用 | `true` |

**无参数 skill（含所有子 skill）可省略 `argument-hint` 和 `arguments`**，不要为对齐而填空字段。

### Frontmatter 示例

```yaml
---
name: client-side-sec
description: 客户端安全子清单 — 逐项排查 CSP 策略、客户端 JS 安全。
tags: code-audit,csp,xss,dom,javascript,client-side
when-to-use: 当需要聚焦审计客户端安全维度，或项目有前端 JS 安全敏感逻辑、CSP 设置、DOM 操作、postMessage 时
allowed-tools: bash,read_file,list_files,rg,list_skills
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---
```

## 三、正文写作风格（核心）

### 3.1 反模式（yellow flags，看到要重写）

Anthropic skill-creator 原文："If you find yourself writing ALWAYS or NEVER in all caps, or using super rigid structures, that's a yellow flag — reframe and explain the reasoning."

本项目内的具体反模式：

- ❌ "**必须逐项执行**"、"**严格按下列顺序**"、"**不得跳过**"用于检查建议（用于交付契约可以，见 3.4）
- ❌ 大段 ALL CAPS 的 MUST / NEVER / ALWAYS
- ❌ 罗列 30+ 条无解释的硬规则，不告诉模型为什么这么做
- ❌ "按以下 checklist 逐项执行，确保覆盖完整。每项标注 [x] done 或 [-] n/a"——这是**检查建议**而非交付契约，硬性措辞会诱导模型在不适用场景凑数、不敢补充清单外的真实发现
- ❌ 把任务步骤当作 step 1 / step 2 / step 3 的固定流水线，丧失模型的上下文自适应能力

### 3.2 正面要点

- ✅ 用 imperative 短句给方向，附一句"为什么"。模型懂 why 才会判断边界
- ✅ checklist 写成「基线 + 自适应」：基线规范已知项，模型可基于代码事实裁剪/补充（详见 3.3）
- ✅ 不变量优先于步骤：写「最终产物应满足 X」比「先做 A 再做 B 最后做 C」更鲁棒
- ✅ 主文件目标 < 500 行（Anthropic 官方建议）；超出走 `references/` 拆分；reference > 300 行带 TOC
- ✅ description 写**触发线索**（"项目有 CSP 配置时""出现 SQL 拼接时"），不要纯定义

### 3.3 基线 checklist 措辞模板

所有"检查角度"类列表统一用「基线检查项」语义，**禁止**用「固定检查项」「必检项」「强制检查」「按以下 checklist 逐项执行」。

**段落标题**：`## 基线检查项`（父 skill 和子 skill 一致；子 skill 不再用 `## 检查项`）

**父 skill 引导语模板**：

```markdown
以下是已知的常见检查角度，作为**基线起点**而非必检硬清单。结合目标代码与上下文动态调整：

- 适用且已完成 → 标注 `[x] done`
- 明确不适用 → 标注 `[-] n/a (原因)`，原因要具体到代码事实（例如"项目无 CSP 配置"），不要笼统写"不适用"
- 基线未列出但实际发现的相关问题 → 新增条目并标注 `[+] added (来源)`，来源指代码位置或上下文线索

不要为了凑齐基线而硬套不适用的检查；也不要因基线没列就漏掉真实发现。基线只是规范已知项，不限定覆盖边界。
```

**子 skill 引导语模板**（无"加载子 skill"这层包装）：

```markdown
以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整：适用且已完成 `[x] done`、明确不适用 `[-] n/a (原因)`、基线外的真实发现 `[+] added (来源)`。
```

### 3.4 交付契约（保留「必须」的边界）

「交付契约」与「检查建议」语义边界不同，**保留**强制措辞的场景仅限三类：

1. **产物格式 / 落库结构**：jsonl 字段、coverage-ledger 落行规则、findings 索引计数闸门
2. **安全边界**：破坏性动作的"哨兵自证 / 非破坏差分 / 停手降级"协议（见 `common/closure-verification.md`）
3. **闭环验证**：完整证据链才判 confirmed、中间信号最多 suspected、取证完整性

这三类直接关系到下游机器消费、安全合规、审计可追溯，缺失会让整条链路失效，因此为**刚性要求**。在标题旁标注「必须遵守」并紧跟一句解释 why。

## 四、章节骨架建议（非强制）

下列章节是项目现有共性的高频结构，**按需选用**，不需要全部出现：

- `## 目标` — 这个 skill 解决什么问题，**不要复述 description**
- `## 适用信号` — 出现哪些代码模式或上下文时加载本 skill
- `## 前置条件与安全边界` — 测试场景的授权范围、不可碰的真实数据
- `## 基线检查项` — 见 3.3
- `## 闭环验证要求（必须遵守）` — 见 3.4 与 `common/closure-verification.md`
- `## 结论口径` — 判定语义、jsonl 字段、按入口点 / 按 (source, sink) 组织等
- `## 和其他 skill 的关系` — 上下游依赖、由哪个父 skill 触发
- `## 发现即落行` — append-only jsonl 规则
- `## 框架模式库` — 不同框架（Spring / Django / Gin 等）的鉴权模式与缺口

## 五、子 skill 与父 skill 的关系

- **父 skill**：用户可触发（`user-invocable: true`），负责"清单分发"，下面表格列出每项任务对应的子 skill。
- **子 skill**：通常 `user-invocable: false`，由父 skill 用 `skill` 工具加载，承担具体规则、案例库、深度审计逻辑。

父 skill 引导语提到"加载子 skill 后"，子 skill 自己的"基线检查项"才是真正执行的清单。两层都用「基线 + 自适应」语义，互不冲突。

## 六、轻量校验脚本

`scripts/skill-lint.sh`（不在本次范围，预留位置）。最小实现：

```bash
#!/usr/bin/env bash
# 反模式 grep 自检
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
fail=0

for pat in "固定检查项" "按以下 checklist 逐项执行" "确保覆盖完整"; do
  hits=$(grep -rln "$pat" "$ROOT" --include="SKILL.md" || true)
  if [ -n "$hits" ]; then
    echo "❌ 反模式「$pat」命中："
    echo "$hits"
    fail=1
  fi
done

[ "$fail" -eq 0 ] && echo "✅ skill-lint pass"
exit $fail
```

后续提交独立 PR 接入 CI。

## 七、附录：与 Anthropic 官方规范的差异

| 项 | Anthropic 官方 | 本项目 | 理由 |
|---|---|---|---|
| `when-to-use` 字段名 | `when_to_use`（snake）与 kebab 混用 | 统一 `when-to-use`（kebab） | 内部一致性高于跟随上游小不一致 |
| 主文件行数上限 | 建议 < 500 | 同 | 直接采纳 |
| description 字符上限 | description + when_to_use 合计 1536 | 同 | 直接采纳 |
| 编号步骤 vs 指南 | 任务型 skill 用编号步骤，知识型用"指南 + why" | 同 | 直接采纳 |
| 三态标注 | 官方未规定 | `[x] done` / `[-] n/a (原因)` / `[+] added (来源)` | 项目特有"基线 + 上下文自适应"的可追溯落地形式 |

## 八、改造检查清单（提交前自检）

新增或修改 SKILL.md 时，发起 PR 前过一遍：

- [ ] frontmatter 6 必填字段齐全，全 kebab-case
- [ ] description + when-to-use 合计 ≤ 1536 字符（中文按字数估算）
- [ ] 主文件 < 500 行；超长内容拆到 `references/`
- [ ] 无「固定检查项」「必检项」「按以下 checklist 逐项执行」「确保覆盖完整」字样
- [ ] 检查清单段标题统一 `## 基线检查项`
- [ ] 含 checklist 的章节使用三态标注（`[x]` / `[-]` / `[+]`）说明
- [ ] 「必须遵守」仅用于交付契约（产物格式 / 安全边界 / 闭环验证），紧跟 why 解释
- [ ] 无参数 skill 不强填 `argument-hint` / `arguments`
- [ ] 引用其他 skill 用相对路径 `[name](../other-skill/SKILL.md)`，不用绝对路径

## 九、相关文件

- `common/closure-verification.md` — 闭环验证 / 破坏性动作 / 取证完整性共享口径
- `skills/skill_extractor.go` — skill 抽取逻辑，定义目录结构硬约束
- `skills/code-audit/client-side-sec/SKILL.md` — 父 skill 改造样板
- `skills/code-audit/csp-audit/SKILL.md` — 子 skill 改造样板
- `skills/code-audit/business-logic-auth-review/SKILL.md` — 结构特殊的样板
