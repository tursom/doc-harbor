package app

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	aiAnswerVerificationStatusPass                 = "pass"
	aiAnswerVerificationStatusFailed               = "failed"
	aiAnswerVerificationStatusVerificationFailed   = "verification_failed"
	aiAnswerVerificationNextActionPass             = "pass"
	aiAnswerVerificationNextActionRewriteAnswer    = "rewrite_answer"
	aiAnswerVerificationNextActionRetrieveMore     = "retrieve_more"
	aiAnswerVerificationNextActionCompleteWithGaps = "complete_with_gaps"
	aiAnswerVerificationNextActionBlockAnswer      = "block_answer"
)

var (
	aiAnswerCitationPattern            = regexp.MustCompile(`\[C([0-9]+)\]`)
	aiAnswerSQLKeywordPattern          = regexp.MustCompile(`(?i)\b(update|select|insert\s+into|delete\s+from)\b`)
	aiAnswerSQLNeedsPlaceholderPattern = regexp.MustCompile(`(?i)\b(where|set|values)\b`)
	aiAnswerSQLPlaceholderPattern      = regexp.MustCompile(`(\?)|(\$[0-9]+)|(:[A-Za-z_][A-Za-z0-9_]*)|(@[A-Za-z_][A-Za-z0-9_]*)`)
	aiAnswerServiceFieldPattern        = regexp.MustCompile(`(?:服务名|所属服务)\s*[:：]\s*([A-Za-z0-9_.\-/\p{Han}]+)`)
	aiAnswerBranchPromotionMarkers     = []string{"智能最新", "当前最新", "最新事实", "当前事实", "已上线", "已经上线", "已合入", "已经合入", "主分支"}
	aiAnswerBranchScopeNegationMarkers = []string{"尚未确认", "未确认", "无法确认", "不能确认", "尚未证实", "未证实", "没有证据", "暂无证据", "无证据", "不代表", "并非", "不是", "尚未合入", "未合入", "尚未上线", "未上线"}
	aiAnswerBranchScopeSeparators      = []string{"。", "；", ";", "\n", "，", ",", "但", "但是", "不过", "然而"}
)

type aiAnswerVerificationContext struct {
	EvidenceCount    int
	RetrievalRound   int
	ServiceNames     []string
	RewriteAttempted bool
}

type aiAnswerVerificationOutcome struct {
	Result             aiModelResult
	Report             aiAnswerVerificationReport
	RunStatus          string
	VerificationStatus string
}

func verifyAIAnswer(frame aiTaskFrame, contract aiEvidenceContract, coverage aiContractCoverageReport, bundle aiEvidenceBundle, draftAnswer string) aiAnswerVerificationReport {
	return verifyAIAnswerWithContext(frame, contract, coverage, bundle, draftAnswer, aiAnswerVerificationContext{})
}

func verifyAIAnswerWithContext(frame aiTaskFrame, contract aiEvidenceContract, coverage aiContractCoverageReport, bundle aiEvidenceBundle, draftAnswer string, verifyContext aiAnswerVerificationContext) aiAnswerVerificationReport {
	if strings.TrimSpace(frame.Intent) == "" {
		frame.Intent = aiTaskIntentDocumentQA
	}
	intent := normalizeAITaskIntent(frame.Intent)
	if strings.TrimSpace(contract.ContractID) == "" {
		contract = buildAIEvidenceContract(frame)
	}
	if coverage.ContractID == "" && len(coverage.Items) == 0 && len(bundle.Groups) > 0 {
		coverage = checkAIEvidenceContract(contract, bundle)
	}
	displayEvidenceCount := verifyContext.EvidenceCount
	if displayEvidenceCount <= 0 {
		displayEvidenceCount = aiEvidenceBundleIncludedEvidenceCount(bundle)
	}
	report := aiAnswerVerificationReport{
		AgentWorkflowVersion: aiAgentWorkflowVersionV2Shadow,
		AnswerMode:           "answer_verifier",
		Status:               aiAnswerVerificationStatusPass,
		Details:              []string{},
		PassedChecks:         []string{},
		FailedChecks:         []string{},
		NextAction:           aiAnswerVerificationNextActionPass,
		RewriteAttempted:     verifyContext.RewriteAttempted,
	}
	failed := map[string]struct{}{}
	pass := func(check string) {
		if _, ok := failed[check]; ok {
			return
		}
		report.PassedChecks = append(report.PassedChecks, check)
	}
	fail := func(check, detail string) {
		if _, ok := failed[check]; ok {
			return
		}
		failed[check] = struct{}{}
		report.FailedChecks = append(report.FailedChecks, check)
		if detail != "" {
			report.Details = append(report.Details, sanitizeProviderError(detail))
		}
	}

	answer := strings.TrimSpace(draftAnswer)
	answerLower := strings.ToLower(answer)
	citationRefs := extractAIAnswerCitationRefs(answer)
	missingRequired := aiAnswerVerificationMissingRequired(coverage)

	if answer == "" {
		fail("empty_answer", "model returned an empty answer")
	} else {
		pass("non_empty_answer")
	}
	if aiAnswerContainsSecret(answer) {
		fail("secret_exposure", "answer contains a secret-like token or authorization header")
	} else {
		pass("no_secret_exposure")
	}
	if aiAnswerClaimsUnauthorizedSideEffect(answerLower) {
		fail("unauthorized_behavior", "answer claims it executed SQL, modified the database, or refreshed cache")
	} else {
		pass("no_unauthorized_behavior")
	}
	if displayEvidenceCount > 0 {
		if len(citationRefs) == 0 {
			fail("citation_coverage", "answer has evidence available but does not cite any [C#] source")
		} else {
			pass("citation_coverage")
		}
		for _, ref := range citationRefs {
			if !aiAnswerCitationRefExists(ref, displayEvidenceCount) {
				fail("citation_not_found", "answer cites a source label that is not present in the displayed evidence")
				break
			}
		}
		if !hasFailedCheck(failed, "citation_not_found") && len(citationRefs) > 0 {
			pass("citation_exists")
		}
	}

	if len(missingRequired) > 0 && aiAnswerContainsDeterministicOperation(answerLower, intent) {
		fail("missing_required_deterministic_steps", "required evidence is missing but the answer gives deterministic operation steps")
	} else {
		pass("required_gap_policy")
	}

	sqlStatements := extractAIAnswerSQLStatements(answer)
	if len(sqlStatements) > 0 {
		for _, statement := range sqlStatements {
			if aiAnswerSQLNeedsPlaceholderPattern.MatchString(statement) && !aiAnswerSQLPlaceholderPattern.MatchString(statement) {
				fail("sql_missing_placeholder", "SQL examples must use placeholders instead of concrete values")
			}
			if intent == aiTaskIntentDatabaseDirectUpdateForTest && !aiAnswerSQLContextHasCitation(answer, statement) {
				fail("sql_missing_citation", "SQL table, fields, and WHERE conditions must be tied to citations")
			}
		}
		if !hasFailedCheck(failed, "sql_missing_placeholder") {
			pass("sql_placeholders")
		}
		if intent == aiTaskIntentDatabaseDirectUpdateForTest && !hasFailedCheck(failed, "sql_missing_citation") {
			pass("sql_citations")
		}
	}
	if intent == aiTaskIntentCodePathExplanation && aiAnswerSuggestsDirectDatabaseUpdate(answerLower, sqlStatements) {
		fail("code_path_direct_sql", "code-path answers must not recommend direct database UPDATE unless the user explicitly asked for database direct update")
	} else {
		pass("code_path_no_direct_sql")
	}

	if !aiTaskFrameLooksTestFocused(frame) && aiAnswerUsesTestFixtureAsRuntimeFact(answer, bundle) {
		fail("test_fixture_pollution", "non-test answer uses _test.go or fixture evidence as business fact")
	} else {
		pass("no_test_fixture_pollution")
	}

	if aiAnswerHasBranchCandidateEvidence(bundle) && aiAnswerPromotesBranchCandidateAsLatest(answer, bundle) {
		fail("branch_scope_mismatch", "branch-candidate evidence is presented as smart-latest or merged fact")
	} else {
		pass("branch_scope")
	}

	if aiAnswerLooksUnsupportedRefusal(answerLower, intent, coverage) {
		fail("unsupported_refusal", "required evidence is covered, but the answer refuses because no official UPDATE example exists")
	} else {
		pass("no_unsupported_refusal")
	}

	if serviceName := aiAnswerInventedServiceName(answer, verifyContext.ServiceNames); serviceName != "" {
		fail("invented_service_name", "answer names a service not present in the retrieved service candidates")
	} else {
		pass("service_names_supported")
	}

	finalizeAIAnswerVerificationReport(&report, failed, coverage, missingRequired, verifyContext)
	return report
}

func finalizeAIAnswerVerificationReport(report *aiAnswerVerificationReport, failed map[string]struct{}, coverage aiContractCoverageReport, missingRequired []string, verifyContext aiAnswerVerificationContext) {
	report.PassedChecks = uniqueStrings(report.PassedChecks)
	report.FailedChecks = uniqueStrings(report.FailedChecks)
	report.Details = uniqueStrings(report.Details)
	sort.Strings(report.PassedChecks)
	sort.Strings(report.FailedChecks)
	if len(report.FailedChecks) == 0 {
		if len(missingRequired) > 0 || aiRetrievalHasCompletedGaps(&coverage) {
			report.Status = aiWorkflowStatusCompletedWithGaps
			report.WorkflowStatus = aiWorkflowStatusCompletedWithGaps
			report.Reason = "required_gaps"
			if verifyContext.RetrievalRound > 0 && verifyContext.RetrievalRound < aiRetrievalMaxRounds {
				report.NextAction = aiAnswerVerificationNextActionRetrieveMore
			} else {
				report.NextAction = aiAnswerVerificationNextActionCompleteWithGaps
			}
			report.Details = append(report.Details, "required evidence remains incomplete; answer must stay conservative")
			return
		}
		report.Status = aiAnswerVerificationStatusPass
		report.NextAction = aiAnswerVerificationNextActionPass
		return
	}
	report.Status = aiAnswerVerificationStatusFailed
	report.Reason = report.FailedChecks[0]
	switch {
	case hasFailedCheck(failed, "secret_exposure") || hasFailedCheck(failed, "unauthorized_behavior"):
		report.NextAction = aiAnswerVerificationNextActionBlockAnswer
	case hasFailedCheck(failed, "missing_required_deterministic_steps"):
		if verifyContext.RetrievalRound > 0 && verifyContext.RetrievalRound < aiRetrievalMaxRounds {
			report.NextAction = aiAnswerVerificationNextActionRetrieveMore
		} else {
			report.Status = aiWorkflowStatusCompletedWithGaps
			report.WorkflowStatus = aiWorkflowStatusCompletedWithGaps
			report.NextAction = aiAnswerVerificationNextActionCompleteWithGaps
		}
	case len(missingRequired) > 0 && verifyContext.RetrievalRound > 0 && verifyContext.RetrievalRound < aiRetrievalMaxRounds:
		report.NextAction = aiAnswerVerificationNextActionRetrieveMore
	default:
		report.NextAction = aiAnswerVerificationNextActionRewriteAnswer
	}
	if report.NextAction == aiAnswerVerificationNextActionCompleteWithGaps {
		report.WorkflowStatus = aiWorkflowStatusCompletedWithGaps
	}
}

func (s *Server) verifyAndMaybeRewriteAIAnswer(ctx context.Context, cfg AIConfigVersion, question string, retrieval aiRetrievalResult, result aiModelResult, start time.Time) aiAnswerVerificationOutcome {
	frame, contract, coverage, bundle := aiAnswerVerifierInputs(retrieval)
	verifyContext := aiAnswerVerificationContext{
		EvidenceCount:  len(retrieval.Evidence),
		RetrievalRound: len(retrieval.Rounds),
		ServiceNames:   aiVerificationServiceNames(retrieval.ServiceCandidates),
	}
	report := verifyAIAnswerWithContext(frame, contract, coverage, bundle, result.Content, verifyContext)
	finalResult := result

	if report.NextAction == aiAnswerVerificationNextActionRewriteAnswer && cfg.Config.Enabled && len(retrieval.Evidence) > 0 {
		report.RewriteAttempted = true
		rewriteResult, err := s.rewriteAIAnswer(ctx, cfg.Config, question, retrieval, result.Content, report)
		if err == nil && strings.TrimSpace(rewriteResult.Content) != "" {
			rewriteContext := verifyContext
			rewriteContext.RewriteAttempted = true
			rewriteReport := verifyAIAnswerWithContext(frame, contract, coverage, bundle, rewriteResult.Content, rewriteContext)
			rewriteReport.RewriteAttempted = true
			if rewriteReport.NextAction == aiAnswerVerificationNextActionPass || rewriteReport.Status == aiWorkflowStatusCompletedWithGaps {
				finalResult = rewriteResult
				report = rewriteReport
			} else {
				report = rewriteReport
				report.RewriteAttempted = true
				finalResult = aiVerifierConservativeModelResult(question, retrieval, report, start)
				if report.Status != aiWorkflowStatusCompletedWithGaps {
					report.Status = aiAnswerVerificationStatusVerificationFailed
				}
			}
		} else {
			if err != nil {
				report.Details = append(report.Details, "rewrite failed: "+sanitizeProviderError(err.Error()))
			}
			finalResult = aiVerifierConservativeModelResult(question, retrieval, report, start)
			report.RewriteAttempted = true
			if report.Status != aiWorkflowStatusCompletedWithGaps {
				report.Status = aiAnswerVerificationStatusVerificationFailed
			}
		}
	} else if report.NextAction != aiAnswerVerificationNextActionPass {
		finalResult = aiVerifierConservativeModelResult(question, retrieval, report, start)
		if report.NextAction == aiAnswerVerificationNextActionRewriteAnswer || report.NextAction == aiAnswerVerificationNextActionBlockAnswer {
			report.Status = aiAnswerVerificationStatusVerificationFailed
		}
	}
	report.Details = uniqueStrings(report.Details)
	runStatus, verificationStatus := aiRunStatusesForVerification(report, len(retrieval.Evidence))
	return aiAnswerVerificationOutcome{
		Result:             finalResult,
		Report:             report,
		RunStatus:          runStatus,
		VerificationStatus: verificationStatus,
	}
}

func (s *Server) rewriteAIAnswer(ctx context.Context, cfg AIConfigData, question string, retrieval aiRetrievalResult, draftAnswer string, report aiAnswerVerificationReport) (aiModelResult, error) {
	messages := buildAIAnswerRewriteMessages(question, retrieval, draftAnswer, report)
	return s.callRoutedAIChat(ctx, cfg, messages, 0.1, 0)
}

func buildAIAnswerRewriteMessages(question string, retrieval aiRetrievalResult, draftAnswer string, report aiAnswerVerificationReport) []aiChatMessage {
	messages := buildAIChatMessages(question, retrieval)
	rewriteInstruction := strings.Builder{}
	rewriteInstruction.WriteString("\n\nAnswer Verifier 未通过，必须只基于同一批证据重写最终答案。\n")
	rewriteInstruction.WriteString("Verifier Report：\n")
	rewriteInstruction.WriteString(encodeJSON(report))
	rewriteInstruction.WriteString("\n\n原始答案草稿（只用于修正，不是事实来源）：\n")
	rewriteInstruction.WriteString(sanitizeProviderError(draftAnswer))
	rewriteInstruction.WriteString("\n\n重写要求：\n")
	rewriteInstruction.WriteString("- 修复 failed_checks 指出的所有问题。\n")
	rewriteInstruction.WriteString("- 不得声称已经执行 SQL、修改数据库或刷新缓存。\n")
	if retrieval.TaskFrame != nil && aiIntentIsCodePathExplanation(retrieval.TaskFrame.Intent) {
		rewriteInstruction.WriteString("- 当前是代码路径问题，除非用户明确要求数据库直改，否则不得输出 SQL/UPDATE 作为修改方案；改为说明入口、调用链、写入点、持久化对象和副作用。\n")
	} else {
		rewriteInstruction.WriteString("- SQL 示例必须使用占位符，字段、表和 WHERE 条件必须带 [C#] 引用。\n")
	}
	rewriteInstruction.WriteString("- 非测试问题不得把 _test.go 或 fixture 当业务事实。\n")
	rewriteInstruction.WriteString("- 功能分支来源必须标注“功能分支候选”。\n")
	rewriteInstruction.WriteString("- 不要输出 provider 错误、API key、token 或内部配置。\n")
	if len(messages) == 0 {
		return []aiChatMessage{
			{Role: "system", Content: "你是 DocHarbor 的只读 Answer Rewriter，只能基于提供证据修正答案。"},
			{Role: "user", Content: rewriteInstruction.String()},
		}
	}
	messages = append([]aiChatMessage(nil), messages...)
	messages[len(messages)-1].Content += rewriteInstruction.String()
	return messages
}

func buildAIAnswerVerifierStep(frame aiTaskFrame, contract aiEvidenceContract, coverage aiContractCoverageReport, bundle aiEvidenceBundle, report aiAnswerVerificationReport, answer string) AIAgentStep {
	now := nowString()
	return AIAgentStep{
		AgentName: "answer_verifier",
		StepType:  "deterministic",
		Status:    "success",
		InputJSON: encodeJSON(map[string]any{
			"task_frame":         frame,
			"contract":           summarizeAIEvidenceContract(contract),
			"coverage":           summarizeAIContractCoverageReport(coverage),
			"bundle_id":          bundle.BundleID,
			"bundle_group_count": len(bundle.Groups),
			"answer_size":        len(answer),
		}),
		OutputJSON: encodeJSON(map[string]any{"verification_report": report}),
		CreatedAt:  now,
		FinishedAt: now,
	}
}

func aiVerifierConservativeModelResult(question string, retrieval aiRetrievalResult, report aiAnswerVerificationReport, start time.Time) aiModelResult {
	reason := report.NextAction
	if reason == "" {
		reason = report.Reason
	}
	return aiModelResult{
		Content:        localEvidenceAnswer(question, retrieval, false),
		ProviderName:   "local-verifier",
		Model:          "none",
		ModelRouteJSON: encodeJSON(map[string]any{"mode": "local_verifier_fallback", "reason": reason}),
		LatencyMS:      int(time.Since(start).Milliseconds()),
	}
}

func aiAnswerVerifierInputs(retrieval aiRetrievalResult) (aiTaskFrame, aiEvidenceContract, aiContractCoverageReport, aiEvidenceBundle) {
	frame := aiAnswerComposerFrame(&retrieval)
	contract := aiAnswerComposerContract(&retrieval, frame)
	bundle := aiAnswerComposerBundle(&retrieval)
	coverage := aiAnswerComposerCoverage(&retrieval, contract, bundle)
	return frame, contract, coverage, bundle
}

func aiRunStatusesForVerification(report aiAnswerVerificationReport, evidenceCount int) (string, string) {
	if evidenceCount == 0 {
		return "insufficient_evidence", "fail"
	}
	switch report.Status {
	case aiAnswerVerificationStatusPass:
		return "succeeded", aiAnswerVerificationStatusPass
	case aiWorkflowStatusCompletedWithGaps:
		return aiWorkflowStatusCompletedWithGaps, aiWorkflowStatusCompletedWithGaps
	case aiAnswerVerificationStatusVerificationFailed:
		return aiAnswerVerificationStatusVerificationFailed, aiAnswerVerificationStatusVerificationFailed
	default:
		if report.NextAction == aiAnswerVerificationNextActionCompleteWithGaps {
			return aiWorkflowStatusCompletedWithGaps, aiWorkflowStatusCompletedWithGaps
		}
		return aiAnswerVerificationStatusVerificationFailed, aiAnswerVerificationStatusFailed
	}
}

func extractAIAnswerCitationRefs(answer string) []int {
	matches := aiAnswerCitationPattern.FindAllStringSubmatch(answer, -1)
	refs := make([]int, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		ref, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		refs = append(refs, ref)
	}
	sort.Ints(refs)
	return refs
}

func extractAIAnswerSQLStatements(answer string) []string {
	statements := []string{}
	for _, block := range extractAIFencedBlocks(answer) {
		if aiAnswerSQLKeywordPattern.MatchString(block) {
			statements = append(statements, strings.TrimSpace(block))
		}
	}
	for _, line := range strings.Split(answer, "\n") {
		line = strings.TrimSpace(line)
		if aiAnswerLineLooksLikeSQLStatement(line) {
			statements = append(statements, line)
		}
	}
	return uniqueStrings(statements)
}

func aiAnswerLineLooksLikeSQLStatement(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	lower = strings.TrimLeft(lower, "-* \t>0123456789.)")
	switch {
	case strings.HasPrefix(lower, "update "):
		return strings.Contains(lower, " set ")
	case strings.HasPrefix(lower, "select "):
		return strings.Contains(lower, " from ")
	case strings.HasPrefix(lower, "insert into "):
		return true
	case strings.HasPrefix(lower, "delete from "):
		return true
	default:
		return false
	}
}

func extractAIFencedBlocks(answer string) []string {
	blocks := []string{}
	inBlock := false
	var current strings.Builder
	for _, line := range strings.Split(answer, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inBlock {
				blocks = append(blocks, current.String())
				current.Reset()
				inBlock = false
			} else {
				inBlock = true
			}
			continue
		}
		if inBlock {
			current.WriteString(line)
			current.WriteByte('\n')
		}
	}
	if inBlock && current.Len() > 0 {
		blocks = append(blocks, current.String())
	}
	return blocks
}

func aiAnswerSQLContextHasCitation(answer, statement string) bool {
	if aiAnswerCitationPattern.MatchString(statement) {
		return true
	}
	index := strings.Index(answer, statement)
	if index < 0 {
		return len(extractAIAnswerCitationRefs(answer)) > 0
	}
	start := max(0, index-400)
	end := min(len(answer), index+len(statement)+400)
	return aiAnswerCitationPattern.MatchString(answer[start:end])
}

func aiAnswerContainsSecret(answer string) bool {
	return secretTokenPattern.MatchString(answer) ||
		bearerTokenPattern.MatchString(answer) ||
		diagnosticsSummaryAuthHeaderPattern.MatchString(answer) ||
		diagnosticsSummarySensitiveKVPattern.MatchString(answer) ||
		diagnosticsSummarySecretPhrasePattern.MatchString(answer) ||
		diagnosticsSummaryJWTPattern.MatchString(answer)
}

func aiAnswerClaimsUnauthorizedSideEffect(answerLower string) bool {
	for _, marker := range []string{
		"已经执行 sql", "已执行 sql", "我已执行", "我已经执行", "执行了 sql",
		"已经修改数据库", "已修改数据库", "已经改库", "已改库", "已经更新数据库",
		"已更新数据库", "已经刷新缓存", "已刷新缓存", "刷新了缓存",
		"sql 已执行", "update 已执行", "数据库已修改",
	} {
		if strings.Contains(answerLower, marker) {
			return true
		}
	}
	return false
}

func aiAnswerContainsDeterministicOperation(answerLower, intent string) bool {
	if aiAnswerSQLKeywordPattern.MatchString(answerLower) {
		return true
	}
	if intent == aiTaskIntentDatabaseDirectUpdateForTest {
		for _, marker := range []string{"执行以下", "直接执行", "按下面步骤执行", "可以直接改", "把字段改成", "更新为"} {
			if strings.Contains(answerLower, marker) {
				return true
			}
		}
	}
	return false
}

func aiAnswerSuggestsDirectDatabaseUpdate(answerLower string, sqlStatements []string) bool {
	if len(sqlStatements) > 0 {
		return true
	}
	for _, marker := range []string{
		"直接修改数据库", "直接改数据库", "直接改库", "数据库操作完成", "通过数据库操作",
		"只能通过数据库", "update ", " update", "修改数据库表", "直改表",
	} {
		if aiAnswerContainsUnnegatedMarker(answerLower, marker) {
			return true
		}
	}
	return false
}

func aiAnswerContainsUnnegatedMarker(answerLower, marker string) bool {
	searchFrom := 0
	for {
		index := strings.Index(answerLower[searchFrom:], marker)
		if index < 0 {
			return false
		}
		index += searchFrom
		start := max(0, index-24)
		end := min(len(answerLower), index+len(marker)+24)
		context := answerLower[start:end]
		negated := false
		for _, negation := range []string{
			"不要", "不得", "不能", "不应", "不可", "禁止", "避免", "不是", "并非", "不建议", "无需", "没有必要",
			"do not", "don't", "should not", "must not", "not ",
		} {
			if strings.Contains(context, negation) {
				negated = true
				break
			}
		}
		if !negated {
			return true
		}
		searchFrom = index + len(marker)
	}
}

func aiAnswerUsesTestFixtureAsRuntimeFact(answer string, bundle aiEvidenceBundle) bool {
	lower := strings.ToLower(answer)
	if aiPathLooksTest(lower) || strings.Contains(lower, "_test.go") {
		return true
	}
	for _, excluded := range bundle.Excluded {
		if excluded.Reason != aiEvidenceExcludedReasonTestFixtureNonTestTask && !aiPathLooksTest(excluded.FilePath) {
			continue
		}
		if excluded.FilePath != "" && strings.Contains(lower, strings.ToLower(normalizeRepoPath(excluded.FilePath))) {
			return true
		}
	}
	return false
}

func aiAnswerHasBranchCandidateEvidence(bundle aiEvidenceBundle) bool {
	for _, group := range bundle.Groups {
		if group.SourceReliability == aiEvidenceReliabilityMediumBranchCandidate {
			return true
		}
	}
	for _, excluded := range bundle.Excluded {
		if excluded.SourceScope == "branch_candidate" || excluded.SourceReliability == aiEvidenceReliabilityMediumBranchCandidate {
			return true
		}
	}
	return false
}

func aiAnswerHasSmartLatestEvidence(bundle aiEvidenceBundle) bool {
	for _, group := range bundle.Groups {
		switch group.SourceReliability {
		case aiEvidenceReliabilityHighSmartLatest, aiEvidenceReliabilityHighCurrentFile:
			return true
		}
	}
	for _, excluded := range bundle.Excluded {
		switch excluded.SourceReliability {
		case aiEvidenceReliabilityHighSmartLatest, aiEvidenceReliabilityHighCurrentFile:
			return true
		}
	}
	return false
}

func aiAnswerPromotesBranchCandidateAsLatest(answer string, bundle aiEvidenceBundle) bool {
	hasSmartLatestEvidence := aiAnswerHasSmartLatestEvidence(bundle)
	for _, marker := range aiAnswerBranchPromotionMarkers {
		searchFrom := 0
		for {
			index := strings.Index(answer[searchFrom:], marker)
			if index < 0 {
				break
			}
			index += searchFrom
			if !aiAnswerBranchPromotionMarkerNegated(answer, index) {
				if hasSmartLatestEvidence && !aiAnswerBranchPromotionClauseMentionsBranchCandidate(answer, index) {
					searchFrom = index + len(marker)
					continue
				}
				return true
			}
			searchFrom = index + len(marker)
		}
	}
	return false
}

func aiAnswerBranchPromotionClauseMentionsBranchCandidate(answer string, markerIndex int) bool {
	clause := aiAnswerClauseAroundIndex(answer, markerIndex)
	for _, marker := range []string{"功能分支候选", "候选分支", "branch_candidate", "branch candidate"} {
		if strings.Contains(clause, marker) {
			return true
		}
	}
	return false
}

func aiAnswerBranchPromotionMarkerNegated(answer string, markerIndex int) bool {
	if markerIndex <= 0 {
		return false
	}
	context := aiAnswerClausePrefixBeforeIndex(answer, markerIndex)
	for _, marker := range aiAnswerBranchScopeNegationMarkers {
		if strings.Contains(context, marker) {
			return true
		}
	}
	return false
}

func aiAnswerClausePrefixBeforeIndex(answer string, markerIndex int) string {
	if markerIndex <= 0 {
		return ""
	}
	prefix := answer[:markerIndex]
	clauseStart := -1
	for _, separator := range aiAnswerBranchScopeSeparators {
		if index := strings.LastIndex(prefix, separator); index > clauseStart {
			clauseStart = index + len(separator)
		}
	}
	if clauseStart >= 0 && clauseStart < len(prefix) {
		return prefix[clauseStart:]
	}
	return prefix
}

func aiAnswerClauseAroundIndex(answer string, markerIndex int) string {
	if markerIndex < 0 {
		return ""
	}
	start := 0
	prefix := answer[:markerIndex]
	for _, separator := range aiAnswerBranchScopeSeparators {
		if index := strings.LastIndex(prefix, separator); index >= 0 && index+len(separator) > start {
			start = index + len(separator)
		}
	}
	end := len(answer)
	suffix := answer[markerIndex:]
	for _, separator := range aiAnswerBranchScopeSeparators {
		if index := strings.Index(suffix, separator); index >= 0 && markerIndex+index < end {
			end = markerIndex + index
		}
	}
	if start > end {
		return ""
	}
	return answer[start:end]
}

func aiAnswerLooksUnsupportedRefusal(answerLower, intent string, coverage aiContractCoverageReport) bool {
	if intent != aiTaskIntentDatabaseDirectUpdateForTest || len(aiAnswerVerificationMissingRequired(coverage)) > 0 {
		return false
	}
	refusalMarkers := []string{"不能提供", "无法提供", "不能给出", "无法给出", "不能操作", "证据不足", "未确认"}
	sqlMarkers := []string{"update", "sql", "数据库直改", "修改数据库", "没有官方 update", "没有现成 update"}
	hasRefusal := false
	for _, marker := range refusalMarkers {
		if strings.Contains(answerLower, marker) {
			hasRefusal = true
			break
		}
	}
	if !hasRefusal {
		return false
	}
	for _, marker := range sqlMarkers {
		if strings.Contains(answerLower, marker) {
			return true
		}
	}
	return false
}

func aiAnswerInventedServiceName(answer string, serviceNames []string) string {
	if len(serviceNames) == 0 {
		return ""
	}
	known := map[string]struct{}{}
	for _, name := range serviceNames {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			known[name] = struct{}{}
		}
	}
	for _, match := range aiAnswerServiceFieldPattern.FindAllStringSubmatch(answer, -1) {
		if len(match) < 2 {
			continue
		}
		name := strings.ToLower(strings.Trim(strings.TrimSpace(match[1]), "，。；;,."))
		if name == "" {
			continue
		}
		if _, ok := known[name]; !ok {
			return match[1]
		}
	}
	return ""
}

func aiAnswerVerificationMissingRequired(coverage aiContractCoverageReport) []string {
	missing := append([]string(nil), coverage.MissingRequired...)
	for _, item := range coverage.Items {
		if item.Requirement != aiEvidenceCheckerRequirementRequired {
			continue
		}
		switch normalizeAIContractCoverageStatus(item.Status) {
		case aiEvidenceCoverageMissing, aiEvidenceCoveragePartial:
			missing = append(missing, item.Key)
		}
	}
	if coverage.Status == "missing_required" || coverage.Status == aiWorkflowStatusCompletedWithGaps {
		for _, item := range coverage.Items {
			if item.Requirement == aiEvidenceCheckerRequirementRequired && normalizeAIContractCoverageStatus(item.Status) != aiEvidenceCoverageCovered {
				missing = append(missing, item.Key)
			}
		}
	}
	return uniqueStrings(missing)
}

func aiEvidenceBundleIncludedEvidenceCount(bundle aiEvidenceBundle) int {
	includedEvidenceIDs := map[int64]struct{}{}
	for _, group := range bundle.Groups {
		for _, id := range group.EvidenceIDs {
			if id > 0 {
				includedEvidenceIDs[id] = struct{}{}
			}
		}
	}
	return len(includedEvidenceIDs)
}

func aiAnswerCitationRefExists(ref int, evidenceCount int) bool {
	if ref <= 0 {
		return false
	}
	return ref <= evidenceCount
}

func aiVerificationServiceNames(candidates []AIServiceCandidate) []string {
	names := []string{}
	for _, candidate := range candidates {
		names = append(names, candidate.ServiceName, candidate.RepoName)
	}
	return uniqueStrings(names)
}

func hasFailedCheck(failed map[string]struct{}, check string) bool {
	_, ok := failed[check]
	return ok
}
