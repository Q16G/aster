# 业务模型细则（business-model）

> 五张模型之一「业务模型」的建模细则。业务模型是语义型漏洞（越权 / 逻辑 / 状态机 / RBAC / 影响放大）的判据锚点——没有它，逻辑类漏洞只能靠临场直觉。本模型描述"这个系统在业务上做什么、有哪些不能被破坏的规矩"，**不做漏洞判定**，成因与利用由对应 audit skill 消费。

## 建模四要素

按下列四个方面建模，每一项都要落到源码事实（实体类 / service 方法 / 状态字段 / 校验位置），可追溯。

### 1. 核心业务实体与关系

- **识别信号**：领域模型类 / 表 schema / DTO；实体间外键与引用（`order.user_id`、`payment.order_id`、`member.workspace_id`）。
- **须产出**：核心实体清单 + 实体间归属 / 从属关系（谁属于谁、谁引用谁）。与「认证与权限模型」的归属字段对照表交叉引用——归属字段是越权判定的基准，业务关系说明为什么这条归属重要。

### 2. 关键业务流程与状态机

- **识别信号**：跨多个 service 方法 / 多个请求完成的流程（下单→支付→发货、注册→激活→登录、发起提现→审核→打款、邀请→接受→授权）；实体上的 `status` / `state` / `stage` 字段及其流转代码。
- **须产出**：
  - 关键流程的**步骤序列**（每步对应的入口点 / service 方法）。
  - 状态机的**合法迁移图**（允许 A→B，不允许 A→C）——状态机绕过 / 工作流跳步漏洞的判据。
- **判据**：只建模影响放大的流程（金钱 / 提权 / 数据生命周期 / 账户接管），不要把所有 CRUD 都当关键流程。

### 3. 业务不变量（invariants）

系统在任何时刻都不应被破坏的规矩——逻辑漏洞的判定基准。按类枚举本项目实际存在的不变量：

- **数值约束**：金额 / 余额 / 库存 / 积分非负；单价 × 数量 = 总价；折扣不超上限。
- **状态前提**：某操作只在特定前置状态可执行（已支付才能发货、未使用才能核销）。
- **配额 / 频率上限**：每人限领一次、每日限提现 N 次、优惠券每单一张。
- **归属一致性**：操作的资源必须属于当前主体 / 当前租户；跨租户 / 跨用户引用非法。
- **唯一性 / 幂等**：同一订单不能重复支付 / 重复退款；回调幂等。

**须产出**：不变量清单，每条标注"在代码哪里被校验 / 是否有校验"。校验缺失或校验点可绕过的，是下游逻辑 / 越权 / 竞态维度的高价值候选（本模型只标事实，不下判定）。

### 4. 高价值资产与影响放大链路

- **识别信号**：资金 / 凭据 / 个人敏感数据 / 管理能力 / 可外泄数据所在的实体与端点。
- **须产出**：高价值资产清单 + 触达它们的业务链路——作为**信息**供下游各维度识别影响放大点（不据此排测试优先级、不据此取舍要不要测某接口）。

## 与其它模型的关系

- 上承「入口点模型」（流程步骤 = 入口点序列）与「认证与权限模型」（归属字段 = 归属一致性不变量的基准）。
- 下接「全局威胁模型」：业务不变量 + 高价值资产是攻击面映射汇总的核心输入。
- 供下游语义漏洞 skill（`business-logic-auth-review` 等）直接消费：不变量清单 = 它们的检查基准。

## 产物落库

写入机器读 `project-framework-analysis.jsonl`（`kind: business`，`subkind ∈ entity | flow | state_machine | invariant`）与人读 `shared/models/business-model.md`。示例行：

```json
{"kind":"business","subkind":"entity","name":"Order","detail":"归属 user_id；引用 payment_id / 多个 order_item","file_location":"Order.java:12"}
{"kind":"business","subkind":"flow","name":"下单支付发货","detail":"POST /order/create → POST /pay/callback → POST /order/{id}/ship","file_location":"OrderController.java:40; PayController.java:88"}
{"kind":"business","subkind":"state_machine","name":"Order.status","detail":"created→paid→shipped→done；不允许 created→shipped","file_location":"OrderService.java:120"}
{"kind":"business","subkind":"invariant","name":"提现金额<=余额且>0","detail":"WithdrawService.apply 未校验上界，仅校验>0","file_location":"WithdrawService.java:55"}
```

字段 `detail` 落事实、`file_location` 落证据位置；不确定的不变量校验状态写明"未见校验"而非省略。
