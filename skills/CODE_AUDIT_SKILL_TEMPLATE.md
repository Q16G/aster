# 白盒原子 skill 12 段骨架模板（v3.1）

> 本文件是新增 / 改造 `code-audit/<skill-name>/SKILL.md` 的**填空模板**——纯白盒视角，基于代码 pattern / 数据流 / 项目结构。
> 完整规范见 `SKILL_SPEC.md` §6.2（白盒骨架）+ §6.3（共享章节）+ §6.4（common/ 引用规范）。
>
> **关键不变量**：
> - 12 段标题用本表中文标题，便于 grep 自检
> - 不允许颠倒顺序；缺段需在该位置明文写 `本能力 n/a (原因)`
> - **不写黑盒内容**——不出现"HAR 信号 / payload 范式 / 响应观察通道 / 编码绕过"。这些进 `pentest/` 下对应黑盒 skill
> - 任意 agent 只看本 SKILL.md 单文件就能读懂完整白盒审计能力（独立可读）
> - 仅靠 `list_skills` 描述即能命中本 skill（独立可触发）

下面用 **SQL 注入白盒数据流检测**作为贯穿示范的漏洞类型。改造其他白盒漏洞 / 审计能力时把示范段替换为对应内容。

---

## Frontmatter 模板

```yaml
---
name: <skill-name>                              # kebab-case，等于目录名
description: >-
  <一句话讲做什么，跟上一句触发线索（"代码里看到 X 时"/"项目结构含 Y 时"/"依赖含 Z 时"）>。
  适用：<白盒典型场景，如"代码审计 / SAST / 数据流验证">。
allowed-tools: bash,read_file,list_files,rg,list_skills
user-invocable: false
---
```

**白盒 SQLi 示范**（假设新建 `code-audit/sql-injection-static-audit/SKILL.md`）：

```yaml
---
name: sql-injection-static-audit
description: >-
  SQL 注入白盒数据流审计——按 source / sink 追踪静态可达性，识别 ORM raw 通道 / 字段名拼接 / 二阶注入。
  代码里看到字符串拼接进 SQL、ORM `.raw()` 调用、Mapper XML 用 `${}` 时使用。
allowed-tools: bash,read_file,list_files,rg
user-invocable: false
---
```

---

## 1. 触发线索 / 适用信号

> **目的**：让 `list_skills` 能命中本 skill。和 frontmatter `description` 同步。
>
> **白盒视角**：按"代码 pattern + 文件结构 + 依赖"分类，**不**讨论流量 / HAR / 响应特征（属黑盒）。

**白盒 SQLi 示范**：

按 **代码 pattern + 文件位置**分类（不按业务命名）：

**代码 pattern 维度**（grep 命中模式）：
- 字符串拼接进 SQL：`"SELECT ... " + var` / `fmt.Sprintf("WHERE col = %s", val)` / template literal
- ORM Raw 通道：`.raw()` / `.query(string)` / `Raw()` / `nativeQuery()` / `createNativeQuery(String)`
- 字段名 / 表名拼接：`Order(sort)` / `order_by(col)` / 动态 ORDER BY
- ORM 表达式跳过参数化：Sequelize `literal()` / SQLAlchemy `text(format-string)`

**文件位置 / 命名约定维度**：
- `*Mapper.xml`（MyBatis）：含 `${var}` 模板的 SQL
- `*DAO.java` / `*Repository.java`：手写 SQL 拼接
- Django `models.py` 含 `.raw()` / `.extra()`
- Go 项目里 `db.Raw()` / `db.Exec(fmt.Sprintf(...))`

**依赖 / 注解维度**：
- `pom.xml` 含 `mybatis-spring` + 用 `@Select` 注解 + `${}`
- `package.json` 含 `sequelize` / `typeorm` / `mysql2` 的 raw 通道使用
- `go.mod` 含 `gorm.io/gorm` + Raw / `database/sql` 拼接
- `@Query(nativeQuery=true)` + 字符串拼接

业务命名（如方法名 `searchUser`）只作粗筛——sink 语义相同就是审计候选。

---

## 2. 造成原因（共享章节）

> **目的**：独立可读地讲清楚成因。**禁止**写"详见 X skill 漏洞图谱"。
>
> **写法**：用 source-sink 框架（不引用其他 SKILL.md）。**与黑盒版本语义一致**——漏洞成因与检测方式无关。

**白盒 SQLi 示范**：

source 是任何用户可控输入（query / body / header / cookie / 路径参数 / 已入库再回读的字段）。sink 是 SQL 上下文：WHERE / LIKE / ORDER BY / GROUP BY 子句、INSERT 值、UPDATE SET、JOIN ON、HAVING、LIMIT / OFFSET、任何拼接进 SQL 查询的字符串位置。

**任何 source 未经参数化绑定就被拼接到 sink，即构成 SQL 注入**——攻击者控制的字符串改变了 SQL 语法树。预编译参数（PreparedStatement / `$1 $2` / `?` 占位符）把数据和指令在协议层隔离开，是默认防御。

ORM 不是免疫层：所有 ORM 都有 Raw 通道，且字段名 / 表名 / 排序方向不可参数化（必须白名单）。

---

## 3. 领域 source-sink 数据流模型

> **目的**：把"用户可控输入流到危险 sink"的 source-sink 框架在**白盒视角**下具象化。**这是白盒原子 skill 的核心段**——给本漏洞的专属代码层 source / sink 函数 / 拼接位置。
>
> **写法**：
> 1. **代码层 source 集合**（具体到函数 / 注解）
> 2. **代码层 sink 集合**（具体到危险函数 / 拼接位置）
> 3. **数据流追踪规则**（跨函数 / 跨文件 / 跨依赖的追踪边界）
>
> 只列本漏洞专属集合 + 追踪规则；不写 pattern_id 命名规则、不写跨参数 / 跨端点 / 跨子系统横扫机制（agent 自己掌握，不在 SKILL.md 重复）。

**白盒 SQLi 示范**：

**代码层 source 集合**（按框架）：
- HTTP handler：`gin.Context.Query()` / `request.GET.get()` / `req.body.x` / Spring `@RequestParam`
- 反序列化点：JSON unmarshal 后字段 / form parse 结果
- 数据库回读：先入库后再读出来的字段（二阶 source）
- 文件 / 配置：读取的外部内容若可控

**代码层 sink 集合**：
- 直接 SQL 拼接：`db.Query(fmt.Sprintf("..."))` / `cursor.execute(string % var)` / `"..." + var`
- ORM Raw 通道：`db.Raw()` / `sequelize.query()` / `repo.query()` / `objects.raw()` / `EntityManager.createNativeQuery(String)`
- MyBatis `${var}`（XML 或注解里）
- 字段名 / 表名 / 排序方向位置：`Order(sort)` / `order_by(col)` 接受动态字符串

**数据流追踪规则**：
- 跨函数追踪：source 流向其他函数参数、流向类成员、流向闭包捕获变量
- 跨文件追踪：DAO / Repository / Service 调用链
- 框架边界：Spring `@Service` / Django middleware / Express middleware 是否引入过滤
- **闭源依赖**：依赖库内部数据流不可见（参 §11 静态分析边界）

---

## 4. 常见类型（共享章节）

> **目的**：列出本漏洞的主流攻击变体。
>
> **写法**：表格或列表。**与黑盒版本语义一致**。

**白盒 SQLi 示范**：

| 类型 | 静态识别特征 | 白盒识别难点 |
|---|---|---|
| **常规拼接型** | `+` / `format` / template literal 拼接进 SQL 字符串 | 通常直接 grep 即命中 |
| **ORM Raw 型** | `.raw()` / `.query(string)` / `Raw()` / `nativeQuery()` | 看 ORM 文档识别 raw 通道；参数可能"看似参数化"但实际是字符串构造 |
| **字段名拼接型** | `Order(sort)` / `order_by(col)` / 动态 ORDER BY | 易漏报——参数化对字段名无效，需找白名单缺失 |
| **二阶注入** | source 先 INSERT 后被另一段 SELECT 拼接 | 跨函数 / 跨文件追踪；入库点参数化 ≠ 回读点安全 |
| **MyBatis `${}`** | XML 或注解里 `${var}` 而非 `#{var}` | 容易被误以为是合法模板表达式 |
| **动态 SQL 构造器** | 编程方式拼 SQL：`StringBuilder` / `string.Join("WHERE ...")` | 控制流复杂，易绕过简单 grep |

---

## 5. 入口点定位

> **目的**：告诉模型如何在**项目结构**里找本类漏洞的 source / sink。
>
> **白盒视角**：列出 source 在不同项目结构中的典型位置（路由声明 / 控制器 / DAO / Repository / Mapper）。**不**讨论 HAR / 端点账本（属黑盒）。

**白盒 SQLi 示范**：

按项目结构找 SQL sink 候选位置：

> 下列框架 / 项目类型仅作类似项目示例 不限于此；以目标实际栈为准。

### Java / Spring 项目

- `*Mapper.xml` / `@Mapper` 接口：含 `${var}`（危险）vs `#{var}`（安全）
- `*Repository.java` / `*Dao.java`：`JdbcTemplate.queryForObject(String,...)` 无 args 形式、`EntityManager.createNativeQuery(String)`
- `@Query(nativeQuery=true)` + 字符串拼接
- `pom.xml` 确认 MyBatis / JPA / JDBC 版本，识别框架范围

### Python / Django 项目

- `models.py` 含 `objects.raw()` / `.extra(where=[...])`
- `views.py` 直接调用 `cursor.execute(format-string)` / `connection.cursor()`
- `requirements.txt` 看 Django / SQLAlchemy 版本

### Go / Gin 项目

- `db.Raw()` / `db.Exec(fmt.Sprintf(...))` / `sql.DB` 拼接 / Gorm `Where("col = " + var)`
- 路由文件（`router.go` / `main.go`）找 handler 函数，追到 service / repository

### Node.js / Express 项目

- `routes/*.js` / `controllers/*.js`：`sequelize.query(string)` / 原生 `mysql.query(string)`
- `models/*.js`：含 `literal()` / `Sequelize.where(Sequelize.literal(...))`
- `package.json` 确认 sequelize / typeorm / mysql2 版本

### PHP / Laravel 等

- `app/Models/`：`DB::raw()` / `whereRaw()` / 直接 `DB::select(string)`
- `app/Http/Controllers/`：手写 SQL 拼接

### 通用建议

- 优先从路由 / Controller 找 source（用户输入入口），从 DAO / Repository / Mapper 找 sink
- 用 `sast-scan` / `dataflow-analysis` 工具加速 source-to-sink 追踪
- 注意：白盒只能审计**有源码**的部分——闭源依赖见 §11 静态分析边界

---

## 6. 跨框架代码变体

> **目的**：让本 skill 在不同框架的项目都能用。**白盒原子 skill 的复利资产**。
>
> **写法**：表格列出主流框架的"安全形态（参数化） vs 危险形态（拼接）"对照。覆盖 Spring / Django / Gin / Express 至少 4 个，外加 ORM 子方向。

**白盒 SQLi 示范**：

| 框架 | 安全形态（参数化） | 危险形态（拼接） |
|---|---|---|
| **Spring + MyBatis** | `#{var}` 占位符；`@Param("x") String x` | `${var}` 直拼；`@Select("... ${col} ...")` |
| **Spring + JdbcTemplate** | `queryForObject(String sql, Object[] args)` 占位符 `?` | `queryForObject(String sql)` 无 args 形式 |
| **Spring + JPA** | `@Query` 用 `:param` / 方法名派生 | `createNativeQuery(String)` + 字符串拼接 |
| **Django ORM** | `Model.objects.filter(field=value)` / `Q(...)` 表达式 | `Model.objects.raw(format-string)` / `.extra(where=[str])` |
| **Gin + Gorm** | `db.Where("col = ?", val)` / 结构体查询 | `db.Where("col = " + val)` / `db.Raw(fmt.Sprintf(...))` |
| **Gin + database/sql** | `db.Query("... WHERE col = ?", val)` | `db.Query("... WHERE col = " + val)` / `fmt.Sprintf` |
| **Express + Sequelize** | `Model.findAll({where: {col: val}})` | ``sequelize.query(`SELECT ... ${val}`)`` |
| **Express + TypeORM** | `repo.find({where: {col: val}})` / QueryBuilder `setParameter` | `repo.query(string with template literal)` |
| **Express + mysql2** | `conn.query("... ?", [val])` | `conn.query("... " + val)` |
| **PHP + PDO** | `prepare("... :val")` + `execute(['val' => v])` | `mysqli_query("..." . $val)` |
| **Python + SQLAlchemy** | `session.query(Model).filter(Model.col==val)` / `text("... :v").bindparams(v=val)` | `text("... " + val)` / `engine.execute(format-string)` |

**ORM 特殊危险点**（任何 ORM 都适用）：
- **字段名 / 表名注入**：ORM 一般只参数化「值」，不参数化「字段名」/「表名」/「排序方向」
- **Raw / Native 通道**：所有 ORM 都有 raw 通道——常见漏点
- **`literal()` / `unsafe()` 表达式**：Sequelize / SQLAlchemy 都有"显式跳过参数化"的表达式构造器

---

## 7. 思考检查点（共享章节）

> **目的**：给模型 3-5 条按 sink 语义（而非业务命名）思考的引导问题。
>
> **写法**：开放式问题。**与黑盒版本语义一致**——按 sink 语义思考。

**白盒 SQLi 示范**：

加载本 skill 时按这些问题思考：

- 这个参数从 HTTP handler 入口到 SQL 之间经过了哪几个函数？任一环节做了参数化绑定吗？
- 同端点其他参数是否也走同一段 SQL 模板？（同模板下其他字段往往是候选）
- 是不是 ORM？ORM 是否走 raw 通道（`raw()` / `query()` / `Raw()` / `nativeQuery()`）？
- 是不是字段名 / 表名 / 排序方向拼接？这类不能参数化，必须看白名单是否存在。
- 是不是二阶？source 是否会先入库再被另一段 SQL 回读拼接？
- 跨子系统是否有同 pattern_id 端点？（admin 端常复用普通端代码）

---

## 8. 检测方法论 / 数据流追踪

> **目的**：本 sink 怎么测的核心白盒方法论。
>
> **白盒视角**：
> - 找 source → 跨函数追数据流 → 到 sink → 判可达性 / 参数化绑定 / 白名单存在性
> - 含**反编译 / 依赖追踪**约定（闭源库怎么处理）
> - 含静态分析工具调用（sast-scan / dataflow-analysis）
> - 含基线检查项（§4.3 三态标注协议）

**白盒 SQLi 示范**：

### Step 0：基线侦察

- 加载 `project-framework-analysis` 输出的项目结构 / 依赖图谱
- 识别 web 框架（Spring / Django / Gin / Express / Laravel 等）+ ORM / SQL 库版本
- 列出本项目里所有"SQL sink 候选位置"：Mapper / Repository / DAO / 含 raw 调用的文件

### Step 1：grep 出候选 sink

```bash
# 通用拼接 pattern
rg "fmt\.Sprintf.*SELECT|fmt\.Sprintf.*WHERE|fmt\.Sprintf.*ORDER BY"
rg "\"SELECT .+\" \+|\"WHERE \" \+|\\$\\{.+SELECT"  # JS / Java template literal
# MyBatis 危险位
rg '\$\{[a-zA-Z_]+\}' --type xml
# ORM raw 通道
rg '\.raw\(|\.query\(|\.Raw\(|nativeQuery|whereRaw\('
# 字段名拼接
rg 'Order\(.+\)|order_by\(.+\)|OrderBy\(.+\)'
```

### Step 2：source 追踪

对每个 sink 候选，从函数参数倒推到 HTTP handler 入口：
1. 看 sink 所在函数的参数 → 谁调用了它？
2. 调用栈最终到 Controller / Handler 入口
3. 入口的参数是否来自用户可控位置（query / body / header / cookie / 路径参数）
4. 中间是否有过滤 / 校验 / 白名单（如 `int.Parse(...)` 类型转换 / regex 校验）

工具加速：调用 `dataflow-analysis` MCP 工具做跨函数数据流追踪。

### Step 3：判可达性 / 参数化绑定

- **直接拼接** + source 可达 → **静态可达，confirmed-static**
- **ORM Raw 通道** + source 可达 → **静态可达，confirmed-static**
- **字段名 / 排序方向位置** + 无白名单 → **静态可达，confirmed-static**
- 有参数化绑定 + 无 raw 通道 → **not-vulnerable**
- 中间过滤层未追到（动态字符串构造 / 反射调用 / 闭源依赖） → **static-unknown**（参 §11）

### Step 4：二阶通道扫描

- 找所有 INSERT / UPDATE / 写库的端点，记录写入字段
- 对每个写入字段，grep 哪些 SELECT 引用它
- 每个回读点单独看是否参数化

### 基线检查项（按 §4.3 三态标注）

- [ ] 所有 sink 候选位置都被 grep 覆盖
- [ ] 所有 source 候选位置（每个 handler / Controller）都被追踪
- [ ] ORM Raw 通道独立扫描
- [ ] 字段名 / 排序方向位置独立看白名单
- [ ] 二阶通道：所有写入点 → 所有回读点扫描完毕
- [ ] 闭源依赖范围内的可达性已标 `static-unknown` 而非默认 not_vulnerable

---

## 9. 闭环要求（必须遵守）

> **目的**：白盒判定上限 + 升级路径。**引用 `common/closure-verification.md`，不复述契约**。
>
> **白盒视角**：本能力只能到 `static-confirmed`（数据流可达性已证），**不等于动态 confirmed**；要升级到 confirmed 必须靠黑盒端验证或 graybox 流程。

**白盒 SQLi 示范**：

> 闭环判定（confirmed / suspected / not_vulnerable）以 `common/closure-verification.md` 为准。本能力作为白盒原子能力，判定上限为 `static-confirmed`，不等于动态 confirmed。

**static-confirmed（白盒上限，落 `status=needs_review`）**：
- source-to-sink 数据流静态可达
- sink 位置是字符串拼接 / Raw 通道 / 字段名拼接 / `${}` 等危险形态
- 中间无参数化绑定、无白名单、无强类型转换

**static-unknown（落 `status=needs_review` + 标注 unknown）**：
- source 经过反射调用 / 动态字符串构造 / 闭源依赖，数据流追踪到能力边界
- 不能默认为 not_vulnerable——这是白盒必须明确的诚信底线（参 §11）

**not_vulnerable（落 `status=not_vulnerable`）**：
- 数据流分析证明 source 经参数化绑定 + 无 raw 通道 + 无字段名拼接候选
- 端点不接触 SQL（静态资源 / 健康检查）

**升级路径**（白盒不能独立给 confirmed）：
- 走 graybox 流程：用白盒候选指导黑盒端测试
- 黑盒端按 `pentest/sql-injection-comprehensive` 等原子 skill 的闭环要求收可观测效果证据
- 黑盒拿到强证据后，结论从 `static-confirmed` 升为 `confirmed`

**禁止**白盒独立判 `confirmed`——无可观测效果证据，仅静态可达不构成动态利用。

### 反例义务（必须遵守）

> **why**：白盒"已防护"结论是覆盖完整性产物声明，缺失反向验证会让下游误信"该子系统该维度安全"。

写"未发现 SQLi"或"已防护"前，产物必须包含：
- 所有 sink 候选位置完整清单（grep 覆盖证据）
- 所有 source 候选位置完整清单（每个 handler / Controller 都有追踪结论）
- 每个 source-sink 对的判定结果（参数化 / 拼接 / unknown）
- `static-unknown` 单元格的具体原因（反射 / 闭源 / 动态构造）

清单不完整 → 结论降级为 `partial-coverage`。

---

## 10. 具象化反例库（共享章节）

> **目的**：把抽象误判规则落到可识别的具体场景。
>
> **写法**：每条反例按四步：抽象规则 → 具体场景 → 关键识别特征 → 如何排除/确认。

**白盒 SQLi 示范**：

### FP（看似命中实际不构成）

**反例 1：ORM `.findAll({where: {col: userInput}})` 不构成注入**

- 抽象规则：ORM 通过表达式对象传值 ≠ 字符串拼接
- 具体场景：`Model.findAll({ where: { name: req.query.name } })` 形态
- 关键识别特征：where 接收 plain object 而非 template literal；不出现 `${}` / `+` 拼接
- 排除方法：grep 该 Model 是否同时有 `sequelize.query` / `.literal(` / `Sequelize.where(Sequelize.literal(...))` 使用

**反例 2：中间过滤层未追到**

- 抽象规则：grep 命中 raw 但实际有过滤层
- 具体场景：`db.Raw(buildSafeQuery(input))` 形态——拼接发生在 `buildSafeQuery` 内但该函数做了白名单
- 关键识别特征：sink 位置接收的是函数调用结果而非直接 source
- 排除方法：追到 `buildSafeQuery` 实现看是否有白名单 / 参数化

**反例 3：框架自动转义**

- 抽象规则：某些框架版本对 raw 通道做了自动转义
- 具体场景：Spring JdbcTemplate 某些封装方法自带预编译；TypeORM `repo.query` 在新版本有参数化形式
- 关键识别特征：调用形式带占位符（`$1` / `?`）+ 单独传 args
- 排除方法：核对框架文档版本对应的 raw 通道签名

### FN（看似不命中实际是真洞）

**反例 4：字段名拼接（无引号不报错）**

- 抽象规则：`ORDER BY` 后接的字段名不能用引号包裹，参数化对它无效
- 具体场景：`?sort=created_at` → `db.Order(sort).Find(...)` 直接把字符串拼到 ORDER BY
- 关键识别特征：grep `Order(` / `order_by(` / `OrderBy(` 接收的参数是否直接来自请求；无白名单
- 确认方法：追到调用点看上游是否有白名单 enum

**反例 5：二阶注入（入库时不报错，回读时触发）**

- 抽象规则：source 入库走参数化不报错，不代表回读拼接安全
- 具体场景：注册时邮箱字段含 `'`，入库正常；后台 admin 页面以邮箱查日志，回读时拼接进新查询
- 关键识别特征：「同一字段在 2 个 SQL 上下文里出现」+「其中至少 1 个用拼接」
- 确认方法：追入库字段被哪些回读端点消费，每个回读点都看是否参数化

### 易混淆案例

**反例 6：闭源依赖里的 sink 默默被忽略**

- 抽象规则：依赖库内部有 sink 但开发者只看自家代码
- 具体场景：项目调用 `external-lib.queryUser(name)` 没问题，但 external-lib 内部用拼接
- 关键识别特征：调用点看起来安全，但下游闭源（依赖图谱里的 `unknown` 标注）
- 排除方法：标 `static-unknown` 推 dependency-decompile 做反编译；不能默认为 not_vulnerable

---

## 11. 静态分析边界

> **目的**：白盒能力的边界——什么场景下数据流追不到，必须标 unknown。
>
> **白盒底线**：**不假装看到看不到的代码**。

**白盒 SQLi 示范**：

### 数据流追踪到边界的情形

下面这些情形数据流分析无法继续追踪，**必须标 `static-unknown`**，不允许默认为 not_vulnerable：

1. **反射调用 / 动态方法分派**
   - Java：`Method.invoke()` / 通过字符串决定调用哪个 DAO 方法
   - Python：`getattr(obj, method_name)(args)` / `eval()` / `exec()`
   - JS：`obj[methodName]()` / `new Function(...)`
   - **处置**：标 `static-unknown`，记录"反射点"行号

2. **闭源 / 无源码依赖**
   - 三方依赖（jar / dll / so / 闭源 SaaS SDK）
   - 项目用了这些依赖但能力链路涉及它
   - **处置**：依赖图谱标 `unknown`，推 `dependency-decompile` 做反编译；不能直接 not_vulnerable

3. **动态字符串构造**
   - 配置文件驱动的 SQL：`config.get("query.user")` 然后执行
   - 代码生成器（mybatis-generator / sequelize-cli）生成的 SQL
   - **处置**：标 `static-unknown`，记录配置位置；如有必要去读取实际配置文件验证

4. **跨服务 / 跨进程边界**
   - RPC 调用（gRPC / Thrift / REST）跨微服务
   - 消息队列（Kafka / RabbitMQ）异步消费
   - **处置**：本服务范围内的 source 追踪到出站调用即停；下游服务由对应 skill 单独审计

5. **框架自动注入 / AOP**
   - Spring `@Aspect` 拦截器在 sink 调用前后插入逻辑
   - Django middleware 修改 request
   - **处置**：列出所有拦截器 / middleware 的实现，独立确认是否引入过滤

6. **运行时配置切换**
   - dev / prod 不同分支用不同 SQL 模板
   - feature flag 控制查询路径
   - **处置**：每个分支独立审计；不能只看 dev 分支下结论

**底线**：白盒 SKILL 写"该子系统无 SQLi"前，所有 unknown 单元格必须显式列出原因。否则结论降级为 `partial-coverage`。

---

## 12. 修复建议（共享章节）

> **目的**：给出标准修复路径。**与黑盒版本语义一致**——修复方法对黑白盒读者都相同。

**白盒 SQLi 示范**：

### 源头治理（首选）

- 全部走参数化 / 占位符：JDBC / PDO / Go database/sql 用 `?` / `$N` + 单独传 args；MyBatis 用 `#{}`；ORM 用表达式对象 `where: {col: val}`
- 字段名 / 表名 / ORDER BY 子句：把允许值 enum 化做白名单

```go
allowedSortColumns := map[string]struct{}{"created_at": {}, "id": {}, "name": {}}
if _, ok := allowedSortColumns[sortCol]; !ok {
    sortCol = "created_at" // 或返回 400
}
db.Order(sortCol).Find(&items)
```

### 二阶注入

- **入库即参数化**：防一阶
- **回读必参数化**：任何 source 字段被回读到新 SQL 上下文时，按 source 重新参数化绑定

### 边界过滤（次选，深度防御）

- WAF 规则覆盖常见 payload——**仅作辅助**，不替代参数化
- 应用层拒绝单引号 / 特殊字符——**易绕过**

### 兜底拒绝

- 数据库最小权限：业务账号禁止 DDL / 跨库 / `FILE` 权限
- 错误响应不暴露 SQL 语法信息

### 参考

- [OWASP SQL Injection Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html)

---

## 改造现有 skill 的步骤

1. **复制本模板**：`cp skills/CODE_AUDIT_SKILL_TEMPLATE.md skills/code-audit/<skill-name>/SKILL.md`
2. **填 frontmatter**：name = 目录名；description 写"做什么 + 触发线索"
3. **逐段填空**：12 段全部覆盖。每段开头的 `> 目的 / 写法` 注释在最终版**删除**
4. **删示范段**：把所有"**白盒 SQLi 示范**："开头的段落替换为本能力 / 漏洞内容
5. **共享段（§2 / §4 / §7 / §10 / §12）**：直接写本漏洞 / 能力语义
6. **自检**：按 `SKILL_SPEC.md` §10「code-audit/（白盒）特有项」过一遍
7. **解析验证**：`cd /Users/qinchenkai/go/sastx && go test ./skills/... -run SkillExtractor`
8. **触发验证**：在 aster 里 `list_skills` 看 description 是否独立描述清晰

---

## 相关文件

- `SKILL_SPEC.md` §6.2 — 白盒骨架权威规范
- `SKILL_SPEC.md` §6.3 — 共享章节定义
- `PENTEST_SKILL_TEMPLATE.md` — 黑盒原子 skill 模板（**禁止跨方向复制**）
- `common/closure-verification.md` — 闭环 / 静态可达性契约
- `common/web-vuln-cause-map.md` — 漏洞成因图谱（领域参考）
- `code-audit/sast-scan/SKILL.md` / `code-audit/dataflow-analysis/SKILL.md` — 白盒通用工具（不替代具体 sink 审计）
- `internal/tui/config.go` `defaultAgentFiles["code-audit.yaml"]` — 改造完成后把 skill name 加入 `skill_names`
