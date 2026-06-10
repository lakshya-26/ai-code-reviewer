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

const (
	claudeAPIURL = "https://api.anthropic.com/v1/messages"
	claudeAPIVersion = "2023-06-01"
)

// claudeProvider calls Anthropic's Messages API.
// Anthropic uses a different wire format from OpenAI — different request shape,
// different auth headers, and a different response structure.
type claudeProvider struct {
	apiKey    string
	model     string
	maxTokens int
	client    *http.Client
}

func newClaudeProvider(apiKey, model string, maxTokens int, timeout time.Duration) *claudeProvider {
	return &claudeProvider{
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		client:    &http.Client{Timeout: timeout},
	}
}

func (p *claudeProvider) Name() string { return "claude" }

// ─── Anthropic wire types ─────────────────────────────────────────────────────

type claudeRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []claudeMessage  `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	StopReason string `json:"stop_reason"`
}

// ─── AnalyzeFile ──────────────────────────────────────────────────────────────

func (p *claudeProvider) AnalyzeFile(ctx context.Context, filename, patch string) ([]ReviewComment, error) {
	prompt := buildPromptPlaceholder(filename, patch)

	var comments []ReviewComment
	err := withRetry(ctx, 3, func() error {
		raw, err := p.callAPI(ctx, prompt)
		if err != nil {
			return err
		}
		comments = parseJSONComments(raw, filename, p.Name())
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claude: AnalyzeFile(%s): %w", filename, err)
	}

	slog.Debug("AI analysis complete",
		"provider", p.Name(),
		"file", filename,
		"comments", len(comments),
	)
	return comments, nil
}

func (p *claudeProvider) callAPI(ctx context.Context, prompt string) (string, error) {
	reqBody := claudeRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		Messages: []claudeMessage{
			{Role: "user", Content: prompt},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", claudeAPIVersion)

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
	if resp.StatusCode == 529 { // Anthropic-specific overload code
		return "", &retryableError{cause: fmt.Errorf("API overloaded (529)")}
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("client error %d: %s", resp.StatusCode, truncate(string(respBytes), 200))
	}

	var apiResp claudeResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return "", fmt.Errorf("parsing response JSON: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("API error [%s]: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	for _, block := range apiResp.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", nil
}
