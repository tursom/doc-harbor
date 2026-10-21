package app

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// piCitation 是最终回答中单个引用的核实结果。核实只表示与实际展示行重叠，
// 不表示后端替模型判断了结论是否正确。Repo/Ref/Path 必须全部精确相同。
type piCitation struct {
	Key       string `json:"key"`
	Repo      string `json:"repo"`
	Ref       string `json:"ref"`
	Path      string `json:"path"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Status    string `json:"status"`
	URL       string `json:"url"`
	RepoID    int64  `json:"repo_id,omitempty"`
	VersionID int64  `json:"version_id,omitempty"`
	CommitSHA string `json:"commit_sha,omitempty"`
}

type piCitationFile struct {
	Repo string
	Ref  string
	Path string
}

type piLineRange struct {
	Start int
	End   int
}

// piSeenRanges 的空切片也有意义：文件出现过，但没有实际输出任何行。
type piSeenRanges map[piCitationFile][]piLineRange

var (
	// 与实施文档 §8 一致，路径允许中文和空格，不能用 Fields 拆引用键。
	piAnswerCitationPattern = regexp.MustCompile(`\[([^\[\]@]+)@([^:\]]+):([^\]#]+)#L(\d+)(?:-(\d+))?\]`)
	piHeaderRangePattern    = regexp.MustCompile(`#L\d+(?:-\d+)?(?:\s.*)?$`)
	piReadLinePattern       = regexp.MustCompile(`^[ \t]*([1-9][0-9]*)\t`)
	piSearchLinePattern     = regexp.MustCompile(`^L([1-9][0-9]*)(?::|-)`)
)

// piParseCitationFile 解析纯文件键；范围和标题由工具组头解析器提前移除。
func piParseCitationFile(key string) (piCitationFile, bool) {
	repo, rest, ok := strings.Cut(key, "@")
	if !ok {
		return piCitationFile{}, false
	}
	ref, filePath, ok := strings.Cut(rest, ":")
	if !ok || repo == "" || ref == "" || filePath == "" || strings.ContainsAny(key, "\r\n") {
		return piCitationFile{}, false
	}
	return piCitationFile{Repo: repo, Ref: ref, Path: filePath}, true
}

// AddToolResult 仅记录工具实际输出的编号行，绝不把组头 #L1-400 当成已经
// 读过整个区间。搜索可能只输出 L1 与 L400，中间 398 行不能被标成 verified。
// 组头即使没有编号行也登记文件，从而区分 path_only 和 unverified。
func (seen piSeenRanges) AddToolResult(text string) {
	var current piCitationFile
	hasFile := false
	touched := make(map[piCitationFile]bool)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "== ") {
			key := strings.TrimPrefix(line, "== ")
			if position := piHeaderRangePattern.FindStringIndex(key); position != nil {
				key = key[:position[0]]
			} else if position := strings.Index(key, " （"); position >= 0 {
				key = key[:position]
			}
			current, hasFile = piParseCitationFile(key)
			if hasFile {
				if _, exists := seen[current]; !exists {
					seen[current] = []piLineRange{}
				}
				touched[current] = true
			}
			continue
		}
		if !hasFile {
			continue
		}
		match := piReadLinePattern.FindStringSubmatch(line)
		if match == nil {
			match = piSearchLinePattern.FindStringSubmatch(line)
		}
		if match == nil {
			continue
		}
		number, err := strconv.Atoi(match[1])
		if err != nil || number <= 0 {
			continue
		}
		seen[current] = append(seen[current], piLineRange{Start: number, End: number})
	}
	for key := range touched {
		seen[key] = piMergeLineRanges(seen[key])
	}
}

func piMergeLineRanges(ranges []piLineRange) []piLineRange {
	if len(ranges) == 0 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	merged := ranges[:0]
	for _, span := range ranges {
		last := len(merged) - 1
		// 不做 End+1，避免极端行号整数溢出；相邻行也合并为连续区间。
		if last >= 0 && (span.Start <= merged[last].End || span.Start-1 == merged[last].End) {
			if span.End > merged[last].End {
				merged[last].End = span.End
			}
		} else {
			merged = append(merged, span)
		}
	}
	return merged
}

// verifyPiCitations 是不依赖数据库的纯核实步骤。重复出现的同一键只返回一次，
// 保留首次出现的顺序；只要和已读范围重叠一行就满足文档规定的 verified。
func verifyPiCitations(answer string, seen piSeenRanges) []piCitation {
	result := make([]piCitation, 0)
	keys := make(map[string]bool)
	for _, match := range piAnswerCitationPattern.FindAllStringSubmatch(answer, -1) {
		key := match[0][1 : len(match[0])-1]
		if keys[key] {
			continue
		}
		keys[key] = true
		start, startErr := strconv.Atoi(match[4])
		end, endErr := start, error(nil)
		if match[5] != "" {
			end, endErr = strconv.Atoi(match[5])
		}
		citation := piCitation{Key: key, Repo: match[1], Ref: match[2], Path: match[3], LineStart: start, LineEnd: end, Status: "unverified"}
		ranges, fileSeen := seen[piCitationFile{Repo: citation.Repo, Ref: citation.Ref, Path: citation.Path}]
		if fileSeen {
			citation.Status = "path_only"
		}
		// 行号为零、倒序或溢出的引用不能变成已核实。
		if startErr == nil && endErr == nil && start > 0 && end >= start {
			for _, span := range ranges {
				if start <= span.End && end >= span.Start {
					citation.Status = "verified"
					break
				}
			}
		}
		result = append(result, citation)
	}
	return result
}

// resolvePiCitationURLs 复用 MCP 刚解析到的仓库/ref 规则，避免另写智能最新逻辑。
// 找不到仓库或 ref 时保留核实状态但不捏造 URL；核实与链接解析相互独立。
// 单个键只解析一次，重复行引用共享同一份不可变 commit。
func (s *Server) resolvePiCitationURLs(ctx context.Context, citations []piCitation) ([]piCitation, error) {
	result := append([]piCitation{}, citations...)
	repos := make(map[string]Repository)
	resolved := make(map[piCitationFile]mcpResolvedRef)
	attempted := make(map[piCitationFile]bool)
	for i := range result {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		citation := &result[i]
		repo, exists := repos[citation.Repo]
		if !exists {
			var err error
			repo, err = s.resolveMCPRepository(ctx, citation.Repo)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				continue
			}
			repos[citation.Repo] = repo
		}
		citation.RepoID = repo.ID
		key := piCitationFile{Repo: citation.Repo, Ref: citation.Ref, Path: citation.Path}
		if !attempted[key] {
			attempted[key] = true
			target, err := s.resolveMCPRef(ctx, repo, citation.Ref, citation.Path)
			if err == nil {
				resolved[key] = target
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		target, ok := resolved[key]
		if !ok {
			continue
		}
		citation.CommitSHA, citation.VersionID = target.Commit, target.VersionID
		if target.VersionID != 0 {
			citation.URL = fmt.Sprintf("/?repo=%d&version=%d", repo.ID, target.VersionID)
		} else {
			query := url.Values{"commit_sha": {target.Commit}, "path": {citation.Path}, "inline": {"1"}}
			citation.URL = fmt.Sprintf("/api/repos/%d/blob/download?%s", repo.ID, query.Encode())
		}
	}
	return result, nil
}
