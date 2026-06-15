---
name: secret-detection
description: >-
  硬编码凭据 / 密钥白盒扫描——系统识别仓库 / 配置 / CI / git 历史里的密码、API Key、OAuth
  Secret、JWT Secret、数据库连接串、私钥、Cookie 加密 Key、IV、Cloud Access Key 等敏感字面量。
allowed-tools: bash,read_file,list_files,rg
user-invocable: true
argument-hint: "[target_path]"
arguments:
  - target_path
---

# 敏感信息白盒检测

## 1. 触发线索 / 适用信号

按"代码 pattern + 配置介质 + 文件格式"三维识别本能力命中场景（不按业务命名）。

**代码 pattern 维度**（grep 命中字面量赋值）：
- 通用赋值：`password\s*[:=]` / `passwd\s*[:=]` / `pwd\s*[:=]` / `secret\s*[:=]` / `token\s*[:=]` / `apikey\s*[:=]` / `api_key\s*[:=]` / `access[_-]?key` / `client[_-]?secret` / `private[_-]?key`
- Java：`private static final String PASSWORD = "..."` / `@Value` 默认值含明文
- JS / TS：`const KEY = '...'` / `const TOKEN = "..."` / template literal 拼接凭据
- Python：`SECRET = '...'` / `settings.py` 顶层赋值
- Go：`const SECRET = "..."` / `var apiKey = "..."`
- Shell：`export TOKEN=xxx` / `API_KEY=xxx` 写死

**配置介质维度**（按文件结构识别——只看代码会漏配置）：
- 根目录：`.env` / `.env.local` / `.env.production` / `.envrc`
- Java：`src/main/resources/application*.yml` / `application*.properties` / `bootstrap.yml` / `web.xml`
- Node：`config/*.json` / `package.json` scripts 段含凭据
- Python：`settings.py` / `local_settings.py` / `config.py`
- Go：`config/*.yaml` / `cmd/*/main.go` 默认值
- 通用：`config.yaml` / `secrets.json` / `*.toml` / `*.ini`
- 容器：`Dockerfile` ENV / ARG / `docker-compose*.yml` / `k8s/*-secret.yaml`
- CI：`.github/workflows/*.yml` / `.gitlab-ci.yml` / `Jenkinsfile` / `azure-pipelines.yml`
- 测试 fixture：`**/fixtures/` / `**/testdata/`（即使是测试也可能误入生产构建）

**文件格式维度**（私钥 / 证书签名）：
- 私钥文件落库：`*.pem` / `*.key` / `*.p12` / `*.pfx` / `*.jks` / `*.crt`
- 私钥头标记：`-----BEGIN (RSA |EC |DSA |OPENSSH |ENCRYPTED |)PRIVATE KEY-----`
- 高熵长字符串：长度 > 20 + 字符集随机度高 + 特征前缀

**反向信号**（不命中本能力）：
- 调试 / 默认配置危险值（`debug=true` / `allow-all-origins`）→ 走 [dangerous-config](../dangerous-config/SKILL.md)
- 跨函数追踪密钥到加密 sink 是否参数化 → 走 [dataflow-analysis](../dataflow-analysis/SKILL.md)

---

## 2. 造成原因

凭据明文存在于仓库、构建制品、日志或错误返回，意味着任何能读取该资源的人（开发者、CI runner、镜像下载者、日志聚合系统）都能取得这把钥匙。一旦凭据落入未受信范围，攻击者可凭其调用上游 API、登入数据库、解码 JWT、解密历史数据，进而横向移动 / 接口滥用 / 数据泄露。

git 仓库的特殊放大效应：**一次提交永久残留 git 历史**——即使后续 commit 删除明文，旧 commit 仍可被 `git log -p --all` 取回。轮换（rotate）只能让旧 key 失效，**无法让已泄露的 key 消失**；若上游服务签发的 token 不可主动吊销（如某些长期 API Key），影响周期等于服务可用周期。

---

## 3. 领域 source-sink 数据流模型

本能力以**字面量扫描**为核心，**不做跨函数追踪**（跨函数 source 到加密 sink 的可达性追踪交 [dataflow-analysis](../dataflow-analysis/SKILL.md)）。

**代码层 source 集合**（白盒视角下的"凭据字面量"）：
- 双引号 / 单引号 / 反引号包裹的长字符串
- 已知服务特征前缀：
  - AWS Access Key：`AKIA[0-9A-Z]{16}`
  - GitHub Token：`ghp_[A-Za-z0-9]{36}` / `gho_` / `ghu_` / `ghs_`
  - Stripe：`sk_(test|live)_[A-Za-z0-9]{24,}` / `pk_(test|live)_`
  - Slack：`xox[abprs]-[A-Za-z0-9-]+`
  - GCP API Key：`AIza[0-9A-Za-z_-]{35}`
  - Azure：`[a-zA-Z0-9+/]{86}==`
  - JWT：`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`
  - 私钥：`-----BEGIN .*PRIVATE KEY-----`
- 字段名 + 长字符串组合：`password|passwd|pwd|secret|token|apikey|api[_-]?key|access[_-]?key|client[_-]?secret` 后接非占位符字符串

**代码层 sink 集合**（凭据被消费 / 落库的位置——本能力主要做"sink 位置就地标注"，不追跨函数）：
- HTTP client 调用：`http.Header.Set("Authorization", ...)` / `axios.create({headers})`
- SDK 初始化：`aws.NewSession(credentials.NewStaticCredentials(...))` / `stripe.Client(secret)`
- 加密初始化：`AES.new(key, ...)` / `cipher.Init(key)` / IV 字面量
- 数据库连接：`mysql://user:pass@host` / `Database.connect(dsn)`

**数据流追踪规则**：
- 本能力的扫描单位是**单文件**——在该文件内识别"字面量 + 字段名"组合即可
- 跨文件追踪（如常量 `SECRET = "..."` 定义在一处、消费在另一处）只追**同包 / 同模块**内的引用，确认是否真被加密 / 鉴权 sink 消费
- 跨函数 / 跨依赖 / 跨服务追踪 → 升级到 [dataflow-analysis](../dataflow-analysis/SKILL.md)
- git 历史维度：扫描深度按 commit 数 / 时间窗口限制，受限范围必须显式标注

---

## 4. 常见类型

本能力覆盖的密钥 / 凭据主流变体（按已知主流覆盖，不追求穷举）：

| 类型 | 静态识别特征 | 白盒识别难点 |
|---|---|---|
| **硬编码密码** | 字段名 `password|passwd|pwd` + 字面量 | 易与占位符（`changeme` / `xxx`）混淆 |
| **API Key** | 字段名 `api[_-]?key|apikey` + 长字符串；含服务前缀（`AKIA` / `sk_`） | 自研内部 API Key 无前缀；高熵但无特征 |
| **OAuth Client Secret** | `client_secret` / `clientSecret` 字段 | 测试 client_secret 与生产难区分 |
| **JWT Secret（签名密钥）** | `jwt.sign(payload, "<secret>")` / `setSigningKey` | 弱 secret（`secret` / `123456`）字符串短，被字段名约束识别 |
| **数据库连接串明文** | `mysql\|postgres\|mongodb\|redis://user:pass@host` | password 段可能 URL 编码 |
| **私钥文件落库** | 头标记 `-----BEGIN .*PRIVATE KEY-----` / 扩展名 `.pem/.key/.p12/.pfx` | 无头标记的裸 base64 段易漏 |
| **Cookie 加密 Key / Session Secret** | `SECRET_KEY` / `cookie_secret` / `session_key` 字段 | Django / Flask 默认占位符常被遗留 |
| **加密 IV 硬编码** | `iv = "1234567890ABCDEF"` / 16/12 字节字面量 | IV 看似无害，但固定 IV 让 AES-CBC / GCM 失效（密钥重用） |
| **测试凭据混入生产** | 真凭据出现在 `test/` / `fixtures/` 但被生产 build 引用 | 需追构建 include / exclude 规则 |
| **Webhook Secret** | `webhook_secret` / `WEBHOOK_TOKEN` / Stripe `whsec_` / GitHub `X-Hub-Signature` 密钥 | 通常短字符串无特征前缀 |
| **Cloud Access Key（AWS / Azure / GCP）** | 各平台前缀 + 配套 secret access key 紧邻出现 | secret access key 无前缀，需靠"Access Key 附近"上下文识别 |

---

## 5. 入口点定位

按项目结构构建"扫描面"——能否命中真凭据取决于扫描面是否覆盖配置 / CI / 私钥介质，**只看源码扩展名会漏 `.env` / `.yml` / `.pem`**。

> 下列框架 / 项目类型仅作类似项目示例 不限于此；以目标实际栈为准。

### 通用根目录

- `.env` / `.env.local` / `.env.development` / `.env.production` / `.env.test` / `.envrc`
- `.env.example`（应只含占位符——若含真凭据是误入）
- `secrets/` 子目录
- `.git/`（若项目含 git 历史扫描需求）

### Java / Spring 项目

- `src/main/resources/application*.{yml,yaml,properties}` / `bootstrap.yml`（Config Server 客户端）
- `WEB-INF/web.xml` / `pom.xml` settings server 段
- `src/main/resources/*.jks` / `*.p12`（keystore）
- `src/test/resources/` —— 测试 fixture（可能误入生产构建）

### Python 项目

- `settings.py` / `local_settings.py` / `config.py` / `secrets.py`
- Django `*/settings/*.py`（按环境拆分）/ `instance/config.py`（Flask）
- `pyproject.toml` / `setup.cfg` / `.env*`

### Node.js / TypeScript 项目

- `package.json` scripts 段（CI 步骤写死的密钥）
- `config/*.json` / `config/default.json`（node-config）
- `.npmrc`（含 npm token）/ `src/config/*.ts` / `.env*`

### Go 项目

- `config/*.yaml` / `config/*.toml`
- `cmd/*/main.go` 默认值 / `internal/config/*.go` 常量定义

### 容器 / 编排

- `Dockerfile`：`ENV KEY=...` / `ARG SECRET=...`
- `docker-compose*.yml`：`environment:` 段明文凭据
- `k8s/*.yaml` / `helm/values.yaml`：`Secret` 资源 base64 明文（即使是 `kind: Secret` 资源也可能错放进仓库——base64 ≠ 加密）

### CI / CD 配置

- `.github/workflows/*.yml`：`env:` 段硬编码而非 `${{ secrets.X }}`
- `.gitlab-ci.yml`：`variables:` 段
- `Jenkinsfile`：`environment {}` 段
- `azure-pipelines.yml` / `bitbucket-pipelines.yml`
- `.circleci/config.yml`

### Shell 脚本

- `scripts/*.sh` / `*.ps1` / `Makefile`
- 含 `export TOKEN=xxx` / `KEY=xxx` 直接赋值

### git 历史

- `.git/` 全量克隆后用 `git log -p --all -S "<候选片段>"`
- 限定深度：`--since=<date>` 或 `-n <N>` 控制扫描成本

### 与 [dangerous-config](../dangerous-config/SKILL.md) 边界

同目录下的配置文件可能同时触发两个能力——**凭据 / 密码 / 密钥归本能力；调试模式 / 默认危险值 / 弱 TLS / 过宽 CORS 归 dangerous-config**。同一行配置可能命中两个能力（如 `debug=true` + `admin_password=admin`），各自独立成行。

---

## 6. 跨框架代码变体

下表列主流框架的"安全形态 vs 危险形态"对照——帮助识别"看似在用 Vault / KMS 但代码里残留默认明文"这类常见漏点。

| 框架 / 平台 | 安全形态 | 危险形态 |
|---|---|---|
| **Spring Boot** | `@Value("${db.password}")` + 外部 Vault / Config Server | `private static final String PASSWORD = "..."` / `application.yml` 明文 |
| **Spring Cloud Vault** | `spring.cloud.vault.token` 由 K8s ServiceAccount 注入 | `bootstrap.yml` 写死 root token |
| **Node.js** | `process.env.API_KEY` + dotenv-vault / AWS SM SDK | `const KEY = '...'` / `package.json` scripts 内联凭据 |
| **Python / Django** | `os.environ['SECRET']` + django-environ + KMS | `SECRET_KEY = '...'`（settings.py 顶层） |
| **Python / Flask** | `app.config.from_envvar('CONFIG')` + Vault | `app.config['SECRET_KEY'] = '...'` |
| **Go** | `os.Getenv("API_KEY")` + Vault SDK / AWS SM | `const SECRET = "..."` / `var apiKey = "..."` |
| **HashiCorp Vault 集成** | `vault.Read("secret/data/<path>")` 运行时拉取 | 配置里写 `vault_root_token = "..."` |
| **AWS Secrets Manager** | `secretsmanager.GetSecretValue` 运行时拉取 | 配置文件硬编码 `aws_access_key_id` / `aws_secret_access_key` |
| **Azure Key Vault** | `SecretClient` + `DefaultAzureCredential`（Managed Identity） | `connection_string = "..."` 写死 storage key |
| **K8s** | `valueFrom: secretKeyRef:` | `value: "明文凭据"` |
| **CI / GitHub Actions** | `${{ secrets.X }}` 引用 | `env: KEY: "明文"` |
| **CI / GitLab** | masked / file-type CI variable | `variables: KEY: "明文"` |

**通用安全反模式**（任何栈都适用）：
- "Vault / KMS 已接入但 `.env` 里仍残留默认明文兜底"——开发者改测试环境时遗留
- "私钥文件本不应入库但 `.gitignore` 漏写"
- "测试 fixture 用了真凭据"（如 e2e 测试连了真 Stripe test key）

---

## 7. 思考检查点

加载本 skill 时按这些问题思考（按"凭据语义"而非业务命名）：

- 这个看似密钥的字符串是**真凭据**还是**占位符**？占位符特征：`xxx` / `your-` / `replace-me` / `<placeholder>` / `example` / `changeme` / `dummy`
- 字符串**熵和长度**是否符合凭据特征？长度 < 8 或字符集单一（全字母 / 全数字）的"密钥"多为占位
- 出现位置是否在**测试目录 / mock 数据 / fixture**？测试凭据仍要标注但严重度可降——除非有构建规则把测试资源打进生产
- 项目是否**已用 KMS / Vault / 环境变量**替代但代码里残留默认值？典型如 `os.getenv("KEY") or "DEFAULT_SECRET"`
- **git 历史**是否有 rotate 过期未清理的旧 key？即使当前 HEAD 干净，旧 commit 仍可取
- 该凭据**关联的服务可达性**如何？指向 `localhost` / 内网服务的凭据风险低于指向公网 SaaS 的凭据

---

## 8. 检测方法论 / 数据流追踪

> 本能力是**字面量扫描**为主、轻度跨文件回溯为辅，**不做跨函数链路追踪**——跨函数追踪走 [dataflow-analysis](../dataflow-analysis/SKILL.md)，规则化高频 sink 命中走 [sast-scan](../sast-scan/SKILL.md) `secret` 桶。

### Step 0：基线侦察

- 加载 [project-framework-analysis](../project-framework-analysis/SKILL.md) 输出，识别配置文件层级（按环境拆分？走 Vault？）
- 列出本项目所有"凭据可能落处"：配置目录 / `.env*` / CI / 私钥介质 / Shell 脚本 / git 历史
- 统计扫描面：配置文件数 / 私钥文件数 / CI 配置数 / shell 脚本数

### Step 1：按文件类型分批 grep

```bash
# 字段名 + 字面量赋值（覆盖多种引号风格）
rg -i --pcre2 '(password|passwd|pwd|secret|token|apikey|api[_-]?key|access[_-]?key|client[_-]?secret|private[_-]?key)\s*[:=]\s*["\x27`][^"\x27`]{4,}["\x27`]'

# 配置文件中 YAML / properties 风格
rg -i '(password|secret|token|apikey|api[_-]?key):\s*\S+' --type yaml
rg -i '(password|secret|token|apikey)\s*=\s*\S+' --type properties

# 私钥文件签名
rg -- '-----BEGIN (RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----'

# 私钥文件落库（按扩展名 + 内容核验）
list_files **/*.{pem,key,p12,pfx,jks,crt}
```

### Step 2：已知服务前缀模式匹配

```bash
# AWS Access Key
rg 'AKIA[0-9A-Z]{16}'
# GitHub Token
rg 'gh[oprsu]_[A-Za-z0-9]{36,}'
# Stripe
rg 'sk_(test|live)_[A-Za-z0-9]{24,}'
# Slack
rg 'xox[abprs]-[A-Za-z0-9-]+'
# GCP API Key
rg 'AIza[0-9A-Za-z_-]{35}'
# JWT
rg 'eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+'
```

### Step 3：高熵字符串扫描

- 对长度 > 20 且字符集随机度高（包含大小写 + 数字混合，可选含 `+/=_-`）的字符串重点核验
- 工具加速：可用 `detect-secrets` / `trufflehog filesystem <path> --json` / `gitleaks detect --source <path>`（若环境可用）

### Step 4：git 历史扫描

```bash
# 限定深度（避免大仓库扫描爆炸）
git log -p --all --since="1 year ago" -S "<候选密钥片段>"
# 工具加速
gitleaks detect --source . --log-opts="--all --since=2024-01-01"
```

扫描深度（commit 数 / 时间窗口）必须显式标注在产物里——受限范围不等于"该子系统无历史泄露"。

### Step 5：占位符过滤

排除以下模式（FP 高发）：
- 字面量含 `xxx` / `your-` / `replace-me` / `<placeholder>` / `example` / `changeme` / `dummy` / `foobar` / `test123`
- 全数字短串（`1234567` / `password123`——除非字段名暗示是默认密码）
- 重复字符（`aaaaaaaa` / `00000000`）

### Step 6：测试 / fixture 上下文判定

- 路径含 `test/` / `tests/` / `__tests__/` / `spec/` / `fixtures/` / `testdata/` / `mock/` 的命中：仍要标注，但严重度可降
- 例外：若构建配置（`webpack.config.js` / `pom.xml` `<resources>` / Go embed）把测试资源打进生产构建——按生产凭据处理

### 基线检查项

> 以下是已知的检查角度，作为基线起点而非必检硬清单。结合目标代码动态调整，按三态标注（`[x]` / `[-]` / `[+]`）处置。

- [ ] 扫描面统计完整（配置 / 私钥 / CI / Shell / git 历史五类各有数字）
- [ ] 字段名 + 字面量赋值组合 grep 已执行
- [ ] 已知服务前缀（AWS / GitHub / Stripe / Slack / GCP / JWT）正则已扫
- [ ] 私钥头标记 + 私钥扩展名清单已扫
- [ ] 高熵字符串扫描已执行（工具或手动）
- [ ] git 历史扫描深度已显式记录（commit 数 / 时间窗口）
- [ ] 占位符过滤规则已应用，每条排除有记录
- [ ] 测试 / fixture 上下文已判定，构建 include 规则已核验
- [ ] CI 配置（GitHub Actions / GitLab CI / Jenkins）单独扫，未走 `secrets.X` 引用的已列
- [ ] 容器 / k8s manifest 单独扫，`env:` 明文与 base64 `Secret` 资源已识别

---

## 9. 闭环要求（必须遵守）

> 闭环判定 / 取证完整性 / 破坏性动作以 [common/closure-verification.md](../../common/closure-verification.md) 为准，下面只列本能力特有的判定上限与产物契约。
>
> **为什么这里是「必须」**：本节属交付契约——产物结构关系到下游 [dataflow-analysis](../dataflow-analysis/SKILL.md) / `result-with-file` 机器消费；明文凭据若误写入产物会让产物自身成为二次泄露源，因此产物契约是刚性要求。

### 白盒判定上限

本能力作为**白盒原子能力**，判定上限为 `static-confirmed`，**不等于动态 confirmed**。

| 静态状态 | 判据 | 升级路径 |
|---|---|---|
| `static-confirmed`（落 `status=needs_review`） | 字面量在仓库内 + 不是占位符 / 不是测试 mock + 特征匹配真实凭据格式（前缀 / 长度 / 熵） | 验证凭据真实可用（一般不主动验证——避免触发供应商风控）；改为推 owner rotate 后观测；rotate 成功证明凭据真实有效 → `confirmed` |
| `static-unknown`（落 `status=needs_review` + 标注 unknown） | encrypt-at-rest 凭据（`.env.encrypted` / `.sops.yaml`）/ 远程 Vault 引用 / 仅在 CI 注入的 secret 命名变量 / 高熵长串但无任何字段名 / 上下文线索 | 需读取实际配置 / 提取构建期注入逻辑，或推 owner 确认 |
| `not_vulnerable`（落 `status=not_vulnerable`） | 已确认占位符 / 已确认测试 fixture 且不进生产构建 / 已迁移到环境变量且配置文件只剩引用（如 `${ENV_VAR}`） | 不适用 |

**禁止**白盒独立判 `confirmed`——无可观测效果证据（成功调用、成功登录、rotate 反馈）仅静态命中不构成"真凭据真生效"。

### 产物契约（必须遵守 + 特殊安全约束）

**为什么这里是「必须」**：产物结构是下游机器消费的接口，聚合 / 省略 / 区间会让 `result-with-file` 计数闸门失效；而**明文密钥本身**是高敏数据，产物若包含真凭据明文会让 jsonl 自身成为二次泄露源——本能力比其他白盒能力多一条**密钥脱敏**强约束。

每确认一条候选**立即** append 一行到 `shared/coverage-ledger/findings/secret-detection.jsonl`，不等汇总阶段回头整理（why："事后总结"是聚合 / 区间 / "等"省略的根源）：

```json
{
  "id": "secret-001",
  "title": "硬编码 AWS Access Key",
  "severity": "high",
  "cwe": "CWE-798",
  "source": "AWS_ACCESS_KEY",
  "sink": "config/aws.yaml:12",
  "entry_point": "systemic",
  "status": "needs_review",
  "confidence": "static-confirmed",
  "file_location": "config/aws.yaml:12",
  "source_report": "secret-detection",
  "description": "AKIA**** 前缀匹配 AWS Access Key 格式；该文件随仓库分发"
}
```

字段约束：
- `id` 带 `secret-` 前缀全局唯一
- `status ∈ confirmed | needs_review | not_vulnerable | false_positive | superseded`（白盒默认 `needs_review`）
- `confidence ∈ static-confirmed | static-unknown`（与本节判定上限对齐）
- `(file, line, secret_type)` **三元组任一不同即各自独立成行**——禁止合并折叠（如"5 个 API Key 在同一文件"必须落 5 行）
- 本类多无 HTTP 入口点：`entry_point` 填 `systemic`；`source` 填密钥类型，`sink` 填泄露位置（file:line）
- `file_location` 填 `file:line`，不留空、不写区间

**特殊产物契约（密钥脱敏，必须遵守）**：

- **禁止**把真实凭据明文复制到 jsonl / 任何产物文件
- 标注形式：前缀掩码 + 类型——如 `AKIA****` / `ghp_****` / `sk_test_****` / `-----BEGIN PRIVATE KEY-----...****`
- `description` 字段只写"前 N 字符 + 类型 + 上下文线索"，不写完整 base64 / 完整 token
- 即使在私聊 / 内部审计场景也按脱敏规则走——产物可能被复用 / 转发

**禁止**：
- 聚合计数（"该目录 12 处硬编码密码"）—— 丢失了具体 file:line，下游无法消费
- "等" / "..." / "（略）" 省略 finding —— 看似覆盖完整实则漏检
- 因项目大就只扫了部分目录却宣称"完整审计已完成"——未扫的目录 / 未扫的 git 历史深度必须显式列入"扫描缺口"
- 把真实凭据明文写进产物 / 报告 / 日志——脱敏是硬约束

### 反例义务（必须遵守）

> **why**：白盒"已无敏感信息"结论是覆盖完整性产物声明，缺失反向验证会让下游误信"该仓库无凭据泄露"。

写"未发现硬编码凭据"或"已完整审计"前，产物必须包含：
- 扫描面统计（配置 / 私钥 / CI / Shell / git 历史五类介质数清单）
- 已扫描的 git 历史深度（commit 数 / 时间窗口）
- 占位符过滤规则的具体清单与排除条目数
- 测试 / fixture 命中条目的构建 include 规则核验结论
- 任一未扫到的介质 / 未扫到的子目录在"扫描缺口"段显式列出
- `static-unknown` 单元格的具体原因（加密 at-rest / Vault 引用 / 高熵无上下文）

清单不完整 → 结论降级为 `partial-coverage`。

---

## 10. 具象化反例库

### FP（看似命中实际不构成）

**反例 1：测试 fixture 里的占位符 token**

- 抽象规则：路径含 `fixtures/` / `testdata/` + 字符串明显是凑数（`test-api-key-1234` / `dummy-secret`）→ 非真凭据
- 具体场景：`tests/fixtures/auth_response.json` 含 `"token": "test-api-key-1234"`
- 关键识别特征：字符串本身含 `test` / `dummy` / `fake` / `mock` 关键字；上下文是 mock 响应数据
- 排除方法：标 `false_positive`；但若构建规则把 `fixtures/` 打进生产产物则仍要标注

**反例 2：文档示例代码片段**

- 抽象规则：README / docs / Swagger 示例里展示用 key 非真凭据
- 具体场景：`README.md` 含 `export AWS_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE`（AWS 官方示例 key）
- 关键识别特征：来源文件是 `.md` / `*.rst` / `docs/`；key 是已知公开示例值（`AKIAIOSFODNN7EXAMPLE` 是 AWS 文档广为使用的示例）
- 排除方法：核对已知示例 key 黑名单；标 `false_positive`

**反例 3：OpenAPI / Swagger 文档展示用 key**

- 抽象规则：API 文档示例段含 key 通常是文档生成器填充
- 具体场景：`openapi.yaml` 含 `example: "Bearer eyJhbGc..."` 是 spec 自带的展示
- 关键识别特征：key 出现在 `example:` / `default:` 段；JWT payload 解码后字段无业务含义
- 排除方法：解码 JWT 看 payload；若 payload 含 `"sub": "string"` 等占位则归 FP

### FN（看似不命中实际是真洞）

**反例 4：base64 编码的凭据（特征前缀消失）**

- 抽象规则：base64 编码会消去 `AKIA` 等前缀特征，单纯特征前缀扫描漏报
- 具体场景：`config.yaml` 含 `key: QUtJQVlPV1lYV1lPUVVZWVdYVlk=`（base64 编码的 AWS key）
- 关键识别特征：字段名暗示是凭据（`key` / `secret`）+ 字符串是 base64 形态（仅 `[A-Za-z0-9+/=]`）+ 长度匹配解码后凭据长度
- 确认方法：对疑似 base64 字段做解码再匹配特征前缀

**反例 5：多段拼接构造的凭据**

- 抽象规则：开发者把凭据拆成多段拼接以"骗过"扫描器
- 具体场景：`const KEY = 'AKIA' + 'IOSFOD' + 'NN7EXAMPLE'`
- 关键识别特征：同一行 / 同一函数内有多次字符串拼接，拼接结果落入凭据字段
- 确认方法：识别字符串拼接到凭据相关变量名的代码模式；必要时执行语义合并

**反例 6：自定义混淆但运行时可复原**

- 抽象规则：异或 / 简单加密 / base64 多轮编码——运行时可复原即等同明文
- 具体场景：`var key = decode(rot13("..."))` / `xor(constBytes, magicByte)`
- 关键识别特征：函数名暗示编解码（`decode` / `decrypt` / `obfuscate`）+ 常量字节数组传入
- 确认方法：追到 decode 函数体看是否是真加密；非真加密 → 等同明文

**反例 7：私钥未带头尾标记仅纯 base64 段落**

- 抽象规则：开发者去掉 PEM 头尾以"骗过"扫描器
- 具体场景：`.env` 含 `RSA_PRIVATE_KEY="MIIEvQIBADANBgkq..."`（无 `-----BEGIN`）
- 关键识别特征：字段名暗示是私钥（`private_key` / `rsa_key` / `ssh_key`）+ base64 长度匹配 RSA 私钥（通常 > 1600 字符）
- 确认方法：按字段名 + 长度 + base64 字符集组合判定

**反例 8：YAML / TOML 字符串折叠形态**

- 抽象规则：YAML `>-` / `|-` 折叠让多行字符串看似不像凭据
- 具体场景：`secret: >-\n  AKIA\n  IOSF...`
- 关键识别特征：YAML 折叠标记紧接 `password|secret|key` 字段
- 确认方法：YAML parser 解析后再做匹配

### 易混淆案例

**反例 9：环境变量引用 vs 默认明文**

- 抽象规则：`${VAR}` 引用是安全的，但 `${VAR:-默认明文}` 兜底等同明文
- 具体场景：`docker-compose.yml` 含 `KEY: "${API_KEY:-sk_live_realsecret123}"`
- 关键识别特征：`${...:-...}` / `${...:?...}` 兜底语法 + 兜底值是真凭据
- 处置：兜底段视同硬编码——标 `static-confirmed`

---

## 11. 静态分析边界

> 白盒底线：**不假装看到看不到的代码**。本能力的可观测能力到字面量 + 字段名匹配为止。

下面这些情形本能力**无法继续追踪**，必须落 `static-unknown` 并显式标注原因，**不允许**默认为 not_vulnerable：

1. **远程 Vault / KMS 引用**——`secretRef:` / `valueFrom:` / `vault.read(...)` / `secretsmanager.get(...)`。代码只看到引用名，不见明文。处置：标 `n/a`（不在白盒可见范围）；若需确认 Vault 内容则推 owner / 走运维侧。
2. **运行时环境变量注入**——代码只有 `os.environ['X']` / `process.env.X`，凭据由 K8s ServiceAccount / 进程启动脚本注入。处置：标 `not_applicable`（不是漏洞——这是正确做法）；但若发现 `.env` 文件随仓库分发则就此回归本能力。
3. **构建期注入的 secret**——webpack `DefinePlugin` / Vite `import.meta.env` / Spring `@Value` 绑定外部源。处置：需读构建配置确认注入源；构建配置不可见则标 `static-unknown`。
4. **encrypt-at-rest 凭据**——`.env.encrypted` / `.sops.yaml` / `ansible-vault` 文件。处置：标 `static-unknown`，记录加密形态；解密需密钥不在审计范围。
5. **git 历史扫描深度受限**——大仓库 `git log -p --all` 可能 OOM / 超时。处置：显式标注"扫描深度 N 个 commit / 时间窗口 X 月"，未扫深度按 `partial-coverage` 处理。
6. **闭源 / 依赖内嵌凭据**——三方 jar / dll / so / npm 包内 bundled 凭据。处置：依赖图谱标 `unknown` 推 [dependency-decompile](../dependency-decompile/SKILL.md)；不能直接 not_vulnerable。
7. **运行时动态生成的凭据**——`uuid.v4()` 生成的 session secret 写入运行时缓存。处置：本能力不追运行时——若静态代码里看到生成逻辑无明文落库，标 `not_applicable`。
8. **跨服务凭据流转**——RPC / 消息队列传递的凭据（如把 token 通过 Kafka 传给下游服务）。处置：本服务范围内的字面量扫描到出站调用即停；下游由对应服务的本能力实例单独审计。

**底线**：本能力写"该仓库无硬编码凭据"前，所有 `static-unknown` 单元格必须显式列出原因。否则结论降级为 `partial-coverage`。

---

## 12. 修复建议

### 源头治理（首选）

- **全部凭据走密钥管理服务**：HashiCorp Vault / AWS Secrets Manager / Azure Key Vault / GCP Secret Manager
- **CI / CD 用 OIDC short-lived token** 替代长期 key：GitHub Actions `id-token` / GitLab `JWT` / AWS `AssumeRoleWithWebIdentity`
- **K8s** 用 `valueFrom: secretKeyRef:` + External Secrets Operator 同步 Vault
- 本地开发用 `.env.example` 留占位符；真 `.env` / `*.pem` / `*.key` / `*.p12` / `*.pfx` / `*.jks` / `secrets/` 全部加入 `.gitignore`

### 已泄露凭据的处置

1. **立即 rotate**——所有触达过仓库 / 构建产物 / 日志的凭据都视同已泄露，先轮换再清理代码
2. **清理 git 历史**——`git filter-branch` / [BFG Repo-Cleaner](https://rtyley.github.io/bfg-repo-cleaner/) 把历史中的明文擦除；推送 force-push 后通知所有 fork / clone 重新同步
3. **吊销 token**——能吊销的就吊销（OAuth refresh token / 短期 JWT）；不能吊销的（永久 API Key）只能 rotate
4. **审计下游使用**——检查泄露窗口期内该凭据是否被异常使用（云平台 CloudTrail / GitHub audit log）

### 边界过滤（次选，深度防御）

- **预提交钩子**拦截：[detect-secrets](https://github.com/Yelp/detect-secrets) / [gitleaks](https://github.com/gitleaks/gitleaks) / [TruffleHog](https://github.com/trufflesecurity/trufflehog) 接入 pre-commit
- **CI 阶段扫描**：每个 PR 自动扫 + 阻断
- **代码审查清单**强制核对：审 PR 时列"是否新增 .env / .pem / 长字符串字面量"勾选项

### 兜底拒绝

- 私钥统一进 Vault 不进仓库；本地开发用临时短期证书
- 日志脱敏：日志框架配置敏感字段过滤（Logback `MaskingConverter` / Winston `format.uncolorize`）
- 错误响应不回显配置内容（避免 stacktrace 泄露 `.env` 路径）

### 参考

- [OWASP Cheat Sheet: Secrets Management](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
- [GitGuardian](https://www.gitguardian.com/) / [TruffleHog](https://github.com/trufflesecurity/trufflehog) / [gitleaks](https://github.com/gitleaks/gitleaks)
- [BFG Repo-Cleaner](https://rtyley.github.io/bfg-repo-cleaner/) — git 历史清理
