package ai

import (
	"strings"
	"testing"
)

func TestBuildPrompt_ContainsFilename(t *testing.T) {
	prompt, _ := BuildPrompt("internal/handler.go", "@@ -1 +1 @@\n+code", 0)
	if !strings.Contains(prompt, "internal/handler.go") {
		t.Error("prompt should contain the filename")
	}
}

func TestBuildPrompt_ContainsLanguage(t *testing.T) {
	prompt, _ := BuildPrompt("main.go", "@@ -1 +1 @@\n+code", 0)
	if !strings.Contains(prompt, "Go") {
		t.Error("prompt should contain the language hint 'Go'")
	}
}

func TestBuildPrompt_ContainsPatch(t *testing.T) {
	patch := "@@ -1 +1 @@\n+myUniqueCode123"
	prompt, _ := BuildPrompt("file.py", patch, 0)
	if !strings.Contains(prompt, "myUniqueCode123") {
		t.Error("prompt should contain the patch content")
	}
}

func TestBuildPrompt_Truncation(t *testing.T) {
	patch := strings.Repeat("+line\n", 1000) // ~6000 chars
	prompt, truncated := BuildPrompt("file.go", patch, 100)

	if !truncated {
		t.Error("expected truncation flag to be true")
	}
	if !strings.Contains(prompt, "truncated") {
		t.Error("prompt should mention truncation when patch was truncated")
	}
}

func TestBuildPrompt_NoTruncation(t *testing.T) {
	patch := "@@ -1 +1 @@\n+small"
	_, truncated := BuildPrompt("file.go", patch, 10_000)
	if truncated {
		t.Error("expected no truncation for a small patch")
	}
}

func TestBuildPrompt_JSONInstructions(t *testing.T) {
	prompt, _ := BuildPrompt("file.go", "@@ -1 +1 @@\n+code", 0)

	for _, required := range []string{"JSON", "line", "severity", "category", "comment"} {
		if !strings.Contains(prompt, required) {
			t.Errorf("prompt missing required JSON schema keyword %q", required)
		}
	}
}

func TestLanguageHintForFile_KnownExtensions(t *testing.T) {
	cases := map[string]string{
		"main.go":       "Go",
		"app.ts":        "TypeScript",
		"index.jsx":     "JavaScript (React)",
		"query.sql":     "SQL",
		"deploy.sh":     "Shell",
		"config.yaml":   "YAML",
		"main.rs":       "Rust",
		"Handler.java":  "Java",
		"script.py":     "Python",
	}
	for filename, want := range cases {
		got := LanguageHintForFile(filename)
		if got != want {
			t.Errorf("LanguageHintForFile(%q) = %q, want %q", filename, got, want)
		}
	}
}

func TestLanguageHintForFile_SpecialFiles(t *testing.T) {
	cases := map[string]string{
		"Dockerfile":    "Dockerfile",
		"Makefile":      "Makefile",
		"Jenkinsfile":   "Groovy (Jenkinsfile)",
	}
	for filename, want := range cases {
		got := LanguageHintForFile(filename)
		if got != want {
			t.Errorf("LanguageHintForFile(%q) = %q, want %q", filename, got, want)
		}
	}
}

func TestTruncatePatch(t *testing.T) {
	patch := "line1\nline2\nline3\nline4"

	// No truncation needed
	out, truncated := TruncatePatch(patch, 1000)
	if truncated || out != patch {
		t.Error("expected no truncation for small patch")
	}

	// Truncation needed
	out, truncated = TruncatePatch(patch, 10)
	if !truncated {
		t.Error("expected truncation")
	}
	if len(out) > 10 {
		t.Errorf("truncated patch too long: %d", len(out))
	}
}
