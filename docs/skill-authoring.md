# Skill 写作标准

本标准约束 `skills/**/SKILL.md` 的写法，依据运行时实际行为（`internal/service/skill_markdown.go`、`internal/service/skill_table.go`、`internal/react/skill_tool.go`），不是凭空约定。新增或重构技能时照此执行，骨架见 [SKILL.template.md](./skill-authoring/SKILL.template.md)。

## 1. 运行时如何使用一个技能（决定怎么写）

1. **发现/抽取**：`skills/` 被 `//go:embed */*` 整目录嵌入，启动时按内容 hash 抽取到 `~/.aster/skills/`（`skills/skill_extractor.go`）。非 `SKILL.md` 文件（如 `references/*.md`、`common/*.md`）也会抽取，并有可解析的磁盘路径；但加载器只把 `*/SKILL.md` 注册为技能，所以普通参考 md **不会进技能表**。
2. **路由（选型）**：规划阶段模型只看到一张技能表，列为 `name | description | when-to-use | path | context | status`（`skill_table.go:174`）。正文不参与路由。技能 > 20 个时系统会提示「按需 `read_file` 读取」（现网已 54 个，已触发）。
3. **加载（注入）**：技能被加载后，**整段正文逐字注入**上下文（`skill_table.go:115`），无 include / 无裁剪。
4. **执行替换**：仅当经「技能工具」执行路径时，`${SKILL_DIR}`/`${SESSION_ID}`/`$ARGUMENTS`/`$N`/`$参数名` 才会被替换（`skill_tool.go:197-227`）；**注入路径不替换**。所以正文里指向其它文件不要依赖 `${SKILL_DIR}`，用「同根目录相对路径 + read_file」描述。

## 2. Frontmatter 字段真值表

以 `skill_markdown.go:31-167` 为准。**不要新增字段**（解析器不认的字段会被静默忽略，且团队约定不靠新增 frontmatter 字段给规划器传参）。

| 字段 | 生效情况 | 写法建议 |
|------|---------|---------|
| `name` | **生效**，技能唯一键、表首列 | 必填、全局唯一、稳定，不轻易改 |
| `description` | **生效**，路由面 | 必填，写「检测什么风险」，一句话、可与同类区分 |
| `when-to-use` | **生效**，路由面 | 写「何时触发/适用信号」，与 description 正交 |
| `trigger_keywords` | **回退**：仅当 `when-to-use` 为空时拼成它 | 二选一即可；已写 `when-to-use` 就不必再写 |
| `tags` | **生效**，用于过滤 | 逗号或列表均可（`flexStrings`） |
| `enabled` | **生效**，`false` 则不进表 | 默认启用，停用才显式 `false` |
| `user-invocable` | **生效**，默认 `true` | 仅作为子技能被加载、不希望用户直呼时设 `false` |
| `context` | **生效**，默认 `inline`；`fork` 走子代理 | 绝大多数填 `inline` 或省略 |
| `agent` | **生效**，`all` 或匹配当前 agent 才可见（`meta.agent` 回退） | 跨 agent 通用就 `all`/省略 |
| `allowed-tools` | **生效**，fork 模式解析工具白名单（`tools` 回退） | 仅 fork 或需限定工具时填 |
| `arguments` / `argument-hint` | **生效**，配合 `$参数名` 替换（仅执行路径） | 需要入参时填 |
| `mcp` | **生效**，运行时加载对应 MCP server | 仅依赖 MCP 工具时填 |
| `version` | **生效**，注入时展示 | 可选 |
| `author` `category` `type` `priority` `metadata` | **装饰**：被解析但从不写入 `MCPSkill` | 不要再加；现存的可在重构时清理 |

## 3. 路由面（description + when-to-use）写法

这是技能表里唯一的选型依据，质量直接决定模型能否选对技能。

- `description`：**检测/解决什么风险**。例：`检测 IDOR 水平越权风险；通过替换/置空/污染资源标识访问他人资源时触发`。
- `when-to-use`：**何时触发、适用信号**。例：`存在可枚举资源标识（ID/UUID/路径）、按资源归属控制的接口`。
- 两列要正交，且与**同类技能**可区分——写完自检：把同簇技能的 description 并排看，是否一眼能分清各自边界。

## 4. Canonical 正文结构（检测类技能）

沿用现网最成熟模板（参考 `skills/pentest/idor-detection/SKILL.md`）：

```
## 目标
## 适用场景
## 前置条件与安全边界
## 检测步骤        # 多向量时用「向量①/②/③」并列，避免只写单一手法
## 闭环验证要求（必须遵守）   # 指向 common/closure-verification.md + 一句本类核心口径
## 判定标准        # confirmed / suspected / not vulnerable 三档表
## 误报控制
## 最小 PoC 输出模板
## 修复建议
```

- **闭环验证去重**：通用 5 条口径已抽到 `skills/common/closure-verification.md`，本节只写「指向行 + 本漏洞特有的闭环要点」，不再整段复制通用文字。指向行示例见任一越权技能。
- **实际效果验证方向 / 判定标准**：保留各技能自有内容（这部分本就因漏洞而异，不抽离）。

## 5. 反模式

- ❌ 整段复制通用「闭环验证要求」5 条 → 改为指向 `common/closure-verification.md`。
- ❌ `description` 与 `when-to-use` 写成同义重复，或与同簇技能雷同到无法区分。
- ❌ 堆砌装饰字段（`author`/`category`/`type`/`priority`/`metadata`）。
- ❌ 用 `${SKILL_DIR}` 在正文里拼跨文件路径（注入路径不替换）。
- ❌ 把多个技能硬编成固定阶段流水线 → 保持松散编排，由规划器按信号选取。
- ❌ 检测类技能只写单一手法 → 用并列向量覆盖「替换/置空/污染/编码绕过」等正交维度。
