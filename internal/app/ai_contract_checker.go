package app

import (
	"sort"
	"strings"
)

const (
	aiEvidenceCheckerRequirementRequired    = "required"
	aiEvidenceCheckerRequirementRecommended = "recommended"
	aiEvidenceCheckerNextActionLegacyAnswer = "legacy_answer"
	aiEvidenceCheckerNextActionRetrieve     = "retrieve_missing_contract_keys"
	aiEvidenceCheckerNextActionResolve      = "resolve_contract_conflict"
	aiEvidenceCheckerNextActionRemove       = "remove_forbidden_evidence"
	aiEvidenceCheckerNextActionReview       = "review_recommended_contract_gaps"
)

func checkAIEvidenceContract(contract aiEvidenceContract, bundle aiEvidenceBundle) aiContractCoverageReport {
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
	groupsByKey := map[string][]aiEvidenceGroup{}
	for _, group := range bundle.Groups {
		if strings.TrimSpace(group.Key) == "" {
			continue
		}
		groupsByKey[group.Key] = append(groupsByKey[group.Key], group)
	}
	for _, requirement := range contract.Required {
		item := checkAIEvidenceRequirement(contract, requirement, aiEvidenceCheckerRequirementRequired, bundle, groupsByKey[requirement.Key])
		appendAIContractCoverageItem(&report, item)
	}
	for _, requirement := range contract.Recommended {
		item := checkAIEvidenceRequirement(contract, requirement, aiEvidenceCheckerRequirementRecommended, bundle, groupsByKey[requirement.Key])
		appendAIContractCoverageItem(&report, item)
	}
	report.ForbiddenMatched = uniqueStrings(report.ForbiddenMatched)
	sort.Strings(report.Covered)
	sort.Strings(report.Partial)
	sort.Strings(report.MissingRequired)
	sort.Strings(report.MissingRecommended)
	sort.Strings(report.ForbiddenMatched)
	finalizeAIContractCoverageReport(&report)
	return report
}

func checkAIEvidenceRequirement(contract aiEvidenceContract, requirement aiEvidenceRequirement, requirementType string, bundle aiEvidenceBundle, groups []aiEvidenceGroup) aiContractCoverageItem {
	item := aiContractCoverageItem{
		Key:         requirement.Key,
		Requirement: requirementType,
		EvidenceIDs: []int64{},
	}
	statusHint := normalizeAIContractCoverageStatus(bundle.Coverage[requirement.Key])
	for _, group := range groups {
		item.EvidenceIDs = append(item.EvidenceIDs, group.EvidenceIDs...)
	}
	item.EvidenceIDs = uniqueInt64s(item.EvidenceIDs)
	switch {
	case statusHint == aiEvidenceCoverageForbidden || aiEvidenceGroupsMatchForbidden(contract.Forbidden, groups):
		item.Status = aiEvidenceCoverageForbidden
		item.Confidence = 0
		item.Reason = "forbidden evidence matched this contract key"
		item.MissingDetail = "remove forbidden evidence before using this key"
	case statusHint == aiEvidenceCoverageConflict || aiEvidenceGroupsLookConflicting(groups):
		item.Status = aiEvidenceCoverageConflict
		item.Confidence = 0.2
		item.Reason = "conflicting evidence is attached to this contract key"
		item.MissingDetail = "resolve the conflicting evidence before answering"
	case len(item.EvidenceIDs) == 0:
		item.Status = aiEvidenceCoverageMissing
		item.Confidence = 0
		item.Reason = "missing accepted evidence"
		item.MissingDetail = aiEvidenceRequirementMissingDetail(requirement)
	case statusHint == aiEvidenceCoverageCovered || aiEvidenceRequirementHasCoveringGroup(requirement.Key, groups):
		item.Status = aiEvidenceCoverageCovered
		item.Confidence = aiEvidenceRequirementConfidence(groups, 0.95)
		item.Reason = "accepted evidence covers this contract key"
		item.MissingDetail = ""
	default:
		item.Status = aiEvidenceCoveragePartial
		item.Confidence = aiEvidenceRequirementConfidence(groups, 0.55)
		item.Reason = "only partial or lower-confidence evidence is available"
		item.MissingDetail = aiEvidenceRequirementMissingDetail(requirement)
	}
	if item.Reason == "" {
		item.Reason = "contract key evaluated"
	}
	return item
}

func appendAIContractCoverageItem(report *aiContractCoverageReport, item aiContractCoverageItem) {
	if item.Key == "" {
		return
	}
	report.Items = append(report.Items, item)
	report.Coverage[item.Key] = item.Status
	switch item.Status {
	case aiEvidenceCoverageCovered:
		report.Covered = append(report.Covered, item.Key)
	case aiEvidenceCoveragePartial:
		report.Partial = append(report.Partial, item.Key)
		if item.Requirement == aiEvidenceCheckerRequirementRequired {
			report.MissingRequired = append(report.MissingRequired, item.Key)
		} else {
			report.MissingRecommended = append(report.MissingRecommended, item.Key)
		}
		report.Details[item.Key] = item.MissingDetail
	case aiEvidenceCoverageMissing:
		if item.Requirement == aiEvidenceCheckerRequirementRequired {
			report.MissingRequired = append(report.MissingRequired, item.Key)
		} else {
			report.MissingRecommended = append(report.MissingRecommended, item.Key)
		}
		report.Details[item.Key] = item.MissingDetail
	case aiEvidenceCoverageConflict:
		report.Details[item.Key] = item.MissingDetail
	case aiEvidenceCoverageForbidden:
		report.ForbiddenMatched = append(report.ForbiddenMatched, item.Key+":forbidden")
		report.Details[item.Key] = item.MissingDetail
	}
}

func finalizeAIContractCoverageReport(report *aiContractCoverageReport) {
	hasForbidden := false
	hasConflict := false
	for _, item := range report.Items {
		switch item.Status {
		case aiEvidenceCoverageForbidden:
			hasForbidden = true
		case aiEvidenceCoverageConflict:
			hasConflict = true
		}
	}
	switch {
	case hasForbidden:
		report.Status = aiEvidenceCoverageForbidden
		report.NextAction = aiEvidenceCheckerNextActionRemove
	case hasConflict:
		report.Status = aiEvidenceCoverageConflict
		report.NextAction = aiEvidenceCheckerNextActionResolve
	case len(report.MissingRequired) > 0:
		report.Status = "missing_required"
		report.NextAction = aiEvidenceCheckerNextActionRetrieve
	case len(report.MissingRecommended) > 0:
		report.Status = aiEvidenceCoveragePartial
		report.NextAction = aiEvidenceCheckerNextActionReview
	default:
		report.Status = "pass"
		report.NextAction = aiEvidenceCheckerNextActionLegacyAnswer
	}
	if len(report.Details) == 0 {
		report.Details = nil
	}
}

func normalizeAIContractCoverageStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case aiEvidenceCoverageCovered:
		return aiEvidenceCoverageCovered
	case aiEvidenceCoveragePartial:
		return aiEvidenceCoveragePartial
	case aiEvidenceCoverageMissing:
		return aiEvidenceCoverageMissing
	case aiEvidenceCoverageConflict:
		return aiEvidenceCoverageConflict
	case aiEvidenceCoverageForbidden:
		return aiEvidenceCoverageForbidden
	default:
		return ""
	}
}

func aiEvidenceRequirementHasCoveringGroup(contractKey string, groups []aiEvidenceGroup) bool {
	for _, group := range groups {
		if aiEvidenceGroupCoversRequirement(contractKey, group.SourceReliability) {
			return true
		}
	}
	return false
}

func aiEvidenceRequirementConfidence(groups []aiEvidenceGroup, fallback float64) float64 {
	bestRank := 0
	for _, group := range groups {
		if rank := aiEvidenceReliabilityRank(group.SourceReliability); rank > bestRank {
			bestRank = rank
		}
	}
	switch bestRank {
	case 4:
		return 0.95
	case 3:
		return 0.9
	case 2:
		return 0.7
	case 1:
		if fallback > 0.55 {
			return 0.75
		}
		return 0.55
	default:
		return fallback
	}
}

func aiEvidenceRequirementMissingDetail(requirement aiEvidenceRequirement) string {
	if requirement.Description != "" {
		return "need accepted evidence for " + requirement.Key + ": " + requirement.Description
	}
	return "need accepted evidence for " + requirement.Key
}

func aiEvidenceGroupsMatchForbidden(forbidden []string, groups []aiEvidenceGroup) bool {
	if len(forbidden) == 0 || len(groups) == 0 {
		return false
	}
	needles := make([]string, 0, len(forbidden))
	for _, value := range forbidden {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			needles = append(needles, value)
		}
	}
	for _, group := range groups {
		haystack := strings.ToLower(strings.Join([]string{group.Summary, group.EvidenceType, group.SourceReliability, group.GroupKey}, " "))
		for _, needle := range needles {
			if strings.Contains(haystack, needle) {
				return true
			}
		}
	}
	return false
}

func aiEvidenceGroupsLookConflicting(groups []aiEvidenceGroup) bool {
	for _, group := range groups {
		text := strings.ToLower(group.Summary + " " + group.GroupKey + " " + group.SourceReliability)
		if strings.Contains(text, "conflict") || strings.Contains(text, "contradict") || strings.Contains(text, "冲突") {
			return true
		}
	}
	return false
}

func aiContractCoverageUnconfirmedRequiredCount(report *aiContractCoverageReport) int {
	if report == nil {
		return 0
	}
	count := 0
	for _, item := range report.Items {
		if item.Requirement != aiEvidenceCheckerRequirementRequired {
			continue
		}
		if item.Status == aiEvidenceCoverageMissing || item.Status == aiEvidenceCoveragePartial {
			count++
		}
	}
	return count
}

func summarizeAIContractCoverageReport(report aiContractCoverageReport) map[string]any {
	return map[string]any{
		"contract_id":         report.ContractID,
		"status":              report.Status,
		"coverage":            report.Coverage,
		"missing_required":    report.MissingRequired,
		"missing_recommended": report.MissingRecommended,
		"partial":             report.Partial,
		"forbidden_matched":   report.ForbiddenMatched,
		"unconfirmed_count":   aiContractCoverageUnconfirmedRequiredCount(&report),
		"next_action":         report.NextAction,
	}
}

func buildAIContractCheckerStep(contract *aiEvidenceContract, bundle *aiEvidenceBundle, coverage aiContractCoverageReport) AIAgentStep {
	now := nowString()
	inputSummary := map[string]any{}
	if contract != nil {
		inputSummary["contract"] = summarizeAIEvidenceContract(*contract)
	}
	if bundle != nil {
		inputSummary["bundle_id"] = bundle.BundleID
		inputSummary["coverage"] = bundle.Coverage
		inputSummary["group_count"] = len(bundle.Groups)
		inputSummary["excluded_count"] = len(bundle.Excluded)
	}
	return AIAgentStep{
		AgentName:  "contract_checker",
		StepType:   "deterministic",
		Status:     "success",
		InputJSON:  encodeJSON(map[string]any{"input_summary": inputSummary}),
		OutputJSON: encodeJSON(map[string]any{"contract_coverage": coverage, "summary": summarizeAIContractCoverageReport(coverage)}),
		CreatedAt:  now,
		FinishedAt: now,
	}
}

func buildAIShadowVerificationReport(verificationStatus string, citationCount int, coverage *aiContractCoverageReport) string {
	report := map[string]any{
		"ok":                     verificationStatus == "pass",
		"citation_count":         citationCount,
		"agent_workflow_version": aiAgentWorkflowVersionV2Shadow,
		"answer_mode":            "legacy",
		"local_guardrails":       []string{"read_only_tools", "citations_required", "branch_scope_labeled"},
	}
	if coverage != nil {
		report["contract_coverage"] = summarizeAIContractCoverageReport(*coverage)
		report["contract_coverage_ok"] = coverage.Status == "pass"
		report["unconfirmed_count"] = aiContractCoverageUnconfirmedRequiredCount(coverage)
	}
	return encodeJSON(report)
}

func uniqueInt64s(values []int64) []int64 {
	if len(values) == 0 {
		return []int64{}
	}
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
