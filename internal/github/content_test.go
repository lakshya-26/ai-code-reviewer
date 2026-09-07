package github

import "testing"

func TestFilterSourceTreePaths_DropsVendorAndBinaries(t *testing.T) {
	in := []string{
		"cmd/server/main.go",
		"internal/ai/prompt.go",
		"vendor/github.com/foo/bar.go",
		"logo.png",
		"node_modules/react/index.js",
		"README.md",
		"go.sum",
	}
	got := FilterSourceTreePaths(in, 300)
	want := map[string]bool{
		"cmd/server/main.go":    true,
		"internal/ai/prompt.go": true,
		"README.md":             true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want keys %v", got, want)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected path kept: %s", p)
		}
	}
}

func TestFilterSourceTreePaths_CapsCount(t *testing.T) {
	in := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		in = append(in, "pkg/file.go")
	}
	// Each path is unique? Filter doesn't dedupe. Use unique names.
	in = in[:0]
	for i := 0; i < 20; i++ {
		in = append(in, "pkg/file"+string(rune('a'+i))+".go")
	}
	got := FilterSourceTreePaths(in, 5)
	if len(got) != 5 {
		t.Fatalf("cap: got %d paths, want 5", len(got))
	}
}

func TestTruncateChars_Short(t *testing.T) {
	if got := TruncateChars("hello", 100); got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestTruncateChars_Long(t *testing.T) {
	got := TruncateChars("abcdefghij", 7)
	if len(got) > 7 {
		t.Errorf("too long: %q", got)
	}
	if got == "abcdefghij" {
		t.Error("should truncate")
	}
}
