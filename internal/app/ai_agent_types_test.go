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
