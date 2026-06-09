package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/go-github/v62/github"
)

// RateLimitThreshold is the remaining-request count below which we log a warning.
const RateLimitThreshold = 100

// CheckRateLimit inspects the GitHub rate-limit headers from a response and
// logs a warning when the remaining quota is low. It is called after every
// GitHub API response inside the reviewer pipeline.
func CheckRateLimit(resp *github.Response) {
	if resp == nil {
		return
	}
	remaining := resp.Rate.Remaining
	if remaining > 0 && remaining < RateLimitThreshold {
		slog.Warn("GitHub API rate limit low",
			"remaining", remaining,
			"reset_at", resp.Rate.Reset.Time,
		)
	}
}

// IsNotFound returns true when a GitHub API error has HTTP status 404.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if ghErr, ok := err.(*github.ErrorResponse); ok {
		return ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound
	}
	return false
}

// IsUnprocessable returns true when the GitHub API returns 422 (e.g. invalid line number).
func IsUnprocessable(err error) bool {
	if err == nil {
		return false
	}
	if ghErr, ok := err.(*github.ErrorResponse); ok {
		return ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusUnprocessableEntity
	}
	return false
}

// ValidateInstallation pings the GitHub API with the given client to confirm
// the installation is reachable and the credentials are valid.
// Used at startup or during initial cache population.
func ValidateInstallation(ctx context.Context, client *github.Client, installationID int64) error {
	_, _, err := client.Apps.GetInstallation(ctx, installationID)
	if err != nil {
		return fmt.Errorf("installation %d is unreachable: %w", installationID, err)
	}
	return nil
}
