package ai

import (
	"strings"
	"testing"
)

var emptyCtx = PRContext{}

func bp(filename, patch string, max int, ctx PRContext) BuiltPrompt {
	return BuildPrompt(FileAnalysisInput{Filename: filename, Patch: patch, PRContext: ctx}, max)
}

func TestBuildPrompt_ContainsFilename(t *testing.T) {
	p := bp("internal/handler.go", "@@ -1 +1 @@\n+code", 0, emptyCtx)
	if !strings.Contains(p.User, "internal/handler.go") {
		t.Error("user prompt should contain the filename")
	}
}

func TestBuildPrompt_ContainsLanguage(t *testing.T) {
	p := bp("main.go", "@@ -1 +1 @@\n+code", 0, emptyCtx)
	if !strings.Contains(p.User, "Go") {
		t.Error("user prompt should contain the language hint 'Go'")
	}
}

func TestBuildPrompt_ContainsPatch(t *testing.T) {
	patch := "@@ -1 +1 @@\n+myUniqueCode123"
	p := bp("file.py", patch, 0, emptyCtx)
	if !strings.Contains(p.User, "myUniqueCode123") {
		t.Error("user prompt should contain the patch content")
	}
}

func TestBuildPrompt_Truncation(t *testing.T) {
	patch := strings.Repeat("+line\n", 1000) // ~6000 chars
	p := bp("file.go", patch, 100, emptyCtx)

	if !p.Truncated {
		t.Error("expected truncation flag to be true")
	}
	if !strings.Contains(p.User, "truncated") {
		t.Error("user prompt should mention truncation when patch was truncated")
	}
}

func TestBuildPrompt_NoTruncation(t *testing.T) {
	patch := "@@ -1 +1 @@\n+small"
	p := bp("file.go", patch, 10_000, emptyCtx)
	if p.Truncated {
		t.Error("expected no truncation for a small patch")
	}
}

func TestBuildPrompt_SystemPromptHasFewShot(t *testing.T) {
	p := bp("file.go", "@@ -1 +1 @@\n+code", 0, emptyCtx)
	if !strings.Contains(p.System, "SQL injection") {
		t.Error("system prompt should contain few-shot SQL injection example")
	}
	if !strings.Contains(p.System, "Resource leak") {
		t.Error("system prompt should contain few-shot resource leak example")
	}
}

func TestBuildPrompt_SystemPromptHasLanguageRules(t *testing.T) {
	p := bp("file.go", "@@ -1 +1 @@\n+code", 0, emptyCtx)
	if !strings.Contains(p.System, "goroutine") {
		t.Error("system prompt should contain Go-specific rules")
	}
}

func TestBuildPrompt_PRContextIncluded(t *testing.T) {
	ctx := PRContext{
		Title:       "Fix auth token expiry bug",
		Description: "This fixes the JWT expiry check",
		RepoName:    "acme/backend",
	}
	p := bp("auth/jwt.go", "@@ -1 +1 @@\n+code", 0, ctx)

	if !strings.Contains(p.User, "Fix auth token expiry bug") {
		t.Error("user prompt should contain PR title")
	}
	if !strings.Contains(p.User, "acme/backend") {
		t.Error("user prompt should contain repo name")
	}
}

func TestBuildPrompt_PRContextEmpty(t *testing.T) {
	p := bp("file.go", "@@ -1 +1 @@\n+code", 0, emptyCtx)
	// Should not crash and should not emit empty context headers
	if strings.Contains(p.User, "PULL REQUEST CONTEXT:") {
		t.Error("should not include PR context section when ctx is empty")
	}
}

func TestBuildPrompt_SeparateSystemAndUser(t *testing.T) {
	p := bp("file.go", "@@ -1 +1 @@\n+code", 0, emptyCtx)
	if p.System == "" {
		t.Error("system prompt should not be empty")
	}
	if p.User == "" {
		t.Error("user prompt should not be empty")
	}
	// System should not contain the diff
	if strings.Contains(p.System, "@@") {
		t.Error("system prompt should not contain the diff — that belongs in user prompt")
	}
	// User should contain the diff
	if !strings.Contains(p.User, "@@") {
		t.Error("user prompt should contain the diff")
	}
}

func TestBuildPrompt_JSONInstructions(t *testing.T) {
	p := bp("file.go", "@@ -1 +1 @@\n+code", 0, emptyCtx)
	for _, required := range []string{"line", "severity", "category", "comment"} {
		if !strings.Contains(p.System, required) {
			t.Errorf("system prompt missing required JSON schema keyword %q", required)
		}
	}
}

func TestLanguageHintForFile_KnownExtensions(t *testing.T) {
	cases := map[string]string{
		"main.go":      "Go",
		"app.ts":       "TypeScript",
		"index.jsx":    "JavaScript (React)",
		"query.sql":    "SQL",
		"deploy.sh":    "Shell",
		"config.yaml":  "YAML",
		"main.rs":      "Rust",
		"Handler.java": "Java",
		"script.py":    "Python",
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
		"Dockerfile":  "Dockerfile",
		"Makefile":    "Makefile",
		"Jenkinsfile": "Groovy (Jenkinsfile)",
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

	out, truncated := TruncatePatch(patch, 1000)
	if truncated || out != patch {
		t.Error("expected no truncation for small patch")
	}

	out, truncated = TruncatePatch(patch, 10)
	if !truncated {
		t.Error("expected truncation")
	}
	if len(out) > 10 {
		t.Errorf("truncated patch too long: %d", len(out))
	}
}

func TestParseJSONComments_ChainOfThought(t *testing.T) {
	// Model emits reasoning before the JSON array
	raw := `Looking at the diff, I can see a potential SQL injection on line 5.

[{"line":5,"severity":"error","category":"security","comment":"SQL injection risk"}]`

	result := parseJSONComments(raw, "file.go", "test")
	if len(result) != 1 {
		t.Fatalf("expected 1 comment from CoT output, got %d", len(result))
	}
	if result[0].Line != 5 {
		t.Errorf("wrong line: %d", result[0].Line)
	}
}

func TestParseJSONComments_FencedWithinCoT(t *testing.T) {
	raw := "Here are my findings:\n```json\n[{\"line\":3,\"severity\":\"warning\",\"category\":\"bug\",\"comment\":\"issue\"}]\n```"
	result := parseJSONComments(raw, "file.go", "test")
	if len(result) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(result))
	}
}

func TestBuildPrompt_IncludesFileBodyAndGuidelines(t *testing.T) {
	p := BuildPrompt(FileAnalysisInput{
		Filename:   "main.go",
		Patch:      "@@ -1 +1 @@\n+code",
		FileBody:   "func main() {}",
		Guidelines: "## CONTRIBUTING.md\nBe kind",
		RepoMap:    "cmd/server/main.go\ninternal/ai/prompt.go",
		PathPrompt: "Return JSON only",
	}, 0)
	if !strings.Contains(p.User, "func main() {}") {
		t.Error("user prompt should include file body")
	}
	if !strings.Contains(p.User, "Be kind") {
		t.Error("user prompt should include guidelines")
	}
	if !strings.Contains(p.User, "internal/ai/prompt.go") {
		t.Error("user prompt should include repo map")
	}
	if !strings.Contains(p.System, "Return JSON only") {
		t.Error("system prompt should include path-specific instructions")
	}
	if !strings.Contains(p.System, `"fix"`) && !strings.Contains(p.System, "fix") {
		t.Error("system prompt should mention fix field")
	}
}

func TestParseJSONComments_FixField(t *testing.T) {
	raw := `[{"line":4,"severity":"error","category":"bug","comment":"missing err check","fix":"if err != nil {\n  return err\n}"}]`
	got := parseJSONComments(raw, "file.go", "test")
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if !strings.Contains(got[0].Fix, "return err") {
		t.Fatalf("fix not parsed: %q", got[0].Fix)
	}
}

func TestFenceLanguage(t *testing.T) {
	if FenceLanguage("internal/foo.go") != "go" {
		t.Fatalf("got %q", FenceLanguage("internal/foo.go"))
	}
	if FenceLanguage("a.ts") != "ts" {
		t.Fatalf("got %q", FenceLanguage("a.ts"))
	}
	if FenceLanguage("x.suggestion") == "suggestion" {
		t.Fatal("must not use suggestion fence")
	}
}
