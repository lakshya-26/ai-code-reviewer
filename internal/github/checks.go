package github

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/go-github/v62/github"
)

const checkRunName = "DiffSense AI"

// CheckConclusion maps review outcome to a GitHub Check Run conclusion.
// Clean reviews are "success". Issues or pipeline errors are "neutral"
// (visible, but does not block merge).
func CheckConclusion(err error, issueCount int) string {
	if err != nil || issueCount > 0 {
		return "neutral"
	}
	return "success"
}

// StartCheckRun creates an in_progress check on the given commit SHA.
// Returns 0, err if the API call fails (caller should still continue the review).
func StartCheckRun(ctx context.Context, client *github.Client, owner, repo, sha string) (int64, error) {
	now := github.Timestamp{Time: time.Now().UTC()}
	opts := github.CreateCheckRunOptions{
		Name:      checkRunName,
		HeadSHA:   sha,
		Status:    github.String("in_progress"),
		StartedAt: &now,
		Output: &github.CheckRunOutput{
			Title:   github.String(checkRunName),
			Summary: github.String("Review in progress…"),
		},
	}
	run, resp, err := client.Checks.CreateCheckRun(ctx, owner, repo, opts)
	CheckRateLimit(resp)
	if err != nil {
		return 0, fmt.Errorf("creating check run: %w", err)
	}
	return run.GetID(), nil
}

// CompleteCheckRun marks a check run completed with the given conclusion and summary.
func CompleteCheckRun(ctx context.Context, client *github.Client, owner, repo string, id int64, conclusion, summary string) error {
	if id == 0 {
		return nil
	}
	now := github.Timestamp{Time: time.Now().UTC()}
	title := checkRunName
	if conclusion == "success" {
		title = "DiffSense AI — no issues"
	} else {
		title = "DiffSense AI — review complete"
	}
	opts := github.UpdateCheckRunOptions{
		Name:        checkRunName,
		Status:      github.String("completed"),
		Conclusion:  github.String(conclusion),
		CompletedAt: &now,
		Output: &github.CheckRunOutput{
			Title:   github.String(title),
			Summary: github.String(summary),
		},
	}
	_, resp, err := client.Checks.UpdateCheckRun(ctx, owner, repo, id, opts)
	CheckRateLimit(resp)
	if err != nil {
		slog.Warn("failed to complete check run", "id", id, "err", err)
		return fmt.Errorf("completing check run %d: %w", id, err)
	}
	return nil
}
