package github

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ai-code-reviewer/ai-code-reviewer/internal/ai"
)

func TestFormatCommentBody_ErrorSeverity(t *testing.T) {
	c := ai.ReviewComment{
		Line:     5,
		Severity: "error",
		Category: "bug",
		Comment:  "nil dereference — add a nil check before dereferencing ptr",
	}
	body := formatCommentBody(c, "handler.go")

	if !strings.Contains(body, "**[ERROR]**") {
		t.Error("body should contain **[ERROR]**")
	}
	if !strings.Contains(body, "`bug`") {
		t.Error("body should contain the category in backticks")
	}
	if !strings.Contains(body, c.Comment) {
		t.Error("body should contain the comment text")
	}
}

func TestFormatCommentBody_AllSeverities(t *testing.T) {
	for _, sev := range []string{"error", "warning", "suggestion"} {
		c := ai.ReviewComment{Severity: sev, Category: "bug", Comment: "issue", Line: 1}
		body := formatCommentBody(c, "x.go")
		tag := "**[" + strings.ToUpper(sev) + "]**"
		if !strings.Contains(body, tag) {
			t.Errorf("severity %q: expected %q in body", sev, tag)
		}
	}
}

func TestBuildSummaryBody_WithIssues(t *testing.T) {
	reviews := []ai.FileReview{
		{
			Filename: "main.go",
			Comments: []ai.ReviewComment{
				{Severity: "error", Category: "bug"},
				{Severity: "warning", Category: "security"},
			},
		},
		{
			Filename: "handler.go",
			Comments: []ai.ReviewComment{
				{Severity: "suggestion", Category: "performance"},
			},
		},
	}

	body := buildSummaryBody(3, 2, reviews)

	if !strings.Contains(body, "3 issues") {
		t.Error("body should mention issue count")
	}
	if !strings.Contains(body, "2 files") {
		t.Error("body should mention file count")
	}
	if !strings.Contains(body, "main.go") {
		t.Error("body should list reviewed files")
	}
	if !strings.Contains(body, "handler.go") {
		t.Error("body should list all files with issues")
	}
	if !strings.Contains(body, emojiError) {
		t.Error("body should contain error emoji")
	}
}

func TestBuildSummaryBody_Singular(t *testing.T) {
	reviews := []ai.FileReview{
		{Filename: "x.go", Comments: []ai.ReviewComment{{Severity: "error"}}},
	}
	body := buildSummaryBody(1, 1, reviews)

	// Singular forms
	if !strings.Contains(body, "1 issue") {
		t.Error("expected singular 'issue'")
	}
	if strings.Contains(body, "1 issues") {
		t.Error("should not use plural 'issues' for count 1")
	}
	if !strings.Contains(body, "1 file") {
		t.Error("expected singular 'file'")
	}
}

func TestBuildDraftComments_LineAndPath(t *testing.T) {
	reviews := []ai.FileReview{
		{
			Filename: "internal/handler.go",
			Comments: []ai.ReviewComment{
				{Line: 42, Severity: "error", Category: "bug", Comment: "bad"},
				{Line: 99, Severity: "warning", Category: "security", Comment: "risky"},
			},
		},
	}

	comments := buildDraftComments(reviews)

	if len(comments) != 2 {
		t.Fatalf("expected 2 draft comments, got %d", len(comments))
	}
	if comments[0].GetPath() != "internal/handler.go" {
		t.Errorf("wrong path: %s", comments[0].GetPath())
	}
	if comments[0].GetLine() != 42 {
		t.Errorf("wrong line: %d", comments[0].GetLine())
	}
	if comments[1].GetLine() != 99 {
		t.Errorf("wrong line: %d", comments[1].GetLine())
	}
}

func TestBuildDraftComments_EmptyReviews(t *testing.T) {
	comments := buildDraftComments([]ai.FileReview{})
	if len(comments) != 0 {
		t.Errorf("expected 0 comments for empty reviews, got %d", len(comments))
	}
}

func TestCountFilesWithComments(t *testing.T) {
	reviews := []ai.FileReview{
		{Filename: "a.go", Comments: []ai.ReviewComment{{}}},
		{Filename: "b.go", Comments: []ai.ReviewComment{}},
		{Filename: "c.go", Comments: []ai.ReviewComment{{}, {}}},
	}

	if got := countFilesWithComments(reviews); got != 2 {
		t.Errorf("expected 2 files with comments, got %d", got)
	}
}

func TestTruncateString_ShortString(t *testing.T) {
	s := "hello"
	out := truncateString(s, 100)
	if out != s {
		t.Errorf("short string should not be truncated: %q", out)
	}
}

func TestTruncateString_LongString(t *testing.T) {
	s := strings.Repeat("a", 100)
	out := truncateString(s, 50)

	if len(out) > 50 {
		t.Errorf("truncated string too long: %d bytes", len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Error("truncated string should mention truncation")
	}
}

func TestTruncateString_ValidUTF8Boundary(t *testing.T) {
	// Build a string with multi-byte UTF-8 runes (2 bytes each).
	s := strings.Repeat("é", 100) // é = 2 bytes in UTF-8

	out := truncateString(s, 51) // 51 bytes — cuts in the middle of a 2-byte rune

	if !utf8.ValidString(out) {
		t.Error("truncated string must be valid UTF-8")
	}
}

func TestFormatCommentBody_IncludesCopyPasteFence(t *testing.T) {
	c := ai.ReviewComment{
		Line:     1,
		Severity: "error",
		Category: "bug",
		Comment:  "check the error",
		Fix:      "if err != nil {\n\treturn err\n}",
	}
	body := formatCommentBody(c, "internal/foo.go")
	if !strings.Contains(body, "```go") {
		t.Fatalf("expected go fence, got:\n%s", body)
	}
	if strings.Contains(body, "```suggestion") {
		t.Fatal("must not use GitHub suggestion fences")
	}
	if !strings.Contains(body, "return err") {
		t.Fatal("fix snippet missing")
	}
}
