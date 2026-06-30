# DocHarbor AI Agent 架构重构设计文档

## 1. 文档目标

本文定义 DocHarbor AI 问答 Agent 的架构重构方案。

本次重构要解决的不是某一个检索关键词缺失，而是当前问答链路缺少“问题目标确认、证据契约、迭代检索和答案校验”的闭环，导致系统遇到新问题时只能继续叠加关键词、路径权重和 prompt 规则。

重构后的目标是：

- 从线性 RAG 流水线升级为后端受控的多阶段 Agent 工作流。
- 让每类问题先形成可验证的任务目标，再按目标收集证据。
- 回答前必须检查证据是否满足任务契约，不满足时继续检索或输出明确缺口。
- 回答后必须做引用、证据覆盖和错误模式校验，不合格时自动重试或降级。
- 保持 DocHarbor 的只读边界：Agent 不执行 shell、不执行 SQL、不修改 Git，只能调用后端注册的只读工具。
- 不在通用检索逻辑中硬编码业务名词；需要语义扩展时由模型基于当前问题和已收集证据现场生成。

## 2. 当前问题

### 2.1 现有链路

当前问答链路本质上是一次线性流程：

```text
用户问题
  -> 追问上下文拼接
  -> query planner 生成检索词
  -> keyword / path / content 检索
  -> 截取 evidence snippets
  -> 模型生成答案
  -> 本地引用数量校验
```

这个流程能处理简单接口问答，但对需要多步确认的问题不稳定。

### 2.2 典型失败模式

#### 2.2.1 任务目标没有被结构化

用户问“我想在数据库里直接修改游戏的价格”时，系统应该先识别为：

```text
目标：给测试用途的数据库直接修改方案
需要证据：表名、字段名、价格单位、WHERE 定位条件、读取链路、刷新或缓存风险
```

当前链路只把它当作普通文档问答，无法判断“现有证据是否足以给 SQL 示例”。

#### 2.2.2 证据不足不会触发二次检索

第一轮检索如果只拿到“读取链路”和“缺价补偿”，系统不会意识到还缺表模型和字段定义，而是直接让模型回答。

正确行为应该是：

```text
已找到读取链路 -> 检查契约发现缺表结构 -> 追加检索模型 / migration / TableName / column -> 再回答
```

#### 2.2.3 回答模型承担了过多职责

当前回答模型同时承担：

- 判断用户真正意图。
- 判断证据是否充分。
- 从噪声中挑选可信证据。
- 组织最终答案。
- 决定是否写“未确认”。

这些职责混在一个 prompt 里，导致模型容易把“没有官方 UPDATE 语句”误判为“不能回答测试 SQL 示例”。

#### 2.2.4 检索排序被迫堆规则

为了解决单个事故，系统会继续增加：

- 某类路径加权。
- 某类测试文件降权。
- 某些词形变体归一。
- 某段 prompt 规则。

这些补丁短期有效，但不能保证下一类任务不再失败。

#### 2.2.5 诊断通过不代表语义正确

当前 run 级校验主要检查是否有引用、是否有证据数量、是否成功调用模型。它无法判断：

- 证据是否覆盖了用户目标。
- 答案是否错误拒绝。
- 答案是否把测试夹具当业务事实。
- 答案是否把分支候选当智能最新。

## 3. 设计原则

1. **后端控制流程**：整体执行顺序由后端状态机控制，模型只在受限节点中做结构化判断或文本生成。
2. **先契约后回答**：每个任务先生成 evidence contract，再检索和校验证据。
3. **证据闭环**：证据不满足契约时必须继续检索、缩小问题或输出明确缺口，不能直接让回答模型猜。
4. **泛化机制优先**：检索增强使用任务类型、文件类型、符号类型、结构化字段和模型现场生成词，不硬编码业务名词。
5. **引用是最低要求**：有引用只能说明答案有来源，不代表答案满足问题目标。
6. **测试和生产语义分离**：非测试问题默认不能把 `_test`、fixtures、mock 当业务事实；测试问题可以显式引用它们。
7. **可复盘可重放**：每次 Agent run 必须保存任务帧、契约、检索轮次、证据包、校验报告和重试原因。
8. **兼容现有接口**：首期不打破现有 `/api/ai/sessions/:id/messages/stream` 和诊断接口，先扩展 run/step 数据结构。

## 4. 非目标

- 不让 Agent 直接执行 SQL、shell、Git 写操作或外部网络请求。
- 不引入业务专用词典解决通用检索问题。
- 不在首期引入独立向量数据库作为强依赖。
- 不把所有仓库内容一次性发给模型。
- 不要求前端用户理解 Agent 内部节点。
- 不在本次重构中实现代码修改、提交、MR 或自动修复能力。

## 5. 目标架构

### 5.1 总体流程

```text
用户问题
  -> Run Coordinator
  -> Task Framing Agent
  -> Evidence Contract Builder
  -> Retrieval Orchestrator
      -> Query Planner
      -> Structured Search Tools
      -> Evidence Curator
      -> Contract Checker
      -> 可选：下一轮检索
  -> Answer Composer
  -> Answer Verifier
      -> 通过：保存答案
      -> 不通过：按失败原因回到检索或重写
```

### 5.2 节点职责

| 节点 | 职责 | 输入 | 输出 | 是否调用模型 |
| --- | --- | --- | --- | --- |
| Run Coordinator | 管理状态机、轮次、超时、持久化和 SSE 事件 | session、question、scope、active config | run state | 否 |
| Task Framing Agent | 识别问题类型、用户目标、约束和回答形态 | question、follow-up context | task frame | 是 |
| Evidence Contract Builder | 生成本任务回答前必须具备的证据项 | task frame | evidence contract | 可选 |
| Query Planner | 为当前缺口生成检索计划和检索词 | task frame、contract gaps、已有证据 | search plan | 是 |
| Structured Search Tools | 执行只读检索：路径、内容、符号、分支、文件片段 | search plan | raw evidence | 否 |
| Evidence Curator | 去重、降噪、分组、标注分支和证据类型 | raw evidence | curated evidence bundle | 否 |
| Contract Checker | 判断证据是否满足契约，输出缺口 | contract、evidence bundle | coverage report | 可选 |
| Answer Composer | 基于合格证据组织最终答案 | question、task frame、evidence bundle | draft answer | 是 |
| Answer Verifier | 校验引用、覆盖度、错误拒绝、证据污染和分支口径 | draft answer、contract、evidence | verification report | 可选 |

## 6. Task Frame

Task Frame 是 Agent 对当前问题的结构化理解，必须在检索前生成并保存。

### 6.1 字段定义

```json
{
  "intent": "database_direct_update_for_test",
  "user_goal": "给出测试用途的数据库直接改价方案",
  "answer_shape": "steps_with_sql_example",
  "scope_strategy": "global_first",
  "target_artifacts": ["table", "orm_model", "read_path", "field_units", "side_effects"],
  "must_not": ["invent_business_names", "execute_sql", "treat_test_fixtures_as_runtime_fact"],
  "known_terms": ["用户原问题中的显式词"],
  "generated_terms": ["模型现场生成的检索词"],
  "follow_up": {
    "is_follow_up": true,
    "previous_paths": ["..."],
    "previous_topic_summary": "..."
  }
}
```

### 6.2 首期意图枚举

| intent | 场景 | answer_shape |
| --- | --- | --- |
| `api_integration` | 前端问接口、参数、返回、错误码 | interface_table |
| `database_direct_update_for_test` | 用户明确要求数据库、SQL、字段、直接修改数据用于测试 | sql_steps_with_risk |
| `code_path_explanation` | 用户问某行为在哪段代码、调用链如何走 | call_chain |
| `cross_service_impact` | 用户问影响哪些服务、跨仓库链路 | service_grouped_chain |
| `branch_lookup` | 用户问某功能在哪个分支、新接口是否合入 | branch_candidates |
| `document_qa` | 普通文档事实问答 | evidence_summary |
| `diagnostics` | 用户问为什么 AI 回答错、检索是否命中 | run_analysis |

意图枚举是通用任务类型，不包含业务名词。

## 7. Evidence Contract

Evidence Contract 定义“回答前必须具备哪些证据”。它不是 prompt 文案，而是后端可执行的覆盖度检查对象。

### 7.1 契约结构

```json
{
  "contract_id": "database_direct_update_for_test.v1",
  "required": [
    {
      "key": "table_identity",
      "description": "表名或 ORM TableName",
      "accepted_evidence_types": ["orm_model", "migration_sql", "schema_doc"]
    },
    {
      "key": "update_fields",
      "description": "可修改字段和字段单位",
      "accepted_evidence_types": ["orm_model", "migration_sql", "read_path"]
    }
  ],
  "recommended": [
    {
      "key": "side_effects",
      "description": "缓存、索引、异步补偿或刷新风险"
    }
  ],
  "forbidden": [
    "test_fixture_as_runtime_fact",
    "unsupported_exact_value",
    "unreferenced_sql"
  ]
}
```

### 7.2 数据库直改类契约

用于用户明确要求“数据库里直接改、SQL、字段、表、测试数据”的问题。

| 证据项 | 必需 | 合格证据 | 不合格证据 |
| --- | --- | --- | --- |
| 表身份 | 是 | ORM `TableName()`、migration `CREATE/ALTER TABLE`、明确 schema 文档 | 模型猜测的复数表名 |
| 字段名 | 是 | ORM column tag、migration 字段定义、读取 SQL/GORM where/select | 仅中文描述“价格字段” |
| 字段单位 | 是，涉及金额时 | 字段名、注释、换算代码、调用方除以 100 等 | 只看到 `price` 但不知道元/分 |
| 定位条件 | 是 | 读取链路中的 `WHERE` 条件、唯一索引、业务 code 解析 | 只知道表名 |
| 当前读取链路 | 是 | 查询函数、DAO/GORM 调用、handler/usecase 调用 | 只有 migration |
| 验证方法 | 建议 | SELECT 校验、业务读取入口、索引刷新说明 | 空泛“自行验证” |
| 副作用 | 建议 | 缓存、ES、生成商品、补偿任务、分支候选标注 | 无引用风险提示 |

如果必需项缺失，Answer Composer 不能给确定 SQL，只能输出缺口和下一步应检索或确认的内容。

### 7.3 接口接入类契约

| 证据项 | 必需 | 合格证据 |
| --- | --- | --- |
| 服务或仓库候选 | 是 | 路由、proto、handler、前端 client、文档标题 |
| 接口路径或 RPC | 是 | route 注册、OpenAPI path、proto service/rpc |
| 请求字段 | 是 | request struct、binding tag、proto message、OpenAPI schema |
| 响应字段 | 是 | response struct、proto message、handler 返回 |
| 错误码和业务约束 | 建议 | const、error mapping、validation、service/usecase |
| 分支状态 | 涉及功能分支时必需 | branch、commit、source_scope |

## 8. 迭代检索设计

### 8.1 检索轮次

每次 run 最多执行 3 轮检索：

| 轮次 | 目标 | 输入 |
| --- | --- | --- |
| R1 | 根据 Task Frame 做初始召回 | 用户问题、追问上下文、模型生成 terms |
| R2 | 补齐契约缺口 | Contract Checker 输出的 missing keys |
| R3 | 解决冲突或补强弱证据 | conflict report、low-confidence evidence |

超过轮次仍不满足契约时，答案必须明确写“未确认项”，并列出缺哪些证据。

### 8.2 检索计划

Query Planner 输出结构化计划：

```json
{
  "round": 2,
  "reason": "missing table_identity and field_units",
  "searches": [
    {
      "tool": "symbol_search",
      "query": "SteamGamePrice TableName price_in_cents",
      "file_types": ["go", "sql"],
      "path_hints": ["models", "version/mysql", "db", "mysql"]
    },
    {
      "tool": "content_search",
      "query": "price_in_cents_with_discount country_code package_id is_sell"
    }
  ]
}
```

`path_hints` 是通用路径类型，不是业务词典。业务名词只能来自用户问题、上一轮证据或模型现场生成 terms。

### 8.3 证据类型标注

Evidence Curator 需要给每条证据打标：

| evidence_type | 判定来源 |
| --- | --- |
| `route` | router、route、handler 注册、OpenAPI path |
| `proto` | `.proto` service/rpc/message |
| `handler` | controller、handler、endpoint 实现 |
| `request_response_type` | struct、interface、message、DTO |
| `orm_model` | GORM model、TableName、column tag |
| `migration_sql` | CREATE TABLE、ALTER TABLE、索引变更 |
| `read_path` | DAO、GORM query、SQL select、repository |
| `write_path` | update/save/create 代码路径 |
| `test_fixture` | `_test`、fixtures、testdata、mock |
| `doc` | README、Markdown、项目指导文件 |

Answer Composer 只能依据 evidence_type 和 contract coverage 组织答案，不直接面对未分类的大量片段。

## 9. Evidence Curator

### 9.1 降噪规则

| 规则 | 行为 |
| --- | --- |
| 非测试问题命中测试文件 | 默认降权，不作为必需证据项的唯一来源 |
| 测试问题命中测试文件 | 正常使用，并标注为测试证据 |
| 同一文件多分支重复 | 智能最新优先；功能分支保留但标注 |
| 同一代码片段跨候选分支重复 | 合并为一组，列出分支 |
| 低信息密度片段 | 降权，例如只有注释中泛泛出现关键词 |
| 当前仓库为 DocHarbor 且问题问业务服务 | 不能因为测试夹具高匹配就当业务事实 |

这些规则都基于通用文件类型、来源类型和 scope，不依赖业务名词。

### 9.2 证据包结构

```json
{
  "bundle_id": "run-10-round-2",
  "coverage": {
    "table_identity": "covered",
    "update_fields": "covered",
    "field_units": "covered",
    "where_conditions": "covered",
    "side_effects": "partial"
  },
  "groups": [
    {
      "key": "table_identity",
      "evidence_ids": [1, 2],
      "summary": "ORM model confirms table and columns"
    }
  ],
  "excluded": [
    {
      "evidence_id": 9,
      "reason": "test_fixture_for_non_test_task"
    }
  ]
}
```

## 10. Answer Composer

Answer Composer 不再负责判断证据是否足够。它只接收：

- 原始用户问题。
- Task Frame。
- 已通过或部分通过的 Evidence Contract。
- Evidence Bundle。
- Answer Policy。

### 10.1 回答策略

| coverage | 策略 |
| --- | --- |
| required 全部 covered | 给确定答案，每个关键结论带引用 |
| required 部分 missing | 不给确定操作步骤，列出缺口和已确认事实 |
| recommended missing | 可以回答，但在风险或补偿缺口中说明 |
| 存在 conflict | 先说明冲突来源，不强行合并 |

### 10.2 数据库直改类回答形态

```text
结论

用于测试可以直接改 [表名] 的 [字段]，但需要按读取链路确认定位条件。

先查当前记录：

SELECT ...

测试改价：

UPDATE ...

验证：

SELECT ...

风险和补偿：

- ...

未确认项：

- ...
```

SQL 中必须使用占位符，不生成真实生产值，不声称已经执行。

## 11. Answer Verifier

Answer Verifier 是回答后的质量门禁。

### 11.1 校验项

| 校验项 | 失败例子 | 动作 |
| --- | --- | --- |
| 引用覆盖 | SQL 字段没有引用 | 回到 Answer Composer 重写 |
| 契约覆盖 | 缺表名却给 UPDATE | 回到 Retrieval R2 |
| 错误拒绝 | 证据足够却回答“未确认，不能操作” | 重写答案 |
| 证据污染 | 非测试任务引用 `_test.go` 作为业务事实 | 回到 Evidence Curator |
| 分支口径 | 功能分支代码当作智能最新 | 重写并标注分支 |
| 未授权行为 | 声称已执行 SQL、已修改数据库 | 阻断并重写 |
| 编造服务 | 答案出现证据中没有的服务名 | 阻断并重写 |

### 11.2 输出结构

```json
{
  "status": "failed",
  "reason": "unsupported_refusal",
  "details": [
    "contract required fields are covered, but answer says no SQL can be provided"
  ],
  "next_action": "rewrite_answer"
}
```

## 12. 状态机

```text
created
  -> frame_task
  -> build_contract
  -> retrieval_round
  -> curate_evidence
  -> check_contract
      -> missing_required && round < max_rounds -> retrieval_round
      -> missing_required && round == max_rounds -> compose_partial_answer
      -> covered -> compose_answer
  -> verify_answer
      -> pass -> completed
      -> rewriteable_failure -> compose_answer
      -> retrieval_needed && round < max_rounds -> retrieval_round
      -> hard_failure -> completed_with_gaps
```

### 12.1 终止条件

| 条件 | 结果 |
| --- | --- |
| 答案通过 verifier | `completed` |
| 达到最大检索轮次但缺必需证据 | `completed_with_gaps` |
| provider 全部失败 | `model_failed_local_summary` |
| 用户取消或连接断开 | 保存 partial run |
| 工具调用失败但可继续 | 记录 step error，进入降级路径 |

## 13. 数据模型调整

首期优先复用现有 `ai_agent_runs`、`ai_agent_steps`、`ai_message_citations`，增加 JSON 字段或 step 输出，不急于拆新表。

### 13.1 ai_agent_runs

建议新增或复用 JSON 字段：

| 字段 | 用途 |
| --- | --- |
| `task_frame_json` | 保存 Task Frame |
| `evidence_contract_json` | 保存当前任务契约 |
| `contract_coverage_json` | 保存最终覆盖度 |
| `verification_report_json` | 从引用数量校验升级为 Answer Verifier 报告 |

如果短期不改表结构，可以先把这些内容写入 `checkpoint_json` 和 step `output_json`，但诊断接口必须能展示。

### 13.2 ai_agent_steps

新增标准 agent step 名称：

| agent_name | step_type | 说明 |
| --- | --- | --- |
| `task_framer` | `model_call` | 输出 Task Frame |
| `contract_builder` | `deterministic` / `model_call` | 输出 Evidence Contract |
| `query_planner` | `model_call` | 输出检索计划 |
| `retrieval` | `tool_call` | 执行检索 |
| `evidence_curator` | `deterministic` | 证据去重、标注和排序 |
| `contract_checker` | `deterministic` / `model_call` | 覆盖度判断 |
| `answer_composer` | `model_call` | 生成答案 |
| `answer_verifier` | `model_call` / `deterministic` | 答案门禁 |

## 14. API 和前端诊断

### 14.1 现有问答接口

保留：

```text
POST /api/ai/sessions/:sessionID/messages/stream
POST /api/ai/sessions/:sessionID/messages
```

响应事件新增但兼容旧前端：

| SSE event | 说明 |
| --- | --- |
| `task_frame` | 当前问题结构化理解 |
| `contract` | 本次 evidence contract 摘要 |
| `retrieval_round` | 第几轮检索、目标缺口 |
| `coverage` | 证据覆盖度 |
| `verification` | 回答校验结果 |

前端首期可以只在诊断面板展示这些事件，不影响普通问答页。

### 14.2 诊断接口

`/api/access/ai/diagnostics/runs/:id` 应展示：

- Task Frame。
- Evidence Contract。
- 每轮检索计划。
- 每轮 evidence bundle。
- 被排除证据和原因。
- Contract coverage。
- Answer Verifier 报告。

诊断接口仍不能返回 provider secret、API key、仓库凭据或 token 签名密钥。

## 15. 实施计划

### 阶段一：契约和诊断先行

目标：不大改现有检索工具，先把任务目标和证据覆盖度显性化。

改动：

- 新增 `task_framer` step，输出 Task Frame。
- 新增确定性 `contract_builder`，先支持 `api_integration`、`database_direct_update_for_test`、`document_qa`。
- 新增 `contract_checker`，基于 evidence_type 和关键字段做覆盖度判断。
- 诊断接口展示 Task Frame、Contract、Coverage。

验收：

- “数据库里直接修改价格用于测试”能识别为数据库直改类任务。
- 如果缺表名或字段单位，系统不会给确定 UPDATE。
- 诊断详情能看到缺的是哪一个 contract key。

### 阶段二：Evidence Curator

目标：把噪声治理从临时权重规则提升为证据治理层。

改动：

- 为证据片段增加 `evidence_type`、`source_reliability`、`excluded_reason`。
- 非测试任务默认降权测试夹具。
- 同文件同片段跨分支合并。
- 证据按 contract key 分组。

验收：

- 非测试业务问题不会把 DocHarbor 自己的 `_test.go` 夹具排到主要证据。
- 测试类问题仍能正常使用测试文件。
- 诊断面板能解释某条证据为什么被排除或降权。

### 阶段三：迭代检索

目标：让系统能根据契约缺口自动补检索。

改动：

- `query_planner` 输入增加 `missing_contract_keys` 和已有 evidence summary。
- `retrieval_round` 最多 3 轮。
- 每轮保存 search plan 和 coverage delta。

验收：

- 第一轮只找到读取链路时，第二轮能补找模型、表名、字段。
- 达到最大轮次仍缺证据时，答案明确列缺口，不给假确定结论。

### 阶段四：Answer Verifier

目标：避免格式正确但语义错误的答案落库。

改动：

- 新增 verifier prompt 和确定性引用检查。
- 对 unsupported refusal、test fixture pollution、branch mislabel 等错误给出 next action。
- 支持一次自动重写；重写仍失败则输出带缺口的保守答案。

验收：

- 证据足够时，回答不能再说“没有官方 UPDATE 示例所以未确认”。
- 答案不能声称执行过 SQL。
- 每个关键 SQL 字段和 WHERE 条件都有引用。

### 阶段五：评估集和回放

目标：把线上事故沉淀为可重复评估。

改动：

- 新增 `internal/app/ai_eval_test.go` 或等价 fixture。
- 保存典型失败问题、期望 intent、required contract keys、禁止回答模式。
- 支持从 diagnostics run 回放 Task Frame 和检索计划。

验收：

- 覆盖数据库直改、接口接入、跨服务影响、功能分支、追问上下文五类问题。
- 每次改检索或 prompt 必须跑评估集。

## 16. 与当前止血修复的关系

当前针对单个事故的检索改动可以保留为过渡层，但它不应继续扩张成业务规则库。

| 当前补丁类型 | 长期归属 |
| --- | --- |
| 数据库类问题路径加权 | Task Frame + Search Plan 中的通用 path_hints |
| `_test.go` 降权 | Evidence Curator 的 source reliability |
| 单复数归一 | Query Planner / tokenizer 的通用词形归一 |
| SQL 回答 prompt | Answer Policy 中的任务形态规则 |

后续新增问题时，优先补 contract、curator、verifier 或评估用例，而不是直接补业务关键词。

## 17. 验收用例

### 17.1 数据库直改用于测试

问题：

```text
我想在数据库里直接修改游戏的价格
```

期望：

- Task Frame intent 为 `database_direct_update_for_test`。
- Evidence Contract 包含表身份、字段、单位、定位条件、读取链路、副作用。
- 证据优先包含 ORM model、migration、读取函数。
- 非测试问题不能把 `_test.go` 作为核心业务证据。
- 如果证据确认字段单位和 WHERE 条件，答案给带占位符的 SELECT/UPDATE。
- 如果证据缺字段单位，答案不生成确定 UPDATE。

### 17.2 接口接入

问题：

```text
下单页面需要接哪些接口？请求参数和返回字段是什么？
```

期望：

- Task Frame intent 为 `api_integration`。
- 先给候选服务。
- 每个接口路径、请求字段、响应字段必须有引用。
- 缺错误码时列为未确认，不编造。

### 17.3 功能分支候选

问题：

```text
库存锁定的新接口现在在哪个分支？
```

期望：

- Task Frame intent 为 `branch_lookup`。
- 命中功能分支时标注 branch、commit、source_scope。
- 不把功能分支候选写成智能最新事实。

### 17.4 追问上下文

问题：

```text
上一轮：用户注销接口在哪里？
追问：参数是什么？
```

期望：

- Task Frame 继承上一轮主题和引用路径。
- 检索优先围绕上一轮候选服务和路径。
- 不泛化到其他服务。

### 17.5 回答错误排查

问题：

```text
为什么刚才 AI 没回答出数据库改价 SQL？
```

期望：

- Task Frame intent 为 `diagnostics`。
- 回答基于 run 的 task frame、search plan、evidence bundle、coverage 和 verifier。
- 能指出是任务识别、检索缺口、证据污染还是回答策略问题。

## 18. 风险和补偿

| 风险 | 影响 | 补偿 |
| --- | --- | --- |
| Agent 轮次增加导致延迟上升 | 用户等待变长 | 首期最多 3 轮；SSE 展示阶段进度；低复杂度问题一轮结束 |
| 模型生成 Task Frame 错误 | 检索方向偏 | 保留确定性 intent fallback；Verifier 可触发重新 framing |
| Contract 过严 | 过多“未确认” | 记录 missing key；按评估集调整 required / recommended |
| Contract 过松 | 错误答案仍通过 | 增加事故回放和 verifier 禁止模式 |
| 诊断数据膨胀 | SQLite 增长 | 对 step 大字段做截断；保留摘要和引用，原文按现有 citation 存储 |
| 兼容旧前端 | 新事件旧前端不识别 | SSE 新事件只作为增量；旧事件保留 |

## 19. 最终判断

DocHarbor AI Agent 的核心质量边界不应该建立在“检索词够不够聪明”上，而应该建立在：

```text
用户目标是否被正确结构化
证据契约是否被满足
答案是否通过可复盘校验
```

只有把这三层显式化，后续才能稳定处理接口接入、数据库测试操作、跨服务影响、功能分支追踪和回答排查，而不是为每个线上失败样本继续叠加临时启发式规则。
