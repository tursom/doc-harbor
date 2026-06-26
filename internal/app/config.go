package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DataDir               string
	HTTPAddr              string
	DBDSN                 string
	GitBin                string
	WebDir                string
	DefaultScanInterval   time.Duration
	MaxPreviewFileSize    int64
	AllowedGitHosts       []string
	AllowLocalGit         bool
	GitCommandTimeout     time.Duration
	SchedulerPollInterval time.Duration
}

func LoadConfig() Config {
	dataDir := env("DATA_DIR", "./data")
	dbDSN := env("DB_DSN", filepath.Join(dataDir, "doc-harbor.db"))
	if strings.HasPrefix(dbDSN, "file:") {
		dbDSN = dbDSN + "?_busy_timeout=5000&_journal_mode=WAL"
	} else {
		dbDSN = "file:" + dbDSN + "?_busy_timeout=5000&_journal_mode=WAL"
	}

	return Config{
		DataDir:               dataDir,
		HTTPAddr:              env("HTTP_ADDR", ":8080"),
		DBDSN:                 dbDSN,
		GitBin:                env("GIT_BIN", "git"),
		WebDir:                env("WEB_DIR", "./web/dist"),
		DefaultScanInterval:   secondsEnv("DEFAULT_SCAN_INTERVAL", 3600),
		MaxPreviewFileSize:    int64Env("MAX_PREVIEW_FILE_SIZE", 2*1024*1024),
		AllowedGitHosts:       csvEnv("ALLOWED_GIT_HOSTS"),
		AllowLocalGit:         boolEnv("ALLOW_LOCAL_GIT", false),
		GitCommandTimeout:     secondsEnv("GIT_COMMAND_TIMEOUT", 120),
		SchedulerPollInterval: secondsEnv("SCHEDULER_POLL_INTERVAL", 60),
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func csvEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func secondsEnv(key string, fallback int) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return time.Duration(fallback) * time.Second
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return time.Duration(fallback) * time.Second
	}
	return time.Duration(n) * time.Second
}

func int64Env(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func boolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}
