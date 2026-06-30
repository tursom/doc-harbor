package app

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"unicode"
)

const (
	aiEvidenceReliabilityHighSmartLatest            = "high_smart_latest"
	aiEvidenceReliabilityHighCurrentFile            = "high_current_file"
	aiEvidenceReliabilityMediumBranchCandidate      = "medium_branch_candidate"
	aiEvidenceReliabilityMedium                     = "medium"
	aiEvidenceReliabilityTestFixtureForTestTask     = "test_fixture_for_test_task"
	aiEvidenceReliabilityExcludedTestFixtureNonTest = "excluded_test_fixture_non_test_task"
	aiEvidenceExcludedReasonTestFixtureNonTestTask  = "test_fixture_for_non_test_task"
	aiEvidenceCoverageCovered                       = "covered"
	aiEvidenceCoveragePartial                       = "partial"
	aiEvidenceCoverageMissing                       = "missing"
	aiEvidenceCoverageConflict                      = "conflict"
	aiEvidenceCoverageForbidden                     = "forbidden"
	aiEvidenceCuratorNextActionContractChecker      = "contract_checker"
	aiEvidenceCuratorNextActionRetrieveMissing      = "retrieve_missing_contract_keys"
)

type aiEvidenceCurationResult struct {
	Evidence         []aiEvidence
	ExcludedEvidence []aiEvidence
	Bundle           aiEvidenceBundle
	Coverage         aiContractCoverageReport
	Annotations      []aiEvidenceAnnotation
}

type aiEvidenceAnnotation struct {
	EvidenceID        int64    `json:"evidence_id"`
	RepoID            int64    `json:"repo_id,omitempty"`
	RepoName          string   `json:"repo_name,omitempty"`
	FilePath          string   `json:"file_path,omitempty"`
	SourceScope       string   `json:"source_scope,omitempty"`
	Branch            string   `json:"branch,omitempty"`
	EvidenceType      string   `json:"evidence_type,omitempty"`
	SourceReliability string   `json:"source_reliability,omitempty"`
	ContractKeys      []string `json:"contract_keys,omitempty"`
	ExcludedReason    string   `json:"excluded_reason,omitempty"`
	GroupKey          string   `json:"group_key,omitempty"`
}

func curateAIEvidence(frame *aiTaskFrame, contract *aiEvidenceContract, rawEvidence []aiEvidence) aiEvidenceCurationResult {
	effectiveFrame := aiTaskFrame{Intent: aiTaskIntentDocumentQA}
	if frame != nil {
		effectiveFrame = *frame
	}
	if strings.TrimSpace(effectiveFrame.Intent) == "" {
		effectiveFrame.Intent = aiTaskIntentDocumentQA
	}
	effectiveContract := aiEvidenceContract{}
	if contract != nil {
		effectiveContract = *contract
	} else {
		effectiveContract = buildAIEvidenceContract(effectiveFrame)
	}
	if effectiveContract.ContractID == "" {
		effectiveContract = buildAIEvidenceContract(effectiveFrame)
	}

	testFocused := aiTaskFrameLooksTestFocused(effectiveFrame)
	result := aiEvidenceCurationResult{
		Evidence:         []aiEvidence{},
		ExcludedEvidence: []aiEvidence{},
		Bundle: aiEvidenceBundle{
			BundleID: fmt.Sprintf("curated-%s", emptyDefault(effectiveContract.ContractID, "generic.v1")),
			Coverage: map[string]string{},
			Groups:   []aiEvidenceGroup{},
			Excluded: []aiEvidenceExclusion{},
		},
		Annotations: []aiEvidenceAnnotation{},
	}

	groupsByKey := map[string]*aiEvidenceGroup{}
	for i, item := range rawEvidence {
		evidenceID := int64(i + 1)
		item.EvidenceType = classifyAIEvidenceType(item)
		item.GroupKey = aiEvidenceGroupKey(item)
		item.SourceReliability = classifyAIEvidenceReliability(item, testFocused)
		if item.EvidenceType == "test_fixture" && !testFocused {
			item.ExcludedReason = aiEvidenceExcludedReasonTestFixtureNonTestTask
			item.ContractKeys = nil
		} else {
			item.ContractKeys = aiEvidenceContractKeys(item, effectiveContract, testFocused)
		}

		annotation := aiEvidenceAnnotation{
			EvidenceID:        evidenceID,
			RepoID:            item.Citation.RepoID,
			RepoName:          item.Repo.Name,
			FilePath:          item.Citation.FilePath,
			SourceScope:       item.Citation.SourceScope,
			Branch:            item.Citation.Branch,
			EvidenceType:      item.EvidenceType,
			SourceReliability: item.SourceReliability,
			ContractKeys:      append([]string(nil), item.ContractKeys...),
			ExcludedReason:    item.ExcludedReason,
			GroupKey:          item.GroupKey,
		}
		result.Annotations = append(result.Annotations, annotation)

		if item.ExcludedReason != "" {
			result.ExcludedEvidence = append(result.ExcludedEvidence, item)
			result.Bundle.Excluded = append(result.Bundle.Excluded, aiEvidenceExclusion{
				EvidenceID:        evidenceID,
				Reason:            item.ExcludedReason,
				GroupKey:          item.GroupKey,
				EvidenceType:      item.EvidenceType,
				SourceReliability: item.SourceReliability,
				RepoID:            item.Citation.RepoID,
				RepoName:          item.Repo.Name,
				FilePath:          item.Citation.FilePath,
				SourceScope:       item.Citation.SourceScope,
			})
			continue
		}

		result.Evidence = append(result.Evidence, item)
		for _, contractKey := range item.ContractKeys {
			groupID := contractKey + "|" + item.GroupKey
			group := groupsByKey[groupID]
			if group == nil {
				groupsByKey[groupID] = &aiEvidenceGroup{
					Key:               contractKey,
					GroupKey:          item.GroupKey,
					EvidenceIDs:       []int64{},
					EvidenceType:      item.EvidenceType,
					SourceReliability: item.SourceReliability,
				}
				group = groupsByKey[groupID]
			}
			group.EvidenceIDs = append(group.EvidenceIDs, evidenceID)
			group.EvidenceType = mergeAIEvidenceTypeLabel(group.EvidenceType, item.EvidenceType)
			group.SourceReliability = mergeAIEvidenceReliability(group.SourceReliability, item.SourceReliability)
		}
	}

	for _, group := range groupsByKey {
		group.Summary = summarizeAIEvidenceGroup(*group)
		result.Bundle.Groups = append(result.Bundle.Groups, *group)
	}
	sort.SliceStable(result.Bundle.Groups, func(i, j int) bool {
		if result.Bundle.Groups[i].Key != result.Bundle.Groups[j].Key {
			return result.Bundle.Groups[i].Key < result.Bundle.Groups[j].Key
		}
		return result.Bundle.Groups[i].GroupKey < result.Bundle.Groups[j].GroupKey
	})
	sort.SliceStable(result.Evidence, func(i, j int) bool {
		if result.Evidence[i].GroupKey == result.Evidence[j].GroupKey &&
			result.Evidence[i].Citation.SourceScope != result.Evidence[j].Citation.SourceScope {
			if result.Evidence[i].Citation.SourceScope == "smart_latest" {
				return true
			}
			if result.Evidence[j].Citation.SourceScope == "smart_latest" {
				return false
			}
		}
		if result.Evidence[i].Score != result.Evidence[j].Score {
			return result.Evidence[i].Score > result.Evidence[j].Score
		}
		return result.Evidence[i].Citation.FilePath < result.Evidence[j].Citation.FilePath
	})

	result.Bundle.Coverage = buildAIEvidenceCuratorCoverage(effectiveContract, result.Bundle.Groups)
	result.Coverage = buildAIEvidenceCuratorCoverageReport(effectiveContract, result.Bundle.Coverage, result.Bundle.Excluded)
	return result
}

func classifyAIEvidenceType(evidence aiEvidence) string {
	filePath := strings.ToLower(normalizeRepoPath(evidence.Citation.FilePath))
	content := strings.ToLower(evidence.Content)
	ext := extension(filePath)

	if aiPathLooksTest(filePath) || strings.Contains(filePath, "/mocks/") || strings.Contains(filePath, "/mock/") {
		return "test_fixture"
	}
	if ext == ".proto" || strings.Contains(content, "service ") && strings.Contains(content, " rpc ") ||
		strings.Contains(content, "\nrpc ") || strings.Contains(content, "\nmessage ") {
		return "proto"
	}
	if aiContentLooksMigrationSQL(filePath, content) {
		return "migration_sql"
	}
	if aiContentLooksORMModel(content) {
		return "orm_model"
	}
	if aiContentLooksRoute(filePath, content) {
		return "route"
	}
	if aiContentLooksHandler(filePath, content) {
		return "handler"
	}
	if aiContentLooksReadPath(filePath, content) {
		return "read_path"
	}
	if aiContentLooksWritePath(filePath, content) {
		return "write_path"
	}
	if aiContentLooksRequestResponseType(filePath, content) {
		return "request_response_type"
	}
	if aiPathLooksDocument(filePath) {
		return "doc"
	}
	if aiPathLooksCode(filePath) {
		return "code"
	}
	return "doc"
}

func aiContentLooksMigrationSQL(filePath, content string) bool {
	if strings.HasSuffix(filePath, ".sql") {
		for _, pattern := range []string{"create table", "alter table", "create index", "alter index", "drop index", "add index", "add unique", "unique key"} {
			if strings.Contains(content, pattern) {
				return true
			}
		}
	}
	return strings.Contains(filePath, "migration") && strings.HasSuffix(filePath, ".sql")
}

func aiContentLooksORMModel(content string) bool {
	for _, pattern := range []string{"tablename()", "table_name", "gorm:\"", "gorm:'", "column:"} {
		if strings.Contains(content, pattern) {
			return true
		}
	}
	return false
}

func aiContentLooksRoute(filePath, content string) bool {
	for _, pattern := range []string{"router", "routes", "openapi", "swagger"} {
		if strings.Contains(filePath, pattern) {
			return true
		}
	}
	return aiLineLooksLikeRoute(content) ||
		strings.Contains(content, "@router") ||
		strings.Contains(content, "requestbody") ||
		strings.Contains(content, "\"paths\"")
}

func aiContentLooksHandler(filePath, content string) bool {
	for _, pattern := range []string{"controller", "handler", "endpoint"} {
		if strings.Contains(filePath, pattern) {
			return true
		}
	}
	return strings.Contains(content, "func ") && (strings.Contains(content, "handler") || strings.Contains(content, "controller") || strings.Contains(content, "endpoint"))
}

func aiContentLooksReadPath(filePath, content string) bool {
	for _, pattern := range []string{"dao/", "repository/", "repositories/", "query/", "mysql_methods/", "db/", "/dao/", "/repository/", "/repositories/", "/query/", "/mysql_methods/", "/db/"} {
		if strings.Contains(filePath, pattern) {
			return true
		}
	}
	for _, pattern := range []string{".where(", "where(", " select ", "\nselect ", ".find(", ".first(", ".scan(", ".take(", ".pluck(", "queryrow", "querycontext"} {
		if strings.Contains(content, pattern) {
			return true
		}
	}
	return false
}

func aiContentLooksWritePath(filePath, content string) bool {
	for _, pattern := range []string{"command/", "commands/", "mutation/", "write/", "/command/", "/commands/", "/mutation/", "/write/"} {
		if strings.Contains(filePath, pattern) {
			return true
		}
	}
	for _, pattern := range []string{".update(", ".updates(", ".save(", ".create(", ".delete(", "update(", "updates(", "save(", "create(", "delete(", " insert ", "\ninsert ", " update ", "\nupdate ", " set "} {
		if strings.Contains(content, pattern) {
			return true
		}
	}
	return false
}

func aiContentLooksRequestResponseType(filePath, content string) bool {
	for _, pattern := range []string{"request", "response", "dto", "schema"} {
		if strings.Contains(filePath, pattern) {
			return true
		}
	}
	return (strings.Contains(content, "type ") && (strings.Contains(content, " struct") || strings.Contains(content, " interface"))) ||
		strings.Contains(content, "json:\"") ||
		strings.Contains(content, "binding:\"") ||
		strings.Contains(content, "form:\"")
}

func aiPathLooksDocument(filePath string) bool {
	ext := extension(filePath)
	return ext == ".md" || ext == ".markdown" || ext == ".mdx" ||
		strings.HasSuffix(filePath, "readme") ||
		strings.HasSuffix(filePath, "agents.md") ||
		strings.HasSuffix(filePath, "claude.md")
}

func classifyAIEvidenceReliability(evidence aiEvidence, testFocused bool) string {
	if evidence.EvidenceType == "test_fixture" {
		if testFocused {
			return aiEvidenceReliabilityTestFixtureForTestTask
		}
		return aiEvidenceReliabilityExcludedTestFixtureNonTest
	}
	switch evidence.Citation.SourceScope {
	case "smart_latest":
		return aiEvidenceReliabilityHighSmartLatest
	case "current_file":
		return aiEvidenceReliabilityHighCurrentFile
	case "branch_candidate":
		return aiEvidenceReliabilityMediumBranchCandidate
	default:
		return aiEvidenceReliabilityMedium
	}
}

func aiTaskFrameLooksTestFocused(frame aiTaskFrame) bool {
	if normalizeAITaskIntent(frame.Intent) == aiTaskIntentDatabaseDirectUpdateForTest {
		return false
	}
	values := []string{frame.Intent, frame.UserGoal, frame.AnswerShape, frame.ScopeStrategy}
	values = append(values, frame.TargetArtifacts...)
	values = append(values, frame.KnownTerms...)
	values = append(values, frame.GeneratedTerms...)
	for _, value := range values {
		if aiTextLooksTestFocused(value) {
			return true
		}
	}
	return false
}

func aiTextLooksTestFocused(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"单测", "测试", "测试用例", "测试文件"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, token := range strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		switch token {
		case "test", "tests", "testing", "unittest", "unittests", "fixture", "fixtures", "mock", "mocks":
			return true
		}
	}
	return false
}

func aiEvidenceGroupKey(evidence aiEvidence) string {
	filePath := normalizeRepoPath(evidence.Citation.FilePath)
	if filePath == "" {
		filePath = "."
	}
	return fmt.Sprintf("repo:%d:file:%s:snippet:%s", evidence.Citation.RepoID, filePath, aiEvidenceContentFingerprint(evidence.Content))
}

func aiEvidenceContentFingerprint(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = stripAIEvidenceSnippetLineNumber(line)
	}
	normalized := strings.Join(strings.Fields(strings.Join(lines, "\n")), " ")
	h := fnv.New64a()
	_, _ = h.Write([]byte(normalized))
	return fmt.Sprintf("%x", h.Sum64())
}

func stripAIEvidenceSnippetLineNumber(line string) string {
	trimmed := strings.TrimSpace(line)
	colon := strings.IndexByte(trimmed, ':')
	if colon <= 0 {
		return trimmed
	}
	for _, r := range trimmed[:colon] {
		if r < '0' || r > '9' {
			return trimmed
		}
	}
	return strings.TrimSpace(trimmed[colon+1:])
}

func aiEvidenceContractKeys(evidence aiEvidence, contract aiEvidenceContract, testFocused bool) []string {
	keys := []string{}
	for _, requirement := range append(append([]aiEvidenceRequirement{}, contract.Required...), contract.Recommended...) {
		if requirement.Key == "" {
			continue
		}
		if aiEvidenceMatchesRequirement(evidence, requirement, testFocused) {
			keys = append(keys, requirement.Key)
		}
	}
	return uniqueStrings(keys)
}

func aiEvidenceMatchesRequirement(evidence aiEvidence, requirement aiEvidenceRequirement, testFocused bool) bool {
	key := strings.ToLower(strings.TrimSpace(requirement.Key))
	switch key {
	case "cited_documents", "cited_evidence", "current_fact", "target_artifacts":
		return aiEvidenceTypeIsUsableFact(evidence.EvidenceType, testFocused)
	case "entrypoint":
		return evidence.EvidenceType == "handler" || evidence.EvidenceType == "route" || evidence.EvidenceType == "proto" || evidence.EvidenceType == "request_response_type" || evidence.EvidenceType == "code"
	case "call_chain", "implementation_file":
		return evidence.EvidenceType == "handler" || evidence.EvidenceType == "read_path" || evidence.EvidenceType == "write_path" || evidence.EvidenceType == "code"
	case "scope_boundary", "version_or_branch", "source_scope", "branch_status":
		return evidence.Citation.SourceScope != "" || evidence.Citation.Branch != "" || evidence.Citation.CommitSHA != ""
	case "branch_candidates":
		return evidence.Citation.SourceScope == "branch_candidate"
	case "commit_evidence", "default_branch_baseline":
		return evidence.Citation.CommitSHA != ""
	case "table_identity":
		return evidence.EvidenceType == "orm_model" || evidence.EvidenceType == "migration_sql" || evidence.EvidenceType == "doc"
	case "update_fields":
		return evidence.EvidenceType == "orm_model" || evidence.EvidenceType == "migration_sql" || evidence.EvidenceType == "read_path"
	case "field_units":
		return aiEvidenceLooksLikeFieldUnitEvidence(evidence)
	case "where_conditions":
		return evidence.EvidenceType == "read_path" || evidence.EvidenceType == "migration_sql"
	case "read_path", "verification_method":
		return evidence.EvidenceType == "read_path" || evidence.EvidenceType == "handler" || evidence.EvidenceType == "test_fixture" && testFocused
	case "write_path":
		return evidence.EvidenceType == "write_path" || evidence.EvidenceType == "handler" ||
			evidence.EvidenceType == "read_path" && aiEvidenceLooksLikeWritePathEvidence(evidence) ||
			evidence.EvidenceType == "test_fixture" && testFocused
	case "persistence_target":
		return evidence.EvidenceType == "orm_model" || evidence.EvidenceType == "migration_sql" || evidence.EvidenceType == "write_path" || evidence.EvidenceType == "read_path"
	case "route_or_rpc":
		return evidence.EvidenceType == "route" || evidence.EvidenceType == "proto" || evidence.EvidenceType == "handler"
	case "request_fields", "response_fields":
		return evidence.EvidenceType == "request_response_type" || evidence.EvidenceType == "proto" || evidence.EvidenceType == "handler" || evidence.EvidenceType == "route"
	case "service_candidate", "error_codes", "auth_policy", "compatibility_notes", "compensation_path", "risk_points", "side_effects", "rollback_plan", "scope_limit":
		return aiEvidenceTypeIsUsableFact(evidence.EvidenceType, testFocused)
	}
	for _, accepted := range requirement.AcceptedEvidenceTypes {
		if aiEvidenceTypeAccepted(evidence, accepted, testFocused) {
			return true
		}
	}
	return false
}

func aiEvidenceLooksLikeFieldUnitEvidence(evidence aiEvidence) bool {
	switch evidence.EvidenceType {
	case "orm_model", "migration_sql", "read_path", "write_path", "code":
	default:
		return false
	}
	content := strings.ToLower(evidence.Content)
	filePath := strings.ToLower(normalizeRepoPath(evidence.Citation.FilePath))
	combined := filePath + " " + content
	unitTokens := map[string]struct{}{
		"unit": {}, "units": {}, "currency": {}, "decimal": {}, "precision": {}, "scale": {}, "ratio": {},
		"percent": {}, "percentage": {}, "cents": {}, "fen": {}, "millisecond": {}, "milliseconds": {}, "seconds": {},
	}
	for _, token := range strings.FieldsFunc(combined, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if _, ok := unitTokens[token]; ok {
			return true
		}
	}
	for _, marker := range []string{
		"_cent", "_cents", "minor_unit", "minorunit", "_fen", "amount_fen",
		"_ms", "_millis", "_sec", "_secs",
		"_bytes", "_kb", "_mb", "_gb",
		"单位", "换算", "分", "元", "毫秒", "秒", "字节", "百分比",
		"/ 100", "/100", "* 100", "*100", "divide by 100", "multiply by 100",
	} {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	return false
}

func aiEvidenceLooksLikeWritePathEvidence(evidence aiEvidence) bool {
	content := strings.ToLower(evidence.Content)
	filePath := strings.ToLower(normalizeRepoPath(evidence.Citation.FilePath))
	return aiContentLooksWritePath(filePath, content)
}

func aiEvidenceTypeAccepted(evidence aiEvidence, accepted string, testFocused bool) bool {
	accepted = strings.ToLower(strings.TrimSpace(accepted))
	if accepted == "" {
		return false
	}
	if evidence.EvidenceType == accepted {
		return true
	}
	switch accepted {
	case "code":
		return aiEvidenceTypeIsCode(evidence.EvidenceType, testFocused)
	case "document", "markdown", "operational_doc":
		return evidence.EvidenceType == "doc"
	case "schema", "schema_doc":
		return evidence.EvidenceType == "orm_model" || evidence.EvidenceType == "migration_sql" || evidence.EvidenceType == "doc"
	case "openapi", "rpc_registration":
		return evidence.EvidenceType == "route" || evidence.EvidenceType == "proto"
	case "request_type", "response_type", "proto_message", "openapi_schema", "binding_tag", "validation", "handler_return":
		return evidence.EvidenceType == "request_response_type" || evidence.EvidenceType == "proto" || evidence.EvidenceType == "handler" || evidence.EvidenceType == "route"
	case "service", "service_logic", "dao", "repository", "query_call", "select_query":
		return evidence.EvidenceType == "read_path" || evidence.EvidenceType == "write_path" || evidence.EvidenceType == "handler"
	case "index_path", "cache_path", "async_job", "event_handler", "compensation_path":
		return evidence.EvidenceType == "write_path" || evidence.EvidenceType == "handler" || evidence.EvidenceType == "code"
	case "query_builder", "unique_index":
		return evidence.EvidenceType == "read_path" || evidence.EvidenceType == "migration_sql"
	case "test_case":
		return evidence.EvidenceType == "test_fixture"
	case "branch", "commit", "source_scope", "merge_status", "repository_context":
		return evidence.Citation.SourceScope != "" || evidence.Citation.Branch != "" || evidence.Citation.CommitSHA != "" || evidence.Citation.RepoID > 0
	default:
		return false
	}
}

func aiEvidenceTypeIsCode(evidenceType string, testFocused bool) bool {
	switch evidenceType {
	case "route", "proto", "handler", "request_response_type", "orm_model", "migration_sql", "read_path", "write_path", "code":
		return true
	case "test_fixture":
		return testFocused
	default:
		return false
	}
}

func aiEvidenceTypeIsUsableFact(evidenceType string, testFocused bool) bool {
	return evidenceType == "doc" || aiEvidenceTypeIsCode(evidenceType, testFocused)
}

func mergeAIEvidenceTypeLabel(existing, next string) string {
	if existing == "" {
		return next
	}
	if next == "" || existing == next {
		return existing
	}
	return "mixed"
}

func mergeAIEvidenceReliability(existing, next string) string {
	if existing == "" || aiEvidenceReliabilityRank(next) > aiEvidenceReliabilityRank(existing) {
		return next
	}
	return existing
}

func aiEvidenceReliabilityRank(reliability string) int {
	switch reliability {
	case aiEvidenceReliabilityHighSmartLatest, aiEvidenceReliabilityHighCurrentFile:
		return 4
	case aiEvidenceReliabilityTestFixtureForTestTask:
		return 3
	case aiEvidenceReliabilityMediumBranchCandidate:
		return 2
	case aiEvidenceReliabilityMedium:
		return 1
	default:
		return 0
	}
}

func summarizeAIEvidenceGroup(group aiEvidenceGroup) string {
	if len(group.EvidenceIDs) == 1 {
		return fmt.Sprintf("%s evidence for %s, reliability=%s", group.EvidenceType, group.Key, group.SourceReliability)
	}
	return fmt.Sprintf("%s evidence group for %s, reliability=%s, merged=%d", group.EvidenceType, group.Key, group.SourceReliability, len(group.EvidenceIDs))
}

func buildAIEvidenceCuratorCoverage(contract aiEvidenceContract, groups []aiEvidenceGroup) map[string]string {
	coverage := map[string]string{}
	groupsByKey := map[string][]aiEvidenceGroup{}
	for _, group := range groups {
		groupsByKey[group.Key] = append(groupsByKey[group.Key], group)
	}
	for _, requirement := range append(append([]aiEvidenceRequirement{}, contract.Required...), contract.Recommended...) {
		if requirement.Key == "" {
			continue
		}
		requirementGroups := groupsByKey[requirement.Key]
		if len(requirementGroups) == 0 {
			coverage[requirement.Key] = aiEvidenceCoverageMissing
			continue
		}
		state := aiEvidenceCoveragePartial
		for _, group := range requirementGroups {
			if aiEvidenceGroupCoversRequirement(requirement.Key, group.SourceReliability) {
				state = aiEvidenceCoverageCovered
				break
			}
		}
		coverage[requirement.Key] = state
	}
	return coverage
}

func aiEvidenceGroupCoversRequirement(contractKey, reliability string) bool {
	if reliability == aiEvidenceReliabilityHighSmartLatest || reliability == aiEvidenceReliabilityHighCurrentFile || reliability == aiEvidenceReliabilityTestFixtureForTestTask {
		return true
	}
	if reliability == aiEvidenceReliabilityMediumBranchCandidate {
		switch contractKey {
		case "branch_candidates", "branch_status", "source_scope", "commit_evidence", "merge_status", "stale_branch_risk":
			return true
		default:
			return false
		}
	}
	return reliability == aiEvidenceReliabilityMedium
}

func buildAIEvidenceCuratorCoverageReport(contract aiEvidenceContract, coverage map[string]string, excluded []aiEvidenceExclusion) aiContractCoverageReport {
	report := aiContractCoverageReport{
		ContractID:         contract.ContractID,
		Status:             "pass",
		Covered:            []string{},
		Partial:            []string{},
		MissingRequired:    []string{},
		MissingRecommended: []string{},
		NextAction:         aiEvidenceCuratorNextActionContractChecker,
		Details:            map[string]string{},
	}
	for _, requirement := range contract.Required {
		state := coverage[requirement.Key]
		switch state {
		case aiEvidenceCoverageCovered:
			report.Covered = append(report.Covered, requirement.Key)
		case aiEvidenceCoveragePartial:
			report.Partial = append(report.Partial, requirement.Key)
			report.Details[requirement.Key] = "only lower-reliability evidence is available"
		default:
			report.MissingRequired = append(report.MissingRequired, requirement.Key)
			report.Details[requirement.Key] = "missing accepted evidence from curator perspective"
		}
	}
	for _, requirement := range contract.Recommended {
		state := coverage[requirement.Key]
		switch state {
		case aiEvidenceCoverageCovered:
			report.Covered = append(report.Covered, requirement.Key)
		case aiEvidenceCoveragePartial:
			report.Partial = append(report.Partial, requirement.Key)
			report.Details[requirement.Key] = "only lower-reliability evidence is available"
		default:
			report.MissingRecommended = append(report.MissingRecommended, requirement.Key)
		}
	}
	if len(report.MissingRequired) > 0 {
		report.Status = "missing_required"
		report.NextAction = aiEvidenceCuratorNextActionRetrieveMissing
	} else if len(report.Partial) > 0 {
		report.Status = "partial"
	}
	if len(excluded) > 0 {
		report.Details["excluded"] = summarizeAIEvidenceExclusions(excluded)
	}
	if len(report.Details) == 0 {
		report.Details = nil
	}
	return report
}

func summarizeAIEvidenceExclusions(excluded []aiEvidenceExclusion) string {
	reasons := map[string]int{}
	for _, item := range excluded {
		reasons[item.Reason]++
	}
	parts := make([]string, 0, len(reasons))
	for reason, count := range reasons {
		parts = append(parts, fmt.Sprintf("%s=%d", reason, count))
	}
	sort.Strings(parts)
	return "excluded evidence: " + strings.Join(parts, ", ")
}

func buildAIEvidenceCuratorStep(frame *aiTaskFrame, contract *aiEvidenceContract, curation aiEvidenceCurationResult, rawCount int) AIAgentStep {
	now := nowString()
	input := map[string]any{
		"raw_evidence_count": rawCount,
	}
	if frame != nil {
		input["intent"] = frame.Intent
	}
	if contract != nil {
		input["contract"] = summarizeAIEvidenceContract(*contract)
	}
	return AIAgentStep{
		AgentName:  "evidence_curator",
		StepType:   "deterministic",
		Status:     "success",
		InputJSON:  encodeJSON(input),
		OutputJSON: encodeJSON(map[string]any{"evidence_bundle": curation.Bundle, "coverage": curation.Coverage, "annotations": curation.Annotations, "included_count": len(curation.Evidence), "excluded_count": len(curation.ExcludedEvidence)}),
		CreatedAt:  now,
		FinishedAt: now,
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
