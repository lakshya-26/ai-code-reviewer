package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ai-code-reviewer/ai-code-reviewer/internal/config"
)

// ─── Data types ───────────────────────────────────────────────────────────────

// ReviewComment is one issue found by the AI on a specific line of a file.
// This is the canonical type used throughout the system — all providers parse
// their responses into this shape.
type ReviewComment struct {
	Line     int    `json:"line"`
	Severity string `json:"severity"` // error | warning | suggestion
	Category string `json:"category"` // bug | security | performance | code-smell | best-practice
	Comment  string `json:"comment"`
	Fix      string `json:"fix,omitempty"` // optional corrected snippet for copy-paste
}

// FileReview groups all comments for one file.
type FileReview struct {
	Filename string
	Comments []ReviewComment
}

// FileAnalysisInput is everything the model needs to review one file.
type FileAnalysisInput struct {
	Filename      string
	Patch         string
	FileBody      string // full file or enclosing-function windows
	Guidelines    string // CONTRIBUTING / AGENTS.md
	PathPrompt    string // matching .ai-reviewer.yml path_prompts entry
	RepoMap       string // compact source tree, first review only
	PRContext     PRContext
	MaxPatchChars int
}

// ─── PR context ───────────────────────────────────────────────────────────────

// PRContext carries pull request metadata that gives the AI reviewer
// context about the intent of the change — letting it focus on what matters.
type PRContext struct {
	Title       string // PR title, e.g. "Fix auth token expiry"
	Description string // PR body / description
	Number      int
	RepoName    string // e.g. "acme/backend"
}

// ─── Provider interface ───────────────────────────────────────────────────────

// Provider is the single interface all AI backends implement.
// The orchestrator (reviewer.go) only talks to this interface.
type Provider interface {
	// AnalyzeFile sends the diff for one file to the AI and returns comments.
	// prCtx provides PR-level context so the model can focus its review.
	AnalyzeFile(ctx context.Context, in FileAnalysisInput) ([]ReviewComment, error)

	// Name returns a human-readable label used in logging.
	Name() string
}

// ─── Factory ──────────────────────────────────────────────────────────────────

// NewProvider constructs the appropriate Provider based on cfg.AIProvider.
// Returns an error if the required credentials are missing or the provider
// name is unknown.
func NewProvider(cfg *config.Config) (Provider, error) {
	timeout := time.Duration(cfg.AITimeout) * time.Second

	switch cfg.AIProvider {
	case config.ProviderLocal:
		return newOpenAICompatProvider(openAICompatConfig{
			name:      "local-llm (llama.cpp)",
			baseURL:   strings.TrimRight(cfg.LocalLLMURL, "/") + "/v1/chat/completions",
			apiKey:    "local", // llama.cpp server accepts any non-empty key
			model:     cfg.LocalLLMModel,
			maxTokens: cfg.AIMaxTokens,
			timeout:   timeout,
		}), nil

	case config.ProviderGroq:
		return newOpenAICompatProvider(openAICompatConfig{
			name:      "groq",
			baseURL:   "https://api.groq.com/openai/v1/chat/completions",
			apiKey:    cfg.GroqAPIKey,
			model:     cfg.GroqModel,
			maxTokens: cfg.AIMaxTokens,
			timeout:   timeout,
		}), nil

	case config.ProviderOpenAI:
		return newOpenAICompatProvider(openAICompatConfig{
			name:      "openai",
			baseURL:   "https://api.openai.com/v1/chat/completions",
			apiKey:    cfg.OpenAIAPIKey,
			model:     cfg.OpenAIModel,
			maxTokens: cfg.AIMaxTokens,
			timeout:   timeout,
		}), nil

	case config.ProviderGrok:
		return newOpenAICompatProvider(openAICompatConfig{
			name:      "grok (xAI)",
			baseURL:   "https://api.x.ai/v1/chat/completions",
			apiKey:    cfg.GrokAPIKey,
			model:     cfg.GrokModel,
			maxTokens: cfg.AIMaxTokens,
			timeout:   timeout,
		}), nil

	case config.ProviderClaude:
		return newClaudeProvider(cfg.AnthropicAPIKey, cfg.AnthropicModel, cfg.AIMaxTokens, timeout), nil

	case config.ProviderGemini:
		return newGeminiProvider(cfg.GeminiAPIKey, cfg.GeminiModel, cfg.AIMaxTokens, timeout), nil

	default:
		return nil, fmt.Errorf("unknown AI provider %q", cfg.AIProvider)
	}
}

// NewProviderFromInstallation builds a Provider for a specific installation's API key.
// providerName, apiKey, model are the installation's custom settings.
// cfg is the server config (for timeout, maxTokens).
func NewProviderFromInstallation(providerName, apiKey, model string, cfg *config.Config) (Provider, error) {
	timeout := time.Duration(cfg.AITimeout) * time.Second
	maxTokens := cfg.AIMaxTokens

	switch config.AIProvider(providerName) {
	case config.ProviderGroq:
		return newOpenAICompatProvider(openAICompatConfig{
			name:      "groq (installation)",
			baseURL:   "https://api.groq.com/openai/v1/chat/completions",
			apiKey:    apiKey,
			model:     orDefault(model, cfg.GroqModel),
			maxTokens: maxTokens,
			timeout:   timeout,
		}), nil

	case config.ProviderOpenAI:
		return newOpenAICompatProvider(openAICompatConfig{
			name:      "openai (installation)",
			baseURL:   "https://api.openai.com/v1/chat/completions",
			apiKey:    apiKey,
			model:     orDefault(model, cfg.OpenAIModel),
			maxTokens: maxTokens,
			timeout:   timeout,
		}), nil

	case config.ProviderGrok:
		return newOpenAICompatProvider(openAICompatConfig{
			name:      "grok (installation)",
			baseURL:   "https://api.x.ai/v1/chat/completions",
			apiKey:    apiKey,
			model:     orDefault(model, cfg.GrokModel),
			maxTokens: maxTokens,
			timeout:   timeout,
		}), nil

	case config.ProviderClaude:
		return newClaudeProvider(apiKey, orDefault(model, cfg.AnthropicModel), maxTokens, timeout), nil

	case config.ProviderGemini:
		return newGeminiProvider(apiKey, orDefault(model, cfg.GeminiModel), maxTokens, timeout), nil

	default:
		return nil, fmt.Errorf("unknown provider %q in installation config", providerName)
	}
}

func orDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

// ─── Response parsing (shared) ────────────────────────────────────────────────

// parseJSONComments parses the raw text response from any AI provider into
// a validated slice of ReviewComments. It handles:
//   - Markdown code-fenced JSON (```json ... ```)
//   - Plain JSON arrays
//   - Chain-of-thought output with reasoning before the JSON array
//   - Empty arrays (valid — no issues found)
//   - Non-JSON responses (logged, returns nil)
func parseJSONComments(raw, filename, providerName string) []ReviewComment {
	text := strings.TrimSpace(raw)

	// Strip markdown code fences that some models wrap their JSON in.
	if idx := strings.Index(text, "```json"); idx != -1 {
		text = text[idx+7:]
		if end := strings.Index(text, "```"); end != -1 {
			text = text[:end]
		}
		text = strings.TrimSpace(text)
	} else if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
		if end := strings.LastIndex(text, "```"); end != -1 {
			text = text[:end]
		}
		text = strings.TrimSpace(text)
	}

	// Handle chain-of-thought: model may emit reasoning before the JSON array.
	// Find the first '[' that starts the JSON array.
	if !strings.HasPrefix(text, "[") {
		if idx := strings.Index(text, "\n["); idx != -1 {
			text = text[idx+1:]
		} else if idx := strings.Index(text, "["); idx != -1 {
			text = text[idx:]
		}
	}

	text = strings.TrimSpace(text)

	if text == "" || text == "[]" {
		return nil
	}

	var comments []ReviewComment
	if err := json.Unmarshal([]byte(text), &comments); err != nil {
		preview := text
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		slog.Warn("AI returned non-JSON response",
			"provider", providerName,
			"file", filename,
			"preview", preview,
		)
		return nil
	}

	return validateComments(comments)
}

// validateComments removes malformed entries and normalises field values.
func validateComments(comments []ReviewComment) []ReviewComment {
	valid := make([]ReviewComment, 0, len(comments))
	for _, c := range comments {
		if c.Line <= 0 {
			continue // hallucinated or invalid line number
		}
		if strings.TrimSpace(c.Comment) == "" {
			continue // empty comment body
		}
		if !isValidSeverity(c.Severity) {
			c.Severity = "suggestion"
		}
		if !isValidCategory(c.Category) {
			c.Category = "best-practice"
		}
		c.Comment = strings.TrimSpace(c.Comment)
		c.Fix = strings.TrimSpace(c.Fix)
		valid = append(valid, c)
	}
	return valid
}

func isValidSeverity(s string) bool {
	return s == "error" || s == "warning" || s == "suggestion"
}

func isValidCategory(s string) bool {
	switch s {
	case "bug", "security", "performance", "code-smell", "best-practice":
		return true
	}
	return false
}

// ─── Retry helper ─────────────────────────────────────────────────────────────

// retryableError signals that the operation should be retried.
type retryableError struct{ cause error }

func (e *retryableError) Error() string { return e.cause.Error() }
func (e *retryableError) Unwrap() error { return e.cause }

// withRetry calls fn up to maxAttempts times with exponential backoff (1s, 2s, 4s).
// fn should return a *retryableError to trigger a retry; any other error stops immediately.
func withRetry(ctx context.Context, maxAttempts int, fn func() error) error {
	var lastErr error
	for i := range maxAttempts {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// Only retry on retryable errors.
		var re *retryableError
		isRetryable := false
		if e, ok := lastErr.(*retryableError); ok {
			re = e
			isRetryable = true
		}
		_ = re

		if !isRetryable {
			return lastErr
		}

		if i < maxAttempts-1 {
			wait := time.Duration(1<<i) * time.Second // 1s, 2s, 4s
			slog.Warn("AI call failed, retrying",
				"attempt", i+1,
				"max", maxAttempts,
				"wait", wait,
				"err", lastErr,
			)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return lastErr
}
