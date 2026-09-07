package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PRReview is the persisted incremental-review state for one pull request.
type PRReview struct {
	InstallationID  int64
	RepoFullName    string
	PRNumber        int
	LastReviewedSHA string
	CheckRunID      int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PostedComment is one inline comment already posted on a PR.
type PostedComment struct {
	RepoFullName string
	PRNumber     int
	Path         string
	Line         int
	Category     string
	BodyHash     string
	CommitSHA    string
}

// GetPRReview returns the review-memory row for a PR, or nil if none exists.
func (s *Store) GetPRReview(ctx context.Context, installationID int64, repoFullName string, prNumber int) (*PRReview, error) {
	var row PRReview
	err := s.db.QueryRowContext(ctx, `
		SELECT installation_id, repo_full_name, pr_number, last_reviewed_sha, check_run_id,
		       created_at, updated_at
		FROM pr_reviews
		WHERE installation_id = $1 AND repo_full_name = $2 AND pr_number = $3
	`, installationID, repoFullName, prNumber).Scan(
		&row.InstallationID, &row.RepoFullName, &row.PRNumber,
		&row.LastReviewedSHA, &row.CheckRunID, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pr_review %s#%d: %w", repoFullName, prNumber, err)
	}
	return &row, nil
}

// UpsertPRReview inserts or updates last_reviewed_sha (and optional check_run_id).
func (s *Store) UpsertPRReview(ctx context.Context, row PRReview) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pr_reviews (installation_id, repo_full_name, pr_number, last_reviewed_sha, check_run_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (installation_id, repo_full_name, pr_number) DO UPDATE
			SET last_reviewed_sha = EXCLUDED.last_reviewed_sha,
			    check_run_id      = CASE
			                          WHEN EXCLUDED.check_run_id = 0 THEN pr_reviews.check_run_id
			                          ELSE EXCLUDED.check_run_id
			                        END,
			    updated_at        = NOW()
	`, row.InstallationID, row.RepoFullName, row.PRNumber, row.LastReviewedSHA, row.CheckRunID)
	if err != nil {
		return fmt.Errorf("upsert pr_review %s#%d: %w", row.RepoFullName, row.PRNumber, err)
	}
	return nil
}

// ListPostedCommentKeys returns the (path, line, category) keys already posted on a PR.
func (s *Store) ListPostedCommentKeys(ctx context.Context, repoFullName string, prNumber int) (map[CommentKey]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, line, category
		FROM posted_comments
		WHERE repo_full_name = $1 AND pr_number = $2
	`, repoFullName, prNumber)
	if err != nil {
		return nil, fmt.Errorf("list posted_comments %s#%d: %w", repoFullName, prNumber, err)
	}
	defer rows.Close()

	out := make(map[CommentKey]struct{})
	for rows.Next() {
		var k CommentKey
		if err := rows.Scan(&k.Path, &k.Line, &k.Category); err != nil {
			return nil, fmt.Errorf("scan posted_comments: %w", err)
		}
		out[k] = struct{}{}
	}
	return out, rows.Err()
}

// InsertPostedComments records comments that were successfully posted. Conflicts are ignored.
func (s *Store) InsertPostedComments(ctx context.Context, comments []PostedComment) error {
	for _, c := range comments {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO posted_comments (repo_full_name, pr_number, path, line, category, body_hash, commit_sha)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (repo_full_name, pr_number, path, line, category) DO NOTHING
		`, c.RepoFullName, c.PRNumber, c.Path, c.Line, c.Category, c.BodyHash, c.CommitSHA)
		if err != nil {
			return fmt.Errorf("insert posted_comment %s:%d: %w", c.Path, c.Line, err)
		}
	}
	return nil
}
