package app

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"mime"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var commitRe = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeRepoPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "/")
	if value == "" || value == "." {
		return "."
	}
	clean := path.Clean(value)
	if clean == "." {
		return "."
	}
	clean = strings.TrimPrefix(clean, "/")
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\x00") {
		return ""
	}
	return clean
}

func validRepoPath(value string) bool {
	return normalizeRepoPath(value) != ""
}

func dirName(filePath string) string {
	filePath = normalizeRepoPath(filePath)
	if filePath == "." || filePath == "" {
		return "."
	}
	dir := path.Dir(filePath)
	if dir == "." {
		return "."
	}
	return dir
}

func baseName(filePath string) string {
	filePath = strings.TrimSuffix(normalizeRepoPath(filePath), "/")
	if filePath == "." || filePath == "" {
		return ""
	}
	return path.Base(filePath)
}

func extension(filePath string) string {
	return strings.ToLower(filepath.Ext(filePath))
}

func mimeType(filePath string) string {
	if typ := mime.TypeByExtension(extension(filePath)); typ != "" {
		return typ
	}
	switch extension(filePath) {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".yml", ".yaml":
		return "text/yaml; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func isPreviewable(filePath string) bool {
	switch extension(filePath) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func isMarkdown(filePath string) bool {
	switch extension(filePath) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, errBadRequest("invalid id")
	}
	return id, nil
}

func cleanLimit(value string, fallback, max int) int {
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	if n > max {
		return max
	}
	return n
}

func sanitizeFileName(name string) string {
	name = baseName(name)
	name = strings.ReplaceAll(name, "\"", "")
	if name == "" {
		return "download"
	}
	return name
}

func stableHash(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}

func gitSafeCommit(value string) bool {
	return commitRe.MatchString(value)
}

func matchAny(patterns []string, value string) bool {
	value = normalizeRepoPath(value)
	for _, pattern := range patterns {
		if matchGlob(strings.TrimSpace(pattern), value) {
			return true
		}
	}
	return false
}

func matchGlob(pattern, value string) bool {
	if pattern == "" {
		return false
	}
	pattern = strings.ReplaceAll(pattern, "\\", "/")
	value = strings.ReplaceAll(value, "\\", "/")
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return value == prefix || strings.HasPrefix(value, prefix+"/")
	}
	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		return value == suffix || strings.HasSuffix(value, "/"+suffix)
	}
	if ok, _ := path.Match(pattern, value); ok {
		return true
	}
	if ok, _ := path.Match(pattern, path.Base(value)); ok {
		return true
	}
	return false
}

func branchRank(priority []string, branch string) int {
	for i, pattern := range priority {
		if matchGlob(pattern, branch) {
			return i
		}
	}
	return len(priority) + 100
}

func shortSHA(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}
