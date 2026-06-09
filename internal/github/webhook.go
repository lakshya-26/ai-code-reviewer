package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxBodyBytes is the upper bound on webhook payload size we will read.
// GitHub's documented max is ~25 MB.
const maxBodyBytes = 25 * 1024 * 1024

// ─── Data Models ──────────────────────────────────────────────────────────────

// PullRequestEvent is the top-level payload GitHub sends for pull_request events.
type PullRequestEvent struct {
	Action      string      `json:"action"`
	Number      int         `json:"number"`
	PullRequest PullRequest `json:"pull_request"`
	Repository  Repository  `json:"repository"`
	Installation Installation `json:"installation"`
	Sender      User        `json:"sender"`
}

type PullRequest struct {
	ID     int64  `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Draft  bool   `json:"draft"`
	State  string `json:"state"`
	Head   Ref    `json:"head"`
	Base   Ref    `json:"base"`
	User   User   `json:"user"`
	Body   string `json:"body"`
}

type Ref struct {
	SHA  string     `json:"sha"`
	Ref  string     `json:"ref"`
	Repo Repository `json:"repo"`
}

type Repository struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    User   `json:"owner"`
	Private  bool   `json:"private"`
}

type Installation struct {
	ID int64 `json:"id"`
}

type User struct {
	Login string `json:"login"`
	Type  string `json:"type"` // "User" or "Bot"
}

// ─── Delivery ID Deduplication ────────────────────────────────────────────────

// deliveryCache prevents processing the same webhook delivery twice.
// GitHub retries webhooks on non-200 or timeout — the cache stops double-reviews.
type deliveryCache struct {
	mu      sync.Mutex
	seen    map[string]time.Time
	maxAge  time.Duration
}

func newDeliveryCache() *deliveryCache {
	c := &deliveryCache{
		seen:   make(map[string]time.Time, 1024),
		maxAge: 1 * time.Hour,
	}
	go c.runEviction()
	return c
}

// markSeen returns true if this delivery ID was already processed.
func (c *deliveryCache) markSeen(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.seen[id]; exists {
		return true
	}
	c.seen[id] = time.Now()
	return false
}

// runEviction removes entries older than maxAge every 30 minutes.
func (c *deliveryCache) runEviction() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		cutoff := time.Now().Add(-c.maxAge)
		for id, seen := range c.seen {
			if seen.Before(cutoff) {
				delete(c.seen, id)
			}
		}
		c.mu.Unlock()
	}
}

// ─── Signature Verification ───────────────────────────────────────────────────

// VerifySignature validates the HMAC-SHA256 signature GitHub sends in
// X-Hub-Signature-256. Uses constant-time comparison to prevent timing attacks.
func VerifySignature(body []byte, signature, secret string) bool {
	if secret == "" {
		slog.Error("webhook secret is empty — rejecting all requests")
		return false
	}
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}

// ─── Webhook Handler ──────────────────────────────────────────────────────────

// JobSubmitter is implemented by worker.Pool — defined as an interface here so
// the github package does not import the worker package (avoids a cycle).
type JobSubmitter interface {
	Submit(event PullRequestEvent)
}

// NewWebhookHandler returns the HTTP handler for POST /webhook.
func NewWebhookHandler(secret string, pool JobSubmitter) http.HandlerFunc {
	deliveries := newDeliveryCache()

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Read body first — needed for HMAC verification.
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
		if err != nil {
			slog.Error("failed to read webhook body", "err", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Respond 200 immediately — GitHub retries if it doesn't get 200 within 10s.
		// All real processing happens asynchronously in the worker pool.
		w.WriteHeader(http.StatusOK)

		deliveryID := r.Header.Get("X-GitHub-Delivery")
		event := r.Header.Get("X-GitHub-Event")
		signature := r.Header.Get("X-Hub-Signature-256")

		// Handle GitHub's ping (sent when the App is first installed).
		if event == "ping" {
			slog.Info("github ping received", "delivery", deliveryID)
			return
		}

		if !VerifySignature(body, signature, secret) {
			slog.Warn("webhook signature invalid — request rejected", "delivery", deliveryID)
			return
		}

		if event != "pull_request" {
			return
		}

		// Deduplicate — GitHub may retry the same delivery.
		if deliveries.markSeen(deliveryID) {
			slog.Info("duplicate delivery ignored", "delivery", deliveryID)
			return
		}

		var payload PullRequestEvent
		if err := json.Unmarshal(body, &payload); err != nil {
			slog.Error("failed to parse webhook payload", "delivery", deliveryID, "err", err)
			return
		}

		// Only act on these three actions — everything else is noise.
		switch payload.Action {
		case "opened", "synchronize", "reopened":
		default:
			return
		}

		slog.Info("pull request event received",
			"delivery", deliveryID,
			"action", payload.Action,
			"repo", payload.Repository.FullName,
			"pr", payload.Number,
		)

		pool.Submit(payload)
	}
}

// NewHealthHandler returns a simple liveness probe handler for GET /health.
func NewHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}
}
