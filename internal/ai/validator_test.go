package ai

import (
	"testing"
)

func TestValidateAgainstDiff_FiltersInvalidLines(t *testing.T) {
	validLines := map[int]struct{}{
		5:  {},
		10: {},
		15: {},
	}

	comments := []ReviewComment{
		{Line: 5, Severity: "error", Category: "bug", Comment: "real issue"},
		{Line: 99, Severity: "warning", Category: "security", Comment: "hallucinated line"},
		{Line: 10, Severity: "suggestion", Category: "performance", Comment: "valid"},
	}

	result := ValidateAgainstDiff(comments, validLines, "file.go")

	if len(result) != 2 {
		t.Fatalf("expected 2 valid comments, got %d", len(result))
	}
	if result[0].Line != 5 || result[1].Line != 10 {
		t.Error("wrong comments kept after diff validation")
	}
}

func TestValidateAgainstDiff_EmptyValidLines(t *testing.T) {
	comments := []ReviewComment{
		{Line: 1, Severity: "error", Category: "bug", Comment: "issue"},
	}

	result := ValidateAgainstDiff(comments, map[int]struct{}{}, "file.go")
	if len(result) != 0 {
		t.Error("expected empty result when no valid lines exist")
	}
}

func TestDeduplicateComments_RemovesDuplicates(t *testing.T) {
	comments := []ReviewComment{
		{Line: 5, Category: "bug", Comment: "first"},
		{Line: 5, Category: "bug", Comment: "duplicate — should be dropped"},
		{Line: 5, Category: "security", Comment: "different category — keep"},
		{Line: 10, Category: "bug", Comment: "different line — keep"},
	}

	result := DeduplicateComments(comments)

	if len(result) != 3 {
		t.Fatalf("expected 3 comments after dedup, got %d", len(result))
	}
	if result[0].Comment != "first" {
		t.Error("first occurrence should be kept")
	}
}

func TestDeduplicateComments_NoDuplicates(t *testing.T) {
	comments := []ReviewComment{
		{Line: 1, Category: "bug", Comment: "a"},
		{Line: 2, Category: "security", Comment: "b"},
		{Line: 3, Category: "performance", Comment: "c"},
	}

	result := DeduplicateComments(comments)
	if len(result) != 3 {
		t.Errorf("expected 3 comments, got %d", len(result))
	}
}

func TestCountBySeverity(t *testing.T) {
	reviews := []FileReview{
		{
			Filename: "a.go",
			Comments: []ReviewComment{
				{Severity: "error"},
				{Severity: "warning"},
				{Severity: "error"},
			},
		},
		{
			Filename: "b.go",
			Comments: []ReviewComment{
				{Severity: "suggestion"},
				{Severity: "error"},
			},
		},
	}

	if got := CountBySeverity(reviews, "error"); got != 3 {
		t.Errorf("CountBySeverity(error) = %d, want 3", got)
	}
	if got := CountBySeverity(reviews, "warning"); got != 1 {
		t.Errorf("CountBySeverity(warning) = %d, want 1", got)
	}
	if got := CountBySeverity(reviews, "suggestion"); got != 1 {
		t.Errorf("CountBySeverity(suggestion) = %d, want 1", got)
	}
}

func TestTotalComments(t *testing.T) {
	reviews := []FileReview{
		{Comments: []ReviewComment{{}, {}, {}}},
		{Comments: []ReviewComment{{}}},
	}
	if got := TotalComments(reviews); got != 4 {
		t.Errorf("TotalComments = %d, want 4", got)
	}
}

func TestApplyFilters_FullPipeline(t *testing.T) {
	validLines := map[int]struct{}{
		3: {},
		7: {},
	}

	comments := []ReviewComment{
		{Line: 3, Category: "bug", Comment: "valid"},
		{Line: 3, Category: "bug", Comment: "duplicate"},    // dup
		{Line: 99, Category: "security", Comment: "phantom"}, // invalid line
		{Line: 7, Category: "performance", Comment: "valid"},
	}

	result := ApplyFilters(comments, validLines, "file.go")

	// Expected: line 3 (first), line 7 — phantom and dup removed
	if len(result) != 2 {
		t.Fatalf("expected 2 comments after full pipeline, got %d", len(result))
	}
	if result[0].Line != 3 || result[1].Line != 7 {
		t.Error("wrong comments kept by ApplyFilters")
	}
}

func TestParseJSONComments_ValidJSON(t *testing.T) {
	raw := `[{"line":5,"severity":"error","category":"bug","comment":"nil dereference"}]`
	result := parseJSONComments(raw, "file.go", "test")

	if len(result) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(result))
	}
	if result[0].Line != 5 || result[0].Severity != "error" {
		t.Error("comment fields not parsed correctly")
	}
}

func TestParseJSONComments_MarkdownFenced(t *testing.T) {
	raw := "```json\n[{\"line\":3,\"severity\":\"warning\",\"category\":\"security\",\"comment\":\"issue\"}]\n```"
	result := parseJSONComments(raw, "file.go", "test")

	if len(result) != 1 {
		t.Fatalf("expected 1 comment from markdown-fenced JSON, got %d", len(result))
	}
}

func TestParseJSONComments_EmptyArray(t *testing.T) {
	result := parseJSONComments("[]", "file.go", "test")
	if result != nil && len(result) != 0 {
		t.Error("empty array should return nil or empty slice")
	}
}

func TestParseJSONComments_InvalidJSON(t *testing.T) {
	result := parseJSONComments("not json at all", "file.go", "test")
	if result != nil {
		t.Error("invalid JSON should return nil")
	}
}

func TestParseJSONComments_ZeroLine(t *testing.T) {
	raw := `[{"line":0,"severity":"error","category":"bug","comment":"bad line"}]`
	result := parseJSONComments(raw, "file.go", "test")
	if len(result) != 0 {
		t.Error("comment with line=0 should be rejected")
	}
}

func TestParseJSONComments_InvalidSeverityDefaulted(t *testing.T) {
	raw := `[{"line":5,"severity":"critical","category":"bug","comment":"issue"}]`
	result := parseJSONComments(raw, "file.go", "test")
	if len(result) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(result))
	}
	if result[0].Severity != "suggestion" {
		t.Errorf("invalid severity should default to 'suggestion', got %q", result[0].Severity)
	}
}
