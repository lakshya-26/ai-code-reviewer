package ai

import (
	"log/slog"
)

// ValidateAgainstDiff removes any ReviewComments whose line number does not
// correspond to an actual added line in the parsed diff.
//
// This is the final safety net before posting to GitHub. Posting a comment on
// a line that isn't in the diff causes GitHub to return HTTP 422
// (Unprocessable Entity) for the entire review.
//
// validLines is the map[int]struct{} from parser.ParsedFile.LineNumbers.
func ValidateAgainstDiff(comments []ReviewComment, validLines map[int]struct{}, filename string) []ReviewComment {
	if len(validLines) == 0 {
		return nil
	}

	valid := make([]ReviewComment, 0, len(comments))
	for _, c := range comments {
		if _, ok := validLines[c.Line]; ok {
			valid = append(valid, c)
		} else {
			slog.Debug("dropping comment with line not in diff",
				"file", filename,
				"line", c.Line,
				"category", c.Category,
			)
		}
	}
	return valid
}

// DeduplicateComments removes duplicate comments targeting the same
// (line, category) pair in the same file. When a model is called with a large
// diff it occasionally emits the same issue twice. Keeping only the first
// occurrence preserves the most useful comment.
func DeduplicateComments(comments []ReviewComment) []ReviewComment {
	type key struct {
		line     int
		category string
	}

	seen := make(map[key]struct{}, len(comments))
	result := make([]ReviewComment, 0, len(comments))

	for _, c := range comments {
		k := key{line: c.Line, category: c.Category}
		if _, exists := seen[k]; exists {
			continue
		}
		seen[k] = struct{}{}
		result = append(result, c)
	}
	return result
}

// ApplyFilters runs the full validation + dedup pipeline on a set of comments
// for one file. This is the single entry point the orchestrator calls.
//
//  1. Validate line numbers against the parsed diff
//  2. Deduplicate by (line, category)
func ApplyFilters(
	comments []ReviewComment,
	validLines map[int]struct{},
	filename string,
) []ReviewComment {
	comments = ValidateAgainstDiff(comments, validLines, filename)
	comments = DeduplicateComments(comments)
	return comments
}

// CountBySeverity returns how many comments have the given severity level.
func CountBySeverity(reviews []FileReview, severity string) int {
	count := 0
	for _, r := range reviews {
		for _, c := range r.Comments {
			if c.Severity == severity {
				count++
			}
		}
	}
	return count
}

// TotalComments returns the total number of comments across all file reviews.
func TotalComments(reviews []FileReview) int {
	total := 0
	for _, r := range reviews {
		total += len(r.Comments)
	}
	return total
}
