---
name: graybox-p0
description: 灰盒安全测试 P0 编排入口。用于同时拥有源码或运行目标时，将白盒候选定位与黑盒真实请求验证串成一个最小闭环。
when-to-use: 当任务是灰盒安全测试，且需要一个最小可执行入口来决定先做白盒、黑盒还是交叉验证时，优先加载此 skill
allowed-tools: bash,read_file,list_files,rg,list_skills
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
  - 结论标记为“静态候选”或“白盒可达性已确认（纯静态证据，不等于动态 `confirmed`）”

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
- 依赖图谱：第三方/闭源依赖清单 + 无源码可反编译/混淆/`unknown` 标注 + 相对入口/边界位置（关键路径上的无源码可反编译依赖作为前提变化上报，供 replan 主动规划反编译）
- 证据：文件路径、行号、source -> sink 路径或调用链

如白盒已明确候选类型，再按需加载专项 skill，不要一次性全加载：

- 注入类：`sql-injection-comprehensive`、`xss-testing`、`ssti-testing`、`command-injection`
- 访问控制：`idor-detection`、`vertical-privilege-escalation`、`unauthorized-access`
- 文件/路径：`file-upload`、`path-traversal-lfi`
- 协议/配置：`ssrf-testing`、`cors-misconfiguration`、`jwt-weakness`

## 黑盒阶段的最小动作

- 用 `web-security-testing` 建立侦察和测试顺序
- 需要真实页面操作、抓取请求或截图时，加载 `agent-browser`
- 先捕获真实请求，再基于真实参数、Cookie、Header、路由去构造 payload
- 每个验证结果都要回填到白盒候选上，标记下列状态之一。**状态判定口径统一以 `common/closure-verification.md` 为准**（read_file 读取后据其分级），下列标签与其 confirmed/suspected 语义一一对齐。**括号内是写入报告 jsonl `status` 字段时必须落成的取值**——报告模板不认识 `suspected` 这个词，直接写它会被静默丢条；`suspected` 一律落为 `needs_review`（进正文待复核，绝不丢条）。**注意：graybox 下 `security-code-analysis` 产出的 pure-static `confirmed` /“数据流已确认”只表示白盒证据更强，回填到本阶段时一律按下列动态口径重新裁决，不得直接照抄成报告 `confirmed`。**
  - `已动态复现`（status=`confirmed`）—— 等同 closure-verification 的 `confirmed`：必须有**可观测的真实效果**（命令回显/带外回连/数据回读/写操作回读生效等）并能在流量或日志中回溯。**"请求成功、状态码变化、单次报错、响应为空、执行无异常"都不构成复现**——这些只到 `suspected`。**涉及不可逆动作（删除/覆盖/批量改）时，"写操作回读生效"必须走哨兵自证或非破坏差分，禁止对真实业务数据执行破坏动作**（见 `common/closure-verification.md`《破坏性 / 不可逆动作的闭环边界》）。
  - `仅静态候选`（status=`needs_review`）—— 只有白盒代码线索，或虽已做到"白盒可达性已确认"但**尚无动态可观测效果**，语义上对应 `suspected`。**但这不是默认终态**：按 closure-verification 三档判据，只有当**没有可用验证路径**（黑盒目标不可达、白盒数据流可达性也已做到能力边界）时才合法停在此。**只要还有没走的验证路径——有可达目标却没发真实请求、或静态可达性还没确认——该候选就是"未闭环"，必须在 `coverage_checklist` 落 `uncovered` 推回去验证，不得直接落 `仅静态候选` 交差。**
  - `数据流不可达`（status=`not_vulnerable`）—— 数据流分析证明 source 到不了 sink，对应 `not vulnerable`。
  - `黑盒未命中，待人工复核`（status=`needs_review`）—— 发了请求但既未复现也未排除，语义上对应 `suspected`，须注明缺失的验证环节。

## 输出要求

- 最终输出前加载 `result-with-file`
- 每条发现至少包含：
  - 白盒证据：文件:行 或调用链
  - 黑盒证据：请求/响应/关键 payload/截图
  - 当前状态：已复现、候选、不可达、待复核

### 通用闸门（所有发现都必过，不分是否走了专项 skill）

专项 skill（command-injection、sql-injection 等）各自的闭环 bar 只在它被加载时生效；**很多发现（尤其白盒直出的 RCE/反序列化/框架注入）不会经过任何专项 skill，必须由本编排层兜底**。每条发现写入前逐条核：

- **标 `已动态复现` / `confirmed` 必须挂可观测效果证据**：贴出真实捕获的命令回显 / 带外回连记录 / 数据回读 / 副作用，并能在流量或日志回溯到对应 call。挂不上 → 一律降级为 `suspected`（仅静态候选 / 待人工复核），写入报告时落 `status=needs_review` 留在正文，**降级 ≠ 删除**，不得自报 confirmed。**不可逆动作（删除/覆盖/批量改）的可观测效果只接受哨兵自证或非破坏差分，禁止靠对真实业务数据执行破坏动作来挂证据。**
- **"执行无异常""响应为空""没报错"不是证据**：绕过了某个校验 ≠ 危害已发生（如绕过设计期黑名单 ≠ 运行期命令真的执行）。这类只能写到"绕过了 X 校验（file:line）"为止，危害是否落地未验证就如实标 `suspected`（落 `status=needs_review`）。
- **禁止编造证据**（取证完整性，详见 `common/closure-verification.md`）：引用的源码 / 类名 / 字段 / 黑名单内容必须出自你实际读过的文件并标 `file:line`；反编译或闭源产物标注来源与不确定性；贴的请求/响应必须是真实发出且捕获到的，不得贴理想化的、从未发送的请求当 POC。

## 约束

- 不要把“没有复现”直接等价为“没有风险”
- 不要跳过真实请求就下动态结论
- 不要在没有候选信号时把所有专项 skill 一次性展开
- 不要自报 `confirmed` 而不挂可观测效果证据；不要用编造或理想化的代码/请求/响应充当证据
