package app

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

const (
	aiRetrievalMaxRounds              = 3
	aiWorkflowStatusCompletedWithGaps = "completed_with_gaps"
)

type aiSearchPlanResult struct {
	Evidence []aiEvidence
}

func planAIRetrievalRound(frame *aiTaskFrame, contract *aiEvidenceContract, coverage *aiContractCoverageReport, existingBundle *aiEvidenceBundle, round int) aiRetrievalRoundPlan {
	if round <= 0 {
		round = 1
	}
	intent := aiTaskIntentDocumentQA
	if frame != nil && strings.TrimSpace(frame.Intent) != "" {
		intent = normalizeAITaskIntent(frame.Intent)
	}
	missingKeys := aiRetrievalMissingContractKeys(contract, coverage, round)
	terms := aiRetrievalFrameTerms(frame)
	querySource := "task_frame_known_terms"
	if frame != nil && len(frame.GeneratedTerms) > 0 {
		querySource += ",task_frame_generated_terms"
	}
	if round > 1 {
		terms = mergeTerms(terms, aiRetrievalTermsForContractKeys(missingKeys), aiRetrievalTermsFromBundle(existingBundle))
		querySource += ",contract_gap_terms,existing_evidence_summary"
	}
	if len(terms) == 0 {
		terms = aiRetrievalFallbackTerms(intent, missingKeys)
		querySource += ",deterministic_fallback"
	}
	pathHints := aiRetrievalPathHintsForRound(intent, missingKeys, round)
	search := aiRetrievalRoundSearch{
		Tool:      "content_search",
		Query:     strings.Join(terms, " "),
		FileTypes: aiRetrievalFileTypesForRound(intent, missingKeys, round),
		PathHints: pathHints,
		Terms:     terms,
	}
	return aiRetrievalRoundPlan{
		Round:               round,
		Intent:              intent,
		Reason:              aiRetrievalRoundReason(coverage, missingKeys, round),
		MissingContractKeys: missingKeys,
		Searches:            []aiRetrievalRoundSearch{search},
		QuerySource:         querySource,
		PlannerStatus:       "deterministic",
		CoverageDelta:       map[string]string{},
		NextAction:          "execute_search_plan",
	}
}

func (s *Server) executeAISearchPlan(ctx context.Context, plan aiRetrievalRoundPlan, scope AIQuestionScope, cfg AIConfigData) (aiSearchPlanResult, error) {
	scope = normalizeAIScope(scope)
	repos, err := listRepositories(ctx, s.db)
	if err != nil {
		return aiSearchPlanResult{}, err
	}
	repos = filterAIRepos(repos, scope)
	intent := plan.Intent
	if strings.TrimSpace(intent) == "" {
		intent = aiTaskIntentDocumentQA
	}
	maxChunks := cfg.Chat.MaxContextChunks
	if maxChunks <= 0 {
		maxChunks = 24
	}
	evidence := []aiEvidence{}
	addedCurrentFile := false
	for _, search := range plan.Searches {
		terms := aiRetrievalSearchTerms(search)
		if len(terms) == 0 {
			terms = aiRetrievalFallbackTerms(intent, plan.MissingContractKeys)
		}
		if scope.CurrentFile != nil && !addedCurrentFile {
			if current, err := s.currentFileEvidence(ctx, scope.CurrentFile, terms); err == nil && current.Content != "" {
				evidence = append(evidence, current)
			}
			addedCurrentFile = true
		}
		for _, repo := range repos {
			if !repo.Enabled {
				continue
			}
			latestItems, err := s.searchRepoSmartLatestEvidence(ctx, repo, terms, intent, cfg.Indexing)
			if err == nil {
				evidence = append(evidence, aiFilterEvidenceForSearch(latestItems, search)...)
			}
			targets, err := s.aiRefTargets(ctx, repo, scope.SourceMode)
			if err != nil {
				continue
			}
			for _, target := range targets {
				items, err := s.searchRepoRefEvidence(ctx, repo, target, terms, intent, cfg.Indexing)
				if err != nil {
					continue
				}
				evidence = append(evidence, aiFilterEvidenceForSearch(items, search)...)
			}
		}
	}
	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].Score != evidence[j].Score {
			return evidence[i].Score > evidence[j].Score
		}
		return evidence[i].Citation.FilePath < evidence[j].Citation.FilePath
	})
	evidence = dedupeAIEvidence(evidence)
	if len(evidence) > maxChunks {
		evidence = evidence[:maxChunks]
	}
	return aiSearchPlanResult{Evidence: evidence}, nil
}

func mergeAIEvidence(existing, newlyFound []aiEvidence) []aiEvidence {
	merged := make([]aiEvidence, 0, len(existing)+len(newlyFound))
	merged = append(merged, existing...)
	merged = append(merged, newlyFound...)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Citation.SourceScope != merged[j].Citation.SourceScope {
			if merged[i].Citation.SourceScope == "smart_latest" {
				return true
			}
			if merged[j].Citation.SourceScope == "smart_latest" {
				return false
			}
		}
		if merged[i].Score != merged[j].Score {
			return merged[i].Score > merged[j].Score
		}
		return merged[i].Citation.FilePath < merged[j].Citation.FilePath
	})
	return dedupeAIEvidence(merged)
}

func (s *Server) runAIRetrievalOrchestrator(ctx context.Context, frame *aiTaskFrame, contract *aiEvidenceContract, scope AIQuestionScope, cfg AIConfigData) (aiRetrievalResult, error) {
	scope = normalizeAIScope(scope)
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
	}
	if effectiveContract.ContractID == "" {
		effectiveContract = buildAIEvidenceContract(effectiveFrame)
	}
	intent := aiLegacyIntentForTaskFrame(effectiveFrame)
	rawEvidence := []aiEvidence{}
	rounds := []aiRetrievalRoundPlan{}
	steps := []AIAgentStep{}
	var curation aiEvidenceCurationResult
	var bundle *aiEvidenceBundle
	var contractCoverage *aiContractCoverageReport
	for round := 1; round <= aiRetrievalMaxRounds; round++ {
		previousBundle := bundle
		previousCoverage := contractCoverage
		plan := planAIRetrievalRound(&effectiveFrame, &effectiveContract, previousCoverage, previousBundle, round)
		plan.Intent = intent
		found, err := s.executeAISearchPlan(ctx, plan, scope, cfg)
		if err != nil {
			return aiRetrievalResult{}, err
		}
		beforeKeys := aiEvidenceIdentitySet(rawEvidence)
		rawEvidence = mergeAIEvidence(rawEvidence, found.Evidence)
		plan.NewEvidenceCount = aiCountNewEvidence(beforeKeys, rawEvidence)
		curation = curateAIEvidence(&effectiveFrame, &effectiveContract, rawEvidence)
		coverage := checkAIEvidenceContract(effectiveContract, curation.Bundle)
		if round == aiRetrievalMaxRounds && aiContractCoverageStillNeedsRetrieval(&coverage) {
			markAIContractCoverageCompletedWithGaps(&coverage)
		}
		plan.CoverageDelta = aiContractCoverageDelta(previousCoverage, &coverage)
		if plan.CoverageDelta == nil {
			plan.CoverageDelta = map[string]string{}
		}
		rounds = append(rounds, plan)
		steps = append(steps, buildAIRetrievalRoundStep(&effectiveFrame, &effectiveContract, previousCoverage, previousBundle, plan, coverage))
		bundle = &curation.Bundle
		contractCoverage = &coverage
		if !aiContractCoverageStillNeedsRetrieval(contractCoverage) {
			break
		}
	}
	repos, err := listRepositories(ctx, s.db)
	if err != nil {
		return aiRetrievalResult{}, err
	}
	repos = filterAIRepos(repos, scope)
	candidates := buildAIServiceCandidates(repos, curation.Evidence)
	plan := map[string]any{
		"intent":                  intent,
		"source_mode":             scope.SourceMode,
		"repo_mode":               scope.RepoMode,
		"terms":                   effectiveFrame.KnownTerms,
		"generated_terms":         effectiveFrame.GeneratedTerms,
		"chunker_version":         aiChunkerVersion,
		"candidate_branches":      strings.Contains(scope.SourceMode, "branch"),
		"raw_evidence_count":      len(curation.Annotations),
		"curated_evidence_count":  len(curation.Evidence),
		"excluded_evidence_count": len(curation.ExcludedEvidence),
		"evidence_bundle":         curation.Bundle,
		"curator_coverage":        curation.Coverage,
		"retrieval_rounds":        rounds,
		"task_frame":              &effectiveFrame,
		"evidence_contract":       summarizeAIEvidenceContract(effectiveContract),
	}
	if contractCoverage != nil {
		plan["contract_coverage"] = summarizeAIContractCoverageReport(*contractCoverage)
	}
	return aiRetrievalResult{
		Intent:              intent,
		Scope:               scope,
		Plan:                plan,
		Evidence:            curation.Evidence,
		ServiceCandidates:   candidates,
		TaskFrame:           &effectiveFrame,
		Contract:            &effectiveContract,
		EvidenceBundle:      &curation.Bundle,
		Coverage:            &curation.Coverage,
		ContractCoverage:    contractCoverage,
		Rounds:              rounds,
		Curation:            &curation,
		RetrievalRoundSteps: steps,
	}, nil
}

func aiRetrievalMissingContractKeys(contract *aiEvidenceContract, coverage *aiContractCoverageReport, round int) []string {
	if coverage != nil {
		keys := append([]string(nil), coverage.MissingRequired...)
		if round >= 3 {
			for _, item := range coverage.Items {
				if item.Requirement == aiEvidenceCheckerRequirementRequired && (item.Status == aiEvidenceCoverageConflict || item.Status == aiEvidenceCoveragePartial) {
					keys = append(keys, item.Key)
				}
			}
		}
		return uniqueStrings(keys)
	}
	if round <= 1 || contract == nil {
		return nil
	}
	return aiEvidenceRequirementKeys(contract.Required)
}

func aiRetrievalFrameTerms(frame *aiTaskFrame) []string {
	if frame == nil {
		return nil
	}
	terms := mergeTerms(frame.KnownTerms, frame.GeneratedTerms, frame.TargetArtifacts)
	if frame.FollowUp != nil {
		terms = mergeTerms(terms, aiQueryTerms(frame.FollowUp.PreviousTopicSummary), aiQueryTerms(strings.Join(frame.FollowUp.PreviousPaths, " ")))
	}
	return terms
}

func aiRetrievalTermsForContractKeys(keys []string) []string {
	terms := []string{}
	for _, key := range keys {
		terms = append(terms, key)
		switch key {
		case "entrypoint":
			terms = append(terms, "entrypoint", "handler", "route", "rpc", "service", "controller", "func", "method")
		case "call_chain", "implementation_file":
			terms = append(terms, "call", "chain", "handler", "service", "usecase", "method", "func")
		case "table_identity":
			terms = append(terms, "table", "tablename", "table_name", "schema")
		case "update_fields", "request_fields", "response_fields":
			terms = append(terms, "field", "fields", "column", "columns", "struct", "message", "dto")
		case "field_units":
			terms = append(terms, "unit", "units", "currency", "decimal", "cents", "ratio", "precision", "scale")
		case "where_conditions", "read_path", "verification_method":
			terms = append(terms, "where", "select", "query", "find", "repository", "dao")
		case "write_path":
			terms = append(terms, "update", "updates", "save", "create", "write", "mutation", "repository", "dao")
		case "persistence_target":
			terms = append(terms, "model", "table", "schema", "index", "cache", "repository", "dao", "gorm")
		case "route_or_rpc":
			terms = append(terms, "route", "router", "rpc", "service", "handler")
		case "error_codes":
			terms = append(terms, "error", "errors", "code", "validation")
		case "branch_status", "source_scope", "branch_candidates":
			terms = append(terms, "branch", "commit")
		case "side_effects", "risk_points", "compensation_path":
			terms = append(terms, "cache", "index", "event", "job", "retry", "compensation")
		}
	}
	return uniqueTerms(terms)
}

func aiRetrievalTermsFromBundle(bundle *aiEvidenceBundle) []string {
	if bundle == nil {
		return nil
	}
	terms := []string{}
	for _, group := range bundle.Groups {
		terms = append(terms, aiQueryTerms(group.GroupKey)...)
		terms = append(terms, aiQueryTerms(group.Summary)...)
		if group.EvidenceType != "" {
			terms = append(terms, group.EvidenceType)
		}
	}
	return uniqueTerms(terms)
}

func aiRetrievalFallbackTerms(intent string, missingKeys []string) []string {
	terms := []string{"code", "doc"}
	if aiIntentIsDatabaseDirectUpdate(intent) {
		terms = []string{"table", "tablename", "column", "where", "select", "update", "model", "migration", "db"}
	} else if aiIntentIsAPIIntegration(intent) {
		terms = []string{"api", "route", "router", "handler", "request", "response", "proto", "rpc"}
	} else if aiIntentIsCodePathExplanation(intent) {
		terms = []string{"handler", "service", "func", "method", "update", "write", "model", "db", "proto", "request"}
	}
	return mergeTerms(terms, aiRetrievalTermsForContractKeys(missingKeys))
}

func aiRetrievalPathHintsForRound(intent string, missingKeys []string, round int) []string {
	hints := []string{}
	if round == 1 {
		if aiIntentIsDatabaseDirectUpdate(intent) {
			return []string{"db"}
		}
		if aiIntentIsAPIIntegration(intent) {
			return []string{"router", "proto", "handler", "client", "docs"}
		}
		if aiIntentIsCodePathExplanation(intent) {
			return []string{"handler", "controller", "proto", "service", "db", "models"}
		}
		return nil
	}
	for _, key := range missingKeys {
		switch key {
		case "entrypoint", "call_chain", "implementation_file":
			hints = append(hints, "handler", "controller", "route", "router", "proto", "service")
		case "table_identity", "update_fields", "field_units":
			hints = append(hints, "models", "migration", "db")
		case "where_conditions", "read_path", "verification_method":
			hints = append(hints, "db", "models", "migration")
		case "write_path":
			hints = append(hints, "handler", "db", "dao", "repository", "service")
		case "persistence_target":
			hints = append(hints, "models", "migration", "db", "dao", "repository", "schema")
		case "route_or_rpc", "request_fields", "response_fields", "error_codes", "auth_policy":
			hints = append(hints, "router", "proto", "handler", "client", "docs")
		case "branch_status", "source_scope", "branch_candidates", "commit_evidence":
			hints = append(hints, "docs", "router", "proto", "db")
		case "side_effects", "risk_points", "compensation_path":
			hints = append(hints, "db", "handler", "docs")
		}
	}
	if len(hints) == 0 && aiIntentIsDatabaseDirectUpdate(intent) {
		hints = []string{"models", "migration", "db"}
	}
	if len(hints) == 0 && aiIntentIsCodePathExplanation(intent) {
		hints = []string{"handler", "service", "db", "models", "proto"}
	}
	return sanitizeAIRetrievalPathHints(hints)
}

func aiRetrievalFileTypesForRound(intent string, missingKeys []string, round int) []string {
	if aiIntentIsDatabaseDirectUpdate(intent) {
		return []string{"go", "sql", "md"}
	}
	if aiIntentIsAPIIntegration(intent) {
		return []string{"go", "ts", "js", "proto", "md", "vue"}
	}
	if aiIntentIsCodePathExplanation(intent) {
		return []string{"go", "proto", "md"}
	}
	return nil
}

func aiRetrievalRoundReason(coverage *aiContractCoverageReport, missingKeys []string, round int) string {
	switch round {
	case 1:
		return "initial_recall_from_task_frame"
	case 2:
		if len(missingKeys) > 0 {
			return "fill_required_contract_gaps: " + strings.Join(missingKeys, ",")
		}
		return "fill_required_contract_gaps"
	default:
		if coverage != nil && coverage.Status == aiEvidenceCoverageConflict {
			return "resolve_conflict_or_weak_evidence"
		}
		if len(missingKeys) > 0 {
			return "strengthen_weak_or_missing_evidence: " + strings.Join(missingKeys, ",")
		}
		return "resolve_conflict_or_weak_evidence"
	}
}

func aiRetrievalSearchTerms(search aiRetrievalRoundSearch) []string {
	terms := mergeTerms(search.Terms, aiQueryTerms(search.Query), search.PathHints)
	if len(terms) > 32 {
		terms = terms[:32]
	}
	return terms
}

func sanitizeAIRetrievalPathHints(values []string) []string {
	allowed := map[string]struct{}{
		"models": {}, "model": {}, "migration": {}, "migrations": {}, "db": {}, "database": {},
		"router": {}, "route": {}, "routes": {}, "proto": {}, "handler": {}, "controller": {},
		"client": {}, "docs": {}, "doc": {}, "schema": {}, "dao": {}, "repository": {}, "service": {},
	}
	out := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if _, ok := allowed[value]; !ok {
			continue
		}
		out = append(out, value)
	}
	return uniqueStrings(out)
}

func aiFilterEvidenceForSearch(items []aiEvidence, search aiRetrievalRoundSearch) []aiEvidence {
	out := make([]aiEvidence, 0, len(items))
	pathHints := sanitizeAIRetrievalPathHints(search.PathHints)
	for _, item := range items {
		if !aiEvidenceMatchesFileTypes(item, search.FileTypes) {
			continue
		}
		if len(pathHints) > 0 && !aiEvidenceMatchesPathHints(item, pathHints) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func aiEvidenceMatchesFileTypes(item aiEvidence, fileTypes []string) bool {
	if len(fileTypes) == 0 {
		return true
	}
	ext := strings.TrimPrefix(strings.ToLower(extension(item.Citation.FilePath)), ".")
	for _, fileType := range fileTypes {
		fileType = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileType)), ".")
		if fileType == "" || fileType == "all" {
			return true
		}
		if fileType == ext {
			return true
		}
	}
	return false
}

func aiEvidenceMatchesPathHints(item aiEvidence, hints []string) bool {
	path := strings.ToLower(normalizeRepoPath(item.Citation.FilePath))
	for _, hint := range hints {
		switch hint {
		case "models", "model", "schema":
			if strings.Contains(path, "model") || strings.Contains(path, "schema") || strings.Contains(path, "entity") {
				return true
			}
		case "migration", "migrations":
			if strings.Contains(path, "migration") || strings.HasSuffix(path, ".sql") {
				return true
			}
		case "db", "database":
			if strings.Contains(path, "/db/") || strings.HasPrefix(path, "db/") || strings.Contains(path, "database") || strings.Contains(path, "mysql") || strings.Contains(path, "dao") || strings.Contains(path, "repository") {
				return true
			}
		case "router", "route", "routes":
			if strings.Contains(path, "router") || strings.Contains(path, "route") {
				return true
			}
		case "proto":
			if strings.Contains(path, "proto") || strings.HasSuffix(path, ".proto") {
				return true
			}
		case "handler", "controller":
			if strings.Contains(path, "handler") || strings.Contains(path, "controller") {
				return true
			}
		case "service":
			if strings.Contains(path, "service") || strings.Contains(path, "usecase") || strings.Contains(path, "core/") {
				return true
			}
		case "client":
			if strings.Contains(path, "client") {
				return true
			}
		case "docs", "doc":
			if strings.Contains(path, "doc") || strings.HasSuffix(path, ".md") {
				return true
			}
		case "dao", "repository":
			if strings.Contains(path, "dao") || strings.Contains(path, "repository") {
				return true
			}
		}
	}
	return false
}

func aiEvidenceIdentitySet(items []aiEvidence) map[string]struct{} {
	seen := map[string]struct{}{}
	for _, item := range items {
		seen[aiEvidenceIdentityKey(item)] = struct{}{}
	}
	return seen
}

func aiCountNewEvidence(before map[string]struct{}, after []aiEvidence) int {
	count := 0
	for _, item := range after {
		if _, ok := before[aiEvidenceIdentityKey(item)]; ok {
			continue
		}
		count++
	}
	return count
}

func aiEvidenceIdentityKey(item aiEvidence) string {
	return strings.Join([]string{
		strconv.FormatInt(item.Citation.RepoID, 10),
		item.Citation.CommitSHA,
		item.Citation.FilePath,
		strconv.FormatInt(int64(item.Citation.LineStart), 10),
		strconv.FormatInt(int64(item.Citation.LineEnd), 10),
	}, ":")
}

func aiContractCoverageDelta(before, after *aiContractCoverageReport) map[string]string {
	delta := map[string]string{}
	if after == nil {
		return delta
	}
	keys := map[string]struct{}{}
	if before != nil {
		for key := range before.Coverage {
			keys[key] = struct{}{}
		}
	}
	for key := range after.Coverage {
		keys[key] = struct{}{}
	}
	for key := range keys {
		oldStatus := ""
		if before != nil {
			oldStatus = before.Coverage[key]
		}
		newStatus := after.Coverage[key]
		if oldStatus != newStatus {
			delta[key] = oldStatus + "->" + newStatus
		}
	}
	if before == nil || before.Status != after.Status {
		oldStatus := ""
		if before != nil {
			oldStatus = before.Status
		}
		delta["status"] = oldStatus + "->" + after.Status
	}
	if before == nil || before.NextAction != after.NextAction {
		oldAction := ""
		if before != nil {
			oldAction = before.NextAction
		}
		delta["next_action"] = oldAction + "->" + after.NextAction
	}
	return delta
}

func aiContractCoverageStillNeedsRetrieval(report *aiContractCoverageReport) bool {
	if report == nil {
		return true
	}
	return len(report.MissingRequired) > 0 || report.Status == aiEvidenceCoverageConflict
}

func aiRetrievalContractCoverage(retrieval aiRetrievalResult) aiContractCoverageReport {
	if retrieval.ContractCoverage != nil {
		return *retrieval.ContractCoverage
	}
	if retrieval.Contract != nil && retrieval.EvidenceBundle != nil {
		return checkAIEvidenceContract(*retrieval.Contract, *retrieval.EvidenceBundle)
	}
	return aiContractCoverageReport{}
}

func aiRetrievalHasCompletedGaps(report *aiContractCoverageReport) bool {
	if report == nil {
		return false
	}
	return report.Status == aiWorkflowStatusCompletedWithGaps || report.NextAction == aiWorkflowStatusCompletedWithGaps
}

func markAIContractCoverageCompletedWithGaps(report *aiContractCoverageReport) {
	if report == nil {
		return
	}
	if len(report.MissingRequired) == 0 && report.Status != aiEvidenceCoverageConflict {
		return
	}
	report.Status = aiWorkflowStatusCompletedWithGaps
	report.NextAction = aiWorkflowStatusCompletedWithGaps
	if report.Details == nil {
		report.Details = map[string]string{}
	}
	report.Details["workflow_status"] = aiWorkflowStatusCompletedWithGaps
	report.Details["next_action"] = "answer must list confirmed evidence and unresolved required gaps"
}

func buildAIRetrievalRoundStep(frame *aiTaskFrame, contract *aiEvidenceContract, coverage *aiContractCoverageReport, bundle *aiEvidenceBundle, plan aiRetrievalRoundPlan, after aiContractCoverageReport) AIAgentStep {
	now := nowString()
	input := map[string]any{
		"round":                     plan.Round,
		"task_frame":                frame,
		"contract_missing_keys":     plan.MissingContractKeys,
		"existing_evidence_summary": summarizeAIRetrievalBundle(bundle),
	}
	if frame != nil && frame.FollowUp != nil {
		input["follow_up_context"] = frame.FollowUp
	}
	if coverage != nil {
		input["coverage_before"] = summarizeAIContractCoverageReport(*coverage)
	}
	if contract != nil {
		input["contract"] = summarizeAIEvidenceContract(*contract)
	}
	output := map[string]any{
		"round":              plan.Round,
		"reason":             plan.Reason,
		"searches":           plan.Searches,
		"query_source":       plan.QuerySource,
		"planner_status":     plan.PlannerStatus,
		"new_evidence_count": plan.NewEvidenceCount,
		"coverage_delta":     plan.CoverageDelta,
		"coverage_after":     summarizeAIContractCoverageReport(after),
		"next_action":        after.NextAction,
	}
	return AIAgentStep{
		AgentName:  "retrieval_orchestrator",
		StepType:   "retrieval_round",
		Status:     "success",
		ToolName:   "search_code_evidence",
		InputJSON:  encodeJSON(map[string]any{"input_summary": input}),
		OutputJSON: encodeJSON(map[string]any{"summary": output}),
		CreatedAt:  now,
		FinishedAt: now,
	}
}

func summarizeAIRetrievalBundle(bundle *aiEvidenceBundle) map[string]any {
	if bundle == nil {
		return map[string]any{"group_count": 0, "coverage": map[string]string{}}
	}
	return map[string]any{
		"bundle_id":      bundle.BundleID,
		"coverage":       bundle.Coverage,
		"group_count":    len(bundle.Groups),
		"excluded_count": len(bundle.Excluded),
	}
}

func aiLegacyIntentForTaskFrame(frame aiTaskFrame) string {
	switch normalizeAITaskIntent(frame.Intent) {
	case aiTaskIntentDatabaseDirectUpdateForTest:
		return "database_change"
	case aiTaskIntentBusinessValueChange:
		return "code_path"
	case aiTaskIntentCrossServiceImpact:
		return "cross_service"
	default:
		return normalizeAITaskIntent(frame.Intent)
	}
}
