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

const geminiAPIBase = "https://generativelanguage.googleapis.com/v1beta/models"

// geminiProvider calls Google's Generative Language API (Gemini).
type geminiProvider struct {
	apiKey    string
	model     string
	maxTokens int
	client    *http.Client
}

func newGeminiProvider(apiKey, model string, maxTokens int, timeout time.Duration) *geminiProvider {
	return &geminiProvider{
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		client:    &http.Client{Timeout: timeout},
	}
}

func (p *geminiProvider) Name() string { return "gemini" }

// ─── Gemini wire types ────────────────────────────────────────────────────────

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"system_instruction,omitempty"` // dedicated system prompt field
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  geminiGenConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens"`
	Temperature     float64 `json:"temperature"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// ─── AnalyzeFile ──────────────────────────────────────────────────────────────

func (p *geminiProvider) AnalyzeFile(ctx context.Context, in FileAnalysisInput) ([]ReviewComment, error) {
	prompt := BuildPrompt(in, 0)

	var comments []ReviewComment
	err := withRetry(ctx, 3, func() error {
		raw, err := p.callAPI(ctx, prompt)
		if err != nil {
			return err
		}
		comments = parseJSONComments(raw, in.Filename, p.Name())
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: AnalyzeFile(%s): %w", in.Filename, err)
	}

	slog.Debug("AI analysis complete",
		"provider", p.Name(),
		"file", in.Filename,
		"comments", len(comments),
	)
	return comments, nil
}

func (p *geminiProvider) callAPI(ctx context.Context, prompt BuiltPrompt) (string, error) {
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiAPIBase, p.model, p.apiKey)

	reqBody := geminiRequest{
		// Gemini's system_instruction field — keeps system context separate from user turn.
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: prompt.System}},
		},
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: prompt.User}}},
		},
		GenerationConfig: geminiGenConfig{
			MaxOutputTokens: p.maxTokens,
			Temperature:     0.1,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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

	var apiResp geminiResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return "", fmt.Errorf("parsing response JSON: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("API error [%s]: %s", apiResp.Error.Status, apiResp.Error.Message)
	}

	if len(apiResp.Candidates) == 0 || len(apiResp.Candidates[0].Content.Parts) == 0 {
		return "", nil
	}

	return apiResp.Candidates[0].Content.Parts[0].Text, nil
}
