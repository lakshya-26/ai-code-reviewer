package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v62/github"
)

// PRFile holds the data we need from one file in a pull request.
type PRFile struct {
	Filename    string
	Status      string // added | modified | removed | renamed | copied | changed | unchanged
	Patch       string // raw unified diff — empty for binary files and pure renames
	BlobURL     string // link to the file blob for logging/debugging
	Additions   int
	Deletions   int
}

// FetchPRFiles returns all files changed in a pull request.
//
// GitHub paginates this endpoint at 100 files per page, so we loop until
// NextPage == 0. Callers should apply their own file-count limit after this
// returns (see filter.ShouldSkip and config.MaxFilesPerPR).
func FetchPRFiles(
	ctx context.Context,
	client *github.Client,
	owner, repo string,
	prNumber int,
) ([]PRFile, error) {
	var allFiles []PRFile
	opts := &github.ListOptions{PerPage: 100}

	for {
		files, resp, err := client.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR files (page %d): %w", opts.Page, err)
		}
		CheckRateLimit(resp)

		for _, f := range files {
			allFiles = append(allFiles, PRFile{
				Filename:  f.GetFilename(),
				Status:    f.GetStatus(),
				Patch:     f.GetPatch(),
				BlobURL:   f.GetBlobURL(),
				Additions: f.GetAdditions(),
				Deletions: f.GetDeletions(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}

// IsReviewable returns true when a file has content worth sending to the AI.
// This is a fast pre-filter before the heavier file-extension filter in the
// filter package.
func IsReviewable(f PRFile) bool {
	// Deleted files have no new lines to review.
	if f.Status == "removed" {
		return false
	}

	// Binary files and pure renames have empty patches.
	if f.Patch == "" {
		return false
	}

	// Diff marker for binary content — not parseable.
	if strings.Contains(f.Patch, "Binary files") {
		return false
	}

	// Files with no additions have nothing new for the AI to look at.
	if f.Additions == 0 {
		return false
	}

	return true
}
