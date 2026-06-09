package parser

import (
	"testing"
)

func TestParsePatch_SingleHunk(t *testing.T) {
	patch := `@@ -1,4 +1,6 @@
 package main
+
+import "fmt"
 
 func main() {
+	fmt.Println("hello")
 }`

	pf := ParsePatch("main.go", patch)

	if len(pf.Lines) != 3 {
		t.Fatalf("expected 3 added lines, got %d", len(pf.Lines))
	}

	// Line 2: blank line (import separator)
	if pf.Lines[0].LineNumber != 2 {
		t.Errorf("first added line: want 2, got %d", pf.Lines[0].LineNumber)
	}
	// Line 3: import "fmt"
	if pf.Lines[1].LineNumber != 3 {
		t.Errorf("second added line: want 3, got %d", pf.Lines[1].LineNumber)
	}
	// Line 6: fmt.Println
	if pf.Lines[2].LineNumber != 6 {
		t.Errorf("third added line: want 6, got %d", pf.Lines[2].LineNumber)
	}
}

func TestParsePatch_MultiHunk(t *testing.T) {
	patch := `@@ -1,3 +1,4 @@
 line1
+addedA
 line2
 line3
@@ -10,3 +11,4 @@
 line10
+addedB
 line11
 line12`

	pf := ParsePatch("file.go", patch)

	if len(pf.Lines) != 2 {
		t.Fatalf("expected 2 added lines, got %d", len(pf.Lines))
	}

	if pf.Lines[0].LineNumber != 2 {
		t.Errorf("first hunk added line: want 2, got %d", pf.Lines[0].LineNumber)
	}
	if pf.Lines[0].Content != "addedA" {
		t.Errorf("first hunk content: want 'addedA', got %q", pf.Lines[0].Content)
	}

	if pf.Lines[1].LineNumber != 12 {
		t.Errorf("second hunk added line: want 12, got %d", pf.Lines[1].LineNumber)
	}
	if pf.Lines[1].Content != "addedB" {
		t.Errorf("second hunk content: want 'addedB', got %q", pf.Lines[1].Content)
	}
}

func TestParsePatch_OnlyDeletions(t *testing.T) {
	patch := `@@ -1,3 +1,0 @@
-line1
-line2
-line3`

	pf := ParsePatch("file.go", patch)

	if len(pf.Lines) != 0 {
		t.Errorf("expected 0 added lines for deletion-only patch, got %d", len(pf.Lines))
	}
}

func TestParsePatch_EmptyPatch(t *testing.T) {
	pf := ParsePatch("file.go", "")

	if len(pf.Lines) != 0 {
		t.Errorf("expected 0 lines for empty patch, got %d", len(pf.Lines))
	}
	if pf.Filename != "file.go" {
		t.Errorf("filename not preserved")
	}
}

func TestParsePatch_NoNewlineAtEOF(t *testing.T) {
	patch := `@@ -1,2 +1,3 @@
 existing
+newline
\ No newline at end of file`

	pf := ParsePatch("file.go", patch)

	// Only 1 real added line — the backslash marker must be ignored.
	if len(pf.Lines) != 1 {
		t.Fatalf("expected 1 added line, got %d", len(pf.Lines))
	}
	if pf.Lines[0].Content != "newline" {
		t.Errorf("unexpected content: %q", pf.Lines[0].Content)
	}
}

func TestParsePatch_NewFile(t *testing.T) {
	// New file hunks start at line 0,0 → +1
	patch := `@@ -0,0 +1,3 @@
+first
+second
+third`

	pf := ParsePatch("new.go", patch)

	if len(pf.Lines) != 3 {
		t.Fatalf("expected 3 added lines, got %d", len(pf.Lines))
	}
	for i, want := range []int{1, 2, 3} {
		if pf.Lines[i].LineNumber != want {
			t.Errorf("line[%d]: want %d, got %d", i, want, pf.Lines[i].LineNumber)
		}
	}
}

func TestParsePatch_HunkMissingCount(t *testing.T) {
	// "@@ -5 +5 @@" — both counts omitted, meaning exactly 1 line each.
	patch := `@@ -5 +5 @@
-old
+new`

	pf := ParsePatch("file.go", patch)

	if len(pf.Lines) != 1 {
		t.Fatalf("expected 1 added line, got %d", len(pf.Lines))
	}
	if pf.Lines[0].LineNumber != 5 {
		t.Errorf("want line 5, got %d", pf.Lines[0].LineNumber)
	}
}

func TestParsedFile_HasLine(t *testing.T) {
	patch := `@@ -1,2 +1,3 @@
 context
+added
 context2`

	pf := ParsePatch("file.go", patch)

	if !pf.HasLine(2) {
		t.Error("line 2 should be valid (it was added)")
	}
	if pf.HasLine(1) {
		t.Error("line 1 is a context line, not an added line — should be invalid")
	}
	if pf.HasLine(99) {
		t.Error("line 99 does not exist")
	}
}

func TestParseHunkHeader(t *testing.T) {
	cases := []struct {
		line       string
		wantStart  int
		wantCount  int
		wantOK     bool
	}{
		{"@@ -1,5 +1,8 @@", 1, 8, true},
		{"@@ -10 +10,3 @@", 10, 3, true},
		{"@@ -3,4 +3 @@", 3, 1, true},
		{"@@ -0,0 +1 @@", 1, 1, true},
		{"@@ -0,0 +1,0 @@", 1, 0, true},
		{"not a hunk", 0, 0, false},
	}

	for _, tc := range cases {
		start, count, ok := parseHunkHeader(tc.line)
		if ok != tc.wantOK {
			t.Errorf("%q: ok=%v, want %v", tc.line, ok, tc.wantOK)
			continue
		}
		if ok {
			if start != tc.wantStart {
				t.Errorf("%q: start=%d, want %d", tc.line, start, tc.wantStart)
			}
			if count != tc.wantCount {
				t.Errorf("%q: count=%d, want %d", tc.line, count, tc.wantCount)
			}
		}
	}
}
