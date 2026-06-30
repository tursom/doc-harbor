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
