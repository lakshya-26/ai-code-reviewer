package github

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/go-github/v62/github"

	"github.com/ai-code-reviewer/ai-code-reviewer/internal/ai"
)

const (
	// maxCommentBodyBytes is GitHub's hard limit for any single comment body.
	maxCommentBodyBytes = 65_536

	// maxReviewBodyBytes is the limit for the top-level review summary body.
	maxReviewBodyBytes = 65_536

	// severityEmoji maps severity levels to emoji for the summary table.
	emojiError      = "🔴"
	emojiWarning    = "🟡"
	emojiSuggestion = "💡"

	reviewDisclaimer = "DiffSense is a first pass. Ignore style nits; verify every bug/security flag."
)

// PostReview submits a single GitHub Pull Request Review containing all inline
// comments produced by the AI. It posts one review (not N individual comments)
// so the PR timeline stays clean.
//
// If no issues are found, it posts an APPROVE review.
// If issues are found, it posts a COMMENT review with inline annotations.
//
// On a GitHub 422 (invalid line number) the function retries once without
// inline comments to ensure the summary is always posted.
func PostReview(
	ctx context.Context,
	client *github.Client,
	owner, repo string,
	prNumber int,
	commitSHA string,
	fileReviews []ai.FileReview,
	approveOnClean bool,
) error {
	comments := buildDraftComments(fileReviews)
	totalIssues := ai.TotalComments(fileReviews)
	filesReviewed := countFilesWithComments(fileReviews)

	var event, body string
	if totalIssues == 0 {
		if approveOnClean {
			event = "APPROVE"
		} else {
			event = "COMMENT"
		}
		body = cleanReviewBody()
	} else {
		event = "COMMENT"
		body = buildSummaryBody(totalIssues, filesReviewed, fileReviews)
	}

	review := &github.PullRequestReviewRequest{
		CommitID: github.String(commitSHA),
		Body:     github.String(truncateString(body, maxReviewBodyBytes)),
		Event:    github.String(event),
		Comments: comments,
	}

	_, resp, err := client.PullRequests.CreateReview(ctx, owner, repo, prNumber, review)
	if err != nil {
		CheckRateLimit(resp)

		// 422 means one or more line numbers are invalid in GitHub's view.
		// Retry without inline comments so the summary is always posted.
		if IsUnprocessable(err) {
			slog.Warn("GitHub returned 422 on review with inline comments — retrying summary-only",
				"repo", owner+"/"+repo,
				"pr", prNumber,
				"comments_dropped", len(comments),
			)
			return postSummaryOnly(ctx, client, owner, repo, prNumber, commitSHA, body, event)
		}

		return fmt.Errorf("creating PR review: %w", err)
	}

	CheckRateLimit(resp)
	slog.Info("review posted",
		"repo", owner+"/"+repo,
		"pr", prNumber,
		"event", event,
		"inline_comments", len(comments),
		"total_issues", totalIssues,
	)
	return nil
}

// postSummaryOnly posts a review with only the summary body and no inline
// comments. Used as a fallback when inline comments cause a 422.
func postSummaryOnly(
	ctx context.Context,
	client *github.Client,
	owner, repo string,
	prNumber int,
	commitSHA, body, event string,
) error {
	review := &github.PullRequestReviewRequest{
		CommitID: github.String(commitSHA),
		Body:     github.String(truncateString(body, maxReviewBodyBytes)),
		Event:    github.String(event),
	}
	_, _, err := client.PullRequests.CreateReview(ctx, owner, repo, prNumber, review)
	if err != nil {
		return fmt.Errorf("posting summary-only review: %w", err)
	}
	return nil
}

// ─── Comment formatting ───────────────────────────────────────────────────────

// buildDraftComments converts FileReview objects into GitHub draft review comments.
func buildDraftComments(fileReviews []ai.FileReview) []*github.DraftReviewComment {
	var comments []*github.DraftReviewComment
	for _, fr := range fileReviews {
		for _, c := range fr.Comments {
			body := formatCommentBody(c, fr.Filename)
			comments = append(comments, &github.DraftReviewComment{
				Path: github.String(fr.Filename),
				Line: github.Int(c.Line),
				// Side: "RIGHT" is the default (new file) — no need to set explicitly.
				Body: github.String(body),
			})
		}
	}
	return comments
}

// formatCommentBody renders one ReviewComment into a GitHub markdown comment.
//
// Example output:
//
//	**[ERROR]** `bug`
//
//	nil dereference before the pointer check — add a nil guard before line 12.
func formatCommentBody(c ai.ReviewComment, filename string) string {
	severityTag := strings.ToUpper(c.Severity)
	body := fmt.Sprintf("**[%s]** `%s`\n\n%s", severityTag, c.Category, c.Comment)
	if strings.TrimSpace(c.Fix) != "" {
		lang := ai.FenceLanguage(filename)
		body += fmt.Sprintf("\n\n```%s\n%s\n```", lang, strings.TrimSpace(c.Fix))
	}
	body += reviewFooter()
	return truncateString(body, maxCommentBodyBytes)
}

// ─── Summary body ─────────────────────────────────────────────────────────────

// buildSummaryBody produces the top-level review body shown in the PR timeline.
func buildSummaryBody(totalIssues, filesReviewed int, reviews []ai.FileReview) string {
	errorCount := ai.CountBySeverity(reviews, "error")
	warningCount := ai.CountBySeverity(reviews, "warning")
	suggestionCount := ai.CountBySeverity(reviews, "suggestion")

	fileWord := "file"
	if filesReviewed != 1 {
		fileWord = "files"
	}
	issueWord := "issue"
	if totalIssues != 1 {
		issueWord = "issues"
	}

	var sb strings.Builder
	sb.WriteString("🤖 **DiffSense AI Code Review**\n\n")
	fmt.Fprintf(&sb, "Found **%d %s** across **%d %s**.\n\n", totalIssues, issueWord, filesReviewed, fileWord)

	sb.WriteString("| Severity | Count |\n")
	sb.WriteString("|----------|-------|\n")
	fmt.Fprintf(&sb, "| %s Error | %d |\n", emojiError, errorCount)
	fmt.Fprintf(&sb, "| %s Warning | %d |\n", emojiWarning, warningCount)
	fmt.Fprintf(&sb, "| %s Suggestion | %d |\n", emojiSuggestion, suggestionCount)

	// File breakdown — one line per file with issues.
	if len(reviews) > 0 {
		sb.WriteString("\n**Files reviewed:**\n")
		for _, fr := range reviews {
			if len(fr.Comments) > 0 {
				fmt.Fprintf(&sb, "- `%s` — %d issue(s)\n", fr.Filename, len(fr.Comments))
			}
		}
	}

	sb.WriteString(reviewFooter())

	return sb.String()
}

func cleanReviewBody() string {
	return "✅ **DiffSense AI Code Review**: No issues found. Looks good to merge." + reviewFooter()
}

func reviewFooter() string {
	return "\n\n<sub>Powered by DiffSense AI</sub>\n<sub>" + reviewDisclaimer + "</sub>"
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// countFilesWithComments counts how many FileReviews have at least one comment.
func countFilesWithComments(reviews []ai.FileReview) int {
	count := 0
	for _, r := range reviews {
		if len(r.Comments) > 0 {
			count++
		}
	}
	return count
}

// PostIssueComment posts a plain (non-review) comment on a pull request.
// Used for system messages like "free limit reached".
func PostIssueComment(ctx context.Context, client *github.Client, owner, repo string, prNumber int, body string) error {
	_, _, err := client.Issues.CreateComment(ctx, owner, repo, prNumber, &github.IssueComment{
		Body: github.String(truncateString(body+reviewFooter(), maxCommentBodyBytes)),
	})
	if err != nil {
		return fmt.Errorf("posting issue comment: %w", err)
	}
	return nil
}

// truncateString shortens s to at most maxBytes bytes (not runes), cutting on a
// valid UTF-8 boundary and appending "… (truncated)" if it was shortened.
func truncateString(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	suffix := "\n… (truncated)"
	limit := maxBytes - len(suffix)
	if limit <= 0 {
		return suffix
	}
	// Walk back until we land on a valid UTF-8 rune boundary.
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit] + suffix
}
