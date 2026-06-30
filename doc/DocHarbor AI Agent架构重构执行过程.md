# DocHarbor AI Agent 架构重构执行过程

## 1. 文档目标

本文基于 [DocHarbor AI Agent架构重构设计文档.md](DocHarbor%20AI%20Agent架构重构设计文档.md)，把架构方案拆成可逐步落地、可独立验收的执行过程。

每个过程都必须回答三个问题：

- 目标：本过程要解决什么明确问题，交付后系统能力边界是什么。
- 实现：改哪些模块、引入哪些数据结构、如何接入现有链路。
- 验收：用什么测试、诊断输出或人工检查证明本过程完成。

## 2. 执行边界

- 所属服务：`doc-harbor`。
- 后端主入口：`internal/app/ai.go` 中的 `askAIQuestion`、`askAIQuestionStream`、`retrieveAIEvidence`、`buildAIChatMessages`、`finishAIRun` 和诊断相关函数。
- 数据模型入口：`internal/app/models.go`、`internal/app/db.go` 中的 `AIAgentRun`、`AIAgentStep`、`ai_agent_runs`、`ai_agent_steps`、`ai_message_citations`。
- 前端入口：`src/types.ts`、`src/api.ts`、`src/App.vue`。
- 测试入口：`internal/app/ai_settings_test.go`，后续可新增 `internal/app/ai_agent_eval_test.go`。

本次重构不改变以下边界：

- 不让 Agent 执行 shell、SQL、Git 写操作或外部网络请求。
- 不在通用检索逻辑中硬编码业务名词。
- 不破坏现有问答接口：
  - `POST /api/ai/sessions/:sessionID/messages`
  - `POST /api/ai/sessions/:sessionID/messages/stream`
- 不泄露 provider secret、API key、仓库凭据或 token 签名密钥。
- 首期优先复用 `checkpoint_json`、`verification_report_json` 和 `ai_agent_steps.output_json`，只有当前端或查询能力明确需要时再增加新表字段。

## 3. 当前基线

当前 AI 问答链路已经具备以下基础：

- 运行记录：`ai_agent_runs` 可保存 intent、retrieval plan、verification report、provider failover。
- 步骤记录：`ai_agent_steps` 可保存 agent_name、step_type、input_json、output_json。
- 证据记录：`ai_message_citations` 保存文件路径、分支、commit、片段和分数。
- 检索能力：`retrieveAIEvidence` 支持 smart latest、branch candidate、当前文件上下文和跨仓库候选。
- 模型路由：`callRoutedAIModel` 和 `callRoutedAIModelStream` 已支持多供应商优先级和失败转移。
- 诊断接口：`/api/access/ai/diagnostics/runs/:id` 已能返回 run、steps、候选服务、引用和数据源。

当前缺口：

- `run.intent` 仍来自简单分类，不能表达完整 Task Frame。
- 证据只有排序和引用，没有 evidence type、contract key、excluded reason。
- `verification_report_json` 主要是引用数量和本地守卫，不能判断证据是否覆盖任务目标。
- SSE 流式回答会直接向前端输出模型增量，后续如果加入 Answer Verifier，需要先缓冲或分阶段释放答案，避免错误答案已经展示后再回滚。
- 诊断 step 当前不返回 `input_json/output_json`，如果要展示 Task Frame、Contract、Coverage，需要增加安全摘要字段或经过脱敏后的 step payload。

## 4. 总体拆分

| 过程 | 名称 | 依赖 | 可独立上线 |
| --- | --- | --- | --- |
| 0 | 基线保护和开关策略 | 无 | 是 |
| 1 | Agent 数据契约和内部类型 | 0 | 是 |
| 2 | Task Framing | 1 | 是 |
| 3 | Evidence Contract Builder | 2 | 是 |
| 4 | Evidence Curator 和证据类型标注 | 1、3 | 是 |
| 5 | Contract Checker 和诊断可视化 | 3、4 | 是 |
| 6 | 迭代检索 Orchestrator | 5 | 是 |
| 7 | Answer Policy 和 Answer Composer | 5 | 是 |
| 8 | Answer Verifier、重写和降级 | 6、7 | 是 |
| 9 | SSE、API 和前端诊断闭环 | 2-8 | 是 |
| 10 | 评估集、回放和质量门禁 | 2-8 | 是 |

过程 2 到过程 5 可以先以 shadow mode 写入 run/step/diagnostics，不改变最终回答。过程 7 和过程 8 开始接管最终答案生成。

## 5. 过程 0：基线保护和开关策略

### 目标

在正式改链路前固化当前行为和安全边界，保证后续每个过程可以单独验证、单独回滚，不影响旧接口和已存在的供应商配置能力。

### 实现

- 梳理当前 AI 问答主路径：
  - 同步路径：`askAIQuestion`。
  - SSE 路径：`askAIQuestionStream`。
  - 检索路径：`retrieveAIEvidence`、`searchRepoSmartLatestEvidence`、`searchRepoRefEvidence`。
  - 模型路径：`generateAIAnswer`、`callRoutedAIModel`、`callRoutedAIModelStream`。
  - 诊断路径：`getAIDiagnosticsRunDetail`、`sanitizeAIDiagnosticsSteps`。
- 新增内部 workflow version 概念，建议先放在 run checkpoint 中：
  - `agent_workflow_version: "v2-shadow"`：只记录 Task Frame、Contract、Coverage，不改变回答。
  - `agent_workflow_version: "v2-active"`：由新 Agent 工作流接管回答。
- 明确失败降级：
  - Task Frame 失败：回退到旧 intent 和旧检索。
  - Contract Builder 失败：记录 step error，走旧回答。
  - Curator 或 Checker 失败：记录诊断并走旧回答。
  - Verifier 失败：输出保守答案或本地证据摘要。
- 保留现有接口响应字段，不删除旧 SSE event。

### 验收

- `go test ./internal/app` 通过，特别是 AI settings、diagnostics、retrieval 相关用例不回退。
- `npm run build` 在前端类型变更后通过。
- 新增 workflow version 后，旧问答接口响应 JSON 仍包含 `run`、`message`、`service_candidates`、`citations`。
- SSE 旧事件 `run_started`、`stage`、`provider_attempt`、`citations`、`answer_delta`、`message_done` 仍可被旧前端消费。
- `git diff --check` 无格式错误。

## 6. 过程 1：Agent 数据契约和内部类型

### 目标

先把 Task Frame、Evidence Contract、Evidence Bundle、Coverage Report、Verification Report 变成后端结构化对象，为后续状态机和诊断展示提供稳定契约。

### 实现

- 新增内部类型文件，建议命名为 `internal/app/ai_agent_types.go`，避免继续拉长 `ai.go`。
- 定义核心结构：
  - `aiTaskFrame`
  - `aiEvidenceContract`
  - `aiEvidenceRequirement`
  - `aiEvidenceBundle`
  - `aiEvidenceGroup`
  - `aiContractCoverageReport`
  - `aiAnswerVerificationReport`
  - `aiRetrievalRoundPlan`
- 保留 JSON 可序列化字段名，与设计文档保持一致：
  - `intent`
  - `user_goal`
  - `answer_shape`
  - `scope_strategy`
  - `target_artifacts`
  - `must_not`
  - `known_terms`
  - `generated_terms`
  - `required`
  - `recommended`
  - `forbidden`
  - `missing_required`
  - `next_action`
- 扩展 `aiRetrievalResult`，但保持旧字段可用：
  - `TaskFrame *aiTaskFrame`
  - `Contract *aiEvidenceContract`
  - `EvidenceBundle *aiEvidenceBundle`
  - `Coverage *aiContractCoverageReport`
  - `Rounds []aiRetrievalRoundPlan`
- 扩展 `aiEvidence`，先只在内存和 step output 中使用，不急于改 `ai_message_citations`：
  - `EvidenceType string`
  - `SourceReliability string`
  - `ContractKeys []string`
  - `ExcludedReason string`
  - `GroupKey string`

### 验收

- 新增类型的 JSON 编码结果与设计文档字段一致。
- 空值对象不会导致旧链路 panic，旧 `retrieveAIEvidence` 返回值仍能被 `buildAIChatMessages` 使用。
- 新增单元测试覆盖：
  - Task Frame JSON round-trip。
  - Evidence Contract JSON round-trip。
  - Coverage Report 中 missing/covered/partial 的序列化。
- `go test ./internal/app -run 'TestAIAgent.*JSON|TestAI.*'` 通过。

## 7. 过程 2：Task Framing

### 目标

在检索前把用户问题结构化成可复盘的 Task Frame，明确 intent、用户目标、回答形态、限制条件和追问上下文。

### 实现

- 新增 `frameAITask(ctx, cfg, question, prepared)`。
- 首期采用“确定性优先，模型补充”的策略：
  - 确定性规则负责识别高置信意图，例如 SQL/数据库直改、诊断排查、接口接入、分支查询。
  - 模型只补充 `user_goal`、`answer_shape`、`target_artifacts`、`generated_terms`，并且必须返回 JSON。
  - 模型失败时保留确定性 Task Frame。
- 意图枚举采用设计文档命名：
  - `api_integration`
  - `database_direct_update_for_test`
  - `code_path_explanation`
  - `cross_service_impact`
  - `branch_lookup`
  - `document_qa`
  - `diagnostics`
- 把现有 `classifyAIIntent` 迁移为 Task Framing 的 fallback，不再让旧 intent 直接决定所有检索策略。
- 同步和 SSE 两条路径都在 `expandAIQuestionForRetrieval` 后、正式检索前执行 framing。
- 插入 `ai_agent_steps`：
  - `agent_name = "task_framer"`
  - `step_type = "model_call"` 或 `deterministic`
  - `output_json` 写入 Task Frame。
- run checkpoint 中写入：
  - `agent_workflow_version`
  - `task_frame`
  - `conversation`
  - `effective_question`
- SSE 增加兼容事件：
  - `task_frame`：给新前端诊断使用，旧前端忽略。

### 验收

- 问题“我想在数据库里直接修改游戏的价格”识别为 `database_direct_update_for_test`。
- 问题“下单页面需要接哪些接口？请求参数和返回字段是什么？”识别为 `api_integration`。
- 问题“库存锁定的新接口现在在哪个分支？”识别为 `branch_lookup`。
- 追问“参数是什么？”可以继承上一轮引用路径和主题摘要，不泛化到无关服务。
- Task Frame 不能包含后端硬编码的业务名词；业务词只能来自用户问题、历史上下文、已收集证据或模型现场生成。
- 诊断 run 能看到 `task_framer` step，且 secret/API key 不出现在 step 输出中。

## 8. 过程 3：Evidence Contract Builder

### 目标

基于 Task Frame 生成回答前必须满足的证据契约，让“能不能回答”从 prompt 规则变成后端可检查对象。

### 实现

- 新增 `buildAIEvidenceContract(frame aiTaskFrame) aiEvidenceContract`。
- 首期用确定性模板，不依赖模型：
  - `database_direct_update_for_test.v1`
  - `api_integration.v1`
  - `document_qa.v1`
  - `branch_lookup.v1`
  - `diagnostics.v1`
- 数据库直改契约至少包含：
  - `table_identity`
  - `update_fields`
  - `field_units`
  - `where_conditions`
  - `read_path`
  - `verification_method`
  - `side_effects`
- 接口接入契约至少包含：
  - `service_candidate`
  - `route_or_rpc`
  - `request_fields`
  - `response_fields`
  - `error_codes`
  - `branch_status`
- 契约写入：
  - `contract_builder` step output。
  - run checkpoint 的 `evidence_contract`。
  - SSE `contract` 事件摘要。
- required/recommended 的严谨度首期保守：
  - 影响最终操作步骤的内容放 required。
  - 风险、错误码、补偿路径可先放 recommended。

### 验收

- 数据库直改类问题生成的 required keys 包含表身份、字段名、字段单位、定位条件、读取链路。
- 接口接入类问题生成的 required keys 包含接口路径或 RPC、请求字段、响应字段。
- 普通文档问答不会套用 SQL 或接口契约。
- Contract Builder 不包含业务专用词典和固定服务名。
- `contract_builder` step 输出可被后续 checker 直接消费，不需要解析 prompt 文本。

## 9. 过程 4：Evidence Curator 和证据类型标注

### 目标

把“临时路径加权”和“测试文件降权”升级为通用证据治理层，为 Contract Checker 提供可解释的证据类型、可信度和排除原因。

### 实现

- 新增 `curateAIEvidence(frame, contract, rawEvidence)`。
- 给每条证据标注：
  - `evidence_type`
  - `source_reliability`
  - `contract_keys`
  - `excluded_reason`
  - `group_key`
- 证据类型识别使用通用规则：
  - `route`：router、route、handler 注册、OpenAPI path。
  - `proto`：`.proto` service/rpc/message。
  - `handler`：controller、handler、endpoint。
  - `request_response_type`：struct、interface、DTO、message。
  - `orm_model`：GORM model、`TableName()`、column tag。
  - `migration_sql`：`CREATE TABLE`、`ALTER TABLE`、索引变更。
  - `read_path`：DAO、GORM query、SQL select、repository。
  - `write_path`：update、save、create 代码路径。
  - `test_fixture`：`_test`、fixtures、testdata、mock。
  - `doc`：Markdown、README、项目指导文件。
- 可信度规则：
  - 非测试问题命中 `test_fixture`：降权或排除，不能作为 required key 的唯一证据。
  - 测试类问题命中 `test_fixture`：可使用，但必须标注。
  - `branch_candidate`：保留，但不能覆盖 `smart_latest` 事实。
  - 同文件同片段跨分支重复：合并为一个 evidence group。
- 输出 `aiEvidenceBundle`：
  - `groups` 按 contract key 聚合。
  - `excluded` 保存被排除证据和原因。
  - `coverage` 先只记录 curator 视角的初步覆盖，最终覆盖由 Contract Checker 决定。

### 验收

- 非测试业务问题不会把 DocHarbor 自身 `_test.go` 中的夹具排为核心业务证据。
- 测试类问题可以引用 `_test.go`，但 evidence type 必须是 `test_fixture`。
- ORM `TableName()` 和 GORM column tag 可以被标注为 `orm_model`。
- migration 中的 `ALTER TABLE` 可以被标注为 `migration_sql`。
- 读取函数中的 `Where(...)` 可以被标注为 `read_path`。
- 诊断输出能解释某条证据被排除或降权的原因。

## 10. 过程 5：Contract Checker 和诊断可视化

### 目标

让后端能判断“证据是否满足契约”，并把缺口在诊断接口中展示出来，而不是让 Answer Composer 自己猜。

### 实现

- 新增 `checkAIEvidenceContract(contract, bundle)`。
- 覆盖状态枚举：
  - `covered`
  - `partial`
  - `missing`
  - `conflict`
  - `forbidden`
- 每个 required/recommended key 输出：
  - `status`
  - `evidence_ids`
  - `reason`
  - `missing_detail`
  - `confidence`
- 更新 run 统计：
  - `unconfirmed_count` = missing/partial required 数量。
  - `verification_report_json` 暂时记录 contract coverage 摘要。
  - `checkpoint_json` 记录完整 coverage report 摘要。
- 插入 `contract_checker` step。
- 修改诊断响应：
  - `aiDiagnosticsStep` 增加脱敏后的 `input_json`、`output_json`，或者新增 `input_summary_json`、`output_summary_json`。
  - 对 step payload 使用 `sanitizeProviderError` 同级别的脱敏函数，避免 secret/API key 泄露。
  - 诊断详情增加顶层摘要字段也可以接受，例如 `task_frame`、`evidence_contract`、`contract_coverage`。

### 验收

- 数据库直改问题在缺字段单位时，coverage 显示 `field_units = missing` 或 `partial`。
- 缺 required key 时，run 的 `unconfirmed_count > 0`。
- 诊断详情能直接看到缺哪个 contract key、已有证据是什么、下一步应该补什么。
- 诊断接口仍不会返回 provider secret、API key、仓库凭据、token 签名密钥。
- `go test ./internal/app -run 'Test.*Diagnostics|Test.*Contract'` 覆盖诊断脱敏和 coverage 输出。

## 11. 过程 6：迭代检索 Orchestrator

### 目标

根据 Contract Checker 的缺口自动发起第二轮、第三轮检索，避免第一轮证据不完整时直接生成答案。

### 实现

- 把现有 `retrieveAIEvidence` 拆出更细的内部函数：
  - `planAIRetrievalRound(frame, contract, coverage, existingBundle, round)`
  - `executeAISearchPlan(ctx, plan, scope, cfg)`
  - `mergeAIEvidence(existing, newlyFound)`
  - `runAIRetrievalOrchestrator(ctx, frame, contract, scope, cfg)`
- 保留 `retrieveAIEvidence` 作为旧调用兼容 wrapper，逐步让同步和 SSE 路径切到 orchestrator。
- 每轮检索输入：
  - 当前 Task Frame。
  - contract missing keys。
  - 已有 evidence summary。
  - 追问上下文。
- 每轮检索输出：
  - `round`
  - `reason`
  - `searches`
  - `new_evidence_count`
  - `coverage_delta`
- 最多 3 轮：
  - R1：基于问题和 Task Frame 初始召回。
  - R2：补 required missing keys。
  - R3：解决 conflict 或补强 weak evidence。
- Query Planner 仍然可以调用模型生成检索词，但必须满足：
  - 不加入无来源的具体服务、业务、接口或模块名。
  - path hints 只能是通用目录类型，如 `models`、`migration`、`db`、`router`、`proto`。
  - 生成词来源记录在 retrieval round step 中。

### 验收

- 构造测试仓库：第一轮只命中读取链路，第二轮能补找 ORM model、migration 或字段定义。
- 达到最大轮次仍缺 required key 时，workflow 状态进入 `completed_with_gaps`，而不是给确定操作结论。
- 每轮都有 `retrieval_round` step，诊断里能看到本轮 reason、search plan、coverage delta。
- provider 调用失败时，query planner 可以降级到确定性检索词，不导致整个问答失败。
- 旧 `retrieveAIEvidence` 相关测试仍通过，或者被等价的新 orchestrator 测试替代。

## 12. 过程 7：Answer Policy 和 Answer Composer

### 目标

把回答生成从“模型自由判断证据是否足够”改为“后端先判断覆盖度，再把允许的回答策略交给模型”。

### 实现

- 新增 `buildAIAnswerPolicy(frame, contract, coverage)`。
- 策略规则：
  - required 全部 covered：允许确定回答。
  - required 部分 missing：禁止给确定操作步骤，只能列已确认事实和缺口。
  - recommended missing：允许回答，但必须在风险或补偿缺口中说明。
  - conflict：先说明冲突来源，不能强行合并。
  - forbidden 命中：阻断确定答案。
- 替换或扩展 `buildAIChatMessages`：
  - 输入 Task Frame。
  - 输入 Evidence Contract。
  - 输入 Coverage Report。
  - 输入 Curated Evidence Bundle。
  - 输入 Answer Policy。
- 数据库直改类回答约束：
  - SQL 必须使用占位符。
  - 不生成真实生产值。
  - 不声称已经执行。
  - 字段、表、WHERE 条件必须有引用。
  - required 缺失时不输出确定 UPDATE。
- 同步路径可以直接调用 composer。
- SSE 路径在引入 Verifier 前可以继续流式输出；引入 Verifier 后必须改成“先缓冲、验证后释放”：
  - 仍发送 `stage` 和 `provider_attempt`。
  - 暂存模型 delta，不立即发送 `answer_delta`。
  - Verifier 通过后一次性或分块发送最终 answer。
  - Verifier 要求重写时，用户只看到重写后的答案。

### 验收

- required covered 的数据库直改问题可以给 SELECT/UPDATE 占位符示例。
- required missing 的数据库直改问题不会给确定 UPDATE，只列缺口。
- 接口接入回答中每个接口路径、请求字段、响应字段都有引用。
- 功能分支证据必须标注“功能分支候选”。
- 回答 prompt 不再承担证据是否足够的主判断职责，判断结果来自 Coverage Report。

## 13. 过程 8：Answer Verifier、重写和降级

### 目标

在答案落库和发送给用户前做质量门禁，阻止“格式正确但语义错误”的回答。

### 实现

- 新增 `verifyAIAnswer(frame, contract, coverage, bundle, draftAnswer)`。
- Verifier 分两层：
  - 确定性检查：引用覆盖、未授权行为、缺 required 却给确定步骤、引用不存在、SQL 无占位符。
  - 模型检查：unsupported refusal、证据污染、分支口径错误、编造服务名。
- 输出 `aiAnswerVerificationReport`：
  - `status`
  - `reason`
  - `details`
  - `next_action`
  - `failed_checks`
  - `rewrite_attempted`
- next action：
  - `pass`
  - `rewrite_answer`
  - `retrieve_more`
  - `complete_with_gaps`
  - `block_answer`
- 自动重试策略：
  - `rewrite_answer` 最多 1 次。
  - `retrieve_more` 只有在 round < 3 时允许。
  - 重写仍失败时输出保守答案，状态为 `completed_with_gaps` 或 `verification_failed`.
- 写入：
  - `answer_verifier` step。
  - `verification_report_json`。
  - run `verification_status`。
  - SSE `verification` 事件。

### 验收

- 证据足够时，答案不能再因为“没有官方 UPDATE 示例”而错误拒绝。
- 答案不能声称已经执行 SQL、已经修改数据库或已经刷新缓存。
- SQL 字段、表名、WHERE 条件没有引用时 verifier 必须失败。
- 非测试问题把 `_test.go` 当业务事实时 verifier 必须失败。
- 功能分支候选被写成智能最新事实时 verifier 必须失败。
- verifier 失败后的最终答案不会泄露 provider 错误中的 secret。

## 14. 过程 9：SSE、API 和前端诊断闭环

### 目标

让新 Agent 工作流的内部状态能被诊断面板看到，同时保证普通问答页和旧客户端不被破坏。

### 实现

- 后端 SSE 新增事件：
  - `task_frame`
  - `contract`
  - `retrieval_round`
  - `coverage`
  - `verification`
- `src/types.ts` 扩展 `AIStreamEvent` union。
- `src/api.ts` 的 SSE parser 保持兼容未知事件。
- `src/App.vue`：
  - 普通问答页继续显示现有阶段和答案。
  - 诊断视图显示 Task Frame、Contract、Coverage、Retrieval Rounds、Verifier Report。
  - 对 missing/partial/conflict 使用可扫描的状态展示。
- 诊断 API：
  - `/api/access/ai/diagnostics/runs/:id` 增加 agent workflow payload。
  - step input/output 必须脱敏。
  - data sources 仍保留现有 scope、repo、branch、scan run 信息。
- 前端不需要让普通用户理解所有 Agent 节点；详细结构只放诊断和排查入口。

### 验收

- 旧问答页能继续完成一次普通 AI 问答。
- 新诊断页能看到：
  - Task Frame。
  - Evidence Contract。
  - 每轮 retrieval plan。
  - Evidence Bundle 摘要。
  - Contract Coverage。
  - Answer Verifier 报告。
- 使用 `ai.history.read` token 访问 diagnostics 返回 `403`。
- 使用 `ai.diagnostics.read` token 可以访问 diagnostics。
- 诊断响应中不包含 `api_key`、`sk-` 原文、Authorization header、Bearer token。
- `npm run build` 通过。

## 15. 过程 10：评估集、回放和质量门禁

### 目标

把线上事故和关键任务类型沉淀为可重复评估，避免后续修改检索、prompt 或供应商路由时回退。

### 实现

- 新增 `internal/app/ai_agent_eval_test.go` 或等价文件。
- 评估用例使用本地 Git fixture 和 mock provider，不依赖真实 API key。
- 每个用例定义：
  - `question`
  - `scope`
  - `expected_intent`
  - `expected_required_keys`
  - `expected_covered_keys`
  - `forbidden_answer_patterns`
  - `expected_diagnostics_fields`
- 首期覆盖五类问题：
  - 数据库直改用于测试。
  - 接口接入。
  - 功能分支候选。
  - 追问上下文。
  - 回答错误排查。
- 支持 diagnostics run 回放：
  - 从历史 run 读取 Task Frame、Contract、Retrieval Rounds、Coverage。
  - 用 mock composer/verifier 复现失败原因。
- 每次改动以下模块必须跑评估集：
  - Task Framer。
  - Contract Builder。
  - Query Planner。
  - Curator。
  - Checker。
  - Answer Composer。
  - Verifier。

### 验收

- `go test ./internal/app -run 'TestAIAgentEval|TestAIDatabase|TestAI.*Retrieval|TestAI.*Diagnostics'` 通过。
- 评估集不依赖网络和真实 AI provider。
- 数据库直改用例能验证：
  - intent。
  - required contract keys。
  - 非测试问题排除测试夹具。
  - 缺字段单位时不生成确定 UPDATE。
  - 证据足够时生成占位符 SQL。
- 接口接入用例能验证路径、请求字段、响应字段都有引用。
- 功能分支用例能验证 branch、commit、source_scope 正确展示。

## 16. 推荐提交边界

| 提交 | 内容 | 主要验证 |
| --- | --- | --- |
| 1 | 类型和 workflow version shadow 结构 | `go test ./internal/app` |
| 2 | Task Framing 和 Contract Builder | framing/contract 单元测试 |
| 3 | Curator 和 Contract Checker | curator/coverage/diagnostics 测试 |
| 4 | 迭代检索 Orchestrator | retrieval round fixture 测试 |
| 5 | Answer Policy 和 Composer | composer prompt/answer policy 测试 |
| 6 | Verifier、重写和 SSE 缓冲 | verifier/SSE 测试 |
| 7 | 前端诊断展示 | `npm run build` |
| 8 | 评估集和回放 | eval 目标测试 |

提交可以合并，但不建议把 2 到 8 一次性完成；否则很难判断失败来自 framing、检索、证据治理还是 verifier。

## 17. 共同验收清单

每个过程完成后都要检查：

- 服务边界：只改 `doc-harbor`，除非明确需要共享模型或文档。
- 只读边界：没有新增 shell、SQL、Git 写操作或外部网络工具调用。
- 隐私边界：诊断、step、SSE 不泄露 secret、API key、Authorization、Bearer token。
- 兼容边界：旧问答接口和旧 SSE event 不删除。
- 泛化边界：没有在通用检索、curator、checker 中硬编码业务名词。
- 可复盘边界：run/step 能说明 Task Frame、Contract、Retrieval Round、Coverage、Verifier 结果。
- 验证命令：
  - `go test ./internal/app`
  - `npm run build`，仅前端或类型变更时需要。
  - `git diff --check`

## 18. 最小可用版本定义

最小可用版本不要求所有 intent 都完整覆盖，但必须满足：

- 数据库直改用于测试的问题可以完成 Task Frame、Contract、Curator、Checker。
- required 缺失时不会给确定 SQL。
- required 覆盖时可以给占位符 SELECT/UPDATE，并带引用。
- 诊断详情能解释每个缺口和每轮检索原因。
- Answer Verifier 能阻止未授权行为、证据污染和错误拒绝。
- 现有 AI 供应商配置、多供应商 failover、历史问答和 diagnostics token 权限不回退。

达到这个版本后，再扩展 `cross_service_impact`、`code_path_explanation` 等意图的专用契约和评估用例。
