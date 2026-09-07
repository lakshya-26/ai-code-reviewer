package filter

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v62/github"
	"gopkg.in/yaml.v3"
)

// RepoConfig is the per-repository reviewer configuration loaded from
// .ai-reviewer.yml in the root of the target repository.
// All fields are optional — missing fields fall back to DefaultRepoConfig values.
type RepoConfig struct {
	// SkipDrafts controls whether draft PRs are skipped (default: true).
	SkipDrafts bool `yaml:"skip_drafts"`

	// OnSynchronize controls whether the bot re-reviews on new commits (default: true).
	OnSynchronize bool `yaml:"on_synchronize"`

	// ApproveOnClean posts an APPROVE review when no issues are found (default: true).
	ApproveOnClean bool `yaml:"approve_on_clean"`

	// MaxFiles is the maximum number of files reviewed per PR (default: 50).
	MaxFiles int `yaml:"max_files"`

	// MinSeverity filters out comments below this level.
	// Values: "suggestion" | "warning" | "error" (default: "suggestion" = show all).
	MinSeverity string `yaml:"min_severity"`

	// Categories is the list of issue categories the AI should check.
	// Default: all five categories.
	Categories []string `yaml:"categories"`

	// IgnorePaths is a list of glob patterns for files/directories to skip.
	IgnorePaths []string `yaml:"ignore_paths"`

	// PathPrompts are extra system instructions applied to matching files.
	PathPrompts []PathPrompt `yaml:"path_prompts"`
}

// PathPrompt attaches extra review instructions to files matching Path.
type PathPrompt struct {
	Path   string `yaml:"path"`
	Prompt string `yaml:"prompt"`
}

// DefaultRepoConfig returns the baseline configuration used when a repository
// has no .ai-reviewer.yml or the file cannot be parsed.
func DefaultRepoConfig() *RepoConfig {
	return &RepoConfig{
		SkipDrafts:     true,
		OnSynchronize:  true,
		ApproveOnClean: true,
		MaxFiles:       50,
		MinSeverity:    "suggestion",
		Categories:     []string{"bug", "security", "performance", "code-smell", "best-practice"},
		IgnorePaths:    []string{},
	}
}

// repoConfigFile is the well-known path inside each repository.
const repoConfigFile = ".ai-reviewer.yml"

// LoadRepoConfig fetches and parses .ai-reviewer.yml from the repository root.
// If the file does not exist or cannot be parsed, DefaultRepoConfig is returned —
// a missing config file is never treated as an error.
func LoadRepoConfig(
	ctx context.Context,
	client *github.Client,
	owner, repo string,
) *RepoConfig {
	fileContent, _, _, err := client.Repositories.GetContents(
		ctx, owner, repo, repoConfigFile,
		&github.RepositoryContentGetOptions{},
	)
	if err != nil {
		// 404 is the common case — repo simply has no config file.
		// Any other error is also non-fatal; we fall back to defaults.
		slog.Debug("repo config not found, using defaults",
			"repo", owner+"/"+repo,
			"err", err,
		)
		return DefaultRepoConfig()
	}

	content, err := fileContent.GetContent()
	if err != nil {
		slog.Warn("failed to decode repo config content, using defaults",
			"repo", owner+"/"+repo,
			"err", err,
		)
		return DefaultRepoConfig()
	}

	cfg := DefaultRepoConfig()
	if err := yaml.Unmarshal([]byte(content), cfg); err != nil {
		slog.Warn("failed to parse .ai-reviewer.yml, using defaults",
			"repo", owner+"/"+repo,
			"err", err,
		)
		return DefaultRepoConfig()
	}

	// Apply safety bounds on MaxFiles — protect against misconfigured repos.
	if cfg.MaxFiles <= 0 || cfg.MaxFiles > 200 {
		cfg.MaxFiles = 50
	}

	slog.Debug("repo config loaded",
		"repo", owner+"/"+repo,
		"skip_drafts", cfg.SkipDrafts,
		"max_files", cfg.MaxFiles,
		"min_severity", cfg.MinSeverity,
		"ignore_paths", len(cfg.IgnorePaths),
	)

	return cfg
}

// SeverityLevel converts a severity string to an integer for comparison.
// Higher = more severe.
func SeverityLevel(s string) int {
	switch s {
	case "error":
		return 3
	case "warning":
		return 2
	case "suggestion":
		return 1
	default:
		return 1
	}
}

// MeetsSeverityThreshold returns true if commentSeverity is at or above the
// configured minimum severity for this repo.
func MeetsSeverityThreshold(commentSeverity, minSeverity string) bool {
	return SeverityLevel(commentSeverity) >= SeverityLevel(minSeverity)
}

// IsCategoryEnabled returns true if the given category is in the repo's
// configured category list.
func IsCategoryEnabled(category string, cfg *RepoConfig) bool {
	if len(cfg.Categories) == 0 {
		return true // empty list = all enabled
	}
	for _, c := range cfg.Categories {
		if c == category {
			return true
		}
	}
	return false
}

// PathPromptFor returns the first matching path_prompts entry for filename.
func PathPromptFor(filename string, cfg *RepoConfig) string {
	if cfg == nil {
		return ""
	}
	for _, p := range cfg.PathPrompts {
		if matchPathPattern(p.Path, filename) {
			return p.Prompt
		}
	}
	return ""
}

// matchPathPattern supports filepath.Match plus a trailing "/**" prefix match
// so patterns like "internal/ai/**" apply to nested files.
func matchPathPattern(pattern, name string) bool {
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		if name == prefix || strings.HasPrefix(name, prefix+"/") {
			return true
		}
	}
	if matched, _ := filepath.Match(pattern, name); matched {
		return true
	}
	if matched, _ := filepath.Match(pattern, filepath.Base(name)); matched {
		return true
	}
	return false
}
