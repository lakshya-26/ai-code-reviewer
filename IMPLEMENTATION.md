# AI Code Reviewer Bot — Full Implementation Guide

> Built in Go. Reviews GitHub PRs automatically using Claude AI and posts inline comments.

---

## Table of Contents

1. [Project Overview](#1-project-overview)
2. [Tech Stack](#2-tech-stack)
3. [Project Structure](#3-project-structure)
4. [Environment Variables](#4-environment-variables)
5. [GitHub App Setup](#5-github-app-setup)
6. [Data Models](#6-data-models)
7. [Core Components](#7-core-components)
   - 7.1 [HTTP Server & Webhook Handler](#71-http-server--webhook-handler)
   - 7.2 [Webhook Signature Verification](#72-webhook-signature-verification)
   - 7.3 [GitHub App Authentication](#73-github-app-authentication)
   - 7.4 [Installation Token Cache](#74-installation-token-cache)
   - 7.5 [PR Diff Fetcher](#75-pr-diff-fetcher)
   - 7.6 [Unified Diff Parser](#76-unified-diff-parser)
   - 7.7 [File Filter](#77-file-filter)
   - 7.8 [AI Analyzer (Claude)](#78-ai-analyzer-claude)
   - 7.9 [Prompt Builder](#79-prompt-builder)
   - 7.10 [Review Poster](#710-review-poster)
   - 7.11 [Job Queue (Worker Pool)](#711-job-queue-worker-pool)
8. [Edge Cases — Full List](#8-edge-cases--full-list)
9. [Configuration File (.ai-reviewer.yml)](#9-configuration-file-ai-revieweryml)
10. [Error Handling Strategy](#10-error-handling-strategy)
11. [Rate Limiting Strategy](#11-rate-limiting-strategy)
12. [Deployment](#12-deployment)
13. [Testing Strategy](#13-testing-strategy)
14. [Flow Diagram](#14-flow-diagram)

---

## 1. Project Overview

A GitHub App written in Go that:
- Listens for `pull_request` webhook events (opened, synchronize, reopened)
- Fetches the PR diff from GitHub API
- Parses the diff to extract changed lines with correct line numbers
- Sends each file's diff to Claude API for analysis
- Posts inline review comments on the PR via GitHub Pull Request Review API

The bot posts comments inline on specific lines — exactly like a human reviewer.

---

## 2. Tech Stack

| Concern | Library | Why |
|---------|---------|-----|
| HTTP Server | `net/http` (stdlib) | No need for a framework for a webhook server |
| GitHub API | `github.com/google/go-github/v62` | Official Go GitHub client |
| GitHub App Auth | `github.com/bradleyfalzon/ghinstallation/v2` | Handles JWT + installation token exchange |
| Diff Parsing | `github.com/sourcegraph/go-diff/diff` | Parse unified diff format correctly |
| Claude API | `github.com/anthropics/anthropic-sdk-go` | Official Anthropic Go SDK |
| Config | `github.com/spf13/viper` | Env + yaml config management |
| Logging | `log/slog` (stdlib, Go 1.21+) | Structured logging, no extra dep |
| YAML parsing | `gopkg.in/yaml.v3` | For `.ai-reviewer.yml` per-repo config |
| HTTP Client | `net/http` (stdlib) | Raw HTTP for Claude if SDK unavailable |

Go version: **1.21+** (required for `log/slog`)

---

## 3. Project Structure

```
ai-code-reviewer/
├── cmd/
│   └── server/
│       └── main.go                  # Entry point
├── internal/
│   ├── config/
│   │   └── config.go                # Env vars + app config loading
│   ├── github/
│   │   ├── app.go                   # GitHub App JWT + installation token auth
│   │   ├── client.go                # Authenticated Octokit client factory
│   │   ├── diff.go                  # Fetch PR files and raw diff
│   │   ├── review.go                # Post review comments to GitHub
│   │   └── webhook.go               # Webhook signature verification + parsing
│   ├── parser/
│   │   └── diff.go                  # Unified diff parser — line number mapping
│   ├── ai/
│   │   ├── analyzer.go              # Claude API call + response parsing
│   │   └── prompt.go                # Prompt templates per category
│   ├── filter/
│   │   └── file.go                  # Decide which files to skip
│   ├── worker/
│   │   └── pool.go                  # Goroutine worker pool for PR processing
│   ├── cache/
│   │   └── token.go                 # In-memory installation token cache
│   └── reviewer/
│       └── reviewer.go              # Orchestrator — ties all components together
├── config/
│   └── default.yml                  # Default reviewer config values
├── .env.example
├── go.mod
├── go.sum
├── Dockerfile
└── IMPLEMENTATION.md
```

---

## 4. Environment Variables

```env
# GitHub App credentials
GITHUB_APP_ID=123456
GITHUB_APP_PRIVATE_KEY_PATH=/etc/secrets/private-key.pem
# OR pass the key contents directly (for Railway/Render)
GITHUB_APP_PRIVATE_KEY_CONTENTS=-----BEGIN RSA PRIVATE KEY-----\n...

GITHUB_WEBHOOK_SECRET=your_webhook_secret_here

# Claude API
ANTHROPIC_API_KEY=sk-ant-...
ANTHROPIC_MODEL=claude-sonnet-4-6
ANTHROPIC_MAX_TOKENS=4096

# Server
PORT=3000
ENV=production  # or development

# Worker pool
WORKER_COUNT=5           # concurrent PR processing goroutines
MAX_FILES_PER_PR=50      # skip PRs larger than this
MAX_PATCH_CHARS=10000    # truncate file patches larger than this
```

---

## 5. GitHub App Setup

### Step 1: Create the GitHub App

1. Go to GitHub → Settings → Developer Settings → GitHub Apps → New GitHub App
2. Fill in:
   - **App name**: `ai-code-reviewer` (or anything unique)
   - **Homepage URL**: your server URL or `http://localhost:3000`
   - **Webhook URL**: `https://your-domain.com/webhook` (use ngrok for local)
   - **Webhook Secret**: generate a random string, save it as `GITHUB_WEBHOOK_SECRET`

### Step 2: Set Permissions

Under **Repository Permissions**:
- `Pull requests` → **Read & Write** (to read PR diff and post comments)
- `Contents` → **Read** (to read repo config file `.ai-reviewer.yml`)
- `Metadata` → **Read** (mandatory, auto-set)

### Step 3: Subscribe to Events

Check: `Pull request`

### Step 4: Generate Private Key

- Scroll to bottom → Generate a private key
- Download the `.pem` file
- Save path as `GITHUB_APP_PRIVATE_KEY_PATH`

### Step 5: Install the App

- After creating, go to your App → Install App
- Install on the target repository or all repositories

### Step 6: Note Your App ID

- Found on the App's General settings page
- Save as `GITHUB_APP_ID`

---

## 6. Data Models

```go
// internal/github/webhook.go

// Top-level webhook payload for pull_request events
type PullRequestEvent struct {
    Action      string      `json:"action"`       // opened, synchronize, reopened, closed
    Number      int         `json:"number"`
    PullRequest PullRequest `json:"pull_request"`
    Repository  Repository  `json:"repository"`
    Installation Installation `json:"installation"`
    Sender      User        `json:"sender"`
}

type PullRequest struct {
    ID      int64  `json:"id"`
    Number  int    `json:"number"`
    Title   string `json:"title"`
    Draft   bool   `json:"draft"`
    State   string `json:"state"`
    Head    Ref    `json:"head"`
    Base    Ref    `json:"base"`
    User    User   `json:"user"`
    Body    string `json:"body"`
}

type Ref struct {
    SHA  string `json:"sha"`
    Ref  string `json:"ref"`
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
    Type  string `json:"type"`  // User or Bot
}
```

```go
// internal/parser/diff.go

type ParsedLine struct {
    LineNumber int    // line number in the NEW file
    Content    string // line content without the leading +
}

type ParsedFile struct {
    Filename  string
    Lines     []ParsedLine
    RawPatch  string
}
```

```go
// internal/ai/analyzer.go

type ReviewComment struct {
    Line     int    `json:"line"`
    Severity string `json:"severity"`   // error | warning | suggestion
    Category string `json:"category"`   // bug | security | performance | code-smell | best-practice
    Comment  string `json:"comment"`
}

type FileReview struct {
    Filename string
    Comments []ReviewComment
}
```

---

## 7. Core Components

### 7.1 HTTP Server & Webhook Handler

```go
// cmd/server/main.go

func main() {
    cfg := config.Load()

    pool := worker.NewPool(cfg.WorkerCount)
    pool.Start()

    mux := http.NewServeMux()
    mux.HandleFunc("/webhook", handleWebhook(cfg, pool))
    mux.HandleFunc("/health", handleHealth)

    slog.Info("server starting", "port", cfg.Port)
    http.ListenAndServe(":"+cfg.Port, mux)
}
```

```go
// internal/github/webhook.go

func handleWebhook(cfg *config.Config, pool *worker.Pool) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            w.WriteHeader(http.StatusMethodNotAllowed)
            return
        }

        // Read body first — needed for signature verification
        body, err := io.ReadAll(io.LimitReader(r.Body, 25*1024*1024)) // 25MB limit
        if err != nil {
            w.WriteHeader(http.StatusBadRequest)
            return
        }

        // Always respond 200 immediately — GitHub retries if it doesn't get 200 fast
        w.WriteHeader(http.StatusOK)

        event := r.Header.Get("X-GitHub-Event")
        signature := r.Header.Get("X-Hub-Signature-256")
        deliveryID := r.Header.Get("X-GitHub-Delivery")

        if !VerifySignature(body, signature, cfg.WebhookSecret) {
            slog.Warn("invalid webhook signature", "delivery", deliveryID)
            return
        }

        if event != "pull_request" {
            return
        }

        var payload PullRequestEvent
        if err := json.Unmarshal(body, &payload); err != nil {
            slog.Error("failed to parse webhook payload", "err", err)
            return
        }

        // Only process these actions
        validActions := map[string]bool{
            "opened":      true,
            "synchronize": true,
            "reopened":    true,
        }
        if !validActions[payload.Action] {
            return
        }

        // Submit to worker pool — non-blocking
        pool.Submit(payload)
    }
}
```

**Why respond 200 immediately:**
GitHub marks your webhook as failed if it doesn't receive a 200 within 10 seconds. PR analysis takes longer than that. Always respond first, process asynchronously.

---

### 7.2 Webhook Signature Verification

```go
// internal/github/webhook.go

func VerifySignature(body []byte, signature, secret string) bool {
    if !strings.HasPrefix(signature, "sha256=") {
        return false
    }

    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

    // Use constant-time comparison to prevent timing attacks
    return hmac.Equal([]byte(expected), []byte(signature))
}
```

**Edge cases:**
- Signature header missing → reject silently
- Empty secret configured → log error and reject all requests
- Body too large → reject before reading fully (apply `io.LimitReader`)

---

### 7.3 GitHub App Authentication

GitHub Apps authenticate in two steps:
1. **App-level JWT** — proves you are the App (short-lived, 10 min max)
2. **Installation token** — scoped to a specific repo installation (1 hour)

```go
// internal/github/app.go

func NewInstallationClient(appID int64, privateKeyPath string, installationID int64) (*github.Client, error) {
    var privateKey []byte
    var err error

    // Support both file path and raw key content from env
    if strings.HasPrefix(privateKeyPath, "-----BEGIN") {
        privateKey = []byte(privateKeyPath)
    } else {
        privateKey, err = os.ReadFile(privateKeyPath)
        if err != nil {
            return nil, fmt.Errorf("reading private key: %w", err)
        }
    }

    itr, err := ghinstallation.New(
        http.DefaultTransport,
        appID,
        installationID,
        privateKey,
    )
    if err != nil {
        return nil, fmt.Errorf("creating installation transport: %w", err)
    }

    return github.NewClient(&http.Client{Transport: itr}), nil
}
```

---

### 7.4 Installation Token Cache

Creating a new GitHub client per webhook is expensive — the library refreshes the token when needed, but caching the client avoids repeated JWT creation overhead.

```go
// internal/cache/token.go

type ClientCache struct {
    mu      sync.RWMutex
    clients map[int64]*github.Client  // keyed by installation ID
    appID   int64
    keyPath string
}

func (c *ClientCache) Get(installationID int64) (*github.Client, error) {
    c.mu.RLock()
    if client, ok := c.clients[installationID]; ok {
        c.mu.RUnlock()
        return client, nil
    }
    c.mu.RUnlock()

    c.mu.Lock()
    defer c.mu.Unlock()

    // Double-check after acquiring write lock
    if client, ok := c.clients[installationID]; ok {
        return client, nil
    }

    client, err := NewInstallationClient(c.appID, c.keyPath, installationID)
    if err != nil {
        return nil, err
    }

    c.clients[installationID] = client
    return client, nil
}
```

---

### 7.5 PR Diff Fetcher

```go
// internal/github/diff.go

type PRFile struct {
    Filename string
    Status   string  // added, modified, removed, renamed
    Patch    string  // raw unified diff
    BlobURL  string
}

func FetchPRFiles(ctx context.Context, client *github.Client, owner, repo string, prNumber int) ([]PRFile, error) {
    var allFiles []PRFile
    opts := &github.ListOptions{PerPage: 100}

    for {
        files, resp, err := client.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
        if err != nil {
            return nil, fmt.Errorf("listing PR files: %w", err)
        }

        for _, f := range files {
            allFiles = append(allFiles, PRFile{
                Filename: f.GetFilename(),
                Status:   f.GetStatus(),
                Patch:    f.GetPatch(),
                BlobURL:  f.GetBlobURL(),
            })
        }

        if resp.NextPage == 0 {
            break
        }
        opts.Page = resp.NextPage
    }

    return allFiles, nil
}
```

**Edge cases:**
- PRs with 100+ files require pagination — always loop until `NextPage == 0`
- Binary files have empty `Patch` — skip them
- Deleted files (`status == "removed"`) — skip, nothing to review
- Renamed files with no content change — `Patch` may be empty, skip

---

### 7.6 Unified Diff Parser

This is the most critical component. A wrong line number means your comment lands on the wrong line.

```go
// internal/parser/diff.go

// ParsePatch extracts added lines with their correct line numbers in the new file
func ParsePatch(filename, patch string) ParsedFile {
    result := ParsedFile{
        Filename: filename,
        RawPatch: patch,
    }

    if patch == "" {
        return result
    }

    lines := strings.Split(patch, "\n")
    newLineNum := 0

    for _, line := range lines {
        // Hunk header: @@ -oldStart,oldCount +newStart,newCount @@
        if strings.HasPrefix(line, "@@") {
            // Extract new file starting line number
            re := regexp.MustCompile(`\+(\d+)`)
            match := re.FindStringSubmatch(line)
            if len(match) > 1 {
                num, _ := strconv.Atoi(match[1])
                newLineNum = num - 1 // will be incremented on first real line
            }
            continue
        }

        // Skip diff file headers
        if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
            continue
        }

        if strings.HasPrefix(line, "+") {
            newLineNum++
            result.Lines = append(result.Lines, ParsedLine{
                LineNumber: newLineNum,
                Content:    line[1:], // strip the leading +
            })
        } else if strings.HasPrefix(line, "-") {
            // Removed line — does NOT increment new file line counter
            continue
        } else {
            // Context line — exists in both old and new file
            newLineNum++
        }
    }

    return result
}
```

**Edge cases in diff parsing:**
- Hunk header with no count: `@@ -1 +1 @@` means count=1, handle missing count
- `\ No newline at end of file` lines — skip entirely, not real code
- Empty patch string — return empty ParsedFile, skip AI call
- Multi-hunk patches — reset `newLineNum` correctly on each `@@` line
- Binary file indicator in patch — contains "Binary files differ", return empty

---

### 7.7 File Filter

```go
// internal/filter/file.go

var defaultSkippedExtensions = []string{
    ".lock", ".sum", ".mod",         // dependency lock files
    ".png", ".jpg", ".jpeg", ".gif", // images
    ".svg", ".ico", ".woff",         // assets
    ".pdf", ".zip", ".tar", ".gz",   // archives
    ".min.js", ".min.css",           // minified files
}

var defaultSkippedPaths = []string{
    "vendor/",
    "node_modules/",
    "dist/",
    "build/",
    ".github/",
    "migrations/",          // SQL migrations — schema only, not logic
    "testdata/",
    "mocks/",
    "__generated__/",
}

var defaultSkippedFilenames = []string{
    "package-lock.json",
    "yarn.lock",
    "pnpm-lock.yaml",
    "go.sum",
    "Cargo.lock",
    "poetry.lock",
}

func ShouldSkip(filename string, repoConfig *RepoConfig) bool {
    base := filepath.Base(filename)

    // Check exact filename matches
    for _, name := range defaultSkippedFilenames {
        if base == name {
            return true
        }
    }

    // Check extension
    for _, ext := range defaultSkippedExtensions {
        if strings.HasSuffix(filename, ext) {
            return true
        }
    }

    // Check path prefixes
    for _, path := range defaultSkippedPaths {
        if strings.HasPrefix(filename, path) {
            return true
        }
    }

    // Check repo-level custom ignores from .ai-reviewer.yml
    if repoConfig != nil {
        for _, pattern := range repoConfig.IgnorePaths {
            matched, _ := filepath.Match(pattern, filename)
            if matched {
                return true
            }
        }
    }

    return false
}
```

---

### 7.8 AI Analyzer (Claude)

```go
// internal/ai/analyzer.go

type Analyzer struct {
    client      *anthropic.Client
    model       string
    maxTokens   int
    maxPatchLen int
}

func (a *Analyzer) AnalyzeFile(ctx context.Context, filename, patch string) ([]ReviewComment, error) {
    // Truncate very large patches
    truncated := false
    if len(patch) > a.maxPatchLen {
        patch = patch[:a.maxPatchLen]
        truncated = true
    }

    prompt := BuildPrompt(filename, patch, truncated)

    msg, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     anthropic.F(a.model),
        MaxTokens: anthropic.F(int64(a.maxTokens)),
        Messages: anthropic.F([]anthropic.MessageParam{
            anthropic.UserMessageParam(prompt),
        }),
    })
    if err != nil {
        return nil, fmt.Errorf("claude API call failed: %w", err)
    }

    if len(msg.Content) == 0 {
        return nil, nil
    }

    rawText := msg.Content[0].Text

    // Strip markdown code fences if Claude wraps JSON in them
    rawText = strings.TrimPrefix(rawText, "```json")
    rawText = strings.TrimPrefix(rawText, "```")
    rawText = strings.TrimSuffix(rawText, "```")
    rawText = strings.TrimSpace(rawText)

    var comments []ReviewComment
    if err := json.Unmarshal([]byte(rawText), &comments); err != nil {
        // Claude returned non-JSON — log and return empty
        slog.Warn("claude returned non-JSON response", "file", filename, "raw", rawText[:min(200, len(rawText))])
        return nil, nil
    }

    return validateComments(comments), nil
}

// validateComments removes hallucinated or malformed comments
func validateComments(comments []ReviewComment) []ReviewComment {
    valid := []ReviewComment{}
    for _, c := range comments {
        if c.Line <= 0 {
            continue  // invalid line number
        }
        if c.Comment == "" {
            continue  // empty comment
        }
        if !isValidSeverity(c.Severity) {
            c.Severity = "suggestion"  // default instead of rejecting
        }
        if !isValidCategory(c.Category) {
            c.Category = "best-practice"
        }
        valid = append(valid, c)
    }
    return valid
}
```

**Edge cases:**
- Claude returns markdown-wrapped JSON (`\`\`\`json ... \`\`\``) — strip fences
- Claude returns empty array `[]` — valid, means no issues found
- Claude returns explanatory text instead of JSON — log and skip
- Claude times out — retry once with exponential backoff
- Claude returns comments with `line: 0` — hallucinated, discard
- Claude invents line numbers beyond the file length — validate against parsed line count

---

### 7.9 Prompt Builder

```go
// internal/ai/prompt.go

func BuildPrompt(filename, patch string, truncated bool) string {
    truncationNote := ""
    if truncated {
        truncationNote = "\nNote: This diff was truncated due to size. Review what is visible only.\n"
    }

    ext := filepath.Ext(filename)
    langHint := languageHint(ext)

    return fmt.Sprintf(`You are a senior software engineer conducting a thorough code review.
Language: %s
File: %s
%s
Diff (lines starting with + are additions):
%s

Review ONLY the added lines (starting with +). Do not comment on removed lines or context.

Check for:
- Bugs: logic errors, off-by-one, null/nil dereference, unchecked errors
- Security: SQL injection, XSS, hardcoded secrets, insecure crypto, improper auth checks
- Performance: N+1 queries, unnecessary loops, missing indexes hinted by code, blocking calls in hot paths
- Code smell: duplicated logic, overly complex conditionals, misleading variable names
- Best practices: unhandled errors, missing input validation, resource leaks (unclosed files, connections)

Rules:
- Only comment on lines you are confident have a real issue
- Do not comment on code style, formatting, or personal preference
- Do not suggest adding comments or documentation
- Each comment must reference a specific line number from the added lines
- If no issues found, return an empty array

Return a JSON array only. No explanation. No markdown. Just raw JSON.

Schema:
[
  {
    "line": <integer — line number in the new file>,
    "severity": "error" | "warning" | "suggestion",
    "category": "bug" | "security" | "performance" | "code-smell" | "best-practice",
    "comment": "<clear explanation of the issue and how to fix it>"
  }
]`,
        langHint, filename, truncationNote, patch)
}

func languageHint(ext string) string {
    hints := map[string]string{
        ".go":   "Go",
        ".js":   "JavaScript",
        ".ts":   "TypeScript",
        ".py":   "Python",
        ".java": "Java",
        ".rb":   "Ruby",
        ".rs":   "Rust",
        ".sql":  "SQL",
        ".sh":   "Shell",
        ".yaml": "YAML",
        ".yml":  "YAML",
        ".json": "JSON",
    }
    if lang, ok := hints[ext]; ok {
        return lang
    }
    return "Unknown"
}
```

---

### 7.10 Review Poster

```go
// internal/github/review.go

func PostReview(
    ctx context.Context,
    client *github.Client,
    owner, repo string,
    prNumber int,
    commitSHA string,
    fileReviews []FileReview,
) error {
    var comments []*github.DraftReviewComment
    totalIssues := 0

    for _, fr := range fileReviews {
        for _, c := range fr.Comments {
            body := fmt.Sprintf("**[%s]** `%s`\n\n%s",
                strings.ToUpper(c.Severity),
                c.Category,
                c.Comment,
            )
            line := c.Line
            comments = append(comments, &github.DraftReviewComment{
                Path: github.String(fr.Filename),
                Line: github.Int(line),
                Body: github.String(body),
            })
            totalIssues++
        }
    }

    event := "COMMENT"
    var body string

    if totalIssues == 0 {
        event = "APPROVE"
        body = "✅ AI Review: No issues found. Looks good to merge."
    } else {
        errorCount := countBySeverity(fileReviews, "error")
        warningCount := countBySeverity(fileReviews, "warning")
        suggestionCount := countBySeverity(fileReviews, "suggestion")

        body = fmt.Sprintf(
            "🤖 **AI Code Review**\n\nFound **%d issue(s)** across %d file(s).\n\n"+
                "| Severity | Count |\n|----------|-------|\n"+
                "| 🔴 Error | %d |\n| 🟡 Warning | %d |\n| 💡 Suggestion | %d |",
            totalIssues, len(fileReviews),
            errorCount, warningCount, suggestionCount,
        )
    }

    review := &github.PullRequestReviewRequest{
        CommitID: github.String(commitSHA),
        Body:     github.String(body),
        Event:    github.String(event),
        Comments: comments,
    }

    _, _, err := client.PullRequests.CreateReview(ctx, owner, repo, prNumber, review)
    if err != nil {
        return fmt.Errorf("posting review: %w", err)
    }

    return nil
}
```

**Edge cases in posting:**
- Line number in comment doesn't exist in the diff — GitHub API returns 422. Always validate line numbers against the parsed diff before posting.
- Commit SHA mismatch — use `pull_request.head.sha` from the webhook payload, not fetched separately.
- Comment body too long — GitHub has a 65536 character limit per comment. Truncate if needed.
- Posting 0 comments with APPROVE event — valid and intentional.
- GitHub API rate limit hit during post — retry after the `Retry-After` header value.

---

### 7.11 Job Queue (Worker Pool)

```go
// internal/worker/pool.go

type Pool struct {
    jobs    chan github.PullRequestEvent
    workers int
    wg      sync.WaitGroup
}

func NewPool(workerCount int) *Pool {
    return &Pool{
        jobs:    make(chan github.PullRequestEvent, 100), // buffer 100 jobs
        workers: workerCount,
    }
}

func (p *Pool) Start(reviewer *reviewer.Reviewer) {
    for i := 0; i < p.workers; i++ {
        p.wg.Add(1)
        go func() {
            defer p.wg.Done()
            for event := range p.jobs {
                ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
                if err := reviewer.Review(ctx, event); err != nil {
                    slog.Error("review failed",
                        "repo", event.Repository.FullName,
                        "pr", event.Number,
                        "err", err,
                    )
                }
                cancel()
            }
        }()
    }
}

func (p *Pool) Submit(event github.PullRequestEvent) {
    select {
    case p.jobs <- event:
    default:
        // Queue full — log and drop. Better than blocking the webhook handler.
        slog.Warn("worker queue full, dropping PR event",
            "repo", event.Repository.FullName,
            "pr", event.Number,
        )
    }
}

func (p *Pool) Stop() {
    close(p.jobs)
    p.wg.Wait()
}
```

---

## 8. Edge Cases — Full List

### Webhook Layer
| Case | Handling |
|------|----------|
| GitHub retries the same webhook (X-GitHub-Delivery is same) | Idempotency key — track recent delivery IDs in-memory (last 1000 with TTL), skip duplicates |
| Webhook secret not configured | Startup check — fail fast with clear error message |
| Body larger than 25MB | `io.LimitReader` — reject with 400 |
| Non-PR events received (GitHub sends others too) | Check `X-GitHub-Event` header — only process `pull_request` |
| Malformed JSON payload | `json.Unmarshal` error — log and return, do not crash |
| Action is `closed` or `merged` | Skip — nothing to review |
| Ping event (GitHub sends on App creation) | Return 200, log "ping received" |

### Pull Request Layer
| Case | Handling |
|------|----------|
| Draft PR | Skip by default. Make configurable in `.ai-reviewer.yml` |
| Bot-authored PR | Check `sender.type == "Bot"` — skip to avoid infinite loops |
| PR from a fork | GitHub restricts write permissions to fork PRs for security. Check if `pull_request.head.repo.full_name != repository.full_name` — skip or post limited review |
| PR with 0 files changed | Edge case — skip gracefully |
| PR with 100+ files | Enforce `MAX_FILES_PER_PR` limit — post a comment explaining partial review |
| PR already reviewed by bot | On `synchronize`, a new review is posted. Old reviews remain — this is expected behavior |
| PR closed between webhook and review post | GitHub API will return 404 — catch and log, not an error |

### Diff & File Layer
| Case | Handling |
|------|----------|
| Binary file (image, font, etc.) | `Patch` field is empty — skip |
| Deleted file | `Status == "removed"` — skip, no new lines to review |
| Renamed file with no content change | `Patch` may be empty — skip |
| File with only whitespace changes | Parse finds no added lines with real content — skip AI call |
| Very large file diff (10k+ lines) | Truncate patch to `MAX_PATCH_CHARS`, note truncation in prompt |
| `\\ No newline at end of file` in diff | Strip this line before parsing — it is not code |
| Generated file (proto, graphql schema gen) | Check for common generation headers (`// Code generated`) — skip |
| Hunk header missing count: `@@ -1 +1 @@` | Treat missing count as 1 — handle regex gracefully |
| Multiple hunks in one file patch | Parser must correctly reset line counter at each `@@` marker |

### AI Layer
| Case | Handling |
|------|----------|
| Claude returns markdown-wrapped JSON | Strip `\`\`\`json` and `\`\`\`` before unmarshaling |
| Claude returns empty array `[]` | Valid — no comments to post |
| Claude returns plain text explanation | JSON unmarshal fails — log raw response, return empty |
| Claude returns comment with `line: 0` | Discard — hallucinated line number |
| Claude returns line number beyond file length | Validate all line numbers against `parsedFile.Lines` before posting |
| Claude returns duplicate comments on same line | Deduplicate by `(filename, line)` before posting |
| Claude API timeout | Retry once after 2 seconds. If second fails, skip this file, log error |
| Claude API rate limit (429) | Respect `Retry-After` header. Retry with backoff up to 3 times |
| Claude API returns 500 | Retry once. Log if second failure. Do not crash the whole PR review |
| Very short file (1-2 lines) | Still valid — send to Claude normally |
| File with only test code | Claude will still review — let it. Test code has bugs too |
| Claude invents issues that don't exist | Prompt engineering mitigation — instruct to only flag real, confident issues |

### GitHub API Layer
| Case | Handling |
|------|----------|
| `CreateReview` returns 422 (invalid line) | One or more line numbers don't match the diff. Re-validate before posting. Filter out invalid lines and retry |
| GitHub API rate limit (5000 req/hr) | Check `X-RateLimit-Remaining` header. Back off if under 100. Log warning |
| Installation token expired | `ghinstallation` library auto-refreshes — handled transparently |
| Repo deleted between webhook and API call | 404 response — log and move on |
| GitHub is down or times out | Retry with exponential backoff (1s, 2s, 4s) up to 3 attempts |
| Large PR with pagination (100+ files) | Always paginate `ListFiles` until `NextPage == 0` |
| Commit SHA changed between webhook and post | Use SHA from webhook payload (`pull_request.head.sha`) — don't re-fetch |

### System Layer
| Case | Handling |
|------|----------|
| Worker queue full (burst of PRs) | Drop with log warning — non-blocking submission. Consider increasing buffer |
| Context timeout during long review | 5-minute timeout per PR. Partial reviews are acceptable |
| App crashes mid-review | Stateless design — webhook will be retried by GitHub (up to 3 retries). Idempotency key prevents double-review |
| Private key file not found at startup | Fail fast with clear error — do not start the server |
| Multiple instances deployed | Each instance processes independently. GitHub deduplication on delivery ID prevents double-posting |
| Memory leak from token cache | Bound cache size — LRU eviction if installations exceed 1000 |

---

## 9. Configuration File (.ai-reviewer.yml)

Repos can customize bot behavior by adding this file to their root.

```yaml
# .ai-reviewer.yml — place in root of your repository

# Skip draft PRs (default: true)
skip_drafts: true

# File patterns to ignore (glob syntax)
ignore_paths:
  - "migrations/**"
  - "**/*.generated.go"
  - "docs/**"
  - "scripts/**"

# Only review these severities (default: all)
min_severity: "warning"  # error | warning | suggestion

# Maximum files to review per PR (default: 50)
max_files: 30

# Review behavior when PR is updated
on_synchronize: true  # re-review on new commits (default: true)

# Post approve when no issues found (default: true)
approve_on_clean: true

# Categories to check (default: all)
categories:
  - bug
  - security
  - performance
  # - code-smell    # uncomment to enable
  # - best-practice # uncomment to enable
```

**How to load it:**

```go
func LoadRepoConfig(ctx context.Context, client *github.Client, owner, repo string) (*RepoConfig, error) {
    fileContent, _, _, err := client.Repositories.GetContents(
        ctx, owner, repo, ".ai-reviewer.yml",
        &github.RepositoryContentGetOptions{},
    )
    if err != nil {
        // File doesn't exist — use defaults. Not an error.
        return DefaultRepoConfig(), nil
    }

    content, err := fileContent.GetContent()
    if err != nil {
        return DefaultRepoConfig(), nil
    }

    var cfg RepoConfig
    if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
        return DefaultRepoConfig(), nil  // malformed yaml — fall back to defaults
    }

    return &cfg, nil
}
```

---

## 10. Error Handling Strategy

- **Fail fast at startup**: missing env vars, unreadable private key — crash with clear message
- **Never crash at runtime**: all errors inside webhook handler and worker goroutines are caught and logged
- **Log with context**: always include `repo`, `pr_number`, `file` in log fields
- **Partial success is acceptable**: if 3 of 5 files are reviewed and 2 fail, post comments for the 3. Don't abandon the whole review
- **Never expose internals to GitHub**: errors are logged server-side, never exposed in PR comments
- **Retry policy**: 3 attempts with exponential backoff (1s, 2s, 4s) for transient errors (network timeout, 5xx). Do not retry 4xx except 429

```go
func retry(attempts int, fn func() error) error {
    var err error
    for i := 0; i < attempts; i++ {
        err = fn()
        if err == nil {
            return nil
        }
        if i < attempts-1 {
            time.Sleep(time.Duration(1<<i) * time.Second) // 1s, 2s, 4s
        }
    }
    return err
}
```

---

## 11. Rate Limiting Strategy

### GitHub API
- Default: 5000 requests/hour per installation
- Check `X-RateLimit-Remaining` on every response
- If remaining < 100: log warning, add 1s delay between requests
- If remaining == 0: sleep until `X-RateLimit-Reset` timestamp

### Claude API
- Default tier: varies by plan
- On 429: read `Retry-After` header, sleep that duration, retry
- Per-file analysis: space out calls with a 100ms gap between files to avoid burst

### Self rate limiting for large PRs
- Process files sequentially, not concurrently per PR
- This naturally limits Claude API burst usage
- Concurrency comes from the worker pool (multiple PRs, not multiple files per PR)

---

## 12. Deployment

### Local Development

```bash
# Install dependencies
go mod tidy

# Set up ngrok for webhook tunnel
ngrok http 3000
# Copy https URL to GitHub App webhook settings

# Set env vars
cp .env.example .env
# Fill in GITHUB_APP_ID, key path, webhook secret, Anthropic key

# Run
go run cmd/server/main.go
```

### Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o reviewer ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/reviewer .
EXPOSE 3000
CMD ["./reviewer"]
```

### Railway / Render Deployment

- Set all env vars in the platform dashboard
- For `GITHUB_APP_PRIVATE_KEY_CONTENTS`: paste the full `.pem` file content as a single env var (replace newlines with `\n`)
- Set `PORT` to whatever the platform expects (Railway: auto-set, Render: 3000)
- Point GitHub App webhook URL to your deployment URL + `/webhook`

---

## 13. Testing Strategy

### Unit Tests

| Component | What to Test |
|-----------|-------------|
| `VerifySignature` | valid sig, invalid sig, missing prefix, empty secret |
| `ParsePatch` | single hunk, multi-hunk, only deletions, empty patch, `\\ No newline` |
| `ShouldSkip` | lock files, binary extensions, vendor paths, custom ignore patterns |
| `validateComments` | zero line, empty comment, invalid severity defaults |
| `BuildPrompt` | truncation note present when truncated, language hint correct |

### Integration Tests

- Spin up a mock HTTP server that returns canned GitHub API responses
- Feed it a known PR diff and assert the correct comments are generated
- Test the full flow: webhook → parse → filter → AI → post

### Manual Testing Flow

1. Open a PR in your test repo with a deliberately broken function (SQL injection, unchecked error, etc.)
2. Check the webhook logs — confirm the event was received
3. Check the PR — bot should have posted an inline comment on the bad line
4. Push a fix commit — bot should re-review and either approve or find remaining issues

---

## 14. Flow Diagram

```
GitHub PR Event
       │
       ▼
POST /webhook
       │
       ├── Verify HMAC signature
       ├── Check event type (pull_request only)
       ├── Check action (opened/synchronize/reopened only)
       ├── Respond 200 immediately
       └── Submit to worker pool
                  │
                  ▼
          Worker Goroutine
                  │
                  ├── Get authenticated GitHub client (from cache)
                  ├── Load .ai-reviewer.yml (or defaults)
                  ├── Check: draft PR? → skip
                  ├── Check: bot author? → skip
                  ├── Fetch PR files (paginated)
                  │
                  └── For each file:
                        ├── ShouldSkip? → skip
                        ├── Empty patch? → skip
                        ├── ParsePatch → []ParsedLine with line numbers
                        ├── Call Claude API with diff
                        ├── Parse + validate JSON response
                        └── Collect FileReview
                  │
                  ▼
          Validate all line numbers against parsed lines
                  │
                  ▼
          Post single GitHub Review with all comments
                  │
                  ▼
          Log result (issues found, files reviewed, duration)
```

---

## Quick Start Checklist

- [ ] Create GitHub App with correct permissions
- [ ] Download private key `.pem`
- [ ] Set all env vars in `.env`
- [ ] Start ngrok, update webhook URL in GitHub App settings
- [ ] Run server: `go run cmd/server/main.go`
- [ ] Open a test PR — watch logs for incoming webhook
- [ ] Verify inline comments appear on the PR
- [ ] Deploy to Railway/Render
- [ ] Update webhook URL in GitHub App to production URL
- [ ] Install App on target repositories
