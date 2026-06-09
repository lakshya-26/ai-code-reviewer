package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// hunkHeaderRe matches unified diff hunk headers of the form:
//
//	@@ -oldStart[,oldCount] +newStart[,newCount] @@ [optional context]
//
// Both counts are optional — a hunk touching exactly one line is written as
// "@@ -5 +5 @@" with no comma-count suffix.
var hunkHeaderRe = regexp.MustCompile(`@@\s+-\d+(?:,\d+)?\s+\+(\d+)(?:,(\d+))?\s+@@`)

// ParsedLine represents one added line in the new version of a file, with its
// exact line number in the new file. This is the number we pass to GitHub's
// review API.
type ParsedLine struct {
	LineNumber int    // 1-based line number in the NEW file
	Content    string // line text, leading '+' stripped
}

// ParsedFile is the result of parsing one file's unified diff patch.
type ParsedFile struct {
	Filename    string
	Lines       []ParsedLine // only added lines, with correct new-file line numbers
	RawPatch    string
	LineNumbers map[int]struct{} // set of valid new-file line numbers — for O(1) validation
}

// HasLine reports whether lineNumber is a valid added line in this file.
func (pf *ParsedFile) HasLine(lineNumber int) bool {
	_, ok := pf.LineNumbers[lineNumber]
	return ok
}

// ParsePatch converts a raw unified diff patch string into a ParsedFile.
//
// Line number accuracy is critical: a wrong number causes GitHub's review API
// to return 422, and the comment lands on the wrong line for the user.
//
// Algorithm:
//  1. Split patch into lines.
//  2. On each hunk header (@@ line) extract the new-file start line number.
//  3. Walk lines:
//     - '+' line  → added; assign current newLineNum, increment counter.
//     - '-' line  → removed; does NOT exist in new file, skip (no increment).
//     - ' ' line  → context; exists in both files, increment counter only.
//     - '\' line  → "No newline at end of file" marker — skip entirely.
func ParsePatch(filename, patch string) ParsedFile {
	result := ParsedFile{
		Filename:    filename,
		RawPatch:    patch,
		LineNumbers: make(map[int]struct{}),
	}

	if patch == "" {
		return result
	}

	lines := strings.Split(patch, "\n")
	newLineNum := 0
	inHunk := false // true once we have seen the first @@ header

	for _, line := range lines {
		// Hunk header — resets the line counter for each hunk.
		if strings.HasPrefix(line, "@@") {
			start, _, ok := parseHunkHeader(line)
			if !ok {
				continue
			}
			// newLineNum will become `start` after the first real line increment.
			newLineNum = start - 1
			inHunk = true
			continue
		}

		// Skip everything that appears before the first hunk header
		// (diff file headers like "--- a/file" and "+++ b/file").
		if !inHunk {
			continue
		}

		switch {
		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file" — not code, skip entirely.

		case strings.HasPrefix(line, "+"):
			newLineNum++
			result.Lines = append(result.Lines, ParsedLine{
				LineNumber: newLineNum,
				Content:    line[1:], // strip the leading '+'
			})
			result.LineNumbers[newLineNum] = struct{}{}

		case strings.HasPrefix(line, "-"):
			// Removed line — does not exist in new file, do not increment.

		default:
			// Context line (leading space or blank).
			// Exists in both old and new file — increment the new-file counter.
			newLineNum++
		}
	}

	return result
}

// parseHunkHeader extracts newStart and newCount from a unified diff hunk header.
// Returns (newStart, newCount, ok).
//
// Examples:
//
//	"@@ -1,5 +1,8 @@"       → (1, 8, true)
//	"@@ -10 +10,3 @@"       → (10, 3, true)
//	"@@ -3,4 +3 @@"         → (3, 1, true)   // missing count = 1
//	"@@ -0,0 +1 @@"         → (1, 1, true)   // new file
func parseHunkHeader(line string) (newStart, newCount int, ok bool) {
	m := hunkHeaderRe.FindStringSubmatch(line)
	if m == nil {
		return 0, 0, false
	}

	start, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}

	count := 1 // default when count is omitted
	if m[2] != "" {
		count, err = strconv.Atoi(m[2])
		if err != nil {
			return 0, 0, false
		}
	}

	return start, count, true
}

// ParseAll parses a slice of (filename, patch) pairs concurrently and returns
// the results in the same order. Files with empty patches are returned as
// empty ParsedFiles and should be skipped by the caller.
func ParseAll(files []FilePatch) []ParsedFile {
	results := make([]ParsedFile, len(files))
	for i, f := range files {
		results[i] = ParsePatch(f.Filename, f.Patch)
	}
	return results
}

// FilePatch is a lightweight input to ParseAll.
type FilePatch struct {
	Filename string
	Patch    string
}

// ValidateCommentLines filters out any line numbers that do not correspond to
// actual added lines in the parsed diff. GitHub returns 422 if you try to post
// a comment on a line that isn't in the diff.
func ValidateCommentLines(parsed ParsedFile, lines []int) []int {
	valid := make([]int, 0, len(lines))
	for _, ln := range lines {
		if parsed.HasLine(ln) {
			valid = append(valid, ln)
		}
	}
	return valid
}

// Summary returns a human-readable description of a parsed file for logging.
func (pf *ParsedFile) Summary() string {
	return fmt.Sprintf("%s: %d added lines across %d hunks",
		pf.Filename, len(pf.Lines), countHunks(pf.RawPatch))
}

func countHunks(patch string) int {
	return strings.Count(patch, "\n@@") + func() int {
		if strings.HasPrefix(patch, "@@") {
			return 1
		}
		return 0
	}()
}
