# postMessage 跨源通信安全缺陷（postMessage Origin Bypass）

## 漏洞模式

`window.postMessage` 允许跨窗口/跨 iframe 通信。当**接收端**未校验消息来源（`event.origin`），或**发送端**使用通配符目标（`'*'`），攻击者可以注入恶意消息或窃取敏感数据。

**核心原则：接收端必须严格校验 `event.origin`；发送端必须指定精确的目标 origin（不用 `'*'`）。消息内容不能直接进入 `eval` / `innerHTML` 等危险 sink。**

## 攻击场景

### 场景 1：接收端无 origin 校验

```
攻击者页面（evil.com）
    ↓ iframe 嵌入目标页面
    ↓ postMessage({ action: "updateProfile", data: "<img src=x onerror=alert(1)>" })
目标页面接收消息 → 无 origin 检查 → 将 data 写入 DOM → XSS
```

### 场景 2：发送端使用通配符

```
目标页面（有 OAuth token）
    ↓ postMessage({ token: "bearer_xxx" }, '*')  — 发给所有窗口
    ↓ 攻击者的 iframe 也能收到
攻击者窃取 token
```

## 通用代码示例

### 接收端无 origin 校验

```javascript
// ❌ 接收消息时未验证 origin
window.addEventListener('message', function(event) {
    // 任何来源的消息都被处理
    if (event.data.action === 'updateContent') {
        document.getElementById('widget').innerHTML = event.data.html; // DOM XSS
    }
    if (event.data.action === 'redirect') {
        window.location.href = event.data.url; // Open Redirect
    }
    if (event.data.action === 'eval') {
        eval(event.data.code); // 远程代码执行
    }
});
```

```javascript
// ✅ 严格校验 origin
window.addEventListener('message', function(event) {
    if (event.origin !== 'https://trusted-partner.com') {
        return; // 忽略非信任来源
    }
    if (event.data.action === 'updateContent') {
        document.getElementById('widget').textContent = event.data.text; // 安全：textContent
    }
});
```

### 接收端 origin 校验不严格

```javascript
// ❌ 使用 indexOf 校验（可被绕过）
window.addEventListener('message', function(event) {
    if (event.origin.indexOf('trusted.com') !== -1) {
        // 攻击者可用 evil-trusted.com 或 trusted.com.evil.com 绕过
        processMessage(event.data);
    }
});

// ❌ 使用正则但未锚定
window.addEventListener('message', function(event) {
    if (/trusted\.com/.test(event.origin)) {
        // 同样可被 trusted.com.evil.com 绕过
        processMessage(event.data);
    }
});
```

```javascript
// ✅ 使用严格的全等比较或锚定正则
window.addEventListener('message', function(event) {
    const allowedOrigins = [
        'https://trusted.com',
        'https://app.trusted.com'
    ];
    if (!allowedOrigins.includes(event.origin)) {
        return;
    }
    processMessage(event.data);
});
```

### 发送端使用通配符

```javascript
// ❌ 发送敏感数据时使用 '*'
const token = getAuthToken();
parent.postMessage({ type: 'auth', token: token }, '*');
// 任何嵌入此页面的父窗口都能收到 token

// ❌ OAuth callback 页面
const code = new URLSearchParams(location.search).get('code');
window.opener.postMessage({ oauthCode: code }, '*');
// 攻击者可以打开此 OAuth callback 页面，收到 code
```

```javascript
// ✅ 指定精确的目标 origin
parent.postMessage({ type: 'auth', token: token }, 'https://parent-app.com');

// ✅ OAuth callback
window.opener.postMessage({ oauthCode: code }, 'https://main-app.com');
```

### 消息内容进入危险 sink

```javascript
// ❌ 消息内容进入 eval
window.addEventListener('message', function(event) {
    if (event.origin === 'https://trusted.com') {
        // 即使 origin 正确，trusted.com 被 XSS 后攻击者可注入任意代码
        eval(event.data.script);
    }
});

// ❌ 消息内容进入 innerHTML
window.addEventListener('message', function(event) {
    if (event.origin === 'https://trusted.com') {
        document.getElementById('preview').innerHTML = event.data.html;
    }
});
```

```javascript
// ✅ 限制消息格式和操作类型
window.addEventListener('message', function(event) {
    if (event.origin !== 'https://trusted.com') return;
    
    const allowedActions = ['resize', 'scroll', 'close'];
    if (!allowedActions.includes(event.data.action)) return;
    
    switch (event.data.action) {
        case 'resize':
            if (typeof event.data.height === 'number') {
                iframe.style.height = event.data.height + 'px';
            }
            break;
        case 'close':
            modal.close();
            break;
    }
});
```

## 识别信号

| 信号 | 说明 |
|------|------|
| `addEventListener('message', ...)` 无 `event.origin` 检查 | 接收端无来源校验 |
| `event.origin.indexOf(` / `event.origin.includes(` | origin 校验可被绕过 |
| `postMessage(data, '*')` | 发送端广播敏感数据 |
| `event.data` 进入 `innerHTML` / `eval` / `location` | 消息内容未净化进入危险 sink |
| OAuth/SSO callback 页面使用 `window.opener.postMessage` | 常见的 token 泄露路径 |
| 嵌入第三方 widget（支付/地图/社交登录）使用 postMessage | 第三方通信需要严格的 origin 校验 |

## 审计方法

1. **搜索 postMessage 使用**：`rg "postMessage|addEventListener.*message"` 在所有 JS/TS 文件中
2. **检查接收端**：每个 `addEventListener('message', ...)` 是否在入口处检查 `event.origin`？检查方式是全等比较还是模糊匹配？
3. **检查发送端**：每个 `postMessage(data, target)` 的 `target` 是否为精确 origin？是否使用了 `'*'`？
4. **检查消息处理**：`event.data` 是否进入危险 sink？即使 origin 校验正确，也要检查消息内容的使用方式
5. **枚举 iframe/popup 场景**：OAuth callback、第三方 widget、跨域 iframe 通信是高风险区域
