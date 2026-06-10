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


// openAICompatProvider handles any API that speaks OpenAI's chat completions format.
// This covers: local llama.cpp server, OpenAI, and xAI Grok — all three use
// identical request/response shapes, just different base URLs and auth keys.
type openAICompatProvider struct {
	cfg    openAICompatConfig
	client *http.Client
}

type openAICompatConfig struct {
	name      string
	baseURL   string // full URL, e.g. https://api.openai.com/v1/chat/completions
	apiKey    string
	model     string
	maxTokens int
	timeout   time.Duration
}

func newOpenAICompatProvider(cfg openAICompatConfig) *openAICompatProvider {
	return &openAICompatProvider{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.timeout,
		},
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
		Code    any    `json:"code"`
	} `json:"error"`
}

// ─── AnalyzeFile ──────────────────────────────────────────────────────────────

func (p *openAICompatProvider) AnalyzeFile(ctx context.Context, filename, patch string) ([]ReviewComment, error) {
	prompt, _ := BuildPrompt(filename, patch, 0)

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

func (p *openAICompatProvider) callAPI(ctx context.Context, prompt string) (string, error) {
	reqBody := openAIRequest{
		Model: p.cfg.model,
		Messages: []openAIMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   p.cfg.maxTokens,
		Temperature: 0.1, // low temperature = deterministic, less hallucination
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
		// Network errors are retryable.
		return "", &retryableError{cause: fmt.Errorf("HTTP request failed: %w", err)}
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024)) // 4 MB cap
	if err != nil {
		return "", &retryableError{cause: fmt.Errorf("reading response body: %w", err)}
	}

	// Server errors (5xx) are retryable; client errors (4xx) are not.
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
