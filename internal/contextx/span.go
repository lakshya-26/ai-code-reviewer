package contextx

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	defaultFallbackRadius = 80
	defaultMaxFileChars   = 12_000
)

type lineRange struct {
	start int // 1-based inclusive
	end   int // 1-based inclusive
}

// ExtractSpans returns the enclosing function(s) around added lines, or a
// ±80-line window when function boundaries cannot be found.
//
// New files under maxChars are returned in full. maxChars <= 0 uses 12_000.
func ExtractSpans(filename, content string, addedLines map[int]struct{}, maxChars int, isNewFile bool) string {
	if content == "" || len(addedLines) == 0 {
		return ""
	}
	if maxChars <= 0 {
		maxChars = defaultMaxFileChars
	}
	if isNewFile && len(content) <= maxChars {
		return content
	}

	lines := splitLines(content)
	var ranges []lineRange
	if isGoFile(filename) {
		for lineNo := range addedLines {
			ranges = append(ranges, goEnclosingFunc(lines, lineNo))
		}
	} else {
		for lineNo := range addedLines {
			ranges = append(ranges, fallbackWindow(len(lines), lineNo, defaultFallbackRadius))
		}
	}
	merged := mergeRanges(ranges)
	out := renderRanges(lines, merged)
	if len(out) > maxChars {
		return truncateBytes(out, maxChars)
	}
	return out
}

func isGoFile(filename string) bool {
	return strings.EqualFold(filepath.Ext(filename), ".go")
}

func splitLines(content string) []string {
	// Keep a dummy index-0 so line numbers are 1-based.
	parts := strings.Split(content, "\n")
	out := make([]string, 1, len(parts)+1)
	out = append(out, parts...)
	return out
}

func isGoFuncStart(line string) bool {
	return strings.HasPrefix(line, "func ")
}

func isGoTopLevelDecl(line string) bool {
	return strings.HasPrefix(line, "func ") ||
		strings.HasPrefix(line, "type ") ||
		strings.HasPrefix(line, "var ") ||
		strings.HasPrefix(line, "const ")
}

func goEnclosingFunc(lines []string, lineNo int) lineRange {
	if lineNo < 1 || lineNo >= len(lines) {
		return fallbackWindow(len(lines)-1, lineNo, defaultFallbackRadius)
	}
	start := 0
	for i := lineNo; i >= 1; i-- {
		if isGoFuncStart(lines[i]) {
			start = i
			break
		}
	}
	if start == 0 {
		return fallbackWindow(len(lines)-1, lineNo, defaultFallbackRadius)
	}
	end := len(lines) - 1
	for i := start + 1; i < len(lines); i++ {
		if isGoTopLevelDecl(lines[i]) {
			end = i - 1
			break
		}
	}
	for end > start && strings.TrimSpace(lines[end]) == "" {
		end--
	}
	return lineRange{start: start, end: end}
}

func fallbackWindow(lineCount, lineNo, radius int) lineRange {
	if lineCount < 1 {
		return lineRange{1, 1}
	}
	start := lineNo - radius
	if start < 1 {
		start = 1
	}
	end := lineNo + radius
	if end > lineCount {
		end = lineCount
	}
	return lineRange{start: start, end: end}
}

func mergeRanges(in []lineRange) []lineRange {
	if len(in) == 0 {
		return nil
	}
	// insertion sort by start — N is tiny (added-line count)
	sorted := append([]lineRange(nil), in...)
	for i := 1; i < len(sorted); i++ {
		j := i
		for j > 0 && sorted[j].start < sorted[j-1].start {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			j--
		}
	}
	out := []lineRange{sorted[0]}
	for _, r := range sorted[1:] {
		last := &out[len(out)-1]
		if r.start <= last.end+1 {
			if r.end > last.end {
				last.end = r.end
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

func renderRanges(lines []string, ranges []lineRange) string {
	var b strings.Builder
	for i, r := range ranges {
		if i > 0 {
			b.WriteString("\n")
		}
		for n := r.start; n <= r.end && n < len(lines); n++ {
			b.WriteString(lines[n])
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func truncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	limit := max
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}
