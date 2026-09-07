package github

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/go-github/v62/github"

	"github.com/ai-code-reviewer/ai-code-reviewer/internal/filter"
)

const maxSourceTreePaths = 300

// ChangedFile is one path from a compare-commits response.
type ChangedFile struct {
	Filename string
	Status   string // added | modified | removed | renamed | ...
}

// FetchFileAtRef returns the file contents at a git ref (SHA or branch).
// A missing file returns ("", nil).
func FetchFileAtRef(ctx context.Context, client *github.Client, owner, repo, path, ref string) (string, error) {
	fileContent, _, resp, err := client.Repositories.GetContents(
		ctx, owner, repo, path,
		&github.RepositoryContentGetOptions{Ref: ref},
	)
	if err != nil {
		if IsNotFound(err) {
			return "", nil
		}
		CheckRateLimit(resp)
		return "", fmt.Errorf("fetching %s@%s: %w", path, ref, err)
	}
	CheckRateLimit(resp)
	if fileContent == nil {
		return "", nil
	}
	content, err := fileContent.GetContent()
	if err != nil {
		return "", fmt.Errorf("decoding %s: %w", path, err)
	}
	return content, nil
}

// FetchSourceTree returns a capped list of source-like blob paths at sha.
func FetchSourceTree(ctx context.Context, client *github.Client, owner, repo, sha string) ([]string, error) {
	tree, resp, err := client.Git.GetTree(ctx, owner, repo, sha, true)
	CheckRateLimit(resp)
	if err != nil {
		return nil, fmt.Errorf("fetching git tree at %s: %w", sha, err)
	}
	if tree.GetTruncated() {
		slog.Warn("git tree truncated by GitHub — repo map will be incomplete",
			"repo", owner+"/"+repo, "sha", sha)
	}
	raw := make([]string, 0, len(tree.Entries))
	for _, e := range tree.Entries {
		if e.GetType() != "blob" {
			continue
		}
		raw = append(raw, e.GetPath())
	}
	return FilterSourceTreePaths(raw, maxSourceTreePaths), nil
}

// FilterSourceTreePaths drops vendor/binaries using the same skip rules as reviews.
func FilterSourceTreePaths(paths []string, capCount int) []string {
	if capCount <= 0 {
		capCount = maxSourceTreePaths
	}
	out := make([]string, 0, min(len(paths), capCount))
	for _, p := range paths {
		if filter.ShouldSkip(p, "", nil) {
			continue
		}
		out = append(out, p)
		if len(out) >= capCount {
			break
		}
	}
	return out
}

// FilesChangedBetween lists files that differ between baseSHA and headSHA.
func FilesChangedBetween(ctx context.Context, client *github.Client, owner, repo, baseSHA, headSHA string) ([]ChangedFile, error) {
	cmp, resp, err := client.Repositories.CompareCommits(ctx, owner, repo, baseSHA, headSHA, nil)
	CheckRateLimit(resp)
	if err != nil {
		return nil, fmt.Errorf("comparing %s...%s: %w", baseSHA, headSHA, err)
	}
	out := make([]ChangedFile, 0, len(cmp.Files))
	for _, f := range cmp.Files {
		out = append(out, ChangedFile{
			Filename: f.GetFilename(),
			Status:   f.GetStatus(),
		})
	}
	return out, nil
}

// FetchGuidelines loads CONTRIBUTING.md, CONTRIBUTING, and AGENTS.md at ref.
// Missing files are skipped. Each file is truncated to maxChars.
func FetchGuidelines(ctx context.Context, client *github.Client, owner, repo, ref string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 4000
	}
	var b strings.Builder
	for _, path := range []string{"CONTRIBUTING.md", "CONTRIBUTING", "AGENTS.md"} {
		content, err := FetchFileAtRef(ctx, client, owner, repo, path, ref)
		if err != nil {
			slog.Debug("guideline file fetch failed", "path", path, "err", err)
			continue
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		fmt.Fprintf(&b, "## %s\n%s\n\n", path, TruncateChars(content, maxChars))
	}
	return strings.TrimSpace(b.String())
}

// TruncateChars shortens s to at most max bytes on a UTF-8 boundary.
func TruncateChars(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	limit := max
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}
