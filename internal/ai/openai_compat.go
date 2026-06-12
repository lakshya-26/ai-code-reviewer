package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// openAICompatProvider handles any API that speaks the OpenAI chat completions format.
// Covers: local llama.cpp, OpenAI, and xAI Grok.
type openAICompatProvider struct {
	cfg    openAICompatConfig
	client *http.Client
}

type openAICompatConfig struct {
	name      string
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	timeout   time.Duration
}

func newOpenAICompatProvider(cfg openAICompatConfig) *openAICompatProvider {
	return &openAICompatProvider{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.timeout},
	}
}

func (p *openAICompatProvider) Name() string { return p.cfg.name }

// ─── OpenAI wire types ────────────────────────────────────────────────────────

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ─── AnalyzeFile ──────────────────────────────────────────────────────────────

func (p *openAICompatProvider) AnalyzeFile(ctx context.Context, filename, patch string, prCtx PRContext) ([]ReviewComment, error) {
	prompt := BuildPrompt(filename, patch, 0, prCtx)

	var comments []ReviewComment
	err := withRetry(ctx, 3, func() error {
		raw, err := p.callAPI(ctx, prompt)
		if err != nil {
			return err
		}
		comments = parseJSONComments(raw, filename, p.cfg.name)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s: AnalyzeFile(%s): %w", p.cfg.name, filename, err)
	}

	slog.Debug("AI analysis complete",
		"provider", p.cfg.name,
		"file", filename,
		"comments", len(comments),
	)
	return comments, nil
}

func (p *openAICompatProvider) callAPI(ctx context.Context, prompt BuiltPrompt) (string, error) {
	// System + user message split — models follow system prompts more reliably.
	messages := []openAIMessage{
		{Role: "system", Content: prompt.System},
		{Role: "user", Content: prompt.User},
	}

	// Adaptive temperature: security/bug detection benefits from slightly higher
	// temperature to catch non-obvious issues; JSON formatting stays strict.
	temperature := 0.1

	reqBody := openAIRequest{
		Model:       p.cfg.model,
		Messages:    messages,
		MaxTokens:   p.cfg.maxTokens,
		Temperature: temperature,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", &retryableError{cause: fmt.Errorf("HTTP request failed: %w", err)}
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return "", &retryableError{cause: fmt.Errorf("reading response body: %w", err)}
	}

	if resp.StatusCode >= 500 {
		return "", &retryableError{cause: fmt.Errorf("server error %d: %s", resp.StatusCode, truncate(string(respBytes), 200))}
	}
	if resp.StatusCode == 429 {
		return "", &retryableError{cause: fmt.Errorf("rate limited (429)")}
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("client error %d: %s", resp.StatusCode, truncate(string(respBytes), 200))
	}

	var apiResp openAIResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return "", fmt.Errorf("parsing response JSON: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return "", nil
	}

	return apiResp.Choices[0].Message.Content, nil
}

// truncate shortens a string to n characters, appending "..." if trimmed.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
