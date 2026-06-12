# DiffSense AI

> AI-powered code reviewer that automatically reviews your Pull Requests and posts inline comments with bugs, security issues, performance problems, and best practice violations.

Built with Go · Powered by [Groq](https://groq.com) (default) · Supports OpenAI, Claude, Gemini, Grok, and local LLMs

---

## 🔍 See It In Action

This repo uses DiffSense AI to review its own PRs. Check out real AI reviews starting from **[PR #10](https://github.com/lakshya-26/ai-code-reviewer/pulls?q=is%3Apr+is%3Aclosed)** — every PR has inline comments posted automatically by the bot.

---

## ✨ Features

- **Inline PR comments** — issues posted directly on the exact line of code
- **Multi-provider AI** — Groq (free, fast), OpenAI, Anthropic Claude, Google Gemini, xAI Grok, or local LLM
- **Per-installation API keys** — each user can bring their own key via `/setup` page
- **100 free reviews** per installation using the shared Groq backend
- **Smart filtering** — skips generated files, vendor dirs, binaries, lock files
- **Severity levels** — 🔴 Error, 🟡 Warning, 💡 Suggestion
- **Categories** — Bug, Security, Performance, Code Smell, Best Practice
- **Per-repo config** — customize behavior via `.ai-reviewer.yml`
- **Chain-of-thought prompting** — structured reasoning for better accuracy

---

## 🚀 Try It Live

### Install the GitHub App

1. Go to **[github.com/apps/diffsense-ai](https://github.com/apps/diffsense-ai)**
2. Click **Install** → select your repo(s)
3. Open a Pull Request — the bot will automatically review it within seconds

### Configure Your Own API Key (Optional)

By default your first **100 reviews are free** using the shared Groq backend.

To use your own key (unlimited reviews):

1. Visit: `https://diffsense.up.railway.app/setup?installation_id=YOUR_INSTALLATION_ID`
2. Choose your provider (Groq, OpenAI, Claude, Gemini, or Grok)
3. Enter your API key → Save

> Your installation ID appears in the Railway logs on the first webhook, or in the GitHub App installation URL.

### Per-Repo Configuration

Add `.ai-reviewer.yml` to your repo root to customize behavior:

```yaml
skip_drafts: true
on_synchronize: true
approve_on_clean: true
max_files: 30
min_severity: warning   # error | warning | suggestion
categories:
  - bug
  - security
  - performance
  - code-smell
  - best-practice
ignore_paths:
  - "vendor/**"
  - "*.generated.go"
  - "dist/**"
```

---

## 🛠️ Run Locally

### Prerequisites

- Go 1.21+
- Docker + Docker Compose
- A GitHub App (see setup below)
- `ngrok` for exposing local webhook

### 1. Clone & Configure

```bash
git clone https://github.com/lakshya-26/ai-code-reviewer
cd ai-code-reviewer
cp .env.example .env
```

Edit `.env` with your GitHub App credentials.

### 2. Create a GitHub App

1. Go to [github.com/settings/apps/new](https://github.com/settings/apps/new)
2. Set **Webhook URL** to your ngrok URL (step 5): `https://xxxx.ngrok.io/webhook`
3. Enable permissions: `Pull requests` (Read & Write), `Contents` (Read)
4. Subscribe to events: `Pull request`
5. Generate and download the private key `.pem` file

### 3. Choose Your AI Provider

**Option A — Groq (recommended, free tier)**

```bash
# .env
AI_PROVIDER=groq
GROQ_API_KEY=gsk_xxxx   # get free at console.groq.com
GROQ_MODEL=llama-3.3-70b-versatile
```

**Option B — Local LLM (llama.cpp, no API key needed)**

```bash
# Install llama.cpp
brew install llama.cpp

# Download model
make download-model

# .env
AI_PROVIDER=local
LOCAL_LLM_URL=http://localhost:8080
LOCAL_LLM_MODEL=qwen2.5-coder
```

**Option C — OpenAI / Claude / Gemini / Grok**

```bash
# .env
AI_PROVIDER=openai       # or claude | gemini | grok
OPENAI_API_KEY=sk-...
```

### 4. Start the App

**With Groq (simplest):**

```bash
make up        # starts Go app + Postgres in Docker
make ngrok     # exposes port 3000 to GitHub
```

**With local LLM:**

```bash
make llm       # terminal 1 — starts llama.cpp on port 8080
make up        # terminal 2 — starts Go app in Docker
make ngrok     # terminal 3 — exposes to GitHub
```

### 5. Test It

Open a Pull Request on any repo where your GitHub App is installed. You should see:

```
pull request event received
review started
review completed   elapsed=Xs
```

in the Docker logs, and inline comments on your PR within seconds (Groq) or ~1 minute (local LLM).

---

## 🏗️ Architecture

```
GitHub PR opened/updated
        │
        ▼
   Webhook Handler  ──── HMAC signature verification
        │
        ▼
   Worker Pool (5 goroutines)
        │
        ▼
   Reviewer
   ├── Load per-repo config (.ai-reviewer.yml)
   ├── Fetch changed files (GitHub API)
   ├── Filter files (vendor, generated, binary...)
   ├── Resolve AI provider (per-installation key or server default)
   │
   └── For each file:
       ├── Parse unified diff → extract added lines
       ├── Build prompt (system + user + few-shot examples + PR context)
       ├── Call AI provider (Groq / OpenAI / Claude / Gemini / local)
       ├── Parse JSON response
       └── Validate line numbers against diff
        │
        ▼
   Post single GitHub Review (inline comments + summary)
```

### Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go (standard library `net/http`) |
| GitHub API | `go-github/v62` + `ghinstallation/v2` |
| Diff parsing | `sourcegraph/go-diff` |
| Config | `spf13/viper` + `gopkg.in/yaml.v3` |
| Default AI | Groq (`llama-3.3-70b-versatile`) |
| Database | PostgreSQL (per-installation config) |
| Deployment | Railway |

---

## 🚢 Deploy Your Own Instance

### Railway (recommended)

1. Fork this repo
2. Create a new project at [railway.app](https://railway.app)
3. Connect your forked repo
4. Add a **PostgreSQL** service
5. Set environment variables:

```
GITHUB_APP_ID                   = your app ID
GITHUB_WEBHOOK_SECRET           = your webhook secret
GITHUB_APP_PRIVATE_KEY_CONTENTS = (paste .pem content as single line with \n)
AI_PROVIDER                     = groq
GROQ_API_KEY                    = gsk_xxxx
ENCRYPTION_KEY                  = (run: openssl rand -base64 32)
FREE_REVIEWS_LIMIT              = 100
```

6. Set networking port to `3000`
7. Update your GitHub App's Webhook URL to `https://your-app.up.railway.app/webhook`

---

## 📁 Project Structure

```
.
├── cmd/server/main.go          # Entry point
├── internal/
│   ├── ai/                     # AI providers (Groq, OpenAI, Claude, Gemini, Grok, local)
│   │   ├── provider.go         # Provider interface + factory
│   │   ├── prompt.go           # Prompt builder (system/user/few-shot)
│   │   ├── openai_compat.go    # OpenAI-compatible (Groq, OpenAI, Grok, local)
│   │   ├── claude.go           # Anthropic Claude
│   │   ├── gemini.go           # Google Gemini
│   │   └── validator.go        # Comment validation + deduplication
│   ├── github/                 # GitHub API interactions
│   │   ├── app.go              # App authentication (JWT + installation tokens)
│   │   ├── webhook.go          # Webhook handler + HMAC verification
│   │   ├── diff.go             # Fetch PR files
│   │   └── review.go          # Post PR reviews
│   ├── reviewer/reviewer.go    # Main orchestrator
│   ├── worker/pool.go          # Goroutine worker pool
│   ├── parser/diff.go          # Unified diff parser
│   ├── filter/                 # File + config filtering
│   ├── storage/                # PostgreSQL (per-installation config)
│   ├── web/setup.go            # /setup settings page
│   └── cache/token.go          # Installation client cache
├── config/default.yml          # Default reviewer settings
├── Dockerfile                  # Multi-stage build
├── docker-compose.yml          # Local dev stack (app + Postgres)
├── railway.toml                # Railway deployment config
└── Makefile                    # Dev shortcuts
```

---

## 📄 License

MIT
