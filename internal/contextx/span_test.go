package contextx

import (
	"strings"
	"testing"
)

const twoFuncs = `package demo

func Alpha() {
	x := 1
	_ = x
}

func Beta() {
	y := 2
	_ = y
}
`

func TestExtractSpans_GoSecondFuncOnly(t *testing.T) {
	// "y := 2" is on line 9 (1-based)
	added := map[int]struct{}{9: {}}
	got := ExtractSpans("demo.go", twoFuncs, added, 12_000, false)
	if !strings.Contains(got, "func Beta") {
		t.Fatalf("expected Beta, got:\n%s", got)
	}
	if strings.Contains(got, "func Alpha") {
		t.Fatalf("should not include Alpha, got:\n%s", got)
	}
}

func TestExtractSpans_NewFileUnderCap(t *testing.T) {
	got := ExtractSpans("demo.go", twoFuncs, map[int]struct{}{3: {}}, 12_000, true)
	if got != twoFuncs {
		t.Fatalf("new file under cap should return whole file")
	}
}

func TestExtractSpans_FallbackWindow(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 200; i++ {
		b.WriteString("line\n")
	}
	content := b.String()
	added := map[int]struct{}{100: {}}
	got := ExtractSpans("notes.txt", content, added, 12_000, false)
	n := strings.Count(got, "line\n")
	// ±80 around line 100 → at most 161 lines
	if n > 161 {
		t.Fatalf("fallback window too large: %d lines", n)
	}
	if n < 80 {
		t.Fatalf("fallback window too small: %d lines", n)
	}
}

func TestExtractSpans_HugeNewFileUsesWindow(t *testing.T) {
	content := strings.Repeat("x\n", 20_000)
	got := ExtractSpans("big.go", content, map[int]struct{}{10: {}}, 100, true)
	if len(got) > 200 {
		t.Fatalf("huge file should not dump everything, got %d bytes", len(got))
	}
}

func TestMergeRanges_Overlap(t *testing.T) {
	got := mergeRanges([]lineRange{{1, 10}, {8, 15}, {30, 32}})
	if len(got) != 2 {
		t.Fatalf("want 2 merged ranges, got %#v", got)
	}
	if got[0] != (lineRange{1, 15}) {
		t.Fatalf("first range: %#v", got[0])
	}
}
