# skills/

SAST/safety 项目的 skill 集合。每个 skill 是一段可由 agent 在 ReAct 循环中按需加载的领域知识 + 检查清单。

## 新增或修改 skill 前必读

[**SKILL_SPEC.md**](SKILL_SPEC.md) — 项目权威编写规范，覆盖 frontmatter、正文风格、章节骨架、措辞模板、提交前自检清单。规范本身对齐 [Anthropic Claude Code Skills 官方](https://code.claude.com/docs/en/skills)（skill-creator/SKILL.md）。

## 目录结构

```
skills/
├── SKILL_SPEC.md              # 项目规范（本目录入口必读）
├── README.md                  # 你正在看的文件
├── <category>/                # 一级分类（kebab-case）
│   └── <skill-name>/          # 二级目录，目录名即 skill 标识
│       ├── SKILL.md           # 必需：skill 入口
│       ├── references/        # 可选：案例库、详细规则
│       ├── scripts/           # 可选：辅助脚本
│       └── assets/            # 可选：静态资源
├── common/                    # 共享口径（如 closure-verification.md）+ 元 skill
├── embedded_skills.go         # 编译期 embed
└── skill_extractor.go         # 运行时抽取（强制二级目录结构）
```

二级目录是硬约束：`skill_extractor.go` 按 `category/skill-name/SKILL.md` 路径抽取，扁平化或三级嵌套不被识别。

## 一级分类

| 类别 | 数量 | 用途 |
|---|---|---|
| `code-audit/` | 13 | 静态代码审计（CSP / 配置 / 鉴权 / 业务逻辑等专项） |
| `pentest/` | 28 | 动态渗透测试（注入 / IDOR / SSRF / 文件上传等漏洞类） |
| `host-defense/` | 5 | 主机防御侧（基线检查 / 应急响应 / 入侵检测） |
| `common/` | 3 | 共享基础设施（result-with-file 元 skill / agent-browser / graybox-p0 入口） |
| `ctf/` | 1 | CTF 题型解题流程 |
| `vuln-repro/` | 1 | 漏洞复现方法论 |

## 重要共享文件

- [`common/closure-verification.md`](common/closure-verification.md) — 闭环验证 / 破坏性动作 / 取证完整性共享口径（被 19+ pentest skill 引用）
- [`common/result-with-file/SKILL.md`](common/result-with-file/SKILL.md) — 元 skill，定义最终报告产物格式

## 快速索引：典型场景

| 我想做… | 加载 skill |
|---|---|
| 项目首次接入、识别技术栈/入口点 | `code-audit/project-framework-analysis` |
| 大型项目漏洞候选粗筛 | `code-audit/sast-scan` |
| 跨函数 source→sink 数据流追踪 | `code-audit/dataflow-analysis` |
| CSP 策略审计 | `code-audit/csp-audit` |
| 存储型 XSS | `code-audit/stored-xss-detection` |
| SQL 注入检测 | `pentest/sql-injection-comprehensive` |
| IDOR / 越权 | `pentest/idor-detection` + `code-audit/business-logic-auth-review` |
| 结果落盘 | `common/result-with-file` |

## 原子 skill（v3.1）

v3 重构后已淘汰「父 skill 路由 + 子 skill 执行」两层结构（路由属 agent profile 职责，详见 SKILL_SPEC.md §六 / §七）。**所有 SKILL.md 都是领域原子能力**——任意 agent 都能独立 `load_skill` 调用，单文件可读懂完整检测能力。

`code-audit/` 全部 skill 按 [SKILL_SPEC.md §6.2 白盒 12 段骨架](SKILL_SPEC.md) 组织；`pentest/` 全部 skill 按 §6.1 黑盒 12 段骨架组织。两套模板见 [`CODE_AUDIT_SKILL_TEMPLATE.md`](CODE_AUDIT_SKILL_TEMPLATE.md) / [`PENTEST_SKILL_TEMPLATE.md`](PENTEST_SKILL_TEMPLATE.md)。

## 提交前自检

按 [SKILL_SPEC.md 八节「改造检查清单」](SKILL_SPEC.md#八改造检查清单提交前自检) 逐项过一遍：

```bash
# 反模式 grep 自检
grep -rn "固定检查项" skills/ --include="SKILL.md"            # 期望 0
grep -rn "按以下 checklist 逐项执行" skills/ --include="SKILL.md"  # 期望 0
grep -rn "确保覆盖完整" skills/ --include="SKILL.md"           # 期望 0
```
