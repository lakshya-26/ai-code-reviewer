package reviewer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ai-code-reviewer/ai-code-reviewer/internal/ai"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/cache"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/config"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/filter"
	githubmodels "github.com/ai-code-reviewer/ai-code-reviewer/internal/github"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/parser"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/storage"
)

// interFileDelay spaces out AI calls to avoid bursting the provider's rate limit.
const interFileDelay = 100 * time.Millisecond

// Store is the subset of storage.Store used by the Reviewer.
// nil is valid — when nil, per-installation config and usage tracking are disabled
// and all reviews use the server-default provider.
type Store interface {
	GetOrCreate(ctx context.Context, installationID int64, accountLogin string) (*storage.Installation, error)
	IncrementUsage(ctx context.Context, installationID int64) error
	DecryptAPIKey(inst *storage.Installation) (string, error)
}

// Reviewer orchestrates the full review pipeline for one pull request.
// It is the single component that knows about all other packages — all others
// are isolated and unaware of each other.
type Reviewer struct {
	cache           *cache.ClientCache
	defaultProvider ai.Provider
	cfg             *config.Config
	store           Store // nil when DATABASE_URL is not configured
}

// New creates a Reviewer. store may be nil (disables per-installation config).
func New(clientCache *cache.ClientCache, provider ai.Provider, cfg *config.Config, store Store) *Reviewer {
	return &Reviewer{
		cache:           clientCache,
		defaultProvider: provider,
		cfg:             cfg,
		store:           store,
	}
}

// Review runs the full pipeline for one pull_request webhook event.
// It is designed to be called from a worker goroutine and must never panic.
//
// Pipeline:
//  1. Validate event — skip drafts, bots, fork PRs
//  2. Get authenticated GitHub client
//  3. Load per-repo config (.ai-reviewer.yml or defaults)
//  4. Fetch PR file list (paginated)
//  5. Filter and cap the file list
//  6. For each file: parse diff → call AI → validate comments
//  7. Post a single GitHub review with all inline comments
func (r *Reviewer) Review(ctx context.Context, event githubmodels.PullRequestEvent) error {
	pr := event.PullRequest
	repo := event.Repository
	owner := repo.Owner.Login
	repoName := repo.Name
	prNumber := pr.Number
	commitSHA := pr.Head.SHA

	log := slog.With(
		"repo", repo.FullName,
		"pr", prNumber,
	)

	// ── 1. Early-exit checks ──────────────────────────────────────────────────

	// Skip draft PRs by default (config can override per repo).
	if pr.Draft {
		log.Info("skipping draft PR")
		return nil
	}

	// Skip bot-authored PRs — prevents infinite review loops.
	if strings.EqualFold(event.Sender.Type, "Bot") {
		log.Info("skipping bot-authored PR", "sender", event.Sender.Login)
		return nil
	}

	// Skip fork PRs — GitHub restricts write permissions for cross-repo PRs.
	// A fork PR's head repo differs from the base repo.
	if pr.Head.Repo.FullName != "" && pr.Head.Repo.FullName != repo.FullName {
		log.Info("skipping fork PR — write permissions not available",
			"head_repo", pr.Head.Repo.FullName,
			"base_repo", repo.FullName,
		)
		return nil
	}

	// ── 2. GitHub client ──────────────────────────────────────────────────────

	client, err := r.cache.Get(event.Installation.ID)
	if err != nil {
		return fmt.Errorf("getting github client for installation %d: %w", event.Installation.ID, err)
	}

	// ── 2b. Per-installation AI provider ─────────────────────────────────────

	// Pick the AI provider for this installation.
	// With a database: check for a user-configured key; enforce the free limit.
	// Without a database: always use the server default.
	provider, limitReached, err := r.resolveProvider(ctx, event, log)
	if err != nil {
		return fmt.Errorf("resolving AI provider: %w", err)
	}
	if limitReached {
		// Post a friendly "limit reached" comment on the PR and stop.
		msg := fmt.Sprintf(
			"## DiffSense AI — Free Review Limit Reached\n\n"+
				"This installation has used all **%d free reviews**.\n\n"+
				"To continue receiving AI code reviews, please add your own API key:\n\n"+
				"👉 **[Configure your API key](https://diffsense-ai.up.railway.app/setup?installation_id=%d)**\n\n"+
				"Supported providers: Groq (free tier), OpenAI, Anthropic Claude, Google Gemini, xAI Grok.",
			r.cfg.FreeReviewsLimit, event.Installation.ID,
		)
		return githubmodels.PostIssueComment(ctx, client, owner, repoName, prNumber, msg)
	}

	// ── 3. Per-repo config ────────────────────────────────────────────────────

	repoCfg := filter.LoadRepoConfig(ctx, client, owner, repoName)

	// Honour the repo-level skip_drafts setting (overrides the global check above
	// only when the PR is NOT a draft; draft check already exited).
	if !repoCfg.OnSynchronize && event.Action == "synchronize" {
		log.Info("skipping synchronize event — disabled in repo config")
		return nil
	}

	// ── 4. Fetch PR files ─────────────────────────────────────────────────────

	prFiles, err := githubmodels.FetchPRFiles(ctx, client, owner, repoName, prNumber)
	if err != nil {
		return fmt.Errorf("fetching PR files: %w", err)
	}

	if len(prFiles) == 0 {
		log.Info("PR has no changed files — skipping")
		return nil
	}

	// ── 5. Filter files ───────────────────────────────────────────────────────

	var reviewableFiles []githubmodels.PRFile
	for _, f := range prFiles {
		if !githubmodels.IsReviewable(f) {
			continue
		}
		if filter.ShouldSkip(f.Filename, f.Patch, repoCfg) {
			log.Debug("skipping file", "file", f.Filename)
			continue
		}
		reviewableFiles = append(reviewableFiles, f)
	}

	maxFiles := repoCfg.MaxFiles
	if maxFiles <= 0 {
		maxFiles = r.cfg.MaxFilesPerPR
	}

	truncatedFiles := false
	if len(reviewableFiles) > maxFiles {
		log.Warn("PR exceeds max files limit — reviewing first N files only",
			"total", len(reviewableFiles),
			"limit", maxFiles,
		)
		reviewableFiles = reviewableFiles[:maxFiles]
		truncatedFiles = true
	}

	if len(reviewableFiles) == 0 {
		log.Info("no reviewable files after filtering")
		// Still post an approve/comment so the PR isn't left hanging.
		return githubmodels.PostReview(ctx, client, owner, repoName, prNumber, commitSHA,
			nil, repoCfg.ApproveOnClean)
	}

	log.Info("reviewing files",
		"reviewable", len(reviewableFiles),
		"total_changed", len(prFiles),
	)

	// ── 6. Analyse each file ──────────────────────────────────────────────────

	// Build PR context once — passed to every file analysis so the model
	// understands the intent of the change.
	prCtx := ai.PRContext{
		Title:       pr.Title,
		Description: pr.Body,
		Number:      prNumber,
		RepoName:    repo.FullName,
	}

	var fileReviews []ai.FileReview

	for i, f := range reviewableFiles {
		if ctx.Err() != nil {
			log.Warn("context cancelled — stopping mid-review", "files_done", i)
			break
		}

		fileLog := log.With("file", f.Filename)

		// Parse the diff to extract valid line numbers.
		parsed := parser.ParsePatch(f.Filename, f.Patch)
		if len(parsed.Lines) == 0 {
			fileLog.Debug("no added lines after parsing — skipping AI call")
			continue
		}

		fileLog.Debug("parsed diff", "added_lines", len(parsed.Lines))

		// Truncate patch before sending to AI if it exceeds the limit.
		patch, wasTruncated := ai.TruncatePatch(f.Patch, r.cfg.MaxPatchChars)
		if wasTruncated {
			fileLog.Debug("patch truncated", "original_len", len(f.Patch), "truncated_len", len(patch))
		}

		// Call the AI provider — pass PR context for intent-aware review.
		comments, err := provider.AnalyzeFile(ctx, f.Filename, patch, prCtx)
		if err != nil {
			// Partial success: log and continue with remaining files.
			fileLog.Error("AI analysis failed — skipping file", "err", err)
			continue
		}

		// Validate line numbers against the parsed diff and deduplicate.
		comments = ai.ApplyFilters(comments, parsed.LineNumbers, f.Filename)

		// Apply repo-level severity and category filters.
		comments = applyCategoryAndSeverityFilters(comments, repoCfg)

		if len(comments) > 0 {
			fileReviews = append(fileReviews, ai.FileReview{
				Filename: f.Filename,
				Comments: comments,
			})
			fileLog.Info("file reviewed", "issues_found", len(comments))
		} else {
			fileLog.Debug("no issues found")
		}

		// Rate-limiting courtesy delay between AI calls.
		if i < len(reviewableFiles)-1 {
			select {
			case <-time.After(interFileDelay):
			case <-ctx.Done():
			}
		}
	}

	// ── 7. Post the review ────────────────────────────────────────────────────

	totalIssues := ai.TotalComments(fileReviews)
	log.Info("posting review",
		"files_reviewed", len(reviewableFiles),
		"files_with_issues", len(fileReviews),
		"total_issues", totalIssues,
		"files_truncated", truncatedFiles,
	)

	// Use a fresh context for posting so a cancelled AI context doesn't also
	// prevent the review from being posted (we still want partial results).
	postCtx := context.Background()

	if err := githubmodels.PostReview(
		postCtx, client, owner, repoName, prNumber, commitSHA,
		fileReviews, repoCfg.ApproveOnClean,
	); err != nil {
		return fmt.Errorf("posting review: %w", err)
	}

	return nil
}

// resolveProvider picks the right AI provider for this review.
//
// Logic:
//  1. If no DB store is configured, use the server default.
//  2. Look up (or create) the installation record.
//  3. If the installation has its own API key, build a provider from it.
//  4. Otherwise check the free limit — if exceeded, return limitReached=true.
//  5. If within limit, increment usage and use the server default.
func (r *Reviewer) resolveProvider(
	ctx context.Context,
	event githubmodels.PullRequestEvent,
	log *slog.Logger,
) (provider ai.Provider, limitReached bool, err error) {
	if r.store == nil {
		return r.defaultProvider, false, nil
	}

	accountLogin := event.Repository.Owner.Login
	inst, err := r.store.GetOrCreate(ctx, event.Installation.ID, accountLogin)
	if err != nil {
		// Non-fatal: fall back to server default rather than blocking the review.
		log.Warn("could not load installation config — using server default", "err", err)
		return r.defaultProvider, false, nil
	}

	if inst.HasCustomKey() {
		apiKey, err := r.store.DecryptAPIKey(inst)
		if err != nil {
			log.Warn("could not decrypt installation API key — using server default", "err", err)
			return r.defaultProvider, false, nil
		}
		p, err := ai.NewProviderFromInstallation(inst.Provider, apiKey, inst.Model, r.cfg)
		if err != nil {
			log.Warn("invalid installation provider config — using server default",
				"provider", inst.Provider, "err", err)
			return r.defaultProvider, false, nil
		}
		log.Info("using installation API key", "provider", inst.Provider)
		return p, false, nil
	}

	// No custom key — check free tier.
	if inst.IsOverFreeLimit() {
		log.Warn("installation free review limit reached",
			"used", inst.FreeReviewsUsed, "limit", inst.FreeReviewsLimit)
		return nil, true, nil
	}

	// Within free tier — count this review and use server default.
	if err := r.store.IncrementUsage(ctx, event.Installation.ID); err != nil {
		log.Warn("could not increment usage counter", "err", err)
	}
	log.Info("using server default provider (free tier)",
		"provider", r.defaultProvider.Name(),
		"used", inst.FreeReviewsUsed+1, "limit", inst.FreeReviewsLimit)
	return r.defaultProvider, false, nil
}

// applyCategoryAndSeverityFilters removes comments that fall below the repo's
// minimum severity or belong to a disabled category.
func applyCategoryAndSeverityFilters(comments []ai.ReviewComment, repoCfg *filter.RepoConfig) []ai.ReviewComment {
	result := make([]ai.ReviewComment, 0, len(comments))
	for _, c := range comments {
		if !filter.MeetsSeverityThreshold(c.Severity, repoCfg.MinSeverity) {
			continue
		}
		if !filter.IsCategoryEnabled(c.Category, repoCfg) {
			continue
		}
		result = append(result, c)
	}
	return result
}
