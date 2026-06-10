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
)

// interFileDelay spaces out AI calls to avoid bursting the provider's rate limit.
const interFileDelay = 100 * time.Millisecond

// Reviewer orchestrates the full review pipeline for one pull request.
// It is the single component that knows about all other packages — all others
// are isolated and unaware of each other.
type Reviewer struct {
	cache    *cache.ClientCache
	provider ai.Provider
	cfg      *config.Config
}

// New creates a Reviewer. All three dependencies are required.
func New(clientCache *cache.ClientCache, provider ai.Provider, cfg *config.Config) *Reviewer {
	return &Reviewer{
		cache:    clientCache,
		provider: provider,
		cfg:      cfg,
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
		"provider", r.provider.Name(),
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

		// Call the AI provider.
		comments, err := r.provider.AnalyzeFile(ctx, f.Filename, patch)
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

	if err := githubmodels.PostReview(
		ctx, client, owner, repoName, prNumber, commitSHA,
		fileReviews, repoCfg.ApproveOnClean,
	); err != nil {
		return fmt.Errorf("posting review: %w", err)
	}

	return nil
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
