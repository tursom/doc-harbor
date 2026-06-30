package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAIAgentTaskFrameJSONRoundTrip(t *testing.T) {
	frame := aiTaskFrame{
		Intent:          "database_direct_update_for_test",
		UserGoal:        "给出测试用途的数据库直接改价方案",
		AnswerShape:     "sql_steps_with_risk",
		ScopeStrategy:   "global_first",
		TargetArtifacts: []string{"table", "orm_model", "read_path", "field_units"},
		MustNot:         []string{"invent_business_names", "execute_sql"},
		KnownTerms:      []string{"价格", "数据库"},
		GeneratedTerms:  []string{"TableName", "column"},
		FollowUp: &aiTaskFrameFollowUp{
			IsFollowUp:           true,
			PreviousPaths:        []string{"models/price.go"},
			PreviousTopicSummary: "游戏价格读取链路",
		},
	}

	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal task frame: %v", err)
	}
	for _, field := range []string{
		`"intent"`,
		`"user_goal"`,
		`"answer_shape"`,
		`"scope_strategy"`,
		`"target_artifacts"`,
		`"must_not"`,
		`"known_terms"`,
		`"generated_terms"`,
	} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("task frame JSON missing %s: %s", field, raw)
		}
	}

	var decoded aiTaskFrame
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal task frame: %v", err)
	}
	if !reflect.DeepEqual(decoded, frame) {
		t.Fatalf("task frame round trip mismatch:\ngot:  %+v\nwant: %+v", decoded, frame)
	}
}

func TestAIAgentEvidenceContractJSONRoundTrip(t *testing.T) {
	contract := aiEvidenceContract{
		ContractID: "api_integration.v1",
		Intent:     "api_integration",
		Required: []aiEvidenceRequirement{
			{
				Key:                   "route_or_rpc",
				Description:           "接口路径或 RPC",
				AcceptedEvidenceTypes: []string{"route", "proto"},
			},
			{
				Key:                   "request_fields",
				Description:           "请求字段",
				AcceptedEvidenceTypes: []string{"request_response_type", "proto"},
			},
		},
		Recommended: []aiEvidenceRequirement{
			{Key: "error_codes", Description: "错误码和业务约束"},
		},
		Forbidden: []string{"test_fixture_as_runtime_fact", "unreferenced_sql"},
	}

	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("marshal evidence contract: %v", err)
	}
	for _, field := range []string{`"intent"`, `"required"`, `"recommended"`, `"forbidden"`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("evidence contract JSON missing %s: %s", field, raw)
		}
	}

	var decoded aiEvidenceContract
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal evidence contract: %v", err)
	}
	if !reflect.DeepEqual(decoded, contract) {
		t.Fatalf("evidence contract round trip mismatch:\ngot:  %+v\nwant: %+v", decoded, contract)
	}
}

func TestAIAgentEvidenceContractBuilderTemplates(t *testing.T) {
	databaseContract := buildAIEvidenceContract(aiTaskFrame{Intent: aiTaskIntentDatabaseDirectUpdateForTest})
	if databaseContract.ContractID != "database_direct_update_for_test.v1" {
		t.Fatalf("database contract id = %q", databaseContract.ContractID)
	}
	for _, key := range []string{"table_identity", "update_fields", "field_units", "where_conditions", "read_path", "verification_method", "side_effects"} {
		if !testStringSliceContains(aiEvidenceRequirementKeys(databaseContract.Required), key) {
			t.Fatalf("database contract required keys missing %s: %+v", key, databaseContract.Required)
		}
	}

	apiContract := buildAIEvidenceContract(aiTaskFrame{Intent: aiTaskIntentAPIIntegration})
	if apiContract.ContractID != "api_integration.v1" {
		t.Fatalf("api contract id = %q", apiContract.ContractID)
	}
	for _, key := range []string{"service_candidate", "route_or_rpc", "request_fields", "response_fields", "error_codes", "branch_status"} {
		if !testStringSliceContains(aiEvidenceRequirementKeys(apiContract.Required), key) {
			t.Fatalf("api contract required keys missing %s: %+v", key, apiContract.Required)
		}
	}

	documentContract := buildAIEvidenceContract(aiTaskFrame{Intent: aiTaskIntentDocumentQA})
	if documentContract.ContractID != "document_qa.v1" {
		t.Fatalf("document contract id = %q", documentContract.ContractID)
	}
	documentRequired := aiEvidenceRequirementKeys(documentContract.Required)
	for _, sqlOrAPIKey := range []string{"table_identity", "update_fields", "route_or_rpc", "request_fields", "response_fields"} {
		if testStringSliceContains(documentRequired, sqlOrAPIKey) {
			t.Fatalf("document QA contract should not include SQL/API key %s: %+v", sqlOrAPIKey, documentContract.Required)
		}
	}

	genericContract := buildAIEvidenceContract(aiTaskFrame{Intent: aiTaskIntentCodePathExplanation})
	if genericContract.ContractID != "generic.v1" || genericContract.Intent != aiTaskIntentCodePathExplanation {
		t.Fatalf("generic contract = %+v", genericContract)
	}
	combined := encodeJSON(map[string]aiEvidenceContract{
		"database": databaseContract,
		"api":      apiContract,
		"document": documentContract,
		"generic":  genericContract,
	})
	for _, fixedName := range []string{"doc-harbor", "game-service", "steam", "订单", "库存", "游戏"} {
		if strings.Contains(strings.ToLower(combined), strings.ToLower(fixedName)) {
			t.Fatalf("contract builder should not contain fixed business/service name %q: %s", fixedName, combined)
		}
	}
}

func TestAIQuestionChangeGuidanceUsesCodePathIntentAndGenericTerms(t *testing.T) {
	question := "如何修改游戏售卖的基础价格"
	if got := classifyAITaskIntent(question); got != aiTaskIntentCodePathExplanation {
		t.Fatalf("task intent = %q, want %q", got, aiTaskIntentCodePathExplanation)
	}
	if got := aiTaskIntentFromLegacy(classifyAIIntent(question)); got != aiTaskIntentCodePathExplanation {
		t.Fatalf("legacy intent mapping = %q, want %q", got, aiTaskIntentCodePathExplanation)
	}

	plannerTerms := filterAIQueryPlannerTerms(parseAIQueryPlannerTerms(`{"terms":["price","base_price","sell_price","update","SteamGamePrice"]}`), question, nil)
	for _, want := range []string{"price", "base_price", "sell_price", "update"} {
		if !stringSliceContains(plannerTerms, want) {
			t.Fatalf("generic planner term %q missing from %v", want, plannerTerms)
		}
	}
	if stringSliceContains(plannerTerms, "steamgameprice") {
		t.Fatalf("planner filter accepted unsupported concrete term: %v", plannerTerms)
	}

	terms := aiQueryTerms(question)
	for _, forbidden := range []string{"steam", "game", "price", "base", "update"} {
		if stringSliceContains(terms, forbidden) {
			t.Fatalf("deterministic query terms should not inject semantic alias %q: %v", forbidden, terms)
		}
	}

	dbQuestion := "我想在数据库里直接修改游戏的价格"
	if got := classifyAITaskIntent(dbQuestion); got != aiTaskIntentDatabaseDirectUpdateForTest {
		t.Fatalf("database task intent = %q, want %q", got, aiTaskIntentDatabaseDirectUpdateForTest)
	}
}

func TestLocalEvidenceAnswerIncludesEvidenceSnippets(t *testing.T) {
	answer := localEvidenceAnswer("如何修改价格", aiRetrievalResult{
		Evidence: []aiEvidence{{
			Repo: Repository{Name: "service-a"},
			Citation: AIMessageCitation{
				SourceScope: "smart_latest",
				Branch:      "main",
				CommitSHA:   "abcdef123456",
				FilePath:    "models/item_price.go",
				LineStart:   10,
				LineEnd:     12,
			},
			Content: "10: func (ItemPrice) TableName() string {\n11:     return \"item_price\"\n12: }",
		}},
	}, false)
	for _, want := range []string{"高信号证据摘录", "models/item_price.go:10-12", "return \"item_price\""} {
		if !strings.Contains(answer, want) {
			t.Fatalf("local evidence answer missing %q:\n%s", want, answer)
		}
	}
}

func TestAIAgentCoverageReportJSONSerialization(t *testing.T) {
	report := aiContractCoverageReport{
		ContractID:         "database_direct_update_for_test.v1",
		Status:             "partial",
		Covered:            []string{"table_identity", "update_fields"},
		Partial:            []string{"side_effects"},
		MissingRequired:    []string{"field_units"},
		MissingRecommended: []string{"verification_method"},
		NextAction:         "retrieval_round",
		Details: map[string]string{
			"field_units": "missing accepted evidence",
		},
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal coverage report: %v", err)
	}
	for _, fragment := range []string{
		`"covered":["table_identity","update_fields"]`,
		`"partial":["side_effects"]`,
		`"missing_required":["field_units"]`,
		`"next_action":"retrieval_round"`,
	} {
		if !strings.Contains(string(raw), fragment) {
			t.Fatalf("coverage report JSON missing %s: %s", fragment, raw)
		}
	}

	var decoded aiContractCoverageReport
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal coverage report: %v", err)
	}
	if !reflect.DeepEqual(decoded, report) {
		t.Fatalf("coverage report round trip mismatch:\ngot:  %+v\nwant: %+v", decoded, report)
	}
}

func TestAIEvidenceContractCheckerStatusesAndUnconfirmedCount(t *testing.T) {
	contract := aiEvidenceContract{
		ContractID: "checker.v1",
		Required: []aiEvidenceRequirement{
			{Key: "covered_key", Description: "covered requirement"},
			{Key: "partial_key", Description: "partial requirement"},
			{Key: "missing_key", Description: "missing requirement"},
			{Key: "conflict_key", Description: "conflicting requirement"},
			{Key: "forbidden_key", Description: "forbidden requirement"},
		},
		Recommended: []aiEvidenceRequirement{
			{Key: "recommended_key", Description: "recommended requirement"},
		},
		Forbidden: []string{"forbidden_marker"},
	}
	bundle := aiEvidenceBundle{
		BundleID: "checker-bundle",
		Coverage: map[string]string{
			"covered_key":     aiEvidenceCoverageCovered,
			"partial_key":     aiEvidenceCoveragePartial,
			"conflict_key":    aiEvidenceCoverageConflict,
			"forbidden_key":   aiEvidenceCoverageForbidden,
			"recommended_key": aiEvidenceCoveragePartial,
		},
		Groups: []aiEvidenceGroup{
			{Key: "covered_key", EvidenceIDs: []int64{1}, EvidenceType: "doc", SourceReliability: aiEvidenceReliabilityHighSmartLatest},
			{Key: "partial_key", EvidenceIDs: []int64{2}, EvidenceType: "doc", SourceReliability: aiEvidenceReliabilityMediumBranchCandidate},
			{Key: "conflict_key", EvidenceIDs: []int64{3}, EvidenceType: "doc", SourceReliability: aiEvidenceReliabilityHighSmartLatest, Summary: "conflict between sources"},
			{Key: "forbidden_key", EvidenceIDs: []int64{4}, EvidenceType: "doc", SourceReliability: aiEvidenceReliabilityHighSmartLatest, Summary: "forbidden_marker"},
			{Key: "recommended_key", EvidenceIDs: []int64{5}, EvidenceType: "doc", SourceReliability: aiEvidenceReliabilityMediumBranchCandidate},
		},
	}

	report := checkAIEvidenceContract(contract, bundle)
	items := aiContractCoverageItemsByKeyForTest(report)
	for key, want := range map[string]string{
		"covered_key":     aiEvidenceCoverageCovered,
		"partial_key":     aiEvidenceCoveragePartial,
		"missing_key":     aiEvidenceCoverageMissing,
		"conflict_key":    aiEvidenceCoverageConflict,
		"forbidden_key":   aiEvidenceCoverageForbidden,
		"recommended_key": aiEvidenceCoveragePartial,
	} {
		item, ok := items[key]
		if !ok {
			t.Fatalf("coverage item missing %s: %+v", key, report.Items)
		}
		if item.Status != want || report.Coverage[key] != want {
			t.Fatalf("%s status = item:%s coverage:%s, want %s; report=%+v", key, item.Status, report.Coverage[key], want, report)
		}
		if item.EvidenceIDs == nil || item.Reason == "" || item.Confidence < 0 {
			t.Fatalf("%s missing required item fields: %+v", key, item)
		}
	}
	if got := aiContractCoverageUnconfirmedRequiredCount(&report); got != 2 {
		t.Fatalf("unconfirmed required count = %d, want 2; report=%+v", got, report)
	}
	if report.Status != aiEvidenceCoverageForbidden || report.NextAction != aiEvidenceCheckerNextActionRemove {
		t.Fatalf("report status/next_action = %s/%s, want forbidden/remove; report=%+v", report.Status, report.NextAction, report)
	}
}

func TestAIEvidenceContractCheckerDatabaseDirectUpdateMissingFieldUnits(t *testing.T) {
	frame := aiTaskFrame{
		Intent:     aiTaskIntentDatabaseDirectUpdateForTest,
		UserGoal:   "给出测试用途的数据库直接修改方案",
		KnownTerms: []string{"item_records", "price", "lookup_code"},
	}
	contract := buildAIEvidenceContract(frame)
	raw := []aiEvidence{
		{
			Repo: Repository{Name: "service-a"},
			Citation: AIMessageCitation{
				RepoID:      2,
				SourceScope: "smart_latest",
				FilePath:    "models/item_record.go",
				LineStart:   1,
				LineEnd:     20,
			},
			Content: "func (ItemRecord) TableName() string { return \"item_records\" }\ntype ItemRecord struct {\nPrice int `gorm:\"column:price\"`\nLookupCode string `gorm:\"column:lookup_code\"`\n}",
			Score:   20,
		},
		{
			Repo: Repository{Name: "service-a"},
			Citation: AIMessageCitation{
				RepoID:      2,
				SourceScope: "smart_latest",
				FilePath:    "migrations/20260630_item_records.sql",
				LineStart:   1,
				LineEnd:     8,
			},
			Content: "ALTER TABLE item_records ADD COLUMN price BIGINT NOT NULL DEFAULT 0;",
			Score:   19,
		},
		{
			Repo: Repository{Name: "service-a"},
			Citation: AIMessageCitation{
				RepoID:      2,
				SourceScope: "smart_latest",
				FilePath:    "repository/item_record_repository.go",
				LineStart:   3,
				LineEnd:     16,
			},
			Content: "func FindItemRecord(db *gorm.DB, lookupCode string) {\n	db.Where(\"lookup_code = ?\", lookupCode).Find(&record)\n}",
			Score:   18,
		},
	}

	curation := curateAIEvidence(&frame, &contract, raw)
	report := checkAIEvidenceContract(contract, curation.Bundle)
	status := report.Coverage["field_units"]
	if status != aiEvidenceCoverageMissing && status != aiEvidenceCoveragePartial {
		t.Fatalf("field_units coverage = %q, want missing or partial; bundle=%+v report=%+v", status, curation.Bundle, report)
	}
	if got := aiContractCoverageUnconfirmedRequiredCount(&report); got <= 0 {
		t.Fatalf("unconfirmed required count = %d, want > 0; report=%+v", got, report)
	}
	item := aiContractCoverageItemsByKeyForTest(report)["field_units"]
	if item.Key != "field_units" || item.MissingDetail == "" {
		t.Fatalf("field_units diagnostic item missing detail: %+v", item)
	}
}

func TestAIAnswerPolicyDatabaseRequiredCoveredAllowsPlaceholderSQL(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentDatabaseDirectUpdateForTest, UserGoal: "测试环境直接改价"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, nil, map[string]string{
		"rollback_plan": aiEvidenceCoverageMissing,
	})

	policy := buildAIAnswerPolicy(frame, contract, coverage)
	if policy.AnswerMode != aiAnswerModeDeterministicAllowed || !policy.DeterministicOperationStepsAllowed || !policy.RequiredCovered {
		t.Fatalf("covered database policy should allow deterministic placeholder SQL: %+v", policy)
	}
	if !policy.MustExplainRiskOrCompensationGaps || !strings.Contains(strings.Join(policy.Constraints, "\n"), "SELECT/UPDATE") {
		t.Fatalf("policy should allow placeholder SQL and explain recommended gaps: %+v", policy)
	}

	retrieval := aiAnswerComposerRetrievalForTest(frame, contract, coverage, []aiEvidence{{
		Repo: Repository{Name: "service-a"},
		Citation: AIMessageCitation{
			ID:          1,
			RepoID:      1,
			SourceScope: "smart_latest",
			Branch:      "main",
			FilePath:    "models/item_record.go",
			LineStart:   1,
			LineEnd:     20,
		},
		Content:           "func (ItemRecord) TableName() string { return \"item_records\" }\nPriceCents int `gorm:\"column:price_cents\"`\nLookupCode string `gorm:\"column:lookup_code\"`",
		EvidenceType:      "orm_model",
		SourceReliability: aiEvidenceReliabilityHighSmartLatest,
		ContractKeys:      []string{"table_identity", "update_fields", "field_units", "where_conditions"},
	}})
	messages := buildAIChatMessages("数据库里直接改价用于测试", retrieval)
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{
		"SELECT/UPDATE",
		"不要仅因为证据里没有现成 UPDATE 语句",
		"Coverage Report",
		"Answer Policy",
		`"evidence_sufficiency_source":"coverage_report"`,
		"recommended missing/partial",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("composer prompt missing %s:\n%s", want, combined)
		}
	}
}

func TestAIAnswerPolicyDatabaseRequiredMissingBlocksDeterministicUpdate(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentDatabaseDirectUpdateForTest, UserGoal: "测试环境直接改价"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, map[string]string{
		"field_units": aiEvidenceCoverageMissing,
	}, nil)

	policy := buildAIAnswerPolicy(frame, contract, coverage)
	if policy.AnswerMode != aiAnswerModeRequiredGaps || policy.DeterministicOperationStepsAllowed || !policy.MustListConfirmedFactsAndGapsOnly {
		t.Fatalf("missing required policy should block deterministic UPDATE: %+v", policy)
	}
	if !stringSliceContains(policy.RequiredGaps, "field_units") {
		t.Fatalf("policy missing field_units required gap: %+v", policy)
	}

	retrieval := aiAnswerComposerRetrievalForTest(frame, contract, coverage, nil)
	messages := buildAIChatMessages("数据库里直接改价用于测试", retrieval)
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{
		"不得输出确定 UPDATE",
		"只能列已确认事实和缺口",
		`"required_gaps":["field_units"]`,
		"Answer Composer 不得自行猜测",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("composer prompt missing %s:\n%s", want, combined)
		}
	}
	if strings.Contains(messages[0].Content, "可以给 SELECT/UPDATE 占位符示例") {
		t.Fatalf("missing required prompt should not allow SELECT/UPDATE examples: %s", messages[0].Content)
	}
}

func TestAIAnswerPolicyConflictAndForbiddenRules(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentAPIIntegration, UserGoal: "确认接口接入方式"}
	contract := buildAIEvidenceContract(frame)
	conflictCoverage := aiCoverageReportForTest(contract, map[string]string{
		"route_or_rpc": aiEvidenceCoverageConflict,
	}, nil)

	conflictPolicy := buildAIAnswerPolicy(frame, contract, conflictCoverage)
	if conflictPolicy.AnswerMode != aiAnswerModeConflictFirst || !conflictPolicy.MustStartWithConflict || conflictPolicy.DeterministicAnswerAllowed {
		t.Fatalf("conflict policy should require conflict-first answer: %+v", conflictPolicy)
	}

	forbiddenCoverage := aiCoverageReportForTest(contract, map[string]string{
		"route_or_rpc": aiEvidenceCoverageForbidden,
	}, nil)
	forbiddenPolicy := buildAIAnswerPolicy(frame, contract, forbiddenCoverage)
	if forbiddenPolicy.AnswerMode != aiAnswerModeBlockedForbidden || forbiddenPolicy.AnswerAllowed || !forbiddenPolicy.MustBlockDeterminateAnswer {
		t.Fatalf("forbidden policy should block determinate answer: %+v", forbiddenPolicy)
	}
}

func TestAIAnswerComposerAPIIntegrationRequiresPathRequestResponseCitations(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentAPIIntegration, UserGoal: "前端接入下单接口"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, nil, nil)
	retrieval := aiAnswerComposerRetrievalForTest(frame, contract, coverage, []aiEvidence{{
		Repo: Repository{Name: "order-service"},
		Citation: AIMessageCitation{
			ID:          1,
			RepoID:      2,
			SourceScope: "smart_latest",
			Branch:      "main",
			FilePath:    "internal/router/order.go",
			LineStart:   10,
			LineEnd:     40,
		},
		Content:           "POST /api/orders CreateOrderRequest{sku_id, quantity} CreateOrderResponse{order_no, status}",
		EvidenceType:      "route",
		SourceReliability: aiEvidenceReliabilityHighSmartLatest,
		ContractKeys:      []string{"route_or_rpc", "request_fields", "response_fields"},
	}})

	messages := buildAIChatMessages("下单页面要接哪个接口，参数和返回是什么？", retrieval)
	combined := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{
		"每个接口路径、请求字段、响应字段都必须有引用",
		"每个请求字段都必须引用",
		"每个响应字段都必须引用",
		`"route_or_rpc"`,
		`"request_fields"`,
		`"response_fields"`,
		"[C1]",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("api composer prompt missing %s:\n%s", want, combined)
		}
	}
}

func TestAIAnswerComposerBranchCandidateMarksFunctionalBranchCandidate(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentBranchLookup, UserGoal: "确认功能在哪个分支"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, nil, nil)
	retrieval := aiAnswerComposerRetrievalForTest(frame, contract, coverage, []aiEvidence{{
		Repo: Repository{Name: "inventory-service"},
		Citation: AIMessageCitation{
			ID:          1,
			RepoID:      3,
			SourceScope: "branch_candidate",
			Branch:      "feature/lock-stock",
			CommitSHA:   "1234567890abcdef",
			FilePath:    "internal/router/stock.go",
			LineStart:   8,
			LineEnd:     18,
		},
		Content:           "POST /api/stock/lock is implemented on this branch",
		EvidenceType:      "route",
		SourceReliability: aiEvidenceReliabilityMediumBranchCandidate,
		ContractKeys:      []string{"branch_candidates", "source_scope", "commit_evidence"},
	}})

	messages := buildAIChatMessages("库存锁定的新接口在哪个分支？", retrieval)
	combined := messages[0].Content + "\n" + messages[1].Content
	if strings.Count(combined, "功能分支候选") < 2 {
		t.Fatalf("branch candidate prompt should mark functional branch candidates:\n%s", combined)
	}
	if !strings.Contains(combined, `"evidence_sufficiency_source":"coverage_report"`) {
		t.Fatalf("branch composer prompt missing policy sufficiency source:\n%s", combined)
	}
}

func TestAIAnswerComposerStepsAndCheckpointSummaries(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentDocumentQA, UserGoal: "说明文档事实"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, nil, nil)
	retrieval := aiAnswerComposerRetrievalForTest(frame, contract, coverage, nil)
	prepared := prepareAIAnswerComposer("这个文档说明了什么？", &retrieval)

	policyStep := buildAIAnswerPolicyStep(&prepared.Frame, &prepared.Contract, &prepared.Coverage, prepared.Policy)
	composerStep := buildAIAnswerComposerStep("这个文档说明了什么？", retrieval, prepared.Policy, prepared.Summary)
	for _, step := range []AIAgentStep{policyStep, composerStep} {
		if !strings.Contains(step.OutputJSON, "answer_policy") && !strings.Contains(step.OutputJSON, "composer") {
			t.Fatalf("step should include answer policy/composer summary: %+v", step)
		}
		if !strings.Contains(step.OutputJSON, "coverage_report") && step.AgentName == "answer_composer" {
			t.Fatalf("composer step should record coverage/policy prompt inputs: %s", step.OutputJSON)
		}
	}
	checkpoint := buildAIAgentRunCheckpoint(AIQuestionScope{RepoMode: "global"}, "standard", aiQuestionPreparation{
		Scope:            AIQuestionScope{RepoMode: "global"},
		TaskFrame:        &prepared.Frame,
		Contract:         &prepared.Contract,
		ContractCoverage: &prepared.Coverage,
		AnswerPolicy:     &prepared.Policy,
		AnswerComposer:   &prepared.Summary,
	})
	if checkpoint["answer_policy"] == nil || checkpoint["answer_composer"] == nil {
		t.Fatalf("checkpoint missing answer policy/composer summaries: %+v", checkpoint)
	}
}

func TestAIAnswerVerifierCoreActions(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentDatabaseDirectUpdateForTest, UserGoal: "测试环境直接改价"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, nil, nil)
	bundle := aiVerifierBundleForTest(coverage, aiEvidenceReliabilityHighSmartLatest)

	passed := verifyAIAnswer(frame, contract, coverage, bundle, "表 steam_game_price、字段 price_in_cents_with_discount 和 WHERE id 来自证据 [C1]。\nUPDATE steam_game_price SET price_in_cents_with_discount = ? WHERE id = ?; [C1]")
	if passed.Status != aiAnswerVerificationStatusPass || passed.NextAction != aiAnswerVerificationNextActionPass {
		t.Fatalf("pass report = %+v", passed)
	}

	rewrite := verifyAIAnswer(frame, contract, coverage, bundle, "没有官方 UPDATE 示例，无法提供 SQL。[C1]")
	if rewrite.NextAction != aiAnswerVerificationNextActionRewriteAnswer || !stringSliceContains(rewrite.FailedChecks, "unsupported_refusal") {
		t.Fatalf("unsupported refusal report = %+v", rewrite)
	}

	sqlFailed := verifyAIAnswer(frame, contract, coverage, bundle, "UPDATE steam_game_price SET price_in_cents_with_discount = 100 WHERE id = 1;")
	if sqlFailed.NextAction != aiAnswerVerificationNextActionRewriteAnswer ||
		!stringSliceContains(sqlFailed.FailedChecks, "sql_missing_placeholder") ||
		!stringSliceContains(sqlFailed.FailedChecks, "sql_missing_citation") {
		t.Fatalf("sql failure report = %+v", sqlFailed)
	}

	blocked := verifyAIAnswer(frame, contract, coverage, bundle, "我已经执行 SQL 并已修改数据库。[C1]")
	if blocked.NextAction != aiAnswerVerificationNextActionBlockAnswer || !stringSliceContains(blocked.FailedChecks, "unauthorized_behavior") {
		t.Fatalf("block report = %+v", blocked)
	}

	missingCoverage := aiCoverageReportForTest(contract, map[string]string{"table_identity": aiEvidenceCoverageMissing}, nil)
	missingBundle := aiVerifierBundleForTest(missingCoverage, aiEvidenceReliabilityHighSmartLatest)
	conservative := "已确认读取链路和候选字段 [C1]，但缺少表名证据；这里只列已确认事实和缺口。"
	retrieveMore := verifyAIAnswerWithContext(frame, contract, missingCoverage, missingBundle, conservative, aiAnswerVerificationContext{EvidenceCount: 1, RetrievalRound: aiRetrievalMaxRounds - 1})
	if retrieveMore.NextAction != aiAnswerVerificationNextActionRetrieveMore {
		t.Fatalf("retrieve-more report = %+v", retrieveMore)
	}
	completedWithGaps := verifyAIAnswerWithContext(frame, contract, missingCoverage, missingBundle, conservative, aiAnswerVerificationContext{EvidenceCount: 1, RetrievalRound: aiRetrievalMaxRounds})
	if completedWithGaps.Status != aiWorkflowStatusCompletedWithGaps || completedWithGaps.NextAction != aiAnswerVerificationNextActionCompleteWithGaps {
		t.Fatalf("complete-with-gaps report = %+v", completedWithGaps)
	}
}

func TestAIAnswerVerificationReportStableJSONFields(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentDatabaseDirectUpdateForTest, UserGoal: "测试环境直接改价"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, nil, nil)
	bundle := aiVerifierBundleForTest(coverage, aiEvidenceReliabilityHighSmartLatest)

	report := verifyAIAnswer(frame, contract, coverage, bundle, "表 steam_game_price、字段 price_in_cents_with_discount 和 WHERE id 来自证据 [C1]。\nUPDATE steam_game_price SET price_in_cents_with_discount = ? WHERE id = ?; [C1]")
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal verification report: %v", err)
	}
	body := string(raw)
	for _, required := range []string{`"status":"pass"`, `"reason":""`, `"details":[]`, `"next_action":"pass"`, `"failed_checks":[]`, `"rewrite_attempted":false`} {
		if !strings.Contains(body, required) {
			t.Fatalf("verification report JSON missing stable field %s: %s", required, body)
		}
	}
	var decoded aiAnswerVerificationReport
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("round-trip verification report: %v", err)
	}
	if decoded.Reason != "" || len(decoded.FailedChecks) != 0 || decoded.Status != aiAnswerVerificationStatusPass {
		t.Fatalf("decoded report = %+v", decoded)
	}
}

func TestAIAnswerVerifierUsesDisplayedCitationLabels(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentDocumentQA, UserGoal: "说明当前事实"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, nil, nil)
	bundle := aiEvidenceBundle{
		BundleID: "display-citation-test",
		Coverage: coverage.Coverage,
		Groups: []aiEvidenceGroup{{
			Key:               "cited_documents",
			EvidenceIDs:       []int64{2},
			Summary:           "raw C2 is the first included evidence shown to the model",
			EvidenceType:      "document",
			SourceReliability: aiEvidenceReliabilityHighSmartLatest,
		}},
		Excluded: []aiEvidenceExclusion{{
			EvidenceID:        1,
			Reason:            aiEvidenceExcludedReasonTestFixtureNonTestTask,
			FilePath:          "internal/app/raw_c1_test.go",
			EvidenceType:      "test_fixture",
			SourceReliability: aiEvidenceReliabilityExcludedTestFixtureNonTest,
		}},
	}
	context := aiAnswerVerificationContext{EvidenceCount: 1, RetrievalRound: aiRetrievalMaxRounds}

	includedDisplayC1 := verifyAIAnswerWithContext(frame, contract, coverage, bundle, "当前事实来自展示给模型的第一条证据 [C1]。", context)
	if includedDisplayC1.Status != aiAnswerVerificationStatusPass ||
		!stringSliceContains(includedDisplayC1.PassedChecks, "citation_exists") ||
		stringSliceContains(includedDisplayC1.FailedChecks, "citation_not_found") {
		t.Fatalf("display [C1] should pass even when raw C1 was excluded: %+v", includedDisplayC1)
	}

	rawC2AsDisplayC2 := verifyAIAnswerWithContext(frame, contract, coverage, bundle, "当前事实误用 raw C2 作为展示编号 [C2]。", context)
	if !stringSliceContains(rawC2AsDisplayC2.FailedChecks, "citation_not_found") ||
		stringSliceContains(rawC2AsDisplayC2.PassedChecks, "citation_exists") {
		t.Fatalf("display [C2] should fail when only one evidence item was shown: %+v", rawC2AsDisplayC2)
	}

	fallbackDisplayC1 := verifyAIAnswer(frame, contract, coverage, bundle, "直接校验 fallback 也应接受展示证据 [C1]。")
	if fallbackDisplayC1.Status != aiAnswerVerificationStatusPass ||
		!stringSliceContains(fallbackDisplayC1.PassedChecks, "citation_exists") ||
		stringSliceContains(fallbackDisplayC1.FailedChecks, "citation_not_found") {
		t.Fatalf("fallback display [C1] should pass based on included evidence count: %+v", fallbackDisplayC1)
	}

	fallbackRawC2AsDisplayC2 := verifyAIAnswer(frame, contract, coverage, bundle, "直接校验 fallback 不应接受 raw ID 编号 [C2]。")
	if !stringSliceContains(fallbackRawC2AsDisplayC2.FailedChecks, "citation_not_found") ||
		stringSliceContains(fallbackRawC2AsDisplayC2.PassedChecks, "citation_exists") {
		t.Fatalf("fallback display [C2] should fail based on included evidence count: %+v", fallbackRawC2AsDisplayC2)
	}
}

func TestAIAnswerVerifierSecretRedactionCoversProviderFormats(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentDocumentQA, UserGoal: "说明业务事实"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, nil, nil)
	bundle := aiVerifierBundleForTest(coverage, aiEvidenceReliabilityHighSmartLatest)
	cases := []struct {
		name      string
		answer    string
		forbidden string
	}{
		{name: "api key underscore", answer: "provider failed api_key=raw-provider-api-key [C1]", forbidden: "raw-provider-api-key"},
		{name: "api key space", answer: "provider failed API key: raw-provider-spaced-key [C1]", forbidden: "raw-provider-spaced-key"},
		{name: "token kv", answer: "provider failed token=raw-provider-token [C1]", forbidden: "raw-provider-token"},
		{name: "secret phrase", answer: "provider failed secret raw-provider-secret [C1]", forbidden: "raw-provider-secret"},
		{name: "basic auth", answer: "provider failed Authorization: Basic cmF3LXByb3ZpZGVy [C1]", forbidden: "cmF3LXByb3ZpZGVy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := verifyAIAnswer(frame, contract, coverage, bundle, tc.answer)
			if report.NextAction != aiAnswerVerificationNextActionBlockAnswer || !stringSliceContains(report.FailedChecks, "secret_exposure") {
				t.Fatalf("secret report = %+v", report)
			}
			if safe := sanitizeProviderError(tc.answer); strings.Contains(safe, tc.forbidden) {
				t.Fatalf("provider error sanitizer leaked %q: %s", tc.forbidden, safe)
			}
			if reportJSON := encodeJSON(report); strings.Contains(reportJSON, tc.forbidden) {
				t.Fatalf("verification report leaked %q: %s", tc.forbidden, reportJSON)
			}
		})
	}
}

func TestAIAnswerVerificationEvidencePollutionAndBranchScope(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentDocumentQA, UserGoal: "说明业务事实"}
	contract := buildAIEvidenceContract(frame)
	coverage := aiCoverageReportForTest(contract, nil, nil)
	bundle := aiVerifierBundleForTest(coverage, aiEvidenceReliabilityHighSmartLatest)
	bundle.Excluded = []aiEvidenceExclusion{{
		EvidenceID: 1,
		Reason:     aiEvidenceExcludedReasonTestFixtureNonTestTask,
		FilePath:   "internal/app/payment_test.go",
	}}

	testPollution := verifyAIAnswerWithContext(frame, contract, coverage, bundle, "业务事实来自 internal/app/payment_test.go [C1]。", aiAnswerVerificationContext{EvidenceCount: 1, RetrievalRound: aiRetrievalMaxRounds})
	if !stringSliceContains(testPollution.FailedChecks, "test_fixture_pollution") {
		t.Fatalf("test fixture pollution report = %+v", testPollution)
	}

	excludedCitationBundle := aiVerifierBundleForTest(coverage, aiEvidenceReliabilityHighSmartLatest)
	excludedCitationBundle.Excluded = []aiEvidenceExclusion{{
		EvidenceID: 2,
		Reason:     aiEvidenceExcludedReasonTestFixtureNonTestTask,
		FilePath:   "internal/app/excluded_test.go",
	}}
	excludedCitation := verifyAIAnswerWithContext(frame, contract, coverage, excludedCitationBundle, "业务事实来自被排除证据 [C2]。", aiAnswerVerificationContext{EvidenceCount: 1, RetrievalRound: aiRetrievalMaxRounds})
	if !stringSliceContains(excludedCitation.FailedChecks, "citation_not_found") {
		t.Fatalf("excluded citation report = %+v", excludedCitation)
	}

	branchBundle := aiVerifierBundleForTest(coverage, aiEvidenceReliabilityMediumBranchCandidate)
	branchMismatch := verifyAIAnswerWithContext(frame, contract, coverage, branchBundle, "这是智能最新事实，接口已经合入 [C1]。", aiAnswerVerificationContext{EvidenceCount: 1, RetrievalRound: aiRetrievalMaxRounds})
	if !stringSliceContains(branchMismatch.FailedChecks, "branch_scope_mismatch") {
		t.Fatalf("branch mismatch report = %+v", branchMismatch)
	}
	branchCandidatePromotion := verifyAIAnswerWithContext(frame, contract, coverage, branchBundle, "功能分支候选已合入主分支/已上线/当前最新事实 [C1]", aiAnswerVerificationContext{EvidenceCount: 1, RetrievalRound: aiRetrievalMaxRounds})
	if !stringSliceContains(branchCandidatePromotion.FailedChecks, "branch_scope_mismatch") {
		t.Fatalf("branch candidate promotion report = %+v", branchCandidatePromotion)
	}
	branchCandidateUnconfirmed := verifyAIAnswerWithContext(frame, contract, coverage, branchBundle, "功能分支候选，尚未确认合入主分支或上线 [C1]", aiAnswerVerificationContext{EvidenceCount: 1, RetrievalRound: aiRetrievalMaxRounds})
	if stringSliceContains(branchCandidateUnconfirmed.FailedChecks, "branch_scope_mismatch") || !stringSliceContains(branchCandidateUnconfirmed.PassedChecks, "branch_scope") {
		t.Fatalf("branch candidate unconfirmed report = %+v", branchCandidateUnconfirmed)
	}

	mixedBundle := aiVerifierBundleForTest(coverage, aiEvidenceReliabilityHighSmartLatest)
	mixedBundle.Groups = append(mixedBundle.Groups, aiEvidenceGroup{
		Key:               "current_fact",
		EvidenceIDs:       []int64{2},
		Summary:           "branch candidate evidence",
		EvidenceType:      "code",
		SourceReliability: aiEvidenceReliabilityMediumBranchCandidate,
	})
	mixedSourceLabels := verifyAIAnswerWithContext(frame, contract, coverage, mixedBundle, "引用来源：智能最新 [C1]；功能分支候选 [C2]。", aiAnswerVerificationContext{EvidenceCount: 2, RetrievalRound: aiRetrievalMaxRounds})
	if stringSliceContains(mixedSourceLabels.FailedChecks, "branch_scope_mismatch") || !stringSliceContains(mixedSourceLabels.PassedChecks, "branch_scope") {
		t.Fatalf("mixed source labels should not fail branch scope: %+v", mixedSourceLabels)
	}
	mixedPromotion := verifyAIAnswerWithContext(frame, contract, coverage, mixedBundle, "功能分支候选已合入主分支，当前最新事实见 [C2]。", aiAnswerVerificationContext{EvidenceCount: 2, RetrievalRound: aiRetrievalMaxRounds})
	if !stringSliceContains(mixedPromotion.FailedChecks, "branch_scope_mismatch") {
		t.Fatalf("mixed branch promotion report = %+v", mixedPromotion)
	}
}

func TestAIAgentRetrievalResultLegacyJSONCompatibility(t *testing.T) {
	retrieval := aiRetrievalResult{
		TaskFrame:      &aiTaskFrame{},
		Contract:       &aiEvidenceContract{},
		EvidenceBundle: &aiEvidenceBundle{},
		Coverage:       &aiContractCoverageReport{},
		Rounds:         []aiRetrievalRoundPlan{{Round: 1}},
	}
	messages := buildAIChatMessages("参数是什么？", retrieval)
	if len(messages) != 2 {
		t.Fatalf("chat messages count = %d, want 2", len(messages))
	}
	if !strings.Contains(messages[1].Content, "当前用户问题") {
		t.Fatalf("legacy chat message missing question block: %s", messages[1].Content)
	}
}

func TestAIAgentEvidenceCuratorAnnotatesTypesAndExcludesNonTestFixtures(t *testing.T) {
	frame := aiTaskFrame{
		Intent:     aiTaskIntentDatabaseDirectUpdateForTest,
		UserGoal:   "给出测试用途的数据库直接修改方案",
		KnownTerms: []string{"entity_records", "amount_cents", "lookup_code"},
	}
	contract := buildAIEvidenceContract(frame)
	raw := []aiEvidence{
		{
			Repo: Repository{Name: "doc-harbor"},
			Citation: AIMessageCitation{
				RepoID:      1,
				SourceScope: "smart_latest",
				FilePath:    "internal/app/ai_settings_test.go",
				LineStart:   10,
				LineEnd:     20,
			},
			Content: `func TestNoisyFixture(t *testing.T) {
	_ = "entity_records amount_cents lookup_code"
}`,
			Score: 99,
		},
		{
			Repo: Repository{Name: "service-a"},
			Citation: AIMessageCitation{
				RepoID:      2,
				SourceScope: "smart_latest",
				FilePath:    "models/entity_record.go",
				LineStart:   1,
				LineEnd:     20,
			},
			Content: "func (EntityRecord) TableName() string { return \"entity_records\" }\ntype EntityRecord struct {\nAmountCents int `gorm:\"column:amount_cents\"`\nLookupCode string `gorm:\"column:lookup_code\"`\n}",
			Score:   20,
		},
		{
			Repo: Repository{Name: "service-a"},
			Citation: AIMessageCitation{
				RepoID:      2,
				SourceScope: "smart_latest",
				FilePath:    "migrations/20260630_entity_records.sql",
				LineStart:   1,
				LineEnd:     8,
			},
			Content: "ALTER TABLE entity_records ADD COLUMN amount_cents BIGINT NOT NULL DEFAULT 0;",
			Score:   19,
		},
		{
			Repo: Repository{Name: "service-a"},
			Citation: AIMessageCitation{
				RepoID:      2,
				SourceScope: "smart_latest",
				FilePath:    "repository/entity_record_repository.go",
				LineStart:   3,
				LineEnd:     16,
			},
			Content: "func FindEntityRecord(db *gorm.DB, lookupCode string) {\n	db.Where(\"lookup_code = ?\", lookupCode).Where(\"deleted_at IS NULL\").Find(&record)\n}",
			Score:   18,
		},
	}

	curation := curateAIEvidence(&frame, &contract, raw)
	if len(curation.ExcludedEvidence) != 1 || curation.ExcludedEvidence[0].EvidenceType != "test_fixture" {
		t.Fatalf("non-test task should exclude the test fixture with annotation: %+v", curation.ExcludedEvidence)
	}
	if curation.ExcludedEvidence[0].ExcludedReason != aiEvidenceExcludedReasonTestFixtureNonTestTask {
		t.Fatalf("excluded reason = %q", curation.ExcludedEvidence[0].ExcludedReason)
	}
	for _, evidence := range curation.Evidence {
		if evidence.Repo.Name == "doc-harbor" && strings.HasSuffix(evidence.Citation.FilePath, "_test.go") {
			t.Fatalf("test fixture remained as core evidence: %+v", evidence)
		}
	}

	assertCuratedType := func(filePath, wantType string) {
		t.Helper()
		for _, evidence := range curation.Evidence {
			if evidence.Citation.FilePath == filePath {
				if evidence.EvidenceType != wantType {
					t.Fatalf("%s evidence_type = %q, want %q", filePath, evidence.EvidenceType, wantType)
				}
				if evidence.GroupKey == "" || evidence.SourceReliability == "" || len(evidence.ContractKeys) == 0 {
					t.Fatalf("%s missing curator annotation: %+v", filePath, evidence)
				}
				return
			}
		}
		t.Fatalf("curated evidence missing %s: %+v", filePath, curation.Evidence)
	}
	assertCuratedType("models/entity_record.go", "orm_model")
	assertCuratedType("migrations/20260630_entity_records.sql", "migration_sql")
	assertCuratedType("repository/entity_record_repository.go", "read_path")
	if curation.Bundle.Coverage["table_identity"] == "" || curation.Coverage.Status == "" {
		t.Fatalf("curator coverage missing required keys: bundle=%+v report=%+v", curation.Bundle, curation.Coverage)
	}

	step := buildAIEvidenceCuratorStep(&frame, &contract, curation, len(raw))
	for _, want := range []string{aiEvidenceExcludedReasonTestFixtureNonTestTask, `"evidence_type":"test_fixture"`, `"file_path":"internal/app/ai_settings_test.go"`} {
		if !strings.Contains(step.OutputJSON, want) {
			t.Fatalf("curator step output missing %s: %s", want, step.OutputJSON)
		}
	}
}

func TestAIAgentEvidenceCuratorDoesNotTreatLatestContestAsTestFocus(t *testing.T) {
	frame := aiTaskFrame{
		Intent:     aiTaskIntentDocumentQA,
		UserGoal:   "Explain the latest contest rule update",
		KnownTerms: []string{"latest", "contest", "rule"},
	}
	if aiTaskFrameLooksTestFocused(frame) {
		t.Fatalf("latest/contest should not make a non-test task test-focused: %+v", frame)
	}
	contract := buildAIEvidenceContract(frame)
	raw := []aiEvidence{
		{
			Repo: Repository{Name: "doc-harbor"},
			Citation: AIMessageCitation{
				RepoID:      1,
				SourceScope: "smart_latest",
				FilePath:    "internal/app/contest_rules_test.go",
				LineStart:   1,
				LineEnd:     12,
			},
			Content: `func TestContestRules(t *testing.T) {
	_ = "latest contest fixture"
}`,
			Score: 99,
		},
		{
			Repo: Repository{Name: "doc-harbor"},
			Citation: AIMessageCitation{
				RepoID:      1,
				SourceScope: "smart_latest",
				FilePath:    "docs/contest_rules.md",
				LineStart:   1,
				LineEnd:     6,
			},
			Content: "# Contest rules\n\nLatest contest rule update.",
			Score:   10,
		},
	}

	curation := curateAIEvidence(&frame, &contract, raw)
	if len(curation.ExcludedEvidence) != 1 || curation.ExcludedEvidence[0].Citation.FilePath != "internal/app/contest_rules_test.go" {
		t.Fatalf("latest/contest non-test task should exclude test fixture: %+v", curation.ExcludedEvidence)
	}
	for _, evidence := range curation.Evidence {
		if strings.HasSuffix(evidence.Citation.FilePath, "_test.go") {
			t.Fatalf("test fixture remained as core evidence: %+v", evidence)
		}
	}
}

func TestAIAgentEvidenceCuratorAllowsTestFocusedFixtures(t *testing.T) {
	frame := aiTaskFrame{
		Intent:     aiTaskIntentDocumentQA,
		UserGoal:   "说明测试用例覆盖了哪些输入",
		KnownTerms: []string{"测试", "fixture"},
	}
	contract := buildAIEvidenceContract(frame)
	raw := []aiEvidence{{
		Repo: Repository{Name: "service-a"},
		Citation: AIMessageCitation{
			RepoID:      1,
			SourceScope: "smart_latest",
			FilePath:    "internal/app/entity_test.go",
			LineStart:   1,
			LineEnd:     12,
		},
		Content: "func TestEntityLookup(t *testing.T) { fixture := \"lookup_code\"; _ = fixture }",
		Score:   10,
	}}

	curation := curateAIEvidence(&frame, &contract, raw)
	if len(curation.Evidence) != 1 || len(curation.ExcludedEvidence) != 0 {
		t.Fatalf("test-focused task should keep test fixture: included=%+v excluded=%+v", curation.Evidence, curation.ExcludedEvidence)
	}
	evidence := curation.Evidence[0]
	if evidence.EvidenceType != "test_fixture" || evidence.SourceReliability != aiEvidenceReliabilityTestFixtureForTestTask {
		t.Fatalf("test fixture annotation = %+v", evidence)
	}
}

func TestAIAgentEvidenceCuratorGroupsBranchCandidateDuplicates(t *testing.T) {
	frame := aiTaskFrame{Intent: aiTaskIntentDocumentQA, KnownTerms: []string{"endpoint"}}
	contract := buildAIEvidenceContract(frame)
	content := "# Endpoint\n\nThe endpoint accepts an identifier and returns status."
	raw := []aiEvidence{
		{
			Repo: Repository{Name: "service-a"},
			Citation: AIMessageCitation{
				RepoID:      1,
				SourceScope: "branch_candidate",
				Branch:      "feature/api",
				CommitSHA:   "branchsha",
				FilePath:    "docs/endpoint.md",
			},
			Content: content,
			Score:   100,
		},
		{
			Repo: Repository{Name: "service-a"},
			Citation: AIMessageCitation{
				RepoID:      1,
				SourceScope: "smart_latest",
				Branch:      "main",
				CommitSHA:   "mainsha",
				FilePath:    "docs/endpoint.md",
			},
			Content: content,
			Score:   10,
		},
	}

	curation := curateAIEvidence(&frame, &contract, raw)
	if len(curation.Evidence) != 2 {
		t.Fatalf("branch candidate should be retained: %+v", curation.Evidence)
	}
	if curation.Evidence[0].Citation.SourceScope != "smart_latest" {
		t.Fatalf("smart_latest evidence should lead the duplicate group: %+v", curation.Evidence)
	}
	if curation.Evidence[0].GroupKey == "" || curation.Evidence[0].GroupKey != curation.Evidence[1].GroupKey {
		t.Fatalf("duplicates should share group_key: %+v", curation.Evidence)
	}
	var foundMergedGroup bool
	for _, group := range curation.Bundle.Groups {
		if group.Key == "cited_documents" && len(group.EvidenceIDs) == 2 {
			foundMergedGroup = true
			break
		}
	}
	if !foundMergedGroup {
		t.Fatalf("bundle did not merge duplicate branch evidence: %+v", curation.Bundle.Groups)
	}
}

func aiContractCoverageItemsByKeyForTest(report aiContractCoverageReport) map[string]aiContractCoverageItem {
	items := map[string]aiContractCoverageItem{}
	for _, item := range report.Items {
		items[item.Key] = item
	}
	return items
}

func aiCoverageReportForTest(contract aiEvidenceContract, requiredOverrides map[string]string, recommendedOverrides map[string]string) aiContractCoverageReport {
	report := aiContractCoverageReport{
		ContractID:         contract.ContractID,
		Status:             "pass",
		Coverage:           map[string]string{},
		Items:              []aiContractCoverageItem{},
		Covered:            []string{},
		Partial:            []string{},
		MissingRequired:    []string{},
		MissingRecommended: []string{},
		ForbiddenMatched:   []string{},
		NextAction:         aiEvidenceCheckerNextActionLegacyAnswer,
		Details:            map[string]string{},
	}
	for _, requirement := range contract.Required {
		status := aiEvidenceCoverageCovered
		if requiredOverrides != nil && requiredOverrides[requirement.Key] != "" {
			status = requiredOverrides[requirement.Key]
		}
		appendAIContractCoverageItem(&report, aiContractCoverageItem{
			Key:         requirement.Key,
			Requirement: aiEvidenceCheckerRequirementRequired,
			Status:      status,
			EvidenceIDs: []int64{1},
			Reason:      "test coverage",
			Confidence:  0.95,
		})
	}
	for _, requirement := range contract.Recommended {
		status := aiEvidenceCoverageCovered
		if recommendedOverrides != nil && recommendedOverrides[requirement.Key] != "" {
			status = recommendedOverrides[requirement.Key]
		}
		appendAIContractCoverageItem(&report, aiContractCoverageItem{
			Key:         requirement.Key,
			Requirement: aiEvidenceCheckerRequirementRecommended,
			Status:      status,
			EvidenceIDs: []int64{1},
			Reason:      "test coverage",
			Confidence:  0.9,
		})
	}
	finalizeAIContractCoverageReport(&report)
	return report
}

func aiAnswerComposerRetrievalForTest(frame aiTaskFrame, contract aiEvidenceContract, coverage aiContractCoverageReport, evidence []aiEvidence) aiRetrievalResult {
	groups := make([]aiEvidenceGroup, 0, len(coverage.Items))
	for _, item := range coverage.Items {
		groups = append(groups, aiEvidenceGroup{
			Key:               item.Key,
			EvidenceIDs:       append([]int64(nil), item.EvidenceIDs...),
			Summary:           item.Reason,
			EvidenceType:      "test",
			SourceReliability: aiEvidenceReliabilityHighSmartLatest,
		})
	}
	bundle := aiEvidenceBundle{
		BundleID: "test-bundle",
		Coverage: coverage.Coverage,
		Groups:   groups,
	}
	return aiRetrievalResult{
		Intent:           frame.Intent,
		Scope:            AIQuestionScope{RepoMode: "global", SourceMode: "smart_latest_with_branch_candidates"},
		Plan:             map[string]any{},
		Evidence:         evidence,
		TaskFrame:        &frame,
		Contract:         &contract,
		EvidenceBundle:   &bundle,
		Coverage:         &coverage,
		ContractCoverage: &coverage,
	}
}

func aiVerifierBundleForTest(coverage aiContractCoverageReport, reliability string) aiEvidenceBundle {
	groups := make([]aiEvidenceGroup, 0, len(coverage.Items))
	for _, item := range coverage.Items {
		groups = append(groups, aiEvidenceGroup{
			Key:               item.Key,
			EvidenceIDs:       []int64{1},
			Summary:           item.Reason,
			EvidenceType:      "code",
			SourceReliability: reliability,
		})
	}
	return aiEvidenceBundle{
		BundleID: "verifier-test-bundle",
		Coverage: coverage.Coverage,
		Groups:   groups,
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
