package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// AIProvider identifies which AI backend to use.
type AIProvider string

const (
	ProviderLocal     AIProvider = "local"     // llama.cpp server (default, no key needed)
	ProviderOpenAI    AIProvider = "openai"
	ProviderClaude    AIProvider = "claude"
	ProviderGemini    AIProvider = "gemini"
	ProviderGrok      AIProvider = "grok"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	// Server
	Port string
	Env  string

	// GitHub App
	AppID          int64
	PrivateKeyPath string // file path OR raw PEM content
	WebhookSecret  string

	// Worker pool
	WorkerCount    int
	MaxFilesPerPR  int
	MaxPatchChars  int

	// AI provider selection
	AIProvider AIProvider

	// Local LLM (llama.cpp server)
	LocalLLMURL   string
	LocalLLMModel string

	// External provider API keys (all optional)
	OpenAIAPIKey    string
	OpenAIModel     string
	AnthropicAPIKey string
	AnthropicModel  string
	GeminiAPIKey    string
	GeminiModel     string
	GrokAPIKey      string
	GrokModel       string

	// Shared AI settings
	AIMaxTokens int
	AITimeout   int // seconds
}

// Load reads configuration from environment variables.
// It fails fast if required variables are missing.
func Load() *Config {
	cfg := &Config{
		Port:          getEnv("PORT", "3000"),
		Env:           getEnv("ENV", "production"),
		WebhookSecret: requireEnv("GITHUB_WEBHOOK_SECRET"),

		WorkerCount:   getEnvInt("WORKER_COUNT", 5),
		MaxFilesPerPR: getEnvInt("MAX_FILES_PER_PR", 50),
		MaxPatchChars: getEnvInt("MAX_PATCH_CHARS", 10000),

		AIProvider: AIProvider(strings.ToLower(getEnv("AI_PROVIDER", string(ProviderLocal)))),

		// Local LLM defaults
		LocalLLMURL:   getEnv("LOCAL_LLM_URL", "http://localhost:8080"),
		LocalLLMModel: getEnv("LOCAL_LLM_MODEL", "qwen2.5-coder"),

		// External API keys (all optional)
		OpenAIAPIKey:    getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:     getEnv("OPENAI_MODEL", "gpt-4o"),
		AnthropicAPIKey: getEnv("ANTHROPIC_API_KEY", ""),
		AnthropicModel:  getEnv("ANTHROPIC_MODEL", "claude-sonnet-4-5"),
		GeminiAPIKey:    getEnv("GEMINI_API_KEY", ""),
		GeminiModel:     getEnv("GEMINI_MODEL", "gemini-2.0-flash"),
		GrokAPIKey:      getEnv("GROK_API_KEY", ""),
		GrokModel:       getEnv("GROK_MODEL", "grok-3-mini"),

		AIMaxTokens: getEnvInt("AI_MAX_TOKENS", 4096),
		AITimeout:   getEnvInt("AI_TIMEOUT_SECONDS", 120),
	}

	// GitHub App ID
	appIDStr := requireEnv("GITHUB_APP_ID")
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		fatalf("GITHUB_APP_ID must be a numeric value, got: %s", appIDStr)
	}
	cfg.AppID = appID

	// Private key — accept either file path or raw PEM content
	keyPath := getEnv("GITHUB_APP_PRIVATE_KEY_PATH", "")
	keyContents := getEnv("GITHUB_APP_PRIVATE_KEY_CONTENTS", "")
	switch {
	case keyContents != "":
		// Raw PEM content from env (Railway/Render style)
		// Replace literal \n with actual newlines
		cfg.PrivateKeyPath = strings.ReplaceAll(keyContents, `\n`, "\n")
	case keyPath != "":
		if _, err := os.Stat(keyPath); err != nil {
			fatalf("private key file not found at %s: %v", keyPath, err)
		}
		cfg.PrivateKeyPath = keyPath
	default:
		fatalf("either GITHUB_APP_PRIVATE_KEY_PATH or GITHUB_APP_PRIVATE_KEY_CONTENTS must be set")
	}

	// Validate AI provider
	if err := validateProvider(cfg); err != nil {
		fatalf("AI provider config error: %v", err)
	}

	slog.Info("config loaded",
		"port", cfg.Port,
		"env", cfg.Env,
		"ai_provider", cfg.AIProvider,
		"worker_count", cfg.WorkerCount,
		"max_files_per_pr", cfg.MaxFilesPerPR,
	)

	return cfg
}

func validateProvider(cfg *Config) error {
	switch cfg.AIProvider {
	case ProviderLocal:
		if cfg.LocalLLMURL == "" {
			return fmt.Errorf("LOCAL_LLM_URL must be set when AI_PROVIDER=local")
		}
	case ProviderOpenAI:
		if cfg.OpenAIAPIKey == "" {
			return fmt.Errorf("OPENAI_API_KEY must be set when AI_PROVIDER=openai")
		}
	case ProviderClaude:
		if cfg.AnthropicAPIKey == "" {
			return fmt.Errorf("ANTHROPIC_API_KEY must be set when AI_PROVIDER=claude")
		}
	case ProviderGemini:
		if cfg.GeminiAPIKey == "" {
			return fmt.Errorf("GEMINI_API_KEY must be set when AI_PROVIDER=gemini")
		}
	case ProviderGrok:
		if cfg.GrokAPIKey == "" {
			return fmt.Errorf("GROK_API_KEY must be set when AI_PROVIDER=grok")
		}
	default:
		return fmt.Errorf("unknown AI_PROVIDER %q — must be one of: local, openai, claude, gemini, grok", cfg.AIProvider)
	}
	return nil
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fatalf("required environment variable %s is not set", key)
	}
	return v
}

func getEnvInt(key string, defaultVal int) int {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		slog.Warn("invalid integer env var, using default", "key", key, "value", s, "default", defaultVal)
		return defaultVal
	}
	return v
}

func fatalf(format string, args ...any) {
	slog.Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}
