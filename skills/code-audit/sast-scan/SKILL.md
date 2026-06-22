---
name: sast-scan
description: >-
  多语言多介质静态粗筛——基于本地 Semgrep 规则扫描源码 / XML 配置 / 模板，产出按
  high_confidence / needs_dataflow_confirmation / high_noise 分桶的漏洞候选清单。本能力是
  辅助加速器：只加速结构型 sink 候选，不替代直接阅读源码，不覆盖语义型漏洞（逻辑 / 越权 / RBAC）。
when-to-use: 当需要为大体量 / 多介质项目快速建立结构型漏洞候选集、发现强 sink 或动态 SQL/模板/配置风险时（可选加速手段，非深读前置门槛）
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
argument-hint: "[target_path] [--lang java|go|python|js|php|c]"
arguments:
  - target_path
  - lang
---

# SAST 多漏洞粗筛（Semgrep + 本地规则）

## 1. 触发线索 / 适用信号

按"项目规模 + 介质 + 依赖 + 流程位置"四维识别本能力命中场景。

**项目规模维度**：
- `list_files` 在目标根目录被截断（默认 5000 / 上限 20000）
- monorepo / 多模块 / 多语言混合项目
- Controller / Service 单文件超过 2000 行（人读分页成本高，可用粗筛先圈定密集区再针对性读）

**介质维度**（按项目结构识别——单看扩展名会漏 XML / 模板）：
- 含 `**/mapper/**/*.xml` 或 `**/*Mapper.xml`（MyBatis 模板里的 `${}`）
- 含 `templates/` / `views/` / `WEB-INF/` 等服务端模板目录（Thymeleaf / Freemarker / Jinja2 / Blade）
- 含 `application*.yml` / `*.properties` / `nginx.conf` / `web.xml` 等配置介质
- C/C++ 项目含手写 `Runtime.exec` / `system()` / 格式化字符串 sink

**依赖维度**（粗筛收益高的栈）：
- `pom.xml` 含 `mybatis-spring` / `spring-boot-starter-web`
- `package.json` 含 `sequelize` / `typeorm` / `mysql2`（raw 通道集中）
- `go.mod` 含 `gorm.io/gorm`（`db.Raw` / `db.Exec` 集中）
- `requirements.txt` 含 `django` / `flask` / `sqlalchemy`
- `composer.json` 含 `laravel/framework` / `thinkphp`

**流程位置维度**（本能力是可选加速器，不是深读前置门槛——直接阅读源码始终是合法主路径）：
- 已完成 [project-framework-analysis](../project-framework-analysis/SKILL.md)，且代码体量 / 介质丰富度足以让粗筛为结构型 sink 候选提速
- 大体量 / 多介质项目想先快速圈出结构型 sink 高密度区，再用直接阅读做深挖（粗筛加速候选构建，不代替逐链路阅读）
- 已有上一轮 sast-scan 候选但发现规则覆盖不全（如新规则集发布、新框架进入扫描面）

**反向信号**（不命中本能力）：
- 目标已经是单文件或<100 行的小修改半径——直接走对应漏洞 skill 即可
- 用户已显式指定单漏洞类型（"只看 SQLi"）——直接 [dataflow-analysis](../dataflow-analysis/SKILL.md) + 对应漏洞 skill

**底线：本能力未命中 ≠ 该子系统安全。** 空 / 低命中只代表"规则没匹配到"，常见漏报有项目自有 wrapper（`Runtime.exec` → `CmdUtil.run`）、注解内 SQL（`@Select("... ${} ...")`）、规则未覆盖的栈；而语义型漏洞（业务逻辑 / 越权 / RBAC / 状态机 / 影响评估）完全在本能力（正则 / AST 模式匹配）覆盖外。**不得据本能力空命中下"安全"结论**——这些维度必须靠直接阅读源码触达，与粗筛是否命中无关（参 §11 静态分析边界、§9 反例义务）。

---

## 2. 造成原因

**本能力 n/a**（原因：粗筛跨多种漏洞类型，单类漏洞成因由对应 skill 承担。SQLi 成因看 `business-logic-auth-review` / `dataflow-analysis` 引用的对应能力；XSS 成因看 `stored-xss-detection` / `csp-audit`；密钥泄露成因看 `secret-detection`；以此类推）。

---

## 3. 领域 source-sink 数据流模型

**本能力 n/a**（原因：Semgrep 规则匹配只判"是否出现危险 pattern"，**不做跨函数追踪**。源-汇可达性证明由 [dataflow-analysis](../dataflow-analysis/SKILL.md) 接力。本能力的产物里 `needs_dataflow_confirmation` 桶正是为此设的——把"模式命中但跨函数链路未证"的候选交给数据流分析）。

---

## 4. 常见类型

本能力规则集覆盖的漏洞大类（按已知主流覆盖，不追求穷举）：

| 大类 | 典型 sink 形态 | 主要落桶 |
|---|---|---|
| **代码执行 / 反序列化** | `Runtime.exec` / `eval` / `ObjectInputStream.readObject` / `pickle.loads` / `unserialize` | high_confidence（强 sink）/ needs_dataflow（封装层） |
| **SQL 注入** | 字符串拼接进 SQL / ORM Raw / MyBatis `${}` / ORDER BY 拼接 | high_confidence（XML `${}`）/ needs_dataflow（拼接） |
| **路径穿越 / 任意文件** | `new File(userInput)` / `FileInputStream` / `Path.resolve` 无校验 | needs_dataflow（普遍需追校验层） |
| **命令注入** | `Runtime.exec(String)` / `bash -c` / `system()` 接收变量 | high_confidence（含变量参数）/ needs_dataflow |
| **SSRF** | `URL.openConnection` / `HttpClient.execute` / `requests.get` 用户可控 URL | needs_dataflow |
| **XXE / XML 实体** | `DocumentBuilderFactory` / `SAXParser` 未禁用外部实体 | high_confidence（配置缺失） |
| **模板注入** | Velocity / Freemarker / Jinja2 `render(string)` 用户可控模板 | needs_dataflow / high_noise |
| **XSS（服务端模板）** | 模板原样输出 / `innerHTML` / `dangerouslySetInnerHTML` | high_noise（模式宽，FP 多） |
| **硬编码密钥** | `password=...` / `apiKey=...` 字面量 + 配置文件字段 | high_confidence（与 [secret-detection](../secret-detection/SKILL.md) 协作） |
| **危险配置** | `debug=true` / 默认凭据 / 过宽 CORS / 弱 TLS | high_confidence（与 [dangerous-config](../dangerous-config/SKILL.md) 协作） |
| **信任边界类初筛** | `request.getAttribute` / `Cookie` / `session` 参与分支判断 | needs_dataflow（与 [session-security](../session-security/SKILL.md) / [business-logic-auth-review](../business-logic-auth-review/SKILL.md) 协作） |

注：本表不是穷举清单，本地规则库会随版本更新；粗筛产物本身的覆盖度以**实际命中条目**为准，不靠表格预设。

---

## 5. 入口点定位

按项目结构构建"实际扫描面"——粗筛能否命中真候选取决于扫描面是否覆盖到正确介质，**只看扩展名会漏 XML / 配置 / 模板**。

> 下列框架 / 项目类型仅作类似项目示例，不限于此；以目标实际栈为准。

### 本地规则路径

ASTER 启动时把本地嵌入规则提取到：

```text
~/.aster/rules/
├── java/
├── go/
├── python/
├── javascript/
├── php/
└── c-cpp/
```

每个语言目录同时包含自建高价值规则与 `community/` 子目录（Semgrep 社区高质量规则）。**禁止**用 `--config auto` / `--config p/xxx` 拉在线规则（why：审计环境可能离线、在线规则版本不可固定影响可追溯性）。

### 自研封装层的自定义规则

项目把标准库 sink 封装进自有 wrapper（`CmdUtil.run` 包 `Runtime.exec`、`BaseRepository.rawQuery` 包 JDBC、自有 `Sql.exec` 等）时，内置规则只查标准 API → 命中数异常少（FN）。判据：[project-framework-analysis](../project-framework-analysis/SKILL.md) 的"自有类与方法"清单提示有封装层，但本能力命中数与项目规模不匹配。

此时优先**为自研 wrapper 现写一条项目级 Semgrep 规则并补扫**——wrapper 调用点往往系统性散布多处，规则对"多调用点"的扩展性远好于逐点人读（逐点读留给一次性 / 需语义判断的场景）。操作：

1. 从 framework-analysis / 直接阅读确认 wrapper 的**完整签名与危险参数位**（哪个入参是命令 / SQL / 路径 / 反序列化数据的终点）。
2. 写一条项目级规则，存到工作区项目级规则目录（如 `shared/coverage-ledger/custom-rules/<lang>-wrappers.yml`，不污染 `~/.aster/rules`）。最小形态：

   ```yaml
   rules:
     - id: project-cmdutil-run-cmd-injection
       languages: [java]
       severity: ERROR
       message: "CmdUtil.run 封装 Runtime.exec；入参用户可控即命令注入候选"
       metadata: { confidence: needs_dataflow_confirmation, source_report: sast-scan }  # metadata 仅人读默认标注，不决定最终桶位
       pattern: CmdUtil.run(...)
   ```

3. 与内置规则一起跑（`--config` 可多次传，自定义规则叠加在语言规则之上；仍是本地文件，不违反"禁拉在线规则"；自定义规则路径相对工作区根）：

   ```bash
   semgrep scan \
     --config "$HOME/.aster/rules/<lang>" \
     --config shared/coverage-ledger/custom-rules/<lang>-wrappers.yml \
     <target_path> --json --timeout 600 --max-memory 4096 --jobs 4 \
     --exclude .git --exclude node_modules --exclude vendor \
     --exclude dist --exclude build --exclude out --exclude target
   ```

4. 命中按 §7 思考检查点归入三桶、按 §9 落 jsonl（`source_report` 仍记 `sast-scan`，`description` 注明命中自研 wrapper 规则）；最终归桶按命中点形态判定，不由规则 metadata 决定——wrapper 仅"可能危险"、无法静态定参数化形态时归 `needs_dataflow_confirmation` 交 [dataflow-analysis](../dataflow-analysis/SKILL.md) 追上游。

**适用边界**：自定义规则只为**结构型 sink 的自研封装**（命令 / SQL / 路径 / 反序列化 / 模板执行等）扩展召回；语义型漏洞（逻辑 / 越权 / RBAC / 状态机）规则写不出，仍靠直接阅读。也**不为"水货"维度造规则**（弱加密 / 弱 hash / 弱 TLS 等非安全场景高噪声项——见 §10 高噪声模式）。

### 按项目结构扫描面构建

| 语言 | 特征文件（识别语言+框架） | 必入扫描面的介质 |
|---|---|---|
| Java | `pom.xml` / `build.gradle` / `*.java` | `**/*Mapper.xml`、`**/*.properties`、`**/*.yml`、`templates/` / `views/` / `WEB-INF/` |
| Go | `go.mod` / `*.go` | `config/*.yaml`、模板目录、SQL 构造层（`*repository*.go` / `*dao*.go`） |
| Python | `requirements*.txt` / `pyproject.toml` / `*.py` | `settings.py`、模板（Jinja2 / Django templates）、`*.cfg` / `*.ini` |
| JS/TS | `package.json` / `*.js` / `*.ts` | 服务端模板（EJS / Pug / Handlebars）、SSR、中间件、配置 |
| PHP | `composer.json` / `*.php` | Blade / Twig / Smarty、配置、上传点 |
| C/C++ | `Makefile` / `CMakeLists.txt` / `*.c` / `*.cpp` | 头文件、命令执行点、内存安全点 |

### Java 项目的强信号

识别到任一信号后，**必须**确认 XML mapper 已入扫描面（why：MyBatis `${}` 注入是 Java 项目最高密度的高置信候选源，缺它直接导致"完整审计已完成"结论失真）：

- `pom.xml` 含 `org.mybatis` / `mybatis-spring`
- 项目根目录树含 `**/mapper/` 目录
- 含任意 `*Mapper.xml` 文件

若扫描面统计中 XML mapper 数为 `0` 但识别到上述信号——按 §9 闭环要求输出阻断性告警，不得给"已完整审计 SQL 注入"结论。

### 排除项

明显无关目录加入 `--exclude`：

```
--exclude .git --exclude node_modules --exclude vendor
--exclude dist --exclude build --exclude out --exclude target
```

---

## 6. 跨框架代码变体

本节列粗筛规则集对主流框架的"安全形态 vs 危险形态"对照——帮助理解为何某些命中需要走 §9 三桶判定（同一 sink 形态在不同框架里命中性质不同）。

| 框架 | 安全形态 | 危险形态（规则命中） | 主要落桶 |
|---|---|---|---|
| **MyBatis** | `#{var}` 占位符（参数化） | `${var}` 直拼（XML / 注解 `@Select`） | high_confidence |
| **Gorm** | `db.Where("col = ?", val)` | `db.Raw(fmt.Sprintf(...))` / `db.Where("col = " + val)` | high_confidence（含变量）/ needs_dataflow |
| **JdbcTemplate** | `queryForObject(sql, args)` 带占位符 | `queryForObject(sql)` 无 args 形式 / 字符串拼接 | high_confidence / needs_dataflow |
| **Spring JPA** | `@Query(value="...", nativeQuery=false)` + `:param` | `createNativeQuery(String)` + 字符串拼接 | high_confidence |
| **Sequelize** | `Model.findAll({where: {col: val}})` | `sequelize.query(template literal)` / `Sequelize.literal(...)` | high_confidence / needs_dataflow |
| **Django ORM** | `Model.objects.filter(field=v)` | `objects.raw(format-string)` / `.extra(where=[str])` | high_confidence |
| **Jackson** | `ObjectMapper` 关闭 `enableDefaultTyping` | 开启了 polymorphic typing + 反序列化用户输入 | high_confidence（反序列化） |
| **Velocity / FreeMarker** | 模板从受信文件加载 | `Template.evaluate(userControlledString)` | needs_dataflow / high_noise |
| **DocumentBuilderFactory** | 显式 `setFeature("...disallow-doctype-decl", true)` | 默认未禁用 + 接收用户 XML 输入 | high_confidence（配置缺失） |

> ORM 通用危险点：所有 ORM 都有 Raw 通道；字段名 / 表名 / 排序方向不能参数化（需白名单）。这两类是规则命中后**直接归 high_confidence**的位置。

---

## 7. 思考检查点

候选条目分桶时按 sink 语义思考（不按业务命名）：

- 该 finding 的 sink 是否真到了**用户可控 source**？（粗筛只看 sink；source 可达性是 high_confidence vs needs_dataflow 的分界）
- sink 形态是**强 sink**（无法通过参数化救活，如 `Runtime.exec(String)` / MyBatis `${}` / 字段名拼接）还是**弱 sink**（同名 API 有安全形态，如 `JdbcTemplate.queryForObject` 既能拼也能参数化）？
- 项目里是否已有**中间过滤层**（白名单 / 类型转换 / 框架自动转义）让该 sink 实际安全？sast-scan 看不到跨函数过滤——这类应入 `needs_dataflow_confirmation` 而非直接 `high_confidence`。
- 该 finding 属于**已知高噪声模式**吗？典型如 SSTI 宽匹配、JNDI 审计规则、模板原样输出——这类应入 `high_noise_patterns`，不挤主结论。
- 该候选**该交给哪个下游 skill**？跨函数链路 → [dataflow-analysis](../dataflow-analysis/SKILL.md)；存储型 XSS → [stored-xss-detection](../stored-xss-detection/SKILL.md)；越权 → [business-logic-auth-review](../business-logic-auth-review/SKILL.md)；硬编码密钥 → [secret-detection](../secret-detection/SKILL.md)；配置类 → [dangerous-config](../dangerous-config/SKILL.md)。

---

## 8. 检测方法论 / 数据流追踪

> 本能力**只到候选**——跨函数 / 跨文件可达性证明走 [dataflow-analysis](../dataflow-analysis/SKILL.md)；动态利用证据走对应黑盒 / graybox skill。本节方法论描述粗筛产出，不规定 plan / step 编排。

### 基线流程

1. **环境就绪**：`semgrep --version`；未安装引导用户安装 `semgrep`（why：本能力依赖外部二进制）。
2. **语言与框架识别**：按 §5 表格用特征文件而非扩展名定位；若用户已传 `--lang`，仍要补做框架信号盘点（why：`--lang java` 不告诉你是否要扫 MyBatis XML）。
3. **扫描面构建**：按 §5 在扫描面统计里写明 Java 文件数 / XML mapper 数 / 配置文件数 / 模板文件数；Java 项目识别到 MyBatis 信号但 XML mapper = 0 直接停手并告警（why：缺最高密度高置信源会给出失真"已审"结论，详见 §9 反例义务）。
4. **执行扫描**：

   ```bash
   semgrep scan --config "$HOME/.aster/rules/<lang>" <target_path> \
     --json --timeout 600 --max-memory 4096 --jobs 4 \
     --exclude .git --exclude node_modules --exclude vendor \
     --exclude dist --exclude build --exclude out --exclude target
   ```

   - 优先用工具内置 `--jobs N`（共享规则集解析与 AST 缓存，比多进程高效，避免多 semgrep 同时驻留 OOM）
   - 通过 bash 工具显式传 `timeout_ms`（如 `600000`），避免提前截断；被取消时整棵进程树（含 `semgrep-core`）会被自动清理
   - 识别到自研封装层（命中数与项目规模不匹配）时，按 §5「自研封装层的自定义规则」现写项目级规则、与内置规则 `--config` 叠加补扫，再回到三桶分类

5. **大项目分批**：扫描面文件数 > 5000 时按顶层模块 / 目录边界**串行**切分；详见 [references/semgrep-batching.md](references/semgrep-batching.md)。
6. **三桶分类**：按 §7 思考检查点把每条命中归入 `high_confidence` / `needs_dataflow_confirmation` / `high_noise_patterns`。
7. **产物落库**：按 §9 闭环要求，每条候选独立 jsonl 落 `shared/coverage-ledger/findings/sast-scan.jsonl`。

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

- [ ] 扫描面统计完整（Java 文件 / XML mapper / 配置 / 模板四个介质都有数字）
- [ ] Java 项目识别到 MyBatis 信号时 XML mapper 数 > 0；否则已记入扫描缺口并阻断"已审"结论
- [ ] 识别到自研封装层（命中数与规模不匹配）时，已为其结构型 sink 现写项目级规则补扫（或记入扫描缺口）
- [ ] 每条命中按 sink 语义入三桶之一（不漏不重）
- [ ] high_noise_patterns 桶单独列出，未挤入主结论
- [ ] 每条候选含 `file:line` + source/sink 表达式片段
- [ ] 产物已 append-only 写入 jsonl（不靠汇总阶段事后补全）

---

## 9. 闭环要求（必须遵守）

> 闭环判定 / 取证完整性 / 破坏性动作以 [common/closure-verification.md](../../common/closure-verification.md) 为准，下面只列本能力特有的判定上限与产物契约。
>
> **为什么这里是「必须」**：本节属交付契约——产物结构关系到下游 [dataflow-analysis](../dataflow-analysis/SKILL.md) / 各单漏洞 skill / `result-with-file` 机器消费；产物聚合或省略会让整条链路失效，因此是刚性要求。

### 白盒判定上限

本能力作为**粗筛**原子能力，判定上限为 `static-confirmed`（粗筛级模式命中），**不等于动态 confirmed**。升级路径：

| 三桶 | 上限状态 | 升级路径 |
|---|---|---|
| `high_confidence` | `static-confirmed (粗筛级)` | 跨函数链路确认 → [dataflow-analysis](../dataflow-analysis/SKILL.md) → 黑盒可观测效果验证 → `confirmed` |
| `needs_dataflow_confirmation` | `suspected` | 强制走 [dataflow-analysis](../dataflow-analysis/SKILL.md)；不通过中间信号直接升级 |
| `high_noise_patterns` | `needs_review` | 默认不上主结论；按上下文复核，复核后多归 not_vulnerable |

**禁止**仅凭 sast-scan 命中直接判 `confirmed`——无可观测效果证据，仅模式命中不构成动态利用。

### 产物契约（必须遵守）

**为什么这里是「必须」**：产物结构是下游机器消费的接口，聚合 / 省略 / 区间会让 `result-with-file` 计数闸门失效，并让单漏洞 skill 无法回溯到具体 file:line。

每确认一条候选**立即** append 一行到 `shared/coverage-ledger/findings/sast-scan.jsonl`，不等汇总阶段回头整理（why："事后总结"是聚合 / 区间 / "等"省略的根源）：

```json
{
  "id": "sast-001",
  "title": "MyBatis ${} 动态拼接",
  "severity": "high",
  "cwe": "CWE-89",
  "source": "${name}",
  "sink": "SELECT WHERE",
  "entry_point": "POST /user/search",
  "status": "needs_review",
  "confidence": "high_confidence",
  "file_location": "UserMapper.xml:42",
  "source_report": "sast-scan",
  "description": "..."
}
```

字段约束：
- `id` 带 `sast-` 前缀全局唯一
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`（粗筛默认 `needs_review`）
- `confidence ∈ high_confidence | needs_dataflow_confirmation | high_noise`（与三桶对齐）
- `(source, sink, entry_point)` **三元组任一不同即各自独立成行**——禁止合并折叠
- `entry_point` 填该 sink 可达的 HTTP 入口点（method+URL）；无明确入口点的系统性命中填 `systemic`
- `file_location` 填 `file:line`，不留空、不写区间

**禁止**：
- 聚合计数（"50 处任意文件下载 + 13 处路径穿越"）—— 丢失了具体位置，下游无法消费
- "等" / "..." / "（略）" / "（其余 N 条略）" 省略 finding —— 看似覆盖完整实则漏检
- 因项目大就只扫了部分模块却宣称"完整审计已完成"——分批扫描未扫到的模块必须显式列入"扫描缺口"

### 反例义务（必须遵守）

> **why**：粗筛"已完整覆盖"结论是覆盖完整性产物声明，缺失反向验证会让下游误信"该子系统该维度安全"。

写"已完整审计 SQL 注入 / RCE / 任意文件"等结论前，产物必须包含：
- 扫描面统计（介质数清单 + 与项目结构识别信号一致性核验）
- 分批扫描时每批对应模块清单与覆盖关系
- 任一未扫到的模块在"扫描缺口"段显式列出
- Java 项目 MyBatis 信号触发但 XML mapper = 0 → 阻断结论，标 `partial-coverage`

清单不完整 → 结论降级 `partial-coverage`。

---

## 10. 具象化反例库

### FP（看似命中实际不构成）

**反例 1：MyBatis `${}` 在 ORDER BY 但已白名单**

- 抽象规则：`${col}` 在 ORDER BY 位置仍可注入，但若上游有 enum 白名单则实际安全
- 具体场景：Mapper 含 `ORDER BY ${orderBy}`，但 Service 层 `orderBy = whitelist.contains(input) ? input : "id"`
- 关键识别特征：sink 位置看似危险，但跨函数追到上游有白名单 / enum 校验
- 排除方法：标 `needs_dataflow_confirmation` 推 [dataflow-analysis](../dataflow-analysis/SKILL.md) 追上游；不在 sast-scan 阶段直接判 FP

**反例 2：`Runtime.exec(String[])` 数组形态 + 常量首元素**

- 抽象规则：Java `Runtime.exec(String[])` + 首元素为常量命令，仅参数变量 → 无命令注入风险
- 具体场景：`Runtime.exec(new String[]{"git", "log", "--oneline", userBranch})`
- 关键识别特征：调用形式是数组而非字符串拼接；首元素是字面量
- 排除方法：sast-scan 仍命中（规则按 API 名），归 `needs_dataflow_confirmation` 让下游复核

**反例 3：路径拼接但已 `Path.normalize` + 前缀校验**

- 抽象规则：`new File(input)` 命中，但上游已规范化并校验前缀属于受信目录
- 具体场景：`Path p = Paths.get(base).resolve(input).normalize(); if (!p.startsWith(base)) reject;`
- 关键识别特征：sink 调用前有 `normalize()` + `startsWith(allowedRoot)` 组合
- 排除方法：归 `needs_dataflow_confirmation`，由 dataflow-analysis 确认校验链完整

### FN（看似不命中实际是真洞）

**反例 4：通过封装层调用的 sink（规则没覆盖封装 API）**

- 抽象规则：项目把 `Runtime.exec` 封装成 `CmdUtil.run(String)`，sast-scan 规则只查 JDK API 漏报
- 具体场景：调用点全是 `CmdUtil.run(userInput)`，扫描结果一片"安全"
- 关键识别特征：[project-framework-analysis](../project-framework-analysis/SKILL.md) 的"自有类与方法"清单提示项目有自定义封装但 sast-scan 命中数异常少
- 确认方法：按 §5「自研封装层的自定义规则」为 wrapper API 现写项目级 Semgrep 规则补扫（系统性多调用点首选），或交 [dataflow-analysis](../dataflow-analysis/SKILL.md) 用 source-sink 模式追（一次性 / 需语义判断时）

**反例 5：MyBatis 注解里的 `${}`（XML 扫描面外）**

- 抽象规则：MyBatis 注解 `@Select("SELECT ${col} FROM ...")` 与 XML 模板等价危险，但扫描面只扫 `*.xml` 会漏
- 关键识别特征：项目用 `@Mapper` 接口 + 注解写 SQL，`*Mapper.xml` 数较少
- 确认方法：扫描面同时纳入 `*Mapper.java` 注解级别扫描

### 已知高噪声模式（直接归 high_noise_patterns 桶）

| 模式 | 噪声来源 |
|---|---|
| SSTI 模板变量原样输出 | 服务端模板天然渲染变量，绝大多数是预期行为 |
| JNDI 审计规则（log4j 风格） | 应用层调用 `Context.lookup` 大多是受信内部目录 |
| 序列化器接收 `Object` 类型 | Java 类型擦除导致大量 `ObjectInputStream` 误报 |
| 弱加密算法（MD5 / SHA1） | 用于非安全场景（哈希校验 / cache key）时不构成漏洞 |

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码**。本能力的可观测能力到正则 / AST 模式匹配为止。

下面这些情形 sast-scan **无法继续追踪**，必须落 `needs_dataflow_confirmation` 桶并标 `static-unknown`，**不允许**默认为 not_vulnerable：

1. **跨函数链路**——sast-scan 不做调用栈追踪。命中的 sink 是否真接到用户可控 source 必须交 [dataflow-analysis](../dataflow-analysis/SKILL.md)。
2. **反射调用 / 动态分派**——Java `Method.invoke` / Python `getattr` / JS `obj[name]()` / 字符串决定调用哪个 DAO 方法。处置：标 `static-unknown` 记录反射点行号。
3. **闭源 / 无源码依赖**——三方 jar / dll / so / 闭源 SDK。处置：依赖图谱标 `unknown` 推 [dependency-decompile](../dependency-decompile/SKILL.md) 反编译；**不能**直接判 not_vulnerable。
4. **动态字符串构造**——配置文件驱动的 SQL（`config.get("query.user")`）/ 代码生成器（mybatis-generator）生成的 SQL。处置：标 `static-unknown`，必要时读配置文件验证。
5. **运行时配置切换 / feature flag**——dev / prod 不同分支不同 SQL 模板。处置：每个分支独立审计；不能只看 dev 分支下结论。
6. **AOP / 注解处理器 / 框架自动注入**——Spring `@Aspect` / Django middleware 修改 request 上下文。处置：独立列出拦截器实现，确认是否引入过滤。
7. **超出 `--max-memory 4096` 单批限制的大文件**——单文件超 200MB（如某些自动生成代码）会触发 semgrep 单文件超时。处置：把该文件入"扫描缺口"，必要时按需手工读关键段落。

**底线**：本能力写"该子系统无 X 类漏洞"前，所有 `needs_dataflow_confirmation` 与 `static-unknown` 单元格必须显式列出原因。否则结论降级 `partial-coverage`。

---

## 12. 修复建议

**本能力 n/a**（原因：粗筛产出候选不直接对应单类漏洞修复。各单漏洞的修复路径见对应 skill：SQL 注入 → 走 [dataflow-analysis](../dataflow-analysis/SKILL.md) 升级到 graybox 后参 `pentest/sql-injection-comprehensive` §12；XSS → [stored-xss-detection](../stored-xss-detection/SKILL.md) §12；密钥 → [secret-detection](../secret-detection/SKILL.md) §12；以此类推）。
