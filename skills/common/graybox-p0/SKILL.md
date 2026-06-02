---
name: graybox-p0
description: 灰盒安全测试 P0 编排入口。用于同时拥有源码或运行目标时，将白盒候选定位与黑盒真实请求验证串成一个最小闭环。
when-to-use: 当任务是灰盒安全测试，且需要一个最小可执行入口来决定先做白盒、黑盒还是交叉验证时，优先加载此 skill
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
argument-hint: "[target_path] [target_url]"
arguments:
  - target_path
  - target_url
---

# 灰盒 P0

你是灰盒测试的总控入口。目标不是把白盒和黑盒各跑一遍，而是尽快形成“候选点 -> 真实请求 -> 交叉印证”的闭环。

## 先判断输入条件

- 只有源码，没有可访问目标：
  - 加载 `security-code-analysis`
  - 按其路由继续深入
  - 结论标记为“静态候选”或“数据流已确认”

- 只有目标，没有源码：
  - 加载 `web-security-testing`
  - 需要浏览器交互时同时加载 `agent-browser`
  - 结论标记为“黑盒验证结果”

- 同时有源码和目标：
  - 先加载 `security-code-analysis` 做白盒侦察
  - 再加载 `web-security-testing` 与 `agent-browser` 做真实请求验证
  - 黑盒验证优先围绕白盒候选入口、参数、鉴权边界展开

## 白盒阶段的最小产出

- 入口点：路由、控制器、处理函数、关键中间件
- 候选点：危险 sink、模板渲染点、鉴权/ownership 缺口、文件与命令执行点
- 证据：文件路径、行号、source -> sink 路径或调用链

如白盒已明确候选类型，再按需加载专项 skill，不要一次性全加载：

- 注入类：`SQL注入-多策略综合检测`、`xss-testing`、`ssti-testing`、`command-injection`
- 访问控制：`越权访问-IDOR检测`、`越权访问-垂直越权检测`、`越权访问-未授权访问检测`
- 文件/路径：`文件上传-多策略综合检测`、`path-traversal-lfi`
- 协议/配置：`ssrf-testing`、`CORS-配置错误检测`、`JWT-弱密钥与信息泄露检测`

## 黑盒阶段的最小动作

- 用 `web-security-testing` 建立侦察和测试顺序
- 需要真实页面操作、抓取请求或截图时，加载 `agent-browser`
- 先捕获真实请求，再基于真实参数、Cookie、Header、路由去构造 payload
- 每个验证结果都要回填到白盒候选上，标记：
  - `已动态复现`
  - `仅静态候选`
  - `数据流不可达`
  - `黑盒未命中，待人工复核`

## 输出要求

- 最终输出前加载 `result-with-file`
- 每条发现至少包含：
  - 白盒证据：文件:行 或调用链
  - 黑盒证据：请求/响应/关键 payload/截图
  - 当前状态：已复现、候选、不可达、待复核

## 约束

- 不要把“没有复现”直接等价为“没有风险”
- 不要跳过真实请求就下动态结论
- 不要在没有候选信号时把所有专项 skill 一次性展开
