# Web 漏洞成因图谱（共享领域知识）

> 本文件是 web 安全漏洞的**领域知识参考**，按 source-sink 框架统一表达，覆盖六大分组。
> v3 重构前曾驻留 `pentest/web-security-testing/references/vulnerability-cause-map.md`（作为父 skill 的「静态分诊路由表」）。
> v3 重构后父 skill 形态淘汰，本图谱迁移至 `common/`，**仅作 agent profile 按需 `read_file` 读取的领域参考**，不再充当任何 SKILL.md 的"成因引用源"——每个原子 skill 在自己的 `## 2. 造成原因` 段独立写成因。
>
> 主要消费者：
> - `pentest.yaml` / `graybox-test.yaml` agent profile 在漏洞展开阶段 `read_file` 读取，作为"按 sink 语义找候选维度"的领域索引
> - 新增原子 skill 时，参考本图谱看漏洞类是否已被现有 skill 覆盖，决定是否新建
> - `skillPath` 列只作"漏洞类 → 哪个原子 skill 在管它"的索引，非加载路由

成因图谱用 source-sink 框架统一表达（用户可控输入流向危险 sink 即构成候选漏洞，按 sink 语义判定，不按业务命名）。`skillPath` 标 `(覆盖缺口)` 的为当前未覆盖、建议下一轮迭代新增 skill 的漏洞类——**这本身就是一份 skill 覆盖度对账表**。

## 注入与渲染类

| 漏洞类 | source | sink | 成因（业务命名不可作筛选） | skillPath |
|---|---|---|---|---|
| SQLi | 用户可控输入 | SQL 上下文（WHERE / LIKE / ORDER BY / 字符串拼接进任何 SQL 查询） | 输入未参数化即拼接进 SQL，无论业务命名是登录账号、搜索 keyword、排序字段、ID 路径参数还是 JSON body | `pentest/sql-injection-comprehensive/SKILL.md` |
| XSS | 用户可控输入 | HTML 渲染上下文（template.HTML 直渲、innerHTML 赋值、属性/JS/CSS/URL 上下文） | 输入未编码即输出到 HTML 渲染上下文，无论业务命名是评论、文章正文、广告创意、消息、个人简介 | `pentest/xss-testing/SKILL.md` |
| SSRF | 用户可控输入 | URL 发起请求（http.Get / 任何服务端 HTTP / file:// 协议） | 输入决定服务端要访问的资源，无白名单或协议限制，无论业务命名是 webhook、callback、预览、头像导入、OAuth callback、JSONP | `pentest/ssrf-testing/SKILL.md` |
| SSTI | 用户可控输入 | 模板解析（html/template.Parse、报表生成、邮件模板、广告创意模板） | 输入被作为模板源码而非模板数据解析 | `pentest/ssti-testing/SKILL.md` |
| XXE | 用户可控输入 | XML 解析器（开启外部实体） | XML 输入被解析时允许外部实体引用 | `pentest/xxe-testing/SKILL.md` |
| 命令注入 | 用户可控输入 | shell / exec / os.Command | 输入被拼接进 shell 命令而非作为参数传递 | `pentest/command-injection/SKILL.md` |
| NoSQL / LDAP / 表达式注入 | 用户可控输入 | 对应解析器（Mongo query / LDAP filter / 表达式引擎如 OGNL/SpEL） | 同 SQLi 范式但 sink 不是 SQL | `(覆盖缺口，建议新增 pentest/nosql-ldap-expr-injection/SKILL.md)` |
| 不安全反序列化 | 用户可控的序列化数据 | 反序列化器（Java ObjectInputStream / PHP unserialize / Python pickle / .NET BinaryFormatter） | 反序列化触发 gadget chain 执行 | `(覆盖缺口，建议新增 pentest/insecure-deserialization/SKILL.md)` |
| 原型链污染 | 用户可控的 JSON 字段名 | 对象合并/递归赋值（lodash.merge / Object.assign 深拷贝） | `__proto__` / `constructor.prototype` 链路污染全局对象原型 | `(覆盖缺口，建议新增 pentest/prototype-pollution/SKILL.md)` |

## 路径与文件类

| 漏洞类 | source | sink | 成因 | skillPath |
|---|---|---|---|---|
| 路径穿越 / LFI | 用户可控输入 | 文件读取决策（os.Open、filepath.Join、include） | 输入决定要打开/读取哪个文件，无路径规范化或白名单。**不限于** `/static/*` FileServer，业务接口的 `?file=` 同属此类 | `pentest/path-traversal-lfi/SKILL.md` |
| 文件上传 | 用户上传文件 | 文件存储 / 后续执行解析 | 类型/魔术数/扩展名/路径/解析器校验缺失，可写入可执行内容 | `pentest/file-upload/SKILL.md` |

## 认证、会话、凭证类

| 漏洞类 | source | sink | 成因 | skillPath |
|---|---|---|---|---|
| 越权 IDOR（水平） | 用户可控资源 ID | 资源访问决策 | 资源访问未校验所有权 | `pentest/idor-detection/SKILL.md` |
| 垂直越权 | 用户角色字段 / 角色 cookie | 权限校验逻辑 | 多角色系统权限边界缺失，普通用户可执行管理操作 | `pentest/vertical-privilege-escalation/SKILL.md` |
| 未授权访问 | 任何请求 | 受保护端点 | 端点漏挂认证中间件 / 路由配置错误 | `pentest/unauthorized-access/SKILL.md` |
| 认证综合缺陷 | 登录请求 | 认证流程 | 弱口令 / 暴破无锁定 / 无 CAPTCHA / 密码明文存储 | `pentest/auth-comprehensive/SKILL.md` |
| JWT 弱密钥 / 算法降级 | JWT Token | JWT 验证 | HMAC 密钥可爆破 / 接受 alg=none / 接受公钥作 HMAC 密钥 | `pentest/jwt-weakness/SKILL.md` |
| API Key / Custom Token 弱认证 | 自定义凭证字符串 | 服务端凭证验证 | 凭证空间可枚举/低熵/顺序生成，无速率限制。**不限于** JWT，自定义 Header / Query / Cookie / Path Token 同属此类 | `(覆盖缺口，建议新增 pentest/api-token-sec/SKILL.md)` |
| 会话固定 Session Fixation | 攻击者预设的 Session ID | 登录后服务端是否轮换 Session ID | 登录后不轮换 Session ID，预设 ID 可继承登录态 | `(覆盖缺口，建议新增 pentest/session-management/SKILL.md)` |
| 会话管理缺陷 | Cookie 属性 / 会话生命周期 | 浏览器 / 服务端会话保护 | Cookie 缺 HttpOnly/Secure/SameSite、登出无效化、超时无效、跨子系统会话复用 | `(覆盖缺口，建议新增 pentest/session-management/SKILL.md)` |

## 跨域、跳转、点击劫持类

| 漏洞类 | source | sink | 成因 | skillPath |
|---|---|---|---|---|
| CSRF | 跨站请求（受害者已登录） | 状态变更端点 | 端点接受不经 token / origin 校验的写操作；同站不同端口 SameSite=Lax 不阻止 POST | `pentest/csrf-testing/SKILL.md` |
| CORS 配置错误 | 攻击者控制的 Origin 头 | CORS 响应头 | 反射任意 Origin + Allow-Credentials=true，导致跨域读取受保护数据 | `pentest/cors-misconfiguration/SKILL.md` |
| Open Redirect | url / next / return 参数 | Location 头 / meta refresh / JS 跳转 | next/url 参数无白名单，可重定向到任意外站 | `pentest/open-redirect-testing/SKILL.md` |
| Clickjacking 点击劫持 | 攻击者构造的 iframe 嵌入 | 受害页面 | 缺 X-Frame-Options / CSP frame-ancestors，敏感操作可被诱导点击 | `(覆盖缺口，建议新增 pentest/clickjacking/SKILL.md)` |

## HTTP 协议、配置、加密类

| 漏洞类 | source | sink | 成因 | skillPath |
|---|---|---|---|---|
| 安全响应头缺失 | 全站响应 | 浏览器安全策略 | 缺 CSP / HSTS / X-Content-Type-Options / Referrer-Policy / Permissions-Policy | `code-audit/security-header-audit/SKILL.md`（白盒）/ `(覆盖缺口，建议新增 pentest/security-headers/SKILL.md)`（黑盒） |
| HTTP Host 头注入 | Host / X-Forwarded-Host 头 | 后端构造的 URL（密码重置链接 / 资源引用 / 缓存键） | 后端信任 Host 头构造外发链接，污染密码重置邮件等 | `(覆盖缺口，建议新增 pentest/host-header-injection/SKILL.md)` |
| HTTP 请求走私 | 前端代理与后端解析差异（CL.TE / TE.CL / TE.TE） | 后端请求队列 | Content-Length 与 Transfer-Encoding 解析不一致，可注入伪请求 | `(覆盖缺口，建议新增 pentest/http-smuggling/SKILL.md)` |
| CRLF 注入 / 响应拆分 | URL / 头部参数 | 响应头构造 | 输入含 `%0d%0a` 即可插入新响应头或拆分响应 | `(覆盖缺口，部分在 open-redirect-testing，建议独立)` |
| 弱加密 / 加密误用 | 明文 / 密钥 / IV | 加密原语 | MD5/SHA1/DES/RC4 / ECB 模式 / IV 复用 / 硬编码密钥 / 自实现加密 / 随机数不密码学安全 | `(覆盖缺口，建议新增 pentest/weak-crypto/SKILL.md)` |
| TLS 配置缺陷 | TLS 握手 | TLS 服务端配置 | 接受过期协议（SSLv3/TLS1.0）/ 弱套件 / 自签证书 / Heartbleed 类已知 CVE | `(覆盖缺口，建议新增 pentest/tls-misconfiguration/SKILL.md)` |

## 业务逻辑、滥用、信息泄露类

| 漏洞类 | source | sink | 成因 | skillPath |
|---|---|---|---|---|
| 业务逻辑（价格 / 数量 / 折扣 / 状态机） | 用户可控的业务字段 | 业务计算 / 状态机判定 | 客户端可控金额 / 数量负数 / 优惠券叠加 / 状态机倒退 / 退款无校验 | `(覆盖缺口，曾在 business-logic-testing，已删除；建议按 IDOR / 越权 / race 等具体方向 + page-model 提取信号驱动)` |
| 竞态条件 / TOCTOU | 并发请求 | 共享资源（库存 / 余额 / 限量优惠） | 检查-修改非原子，多并发请求都通过检查后才写入 | `pentest/race-condition/SKILL.md` |
| 通知滥用 | 注册 / 发送 / 触发请求 | 短信 / 邮件 / IM 触达 | 无速率限制 / 无验证码，可批量轰炸 | `pentest/notification-abuse/SKILL.md` |
| 注册滥用 | 注册请求 | 用户创建 | 无速率限制 / 无唯一约束 / 无验证码，可批量创建账号 | `pentest/registration-abuse/SKILL.md` |
| 整数 / 缓冲区溢出 | 用户可控的数值 / 长度字段 | native 代码 / cgo / protobuf 解析 | 数值超出范围导致环绕 / 缓冲区越界（web 应用多见于 native 扩展、protobuf 边界、文件解析器） | `(覆盖缺口，视项目栈决定是否新增)` |
| ReDoS（正则拒绝服务） | 用户可控的字符串 | 正则表达式引擎 | 灾难性回溯正则 `(a+)+$` + 攻击者构造的恶意输入导致 CPU 飙升 | `(覆盖缺口，建议新增 pentest/redos/SKILL.md)` |
| 敏感信息泄露 | 任何请求 | API / 页面响应 / 错误信息 / 日志 | PII / 凭证 / 内部地址 / SQL 错误原文明文返回，无脱敏 | `pentest/sensitive-info-exposure/SKILL.md` |

---

## 使用约定（v3 重构后）

- **本图谱仅作领域参考**，不充当 SKILL.md 之间的"成因引用源"。每个原子 skill 在自己的 `## 2. 造成原因` 段独立写完整成因（参 `SKILL_SPEC.md` §6.1）。
- agent profile 在「漏洞展开阶段」可 `read_file common/web-vuln-cause-map.md` 取得"按 sink 语义找候选维度"的领域索引，但**不**以本图谱作为静态路由表——`list_skills` 的描述驱动发现优先。
- `skillPath` 标 `(覆盖缺口)` 的漏洞类，仅作"建议新增 skill"的对账提醒。新增 skill 时按 `SKILL_TEMPLATE.md` 12 段骨架填写，新建后回填本表 `skillPath` 列。
- 新增漏洞类时，按「注入与渲染 / 路径与文件 / 认证会话凭证 / 跨域跳转点击劫持 / HTTP 协议配置加密 / 业务逻辑滥用信息泄露」六分组就近放入。

## 覆盖声明对账（agent profile 收敛阶段使用）

agent profile 在「收敛对账」阶段按 `(子系统 × 漏洞类)` 矩阵逐单元格标注下列状态：

| 状态 | 含义 |
|---|---|
| `tested-vulnerable` | 已测且命中漏洞 |
| `tested-safe-with-evidence` | 已测且有反例义务清单证明安全——必须挂"测过的端点完整清单 / 每端点 payload 形态 / 每端点响应证据"三项 |
| `n/a-with-reason` | 不适用，原因要具体到事实，不允许笼统写"不适用" |
| `added-from-outside-map (来源)` | 图谱外的真实发现，含来源（HAR / 代码 / 页面结构 / 跨子系统类比迁移）——第四态，承接基线之外的实际命中 |

**禁止"未测试默认安全"**——每个空白单元格在终态提交前要么有上述四态之一结案，要么显式登记为"未结案悬挂"，不能默默缺省。

**多子系统场景独立结账**——每个子系统的覆盖按 `(子系统 × 漏洞类)` 矩阵独立结账。跨子系统的隐式推广（"在 A 测过 B 也安全"）不接受作为对账依据。

---

## 相关文件

- `SKILL_SPEC.md` §6 — 原子 skill 12 段骨架（v3 强制）
- `SKILL_TEMPLATE.md` — 新增原子 skill 时的填空模板
- `common/closure-verification.md` — 闭环验证 / 破坏性动作 / 取证完整性契约
- `internal/tui/config.go` `defaultAgentFiles` — agent profile 层（pentest.yaml / code-audit.yaml / graybox-test.yaml 在漏洞展开阶段读取本图谱）
