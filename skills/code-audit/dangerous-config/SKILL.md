---
name: dangerous-config
description: >-
  危险配置静态值审计——对照最小权限基线检查配置文件字面量，覆盖调试模式、默认凭据、过宽 CORS、
  弱 TLS、上传限制缺失、错误页堆栈泄露、Actuator/Admin 端点未鉴权、反序列化危险默认、XML 外部实体、
  WebSocket 跨源、Cookie 加密 Key 默认值、日志敏感字段、限流/WAF 默认关闭等多种配置类目。项目含
  `application*.yml` / `*.properties` / `web.xml` / `nginx.conf` / `apache.conf` / `Caddyfile` /
  Spring `@ConfigurationProperties` / Django `settings.py` / `config/*.json` / `.env` /
  `docker-compose*.yml` / `k8s/*.yaml` / Tomcat `server.xml` / Spring Boot Actuator 依赖、或出现
  CORS / TLS / 调试 / 上传 / 日志相关配置项时使用。本能力只覆盖**配置项静态值**——硬编码凭据 /
  密钥归 [secret-detection](../secret-detection/SKILL.md)；HTTP 响应头归
  [security-header-audit](../security-header-audit/SKILL.md)。
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
---

# 危险配置静态值审计

## 1. 触发线索 / 适用信号

按 **配置介质 + 框架特征 + 依赖**三维识别本能力命中场景。**白盒视角**——按配置文件类型 / 注解 / 依赖签名分类，不讨论流量 / HAR / 响应特征。

**配置介质维度**（grep / list_files 命中）：
- Spring 配置：`application.yml` / `application-*.yml` / `application.properties` / `bootstrap.yml`
- Java EE 配置：`web.xml` / `server.xml`（Tomcat）/ `context.xml`
- Web 服务器配置：`nginx.conf` / `apache.conf` / `Caddyfile`
- 通用配置：`config/*.json` / `config/*.yaml` / `*.toml` / `*.ini`
- 环境变量：`.env` / `.env.*` / `Dockerfile` 的 `ENV` 指令
- 容器编排：`docker-compose*.yml` / `k8s/*.yaml` / `helm/values.yaml` / `ConfigMap`
- Python / Django：`settings.py` / `local_settings.py` / `config.py`（FastAPI）
- Node：`config/*.json` / 自定义 `config.js`
- PHP / Laravel：`config/*.php` / `.env`

**框架特征 / 注解维度**：
- Spring `@ConfigurationProperties` / `@Value("${...}")` 注解
- Spring Boot Actuator 引入（`spring-boot-starter-actuator`）
- Spring DevTools（`spring-boot-devtools`）
- Jackson 用 `enableDefaultTyping` / `activateDefaultTyping`
- XML 解析器（`DocumentBuilderFactory` / `SAXParserFactory`）未显式禁用 DOCTYPE

**依赖维度**：
- `pom.xml` 含 Spring Boot Actuator / DevTools / H2 Console
- `package.json` 含 `cors` / `helmet` / `express-session`
- `requirements.txt` 含 `django` / `fastapi` / `flask`
- `go.mod` 含 `gin-contrib/cors` / 配置驱动的 TLS 库

**反向信号**（不命中本能力）：
- 纯静态资源站点 / CDN 项目（无可配置后端）
- 已知配置由运行时环境变量完全注入且无任何静态默认值（这类只能黑盒验证）
- 用户明确只关心硬编码密钥（直接走 [secret-detection](../secret-detection/SKILL.md)）

---

## 2. 造成原因

配置项的"默认值"通常倾向**便于开发**而非**生产安全**——框架为降低上手门槛，把 debug / verbose / 全开式 CORS / 默认凭据等设为开箱即用，期望开发者上线时手工收敛。一旦上线流程缺少"配置 lint / 最小权限基线对照 / profile 严格隔离"环节，开发态默认值就会原样进入生产环境，每一项都对应一类直接可用的攻击面：

- 调试 / 堆栈泄露 → 攻击者通过错误页拿到框架版本、内部路径、SQL 片段，加速指纹与漏洞匹配
- 过宽 CORS → 跨源页面可读取受害用户的认证态响应，扩散为账号接管
- 弱 TLS / 弱密码套件 → 中间人可降级解密 / 注入
- Actuator / Admin 端点未鉴权 → 内部健康检查、堆栈、env、heapdump 直接对外暴露
- 默认凭据（admin/admin） → 无需漏洞即可直接登录
- 反序列化危险默认（Jackson default typing / XML 实体未禁） → 一旦反序列化用户输入即升级到 RCE
- 上传限制缺失 / 日志含敏感字段 / 限流关闭 → 单独不是 RCE 但会形成放大效应

**核心成因不在某一项配置错误，而在"基线对照缺失"** —— 没有一份"生产环境每项配置应取值"的清单去逐项核对。本能力的工作即是把这份清单与项目实际配置对齐。

---

## 3. 领域 source-sink 数据流模型

**本能力 n/a**（原因：配置项静态值审计，看配置字面量与最小权限基线对照；无 source-sink 数据流——配置项不是"用户可控输入流向危险函数"的形态，而是"项目配置值与安全基线值的偏差"。与配置消费代码的 source-sink 追踪（如读取了不安全配置后被消费到哪个 sink）由 [dataflow-analysis](../dataflow-analysis/SKILL.md) 接力）。

---

## 4. 常见类型

本能力覆盖的配置维度（按已知主流覆盖，不追求穷举）：

| 配置维度 | 危险默认值 | 安全配置值 | 主要影响 |
|---|---|---|---|
| 调试模式 | Django `DEBUG=True` / Flask `debug=True` / Spring `server.error.include-stacktrace=always` | `False` / `never` / `on_trace_param` | 堆栈泄露 / SQL 片段泄露 |
| 默认凭据 | `admin/admin` / `root/root` / `password/password` | 强随机生成 + 首次登录强制改 | 直接登录 |
| 过宽 CORS | `Access-Control-Allow-Origin: *` / `null` / 反射任意 Origin | 明确域名 allowlist | 跨源数据读取 |
| 弱 TLS | 启用 SSLv3 / TLS 1.0 / TLS 1.1 / RC4 / 3DES 套件 | TLS 1.2+ 且仅 AEAD 套件 | 中间人降级 |
| 上传限制缺失 | 无大小上限 / 无类型校验 / 无路径白名单 | size + MIME + 后缀 + 路径三重约束 | 资源耗尽 / 落 webshell |
| 错误页堆栈泄露 | Spring `server.error.include-stacktrace=always` / Django `DEBUG=True` 错误页 | 通用错误页 + 服务端日志 | 内部细节泄露 |
| Actuator / Admin 未鉴权 | Spring `management.endpoints.web.exposure.include=*` 无 Security | `=health,info` + Security 强绑定 | env / heapdump / 内存数据外泄 |
| 反序列化危险默认 | Jackson `enableDefaultTyping` / `ObjectInputStream` 无白名单 | 禁用 default typing / 显式类型白名单 | RCE |
| XML 外部实体未禁 | `DocumentBuilderFactory` 默认配置 | `setFeature("...disallow-doctype-decl", true)` + 禁用外部实体 | XXE → SSRF / 文件读 |
| WebSocket 跨源默认允许 | Spring `setAllowedOrigins("*")` / Node `ws` 不校验 origin | origin allowlist | 跨源 WS 劫持 |
| Cookie 加密 Key 默认值 | Laravel `APP_KEY` 未生成 / Express `secret: 'keyboard cat'` | 强随机 + Vault 注入 | Session 伪造 |
| 日志含敏感字段 | `password` / `token` / `身份证` 字段未屏蔽 | 字段脱敏 / 结构化日志过滤 | 日志合规 / 凭据外泄 |
| 限流 / WAF 默认关闭 | nginx `limit_req` 未配 / WAF 仅 detect 模式 | 入口处 rate limit + WAF block 模式 | 暴力破解 / 资源耗尽 |

注：本表不是穷举清单——配置维度会随框架版本演化；以项目实际配置介质里出现的项为准，按 §8 基线检查项三态标注处置。

---

## 5. 入口点定位

按项目结构定位**配置文件 + 配置消费代码**——两者必须配对看，单看配置文件会漏"运行时被环境变量 override"，单看消费代码会漏"配置文件里的默认值已经不安全"。

> 下列框架 / 项目类型仅作类似项目示例 不限于此；以目标实际栈为准。

### Java / Spring 项目

- **配置文件**：`src/main/resources/application.yml` / `application-{profile}.yml` / `application.properties` / `bootstrap.yml`
- **profile-specific 覆盖**：先看 `spring.profiles.active` 由谁决定（环境变量 / 启动参数）；再对每个 profile 独立审一遍
- **配置消费代码**：`@Value("${...}")` 注入点 / `@ConfigurationProperties(prefix = "...")` 类
- **Actuator**：`management.*` 配置 + Spring Security 是否配套拦截 `/actuator/**`
- **Tomcat 配置**：`conf/server.xml` 的 `<Connector>` SSL 配置 / `web.xml` 的 `<security-constraint>`
- **依赖识别**：`pom.xml` / `build.gradle` 是否引入 Actuator / DevTools / H2 Console

### Python / Django 项目

- `settings.py` / `local_settings.py` 关键字段：`DEBUG` / `ALLOWED_HOSTS` / `SECRET_KEY` / `CORS_*` / `SESSION_COOKIE_*` / `CSRF_COOKIE_*` / `SECURE_*`
- FastAPI / Flask：`config.py` / `app.config.from_object(...)`
- `requirements.txt` 看 django-cors-headers / django-csp 版本

### Node / Express 项目

- `config/*.json` / `config/*.js`（node-config 约定）
- 自定义 `app.use(cors())` / `app.use(helmet())` 实参
- `package.json` 看 cors / helmet / express-session 版本

### PHP / Laravel 项目

- `config/*.php` 全套（`app.php` / `session.php` / `cors.php` / `database.php`）
- `.env` 看 `APP_DEBUG` / `APP_ENV` / `APP_KEY`

### 服务器层配置

- `nginx.conf` / `sites-available/*` / `sites-enabled/*` —— TLS / CORS / 限流 / 错误页
- `apache.conf` / `httpd.conf` / `.htaccess` —— TLS / Directory 权限 / TraceEnable
- `Caddyfile` —— TLS / 反向代理 header

### 容器 / 编排层

- `Dockerfile` 的 `ENV` 指令（可能 override 镜像内默认配置）
- `docker-compose*.yml` 的 `environment:` 段
- `k8s/*.yaml` 的 `ConfigMap` / `Secret` / `env:` 段
- Helm `values.yaml` —— profile 切换源头

### CI / Pipeline 配置

- `.github/workflows/*.yml` / `.gitlab-ci.yml` / Jenkinsfile —— 部署阶段是否真把 `prod` profile 注入

### 通用建议

- 优先按"配置介质类型"扫一遍，再按"配置消费代码"反推哪些配置被实际使用
- 优先级排序：`application-prod.yml` > `application.yml` > 容器 ENV > 启动参数；遇到多源同名配置时按优先级取最终值
- 注意：白盒只能审计**有源码 / 有配置文件**的部分——运行时由 Vault / Feature Flag 注入的值见 §11 静态分析边界

---

## 6. 跨框架代码变体

本节列出主流框架下"安全配置 vs 危险配置"对照——同一语义在不同框架里写法差异巨大，本表是白盒原子 skill 的复利资产。

| 配置维度 | 框架 | 安全形态 | 危险形态 |
|---|---|---|---|
| 错误堆栈 | Spring Boot | `server.error.include-stacktrace: never` | `server.error.include-stacktrace: always` |
| 错误堆栈 | Django | `DEBUG = False` + `ADMINS = [...]` | `DEBUG = True` |
| 错误堆栈 | Express | `app.use((err,req,res,next) => res.status(500).send('error'))` | 默认堆栈输出 / `errorhandler()` 中间件 |
| Actuator 端点 | Spring Boot | `management.endpoints.web.exposure.include: health,info` + Spring Security 拦截 `/actuator/**` | `management.endpoints.web.exposure.include: '*'` 无 Security |
| 允许 Host | Django | `ALLOWED_HOSTS = ['api.example.com']` | `ALLOWED_HOSTS = ['*']` |
| CORS | Express | `cors({ origin: ['https://app.example.com'], credentials: true })` | `app.use(cors())` 默认全开 / `origin: '*' + credentials: true` |
| CORS | Spring | `CorsConfiguration` 显式 allowlist + `setAllowCredentials(false)` 或精确域 | `addAllowedOriginPattern("*")` + `setAllowCredentials(true)` |
| CORS | Django | `CORS_ALLOWED_ORIGINS = [...]` | `CORS_ALLOW_ALL_ORIGINS = True` |
| TLS 协议 | Tomcat | `<Connector SSLEnabled="true" sslEnabledProtocols="TLSv1.2,TLSv1.3" ...>` | 默认支持 TLSv1.0 / TLSv1.1 |
| TLS 协议 | nginx | `ssl_protocols TLSv1.2 TLSv1.3;` + `ssl_ciphers` 仅 AEAD | `ssl_protocols SSLv3 TLSv1 TLSv1.1 TLSv1.2;` |
| Session Cookie | Django | `SESSION_COOKIE_SECURE = True` + `HTTPONLY = True` + `SAMESITE = 'Strict'` | 全 False / 未设 |
| Session Cookie | Express | `session({ cookie: { secure: true, httpOnly: true, sameSite: 'strict' } })` | 默认或 `secret: 'keyboard cat'` |
| Cookie Key | Laravel | `php artisan key:generate` 生成强随机 `APP_KEY` | `.env` 模板默认空值 |
| Jackson 反序列化 | Spring + Jackson | `objectMapper.deactivateDefaultTyping()` + 类型白名单 | `objectMapper.enableDefaultTyping()` / `activateDefaultTyping(LaissezFaireSubTypeValidator.instance, ...)` |
| XML 解析 | Java | `factory.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)` + `setXIncludeAware(false)` | 默认（DOCTYPE 启用） |
| XML 解析 | Python | `defusedxml.ElementTree` | `xml.etree.ElementTree` 默认 |
| WebSocket Origin | Spring | `setAllowedOrigins("https://app.example.com")` | `setAllowedOrigins("*")` |
| WebSocket Origin | Node `ws` | `verifyClient: (info) => allowlist.includes(info.origin)` | 不校验 |
| 默认凭据 | 各类后台 | 强制首次启动改密 + 强度策略 | `admin/admin` / `root/root` / 文档示例值未替换 |
| 限流 | nginx | `limit_req_zone $binary_remote_addr zone=one:10m rate=10r/s;` + `limit_req zone=one burst=20;` | 未配 |
| 上传大小 | Spring Boot | `spring.servlet.multipart.max-file-size: 10MB` | `unlimited` / 未设 |
| 上传大小 | nginx | `client_max_body_size 10m;` | 默认 1m 但被业务覆盖为大值 |
| H2 Console | Spring Boot | `spring.h2.console.enabled: false` | `true`（生产暴露） |
| DevTools | Spring Boot | 生产构建排除 `spring-boot-devtools` | 生产依赖里仍含 |

---

## 7. 思考检查点

加载本 skill 时按这些问题思考（按 sink 语义、不按业务命名）：

- 这个配置项在 prod 环境的**真值**是什么？dev / staging 配置 OK 不代表 prod 也 OK——是否有 profile-specific 配置覆盖（`application-prod.yml`）？
- 多源配置的**优先级**怎么排？环境变量 / 启动参数 / 配置文件 / 容器 ENV / k8s ConfigMap 谁覆盖谁？最终生效值是谁？
- 配置消费代码是否真按该值行事？是否在运行时被代码 override（`@PostConstruct` 改 bean / 启动时根据 feature flag 切配置）？
- 是否有 **CI / 启动时校验**拦截不安全配置（spring-cloud-config 校验 / Helm 的 schema 校验 / 自研启动 lint）？
- 同一配置维度在**多份配置文件**里是否一致（`application.yml` 关了 debug 但 `bootstrap.yml` 又开了）？
- 配置项是不是**密钥 / 凭据**？是的话归 [secret-detection](../secret-detection/SKILL.md)；本能力只看"非凭据"危险值。

---

## 8. 检测方法论 / 数据流追踪

### Step 0：基线侦察

- 加载 `project-framework-analysis` 输出的项目结构 / 依赖图谱
- 识别 web 框架（Spring / Django / Express / Laravel 等）+ 配置文件介质类型
- 列出本项目里所有"配置介质"：`application*.yml` / `*.properties` / `web.xml` / `nginx.conf` / `Dockerfile` / `k8s/*.yaml` / `.env*` / `config/*.json`
- 识别哪些 profile / 环境名实际存在（dev / staging / prod / test）

### Step 1：按配置维度对账

对 §4 表里每个配置维度，在每个配置文件里查 grep 命中：

```bash
# 调试模式
rg -i "debug.*[:=]\s*true|server\.error\.include-stacktrace" --type yaml --type properties
rg "^DEBUG\s*=" --glob 'settings*.py'
# CORS
rg -i "allow_all_origins|setAllowedOriginPattern.*\*|cors\(\)\s*$|origin.*\*" 
# TLS 协议
rg -i "ssl_protocols|sslEnabledProtocols|TLSv1\.0|TLSv1\.1|SSLv3"
# Actuator
rg "management\.endpoints\.web\.exposure\.include" --type yaml
# H2 / DevTools
rg "h2\.console\.enabled|spring-boot-devtools"
# Jackson default typing
rg "enableDefaultTyping|activateDefaultTyping"
# XML 解析器
rg "DocumentBuilderFactory|SAXParserFactory" 
# WebSocket origin
rg "setAllowedOrigins"
# 默认凭据
rg -i "admin.*admin|root.*root|password.*password" --type yaml --type properties --type env
```

### Step 2：profile-specific 覆盖判定

- 对每个 profile（dev / staging / prod / test）单独看一遍最终生效值
- 重点：dev 配置看着危险但要确认 **prod profile 是否真覆盖了该项**
- 优先级链：环境变量 > 启动参数 > `application-{profile}.yml` > `application.yml` > 注解默认值

### Step 3：服务器层配置同步审

- `nginx.conf` / `apache.conf` / `Caddyfile` 的 TLS / CORS / 限流 / header 段
- Tomcat `server.xml` 的 `<Connector>` SSL 配置
- 与应用层配置交叉确认（应用层关了 CORS 但 nginx 层全开等同失效）

### Step 4：容器层配置同步审

- `Dockerfile` 的 `ENV` 指令——是否在镜像层固化了不安全默认值
- `docker-compose*.yml` 的 `environment:`——是否覆盖了应用配置
- `k8s/*.yaml` 的 `ConfigMap` / `Secret` —— 运行时真实注入值
- CI workflow 看部署阶段是否真把 `SPRING_PROFILES_ACTIVE=prod` 注入

工具加速：调用 [sast-scan](../sast-scan/SKILL.md) 的配置类规则（`high_confidence` 桶含 `debug=true` / 默认凭据 / Jackson default typing 等）作为粗筛起点；本能力对每条命中做"profile + 优先级 + 消费代码"的复核。

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

- [ ] 所有配置介质（应用 / 服务器 / 容器 / k8s / CI）都已列入扫描面
- [ ] 每个 profile（dev / staging / prod / test）独立对账完成
- [ ] §4 表里每个配置维度对每个配置文件都有命中记录或 `[-]` 说明
- [ ] 多源配置优先级链已厘清，最终生效值已确定
- [ ] 配置项与配置消费代码已配对查看（仅看配置文件不算完成）
- [ ] 服务器层（nginx / Tomcat）与应用层配置交叉一致
- [ ] 容器层 ENV / k8s ConfigMap 已纳入审计
- [ ] 凭据 / 密钥类已转交 [secret-detection](../secret-detection/SKILL.md)，未在本产物里重复
- [ ] 运行时注入（Vault / Feature Flag）单元格已标 `static-unknown` 而非默认 not_vulnerable

---

## 9. 闭环要求（必须遵守）

> 闭环判定 / 取证完整性 / 破坏性动作以 [common/closure-verification.md](../../common/closure-verification.md) 为准，下面只列本能力特有的判定上限与产物契约。
>
> **为什么这里是「必须」**：本节属交付契约——产物结构关系到下游 `result-with-file` / coverage-ledger 机器消费；产物聚合或省略会让整条链路失效，因此是刚性要求。

### 白盒判定上限

本能力作为白盒原子能力，判定上限为 `static-confirmed`（配置项值静态危险 + profile 确认是生产配置 + 无运行时 override 旁路），**不等于动态 confirmed**。

| 状态 | 判据 | 升级路径 |
|---|---|---|
| `static-confirmed` | 配置值静态危险 + 已确认是 prod profile + 无运行时 override | 黑盒构造请求触发对应攻击面 → `confirmed` |
| `static-unknown` | 运行时环境变量注入 / Vault 引用 / Feature Flag 切换 | 推黑盒 / 联调环境实测 |
| `suspected` | 配置值危险但 profile / 优先级未完全厘清 | 厘清优先级链或黑盒验证 |
| `not_vulnerable` | 配置值在最小权限基线内 + 多源配置一致 | — |

**升级到 `confirmed` 的样例**（白盒不能独立给）：
- Actuator `=*` 静态危险 → 黑盒 GET `/actuator/env` 拿到响应（含敏感字段） → `confirmed`
- CORS `*` 静态危险 → 黑盒构造跨源请求拿到带认证态的响应 → `confirmed`
- 弱 TLS 静态危险 → 黑盒 `openssl s_client` 协商成功 TLSv1.0 → `confirmed`

**禁止**白盒独立判 `confirmed`——无可观测效果证据，仅配置危险值不构成动态利用。

### 产物契约（必须遵守）

**为什么这里是「必须」**：产物结构是下游机器消费的接口，聚合 / 省略 / 区间会让 `result-with-file` 计数闸门失效，并让上游无法回溯到具体配置项位置。

每条配置 finding **立即** append 一行到 `shared/coverage-ledger/findings/dangerous-config.jsonl`，按 `(file, config_key, current_value, baseline_value, profile)` 五元组独立成行——**任一不同即各自独立成行，禁止合并折叠**。

```json
{
  "id": "cfg-001",
  "title": "Spring Actuator 端点全开",
  "severity": "high",
  "cwe": "CWE-732",
  "file_location": "src/main/resources/application-prod.yml:42",
  "config_key": "management.endpoints.web.exposure.include",
  "current_value": "*",
  "baseline_value": "health,info",
  "profile": "prod",
  "status": "needs_review",
  "confidence": "static-confirmed",
  "source_report": "dangerous-config",
  "description": "..."
}
```

字段约束：
- `id` 带 `cfg-` 前缀全局唯一
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`（白盒默认 `needs_review`）
- `confidence ∈ static-confirmed | static-unknown | suspected`
- `profile` 填实际生效的环境名（`prod` / `staging` / `dev` / `default` / `runtime-env`）
- `file_location` 填 `file:line`，不留空、不写区间

**禁止**：
- 聚合计数（"10 处 debug=true"）—— 丢失了具体位置
- "等" / "..." / "（略）" 省略 finding
- 凭据 / 密钥类条目（必须转交 [secret-detection](../secret-detection/SKILL.md)）

### 反例义务（必须遵守）

> **why**：白盒"已防护"结论是覆盖完整性产物声明，缺失反向验证会让下游误信"该子系统该维度安全"。

写"未发现危险配置"或"已防护"前，产物必须包含：
- 所有配置介质完整清单（应用 / 服务器 / 容器 / k8s / CI 五层）
- 所有 profile 完整清单（每个 profile 独立结论）
- §4 表每个配置维度的判定结果（已防护 / 危险 / unknown）
- `static-unknown` 单元格的具体原因（运行时注入 / Vault / Feature Flag）

清单不完整 → 结论降级为 `partial-coverage`。

---

## 10. 具象化反例库

### FP（看似命中实际不构成）

**反例 1：dev profile 配置看着危险但 prod 有覆盖**

- 抽象规则：`application.yml` 的危险默认值不代表 prod 真用该值
- 具体场景：`application.yml` 写 `server.error.include-stacktrace: always`，但 `application-prod.yml` 覆盖为 `never`
- 关键识别特征：项目存在 `application-{profile}.yml` 且 CI / 部署确实激活 prod profile
- 排除方法：确认 prod profile 文件存在 + 启动入口注入了 `SPRING_PROFILES_ACTIVE=prod` 或等效环境变量

**反例 2：Actuator 全开但有 Security 拦截**

- 抽象规则：`management.endpoints.web.exposure.include=*` 单看是危险，但配套 Spring Security 拦截
- 具体场景：`SecurityConfig` 含 `.requestMatchers("/actuator/**").hasRole("ADMIN")`
- 关键识别特征：Spring Security 配置类有 `/actuator/**` 显式拦截规则
- 排除方法：grep Security 配置类确认 actuator 路径在保护范围内

**反例 3：CORS `*` 但仅作用于公开 API**

- 抽象规则：`cors({ origin: '*' })` 单看危险，但应用于无鉴权公开端点
- 具体场景：单独的 `/public/*` 路由 group 应用 CORS `*`，主业务路由不暴露
- 关键识别特征：CORS 中间件挂载在子路由而非全局 + `credentials: false`
- 排除方法：追 CORS 配置的挂载范围 + 确认不带 cookie / 认证态

**反例 4：调试日志启用但仅 dev 镜像**

- 抽象规则：`Dockerfile.dev` 含 `ENV LOG_LEVEL=debug` 不影响 prod
- 具体场景：项目有 `Dockerfile` 与 `Dockerfile.dev` 两份，prod 用前者
- 关键识别特征：CI workflow 明确指定 `docker build -f Dockerfile`（不带 .dev）
- 排除方法：追 CI pipeline 确认生产镜像构建路径

### FN（看似不命中实际是真洞）

**反例 5：`bootstrap.yml` 覆盖了 `application.yml` 的安全配置**

- 抽象规则：`bootstrap.yml` 加载优先级**高于** `application.yml`，但容易被遗漏
- 具体场景：`application.yml` 关了 debug，`bootstrap.yml` 又开了 `management.endpoints.web.exposure.include: '*'`
- 关键识别特征：项目同时存在两份配置文件且 `bootstrap.yml` 含同名 key
- 确认方法：把 `bootstrap.yml` 纳入扫描面 + 按 Spring 优先级链取最终值

**反例 6：环境变量优先级高于配置文件**

- 抽象规则：Spring 配置项可被同名环境变量覆盖（如 `MANAGEMENT_ENDPOINTS_WEB_EXPOSURE_INCLUDE=*`）
- 具体场景：配置文件写安全值但 k8s ConfigMap 注入了危险环境变量
- 关键识别特征：`k8s/*.yaml` 的 ConfigMap 含同语义 key 但值不一致
- 确认方法：把 ConfigMap / Secret / Dockerfile ENV 全部纳入扫描面

**反例 7：Docker `ENV DEBUG=true` 未被运行时覆盖**

- 抽象规则：镜像层固化的环境变量在容器启动时若无 override 即生效
- 具体场景：`Dockerfile` 含 `ENV DEBUG=true` 但 docker-compose / k8s 部署描述未 unset
- 关键识别特征：镜像层 ENV + 部署描述无对应 unset / override
- 确认方法：追 `docker history` 或全文搜部署描述

**反例 8：多模块项目子模块独立配置文件**

- 抽象规则：monorepo 子模块各自有 `application.yml`，子模块独立配置易被遗漏
- 具体场景：主模块 `application-prod.yml` 安全，但 `payment-service/src/main/resources/application.yml` 仍是默认值
- 关键识别特征：项目根目录 `find . -name application*.yml` 命中数 > 1
- 确认方法：每个子模块独立按 §8 走一遍

**反例 9：YAML 多文档（`---` 分隔）后续文档覆盖前面**

- 抽象规则：YAML `---` 分隔的多文档块按顺序合并，后续 key 覆盖前面
- 具体场景：`application.yml` 第一段写安全配置，第二段 `---` 后写危险覆盖
- 关键识别特征：单个 YAML 文件含多个 `---` 分隔符
- 确认方法：完整读 YAML 文件 + 用 yq / 自写解析器拿最终合并结果

### 易混淆案例

**反例 10：环境变量占位但默认值已不安全**

- 抽象规则：`debug: ${DEBUG:true}` 看似由环境变量决定，但默认 fallback 是 true
- 具体场景：环境变量未注入时使用默认值（即 true）
- 关键识别特征：配置项形如 `${VAR:dangerous_default}`
- 排除方法：确认部署描述真注入了 `VAR=safe_value` 而非依赖默认

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码 / 配置**。

下面这些情形配置审计**无法继续追踪**，必须标 `static-unknown`，不允许默认为 not_vulnerable：

1. **运行时环境变量注入**
   - 配置项写 `${VAR}` 但 VAR 的实际注入值由部署平台决定，源码 / 配置文件不可见
   - **处置**：标 `static-unknown`，记录环境变量名 + 需要黑盒 / 联调环境实测

2. **Vault / Secret Manager 引用**
   - Spring `vault://path/to/key` / HashiCorp Vault / AWS Secrets Manager 引用
   - **处置**：标 `static-unknown`，要求运维侧出运行时实际值

3. **Feature Flag 切换配置**
   - LaunchDarkly / Unleash / 自研 flag 服务在运行时切换配置值
   - **处置**：枚举所有 flag 分支独立审计 + 标"需运行时验证"

4. **多 profile / 多环境配置**
   - 看到 dev / staging / prod 多份配置但不知道生产真用哪份
   - **处置**：标"需运行时验证"，要求出当前生效 profile 与最终配置

5. **服务网格 / API Gateway 覆盖底层配置**
   - Istio VirtualService / Kong / Nginx Gateway 在网格层覆盖应用配置（TLS / CORS / 限流）
   - **处置**：边界处停手，独立审计网格层配置（与应用层结论需交叉一致）

6. **多容器编排 ConfigMap 与镜像 ENV 优先级**
   - k8s ConfigMap / Secret / Pod env / Container env 多层叠加，优先级链复杂
   - **处置**：每一层独立审，最终生效值按 k8s 优先级链推导

7. **密钥相关配置**
   - 数据库密码 / API Key / JWT Secret 等凭据形态
   - **处置**：转交 [secret-detection](../secret-detection/SKILL.md)，本能力不重复

8. **HTTP 响应头配置**
   - `X-Frame-Options` / `Content-Security-Policy` / `X-XSS-Protection` 等
   - **处置**：转交 [security-header-audit](../security-header-audit/SKILL.md)

**底线**：本能力写"该子系统配置已防护"前，所有 `static-unknown` 单元格必须显式列出原因。否则结论降级为 `partial-coverage`。

---

## 12. 修复建议

### 源头治理（首选）

- **生产 profile 配置走最小权限基线**：每个配置维度都有明确"生产应取值"清单（团队 / 公司层面统一基线）
- **CI / 启动时配置 lint**：spring-cloud-config 配置 schema 校验 / Helm `values.schema.json` / 自研启动 lint 拦截不安全配置
- **Spring Profile 严格隔离**：`application-dev.yml` / `application-staging.yml` / `application-prod.yml` 独立维护；prod 配置文件单独 review + 单独权限管控
- **Actuator + Spring Security 强绑定**：`management.endpoints.web.exposure.include` 与 Security 拦截规则必须配对发布（Security 配置缺失即拦截 CI）
- **TLS 1.2+ 强制**：服务器层（nginx / Tomcat / Caddy）显式列出允许协议 + 仅 AEAD 密码套件
- **CORS allowlist 精确域名**：禁止 `*` + `credentials: true` 组合；自研 CORS 中间件按业务域分组配置
- **调试日志走 trace 级别**：生产关闭 debug / trace；通过日志聚合平台按需开启临时排查
- **危险默认显式禁用**：Jackson `deactivateDefaultTyping()` + 类型白名单；XML 解析显式 `setFeature("...disallow-doctype-decl", true)`

### 边界过滤（次选，深度防御）

- 服务网格 / API Gateway 统一注入安全头 / TLS / CORS 策略，应用层兜底
- WAF 规则覆盖已知危险路径（`/actuator/*` / `/h2-console/*`）

### 兜底拒绝

- 部署描述强制覆盖危险默认（Dockerfile `ENV DEBUG=false`）
- 启动时若检测到不安全配置组合（debug=true + prod profile）直接退出而非 warning

### 参考

- [OWASP Secure Configuration Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Secure_Configuration_Cheat_Sheet.html)
- [Spring Boot Production Ready Features](https://docs.spring.io/spring-boot/docs/current/reference/html/actuator.html)
- [Mozilla SSL Configuration Generator](https://ssl-config.mozilla.org/)
