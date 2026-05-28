# Java/Spring 暴露型危险配置（Java Spring Exposure Configuration）

## 漏洞模式

Spring Boot / Spring Framework 的配置项在开发友好的默认值下可能暴露内部信息、管理端点或调试工具。生产环境未关闭这些配置会直接开启攻击面。

**核心原则：开发便利性配置（Actuator 全开、DevTools、H2 Console、详细错误页）在生产环境是安全风险。"开发时好用"的配置在生产时就是攻击入口。**

## 危险配置清单与利用链

| 配置项 | 危险值 | 安全值 | 风险 | 利用链 |
|--------|-------|--------|------|--------|
| `management.endpoints.web.exposure.include` | `*` | 仅 `health,info` | Actuator 端点全暴露 | `/actuator/env` 泄露环境变量（含密钥）、`/actuator/heapdump` 泄露内存数据 |
| `management.endpoint.env.show-values` | `ALWAYS` | `NEVER` | 环境变量明文 | `/actuator/env` 直接显示数据库密码、API Key |
| `spring.devtools.restart.enabled` | `true`（生产） | `false` | 开发工具暴露 | 远程重启、类加载器替换 |
| `spring.devtools.remote.secret` | 任意值（生产存在即危险） | 删除 | 远程 DevTools | 攻击者用 secret 连接远程 DevTools 端点执行代码 |
| `spring.h2.console.enabled` | `true` | `false` | H2 控制台 | `/h2-console` 可直接执行 SQL，H2 支持 `CALL SHELLEXEC('cmd')` |
| `server.error.include-stacktrace` | `always` | `never` | 堆栈泄露 | 错误响应暴露类名、方法名、文件路径、依赖版本 |
| `server.error.include-message` | `always` | `never` | 错误消息泄露 | 异常消息可能含 SQL、文件路径、内部逻辑 |
| `spring.jackson.serialization.FAIL_ON_EMPTY_BEANS` | `false` + 无 `@JsonIgnore` | — | 对象序列化泄露 | 实体类被完整序列化，含密码哈希、内部字段 |
| `spring.jpa.show-sql` | `true`（生产） | `false` | SQL 语句泄露 | 日志中明文 SQL，可能含查询参数 |
| `logging.level.root` | `DEBUG`/`TRACE`（生产） | `INFO`/`WARN` | 过度日志 | 调试日志含请求参数、token、内部状态 |

## 利用链示例

### Actuator 全暴露 → 信息泄露 + RCE

```yaml
# ❌ application.yml 中暴露所有 Actuator 端点
management:
  endpoints:
    web:
      exposure:
        include: "*"
  endpoint:
    env:
      show-values: ALWAYS
```

攻击路径：

```
1. GET /actuator/env
   → 泄露数据库密码、JWT 密钥、第三方 API Key

2. GET /actuator/heapdump
   → 下载 JVM 堆转储 → 用 MAT/jhat 分析 → 提取内存中的 session、token、密码

3. GET /actuator/mappings
   → 泄露所有 URL 映射 → 发现隐藏的管理端点

4. POST /actuator/restart（如果启用）
   → 远程重启应用
```

```yaml
# ✅ 安全配置
management:
  endpoints:
    web:
      exposure:
        include: health,info
  endpoint:
    env:
      show-values: NEVER
    health:
      show-details: never
```

### H2 Console → SQL 执行 → RCE

```yaml
# ❌ 生产环境启用 H2 控制台
spring:
  h2:
    console:
      enabled: true
      path: /h2-console
  datasource:
    url: jdbc:h2:mem:testdb
```

攻击路径：

```
1. 访问 /h2-console → H2 Web Console 登录页
2. 使用默认凭据（通常无密码）连接
3. 执行 SQL：
   CREATE ALIAS EXEC AS 'String exec(String cmd) throws Exception { 
     return new Scanner(Runtime.getRuntime().exec(cmd).getInputStream()).useDelimiter("\\A").next(); 
   }';
   CALL EXEC('whoami');
   → 远程代码执行
```

### DevTools Remote → 远程代码执行

```yaml
# ❌ 生产环境存在 DevTools 远程配置
spring:
  devtools:
    remote:
      secret: mysecret
```

攻击路径：

```
1. 攻击者在本地运行 RemoteSpringApplication，配置 secret
2. 连接到目标的 /.~~spring-boot!~/restart 端点
3. 上传修改后的类文件 → 服务端热重载 → 代码执行
```

### 详细错误页 → 信息泄露

```yaml
# ❌ 生产环境暴露完整堆栈
server:
  error:
    include-stacktrace: always
    include-message: always
    include-binding-errors: always
```

错误响应示例：

```json
{
  "timestamp": "2024-01-15T10:30:00",
  "status": 500,
  "error": "Internal Server Error",
  "message": "could not execute statement; SQL [insert into users (password, role, username) values (?, ?, ?)]; constraint [UK_USERNAME]",
  "trace": "org.hibernate.exception.ConstraintViolationException...\n\tat com.app.service.UserService.create(UserService.java:42)\n\tat com.app.controller.UserController.register(UserController.java:28)...",
  "path": "/api/users"
}
```

泄露信息：表名、字段名、约束名、类名、方法名、行号、ORM 类型。

## 配置文件位置

| 位置 | 说明 |
|------|------|
| `src/main/resources/application.yml` / `application.properties` | 主配置 |
| `src/main/resources/application-{profile}.yml` | 环境配置（注意 `application-prod.yml`） |
| `bootstrap.yml` / `bootstrap.properties` | Spring Cloud 引导配置 |
| `src/main/webapp/WEB-INF/web.xml` | Servlet 配置 |
| 环境变量 / 命令行参数 | 运行时覆盖（可能不在代码中） |

## 其他语言/框架的等价配置

Spring 的暴露型配置风险在其他框架中有类似模式：

| Spring 配置 | Django (Python) | Go (Gin/标准库) | Node.js (Express) |
|------------|-----------------|----------------|-------------------|
| Actuator 全开 | `django-debug-toolbar` 生产启用 / Django admin 未限访问 | `pprof` 端点暴露（`/debug/pprof/`） | `express-status-monitor` / `swagger-ui` 生产暴露 |
| DevTools Remote | `DEBUG=True` 生产启用（Django debug page 暴露源码） | `gin.SetMode(gin.DebugMode)` 生产启用 | `NODE_ENV=development` 暴露堆栈 |
| H2 Console | 无直接等价 | 无直接等价 | 无直接等价 |
| include-stacktrace=always | `DEBUG=True` 错误页暴露完整 traceback | 默认 panic 堆栈输出到日志/响应 | `app.use(errorHandler)` 返回 stack |
| show-sql=true | `LOGGING = {'django.db.backends': 'DEBUG'}` | `db.LogMode(true)`（GORM） | `logging: true`（Sequelize/TypeORM） |

### Django 特有风险

```python
# ❌ 生产启用 DEBUG — 暴露设置、路由、SQL、源码
DEBUG = True  # settings.py

# ❌ ALLOWED_HOSTS 为空 + DEBUG=True — 任何 Host 头都返回调试页
ALLOWED_HOSTS = []
```

### Go 特有风险

```go
// ❌ pprof 端点生产暴露 — 泄露 goroutine、堆内存、CPU profile
import _ "net/http/pprof"
go http.ListenAndServe(":6060", nil)
// /debug/pprof/heap → 下载堆转储
// /debug/pprof/goroutine → 泄露所有 goroutine 堆栈
```

## 审计方法

1. **搜索 Actuator 配置**：`rg "management\.(endpoints|endpoint)" -g "*.yml" -g "*.yaml" -g "*.properties"`
2. **搜索危险开关**：`rg "devtools|h2.console|include-stacktrace|show-sql|DEBUG" -g "*.yml" -g "*.yaml" -g "*.properties"`
3. **检查 profile 配置**：确认 `application-prod.yml` 是否覆盖了开发配置的危险值
4. **检查 web.xml**：`rg "transport-guarantee|session-timeout|cookie-config" -g "web.xml"`
5. **搜索 @Profile 注解**：确认开发相关的 Bean/配置是否被限制在开发 profile 下
6. **跨框架检查**：Django 项目搜 `DEBUG = True`，Go 项目搜 `net/http/pprof`，Node.js 项目搜 `NODE_ENV` 设置
