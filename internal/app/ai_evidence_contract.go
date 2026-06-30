package app

func buildAIEvidenceContract(frame aiTaskFrame) aiEvidenceContract {
	intent := normalizeAITaskIntent(frame.Intent)
	switch intent {
	case aiTaskIntentDatabaseDirectUpdateForTest:
		return aiEvidenceContract{
			ContractID: "database_direct_update_for_test.v1",
			Intent:     intent,
			Required: []aiEvidenceRequirement{
				{Key: "table_identity", Description: "表名、ORM TableName 或明确 schema 身份", AcceptedEvidenceTypes: []string{"orm_model", "migration_sql", "schema_doc"}},
				{Key: "update_fields", Description: "允许修改的字段名和字段来源", AcceptedEvidenceTypes: []string{"orm_model", "migration_sql", "read_path"}},
				{Key: "field_units", Description: "字段单位、换算逻辑或字段注释", AcceptedEvidenceTypes: []string{"orm_model", "migration_sql", "read_path", "field_comment", "conversion_code"}},
				{Key: "where_conditions", Description: "定位测试数据所需的 WHERE 条件或唯一约束", AcceptedEvidenceTypes: []string{"read_path", "query_builder", "unique_index", "schema_doc"}},
				{Key: "read_path", Description: "当前业务读取链路和查询入口", AcceptedEvidenceTypes: []string{"handler", "service", "dao", "repository", "query_call"}},
				{Key: "verification_method", Description: "修改后的 SELECT 校验方式或业务读取验证入口", AcceptedEvidenceTypes: []string{"read_path", "select_query", "test_case", "operational_doc"}},
				{Key: "side_effects", Description: "缓存、索引、异步任务、事件或补偿链路风险", AcceptedEvidenceTypes: []string{"cache_path", "index_path", "async_job", "event_handler", "compensation_path", "operational_doc"}},
			},
			Recommended: []aiEvidenceRequirement{
				{Key: "rollback_plan", Description: "回滚或恢复测试数据的路径", AcceptedEvidenceTypes: []string{"operational_doc", "migration_sql", "read_path"}},
				{Key: "scope_limit", Description: "仅用于测试或指定环境的范围边界", AcceptedEvidenceTypes: []string{"operational_doc", "config", "test_case"}},
			},
			Forbidden: []string{"test_fixture_as_runtime_fact", "unsupported_exact_value", "unreferenced_sql", "execute_sql"},
		}
	case aiTaskIntentAPIIntegration:
		return aiEvidenceContract{
			ContractID: "api_integration.v1",
			Intent:     intent,
			Required: []aiEvidenceRequirement{
				{Key: "service_candidate", Description: "接口所属服务或仓库候选及依据", AcceptedEvidenceTypes: []string{"route", "proto", "frontend_client", "document", "repository_context"}},
				{Key: "route_or_rpc", Description: "HTTP 路由、OpenAPI path、proto service/rpc 或 handler 注册", AcceptedEvidenceTypes: []string{"route", "openapi", "proto", "rpc_registration", "handler"}},
				{Key: "request_fields", Description: "请求字段、绑定 tag、校验约束或 proto message", AcceptedEvidenceTypes: []string{"request_type", "proto_message", "openapi_schema", "binding_tag", "validation"}},
				{Key: "response_fields", Description: "响应字段、返回结构、proto message 或 handler 返回", AcceptedEvidenceTypes: []string{"response_type", "proto_message", "openapi_schema", "handler_return"}},
				{Key: "error_codes", Description: "错误码、错误映射、校验失败和业务约束", AcceptedEvidenceTypes: []string{"error_mapping", "validation", "constant", "service_logic"}},
				{Key: "branch_status", Description: "接口所在分支、commit、source scope 或合入状态", AcceptedEvidenceTypes: []string{"branch", "commit", "source_scope", "merge_status"}},
			},
			Recommended: []aiEvidenceRequirement{
				{Key: "auth_policy", Description: "鉴权、权限或调用方限制", AcceptedEvidenceTypes: []string{"middleware", "config", "document", "service_logic"}},
				{Key: "compatibility_notes", Description: "版本兼容、灰度或客户端适配说明", AcceptedEvidenceTypes: []string{"document", "route", "frontend_client", "config"}},
				{Key: "compensation_path", Description: "失败后的补偿、重试或人工处理路径", AcceptedEvidenceTypes: []string{"service_logic", "async_job", "operational_doc"}},
			},
			Forbidden: []string{"test_fixture_as_runtime_fact", "invented_route", "invented_service_name", "unreferenced_error_code"},
		}
	case aiTaskIntentDocumentQA:
		return aiEvidenceContract{
			ContractID: "document_qa.v1",
			Intent:     intent,
			Required: []aiEvidenceRequirement{
				{Key: "cited_documents", Description: "可引用的文档或代码片段", AcceptedEvidenceTypes: []string{"document", "code", "markdown", "current_file"}},
				{Key: "current_fact", Description: "回答所需的当前事实或明确结论", AcceptedEvidenceTypes: []string{"document", "code", "markdown"}},
				{Key: "scope_boundary", Description: "仓库、分支或当前文件范围", AcceptedEvidenceTypes: []string{"source_scope", "branch", "repository_context"}},
			},
			Recommended: []aiEvidenceRequirement{
				{Key: "version_or_branch", Description: "事实所属版本、分支或 commit", AcceptedEvidenceTypes: []string{"branch", "commit", "source_scope"}},
				{Key: "contradiction_check", Description: "冲突文档或过期事实检查", AcceptedEvidenceTypes: []string{"document", "code", "branch"}},
			},
			Forbidden: []string{"unsupported_fact", "invented_business_name", "test_fixture_as_runtime_fact"},
		}
	case aiTaskIntentCodePathExplanation:
		required := []aiEvidenceRequirement{
			{Key: "entrypoint", Description: "用户要操作或理解的入口函数、handler、RPC、路由或命令入口", AcceptedEvidenceTypes: []string{"handler", "route", "proto", "service", "code"}},
			{Key: "call_chain", Description: "入口到关键实现之间的调用关系或执行路径", AcceptedEvidenceTypes: []string{"handler", "service_logic", "dao", "repository", "code"}},
			{Key: "implementation_file", Description: "承载核心逻辑的文件或模块", AcceptedEvidenceTypes: []string{"code", "service_logic", "handler", "dao", "repository"}},
			{Key: "scope_boundary", Description: "仓库、分支或当前文件范围", AcceptedEvidenceTypes: []string{"source_scope", "branch", "repository_context"}},
		}
		recommended := []aiEvidenceRequirement{
			{Key: "read_path", Description: "现有读取或展示路径，用于区分兜底值、展示值和实际业务值", AcceptedEvidenceTypes: []string{"read_path", "handler", "service_logic", "dao", "repository"}},
			{Key: "write_path", Description: "修改动作真正写入的位置、方法或持久化调用", AcceptedEvidenceTypes: []string{"write_path", "handler", "service_logic", "dao", "repository"}},
			{Key: "persistence_target", Description: "被写入或刷新到的持久化对象、表、模型、索引或缓存", AcceptedEvidenceTypes: []string{"orm_model", "migration_sql", "write_path", "schema_doc", "index_path", "cache_path"}},
			{Key: "side_effects", Description: "修改后的同步、索引、缓存、事件或补偿链路", AcceptedEvidenceTypes: []string{"cache_path", "index_path", "async_job", "event_handler", "compensation_path", "service_logic"}},
			{Key: "branch_status", Description: "涉及功能分支时的分支和 commit 状态", AcceptedEvidenceTypes: []string{"branch", "commit", "source_scope"}},
		}
		if aiTaskFrameLooksChangeGuidance(frame) {
			required = append(required,
				aiEvidenceRequirement{Key: "write_path", Description: "修改动作真正写入的位置、方法或持久化调用", AcceptedEvidenceTypes: []string{"write_path", "handler", "service_logic", "dao", "repository"}},
				aiEvidenceRequirement{Key: "persistence_target", Description: "被写入或刷新到的持久化对象、表、模型、索引或缓存", AcceptedEvidenceTypes: []string{"orm_model", "migration_sql", "write_path", "schema_doc", "index_path", "cache_path"}},
				aiEvidenceRequirement{Key: "side_effects", Description: "修改后的同步、索引、缓存、事件或补偿链路", AcceptedEvidenceTypes: []string{"cache_path", "index_path", "async_job", "event_handler", "compensation_path", "service_logic"}},
			)
			recommended = []aiEvidenceRequirement{
				{Key: "request_fields", Description: "如果存在接口或 RPC，确认相关请求字段和输入约束", AcceptedEvidenceTypes: []string{"request_type", "proto_message", "openapi_schema", "binding_tag", "validation", "handler"}},
				{Key: "read_path", Description: "现有读取或展示路径，用于区分兜底值、展示值和实际业务值", AcceptedEvidenceTypes: []string{"read_path", "handler", "service_logic", "dao", "repository"}},
				{Key: "branch_status", Description: "涉及功能分支时的分支和 commit 状态", AcceptedEvidenceTypes: []string{"branch", "commit", "source_scope"}},
			}
		}
		return aiEvidenceContract{
			ContractID:  "code_path_explanation.v1",
			Intent:      intent,
			Required:    required,
			Recommended: recommended,
			Forbidden:   []string{"unsupported_fact", "unreferenced_claim", "execute_sql", "direct_database_update_without_user_request", "secret_exposure"},
		}
	case aiTaskIntentBranchLookup:
		return aiEvidenceContract{
			ContractID: "branch_lookup.v1",
			Intent:     intent,
			Required: []aiEvidenceRequirement{
				{Key: "branch_candidates", Description: "候选分支及命中依据", AcceptedEvidenceTypes: []string{"branch", "source_scope", "repository_context"}},
				{Key: "source_scope", Description: "智能最新、功能分支候选或指定分支口径", AcceptedEvidenceTypes: []string{"source_scope", "branch"}},
				{Key: "commit_evidence", Description: "候选分支上的 commit 或文件证据", AcceptedEvidenceTypes: []string{"commit", "code", "document"}},
				{Key: "default_branch_baseline", Description: "默认分支或智能最新基线证据", AcceptedEvidenceTypes: []string{"branch", "commit", "source_scope"}},
			},
			Recommended: []aiEvidenceRequirement{
				{Key: "merge_status", Description: "合入、开发中或 stale 状态", AcceptedEvidenceTypes: []string{"branch", "commit", "document"}},
				{Key: "stale_branch_risk", Description: "候选分支过期或未扫描风险", AcceptedEvidenceTypes: []string{"branch", "source_scope", "repository_context"}},
			},
			Forbidden: []string{"unscanned_branch_as_fact", "highest_score_as_latest", "invented_branch"},
		}
	case aiTaskIntentDiagnostics:
		return aiEvidenceContract{
			ContractID: "diagnostics.v1",
			Intent:     intent,
			Required: []aiEvidenceRequirement{
				{Key: "run_identity", Description: "诊断 run、会话或消息身份", AcceptedEvidenceTypes: []string{"run_record", "message_record", "diagnostics_api"}},
				{Key: "run_steps", Description: "运行步骤、状态和错误摘要", AcceptedEvidenceTypes: []string{"agent_step", "diagnostics_api"}},
				{Key: "retrieval_plan", Description: "检索计划、检索词和范围", AcceptedEvidenceTypes: []string{"retrieval_plan", "checkpoint", "agent_step"}},
				{Key: "citations", Description: "引用证据、分支和文件路径", AcceptedEvidenceTypes: []string{"citation", "diagnostics_api"}},
				{Key: "provider_failover", Description: "模型路由和供应商失败转移", AcceptedEvidenceTypes: []string{"provider_attempt", "model_route", "diagnostics_api"}},
				{Key: "checkpoint", Description: "运行 checkpoint 中的结构化状态", AcceptedEvidenceTypes: []string{"checkpoint", "run_record"}},
			},
			Recommended: []aiEvidenceRequirement{
				{Key: "sanitized_errors", Description: "脱敏后的错误信息", AcceptedEvidenceTypes: []string{"agent_step", "diagnostics_api"}},
				{Key: "data_sources", Description: "本次诊断涉及的数据源和扫描范围", AcceptedEvidenceTypes: []string{"repository_context", "diagnostics_api"}},
				{Key: "gaps", Description: "缺失证据或未确认项", AcceptedEvidenceTypes: []string{"coverage_report", "verification_report", "agent_step"}},
			},
			Forbidden: []string{"secret_exposure", "raw_prompt_leak", "api_key_leak", "bearer_token_leak"},
		}
	default:
		return aiEvidenceContract{
			ContractID: "generic.v1",
			Intent:     intent,
			Required: []aiEvidenceRequirement{
				{Key: "cited_evidence", Description: "可引用的代码或文档证据", AcceptedEvidenceTypes: []string{"code", "document", "current_file"}},
				{Key: "scope_boundary", Description: "仓库、分支、当前文件或追问上下文范围", AcceptedEvidenceTypes: []string{"source_scope", "branch", "repository_context"}},
				{Key: "target_artifacts", Description: "Task Frame 指定的目标产物证据", AcceptedEvidenceTypes: []string{"code", "document", "schema", "route", "proto"}},
			},
			Recommended: []aiEvidenceRequirement{
				{Key: "branch_status", Description: "涉及功能分支时的分支和 commit 状态", AcceptedEvidenceTypes: []string{"branch", "commit", "source_scope"}},
				{Key: "risk_points", Description: "风险、约束或补偿路径", AcceptedEvidenceTypes: []string{"code", "document", "operational_doc"}},
				{Key: "missing_items", Description: "仍未确认的证据缺口", AcceptedEvidenceTypes: []string{"coverage_report", "verification_report"}},
			},
			Forbidden: []string{"unsupported_fact", "unreferenced_claim", "secret_exposure"},
		}
	}
}

func buildAIEvidenceContractStep(frame aiTaskFrame, contract aiEvidenceContract) AIAgentStep {
	now := nowString()
	return AIAgentStep{
		AgentName:  "contract_builder",
		StepType:   "deterministic",
		Status:     "success",
		InputJSON:  encodeJSON(frame),
		OutputJSON: encodeJSON(contract),
		CreatedAt:  now,
		FinishedAt: now,
	}
}

type aiEvidenceContractSummary struct {
	ContractID      string   `json:"contract_id"`
	Intent          string   `json:"intent,omitempty"`
	RequiredKeys    []string `json:"required_keys"`
	RecommendedKeys []string `json:"recommended_keys,omitempty"`
	Forbidden       []string `json:"forbidden,omitempty"`
}

func summarizeAIEvidenceContract(contract aiEvidenceContract) aiEvidenceContractSummary {
	return aiEvidenceContractSummary{
		ContractID:      contract.ContractID,
		Intent:          contract.Intent,
		RequiredKeys:    aiEvidenceRequirementKeys(contract.Required),
		RecommendedKeys: aiEvidenceRequirementKeys(contract.Recommended),
		Forbidden:       append([]string(nil), contract.Forbidden...),
	}
}

func aiEvidenceRequirementKeys(requirements []aiEvidenceRequirement) []string {
	keys := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		if requirement.Key == "" {
			continue
		}
		keys = append(keys, requirement.Key)
	}
	return keys
}
