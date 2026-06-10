package ai

import (
	"fmt"
	"path/filepath"
	"strings"
)

// maxPatchCharsDefault is a fallback truncation limit when the caller does not
// provide one. Callers (the orchestrator) pass the config value instead.
const maxPatchCharsDefault = 10_000

// BuildPrompt constructs the complete prompt sent to the AI model for one file.
//
// Design principles:
//   - Explicit JSON schema prevents the model from returning prose
//   - "Only added lines" constraint focuses review on what changed
//   - Low-confidence issues are explicitly banned to reduce noise
//   - Style/formatting feedback is banned — this is not a linter
//   - Language context helps the model apply language-specific rules
func BuildPrompt(filename, patch string, maxPatchChars int) (prompt string, truncated bool) {
	if maxPatchChars <= 0 {
		maxPatchChars = maxPatchCharsDefault
	}

	truncationNote := ""
	if len(patch) > maxPatchChars {
		patch = patch[:maxPatchChars]
		truncated = true
		truncationNote = "\nNote: This diff was truncated due to size. Review only what is visible.\n"
	}

	ext := filepath.Ext(filename)
	lang := languageHint(ext)

	prompt = fmt.Sprintf(`You are a senior software engineer performing a thorough code review.

Language: %s
File: %s
%s
Diff (lines starting with + are additions to review):
%s

INSTRUCTIONS:
- Review ONLY the added lines (lines starting with +).
- Do NOT comment on removed lines (-) or unchanged context lines ( ).
- Do NOT comment on code style, indentation, naming conventions, or formatting.
- Do NOT suggest adding comments, documentation, or TODOs.
- Only flag issues you are CONFIDENT are real problems.
- Each comment MUST reference a specific line number from the added lines.
- If you find no real issues, return an empty array.

CHECK FOR:
- Bugs: logic errors, off-by-one, nil/null dereference, incorrect conditionals, unchecked errors
- Security: SQL injection, XSS, hardcoded credentials, insecure random, improper auth, path traversal
- Performance: N+1 queries, unnecessary allocations in loops, missing bounds checks, blocking calls in hot paths
- Code smell: duplicated logic, deeply nested conditionals, misleading variable names, dead code
- Best practices: unhandled errors, missing input validation, resource leaks (unclosed files/connections/goroutines)

OUTPUT FORMAT:
Return a JSON array only. No explanation text. No markdown. No code fences. Just the raw JSON array.

Schema:
[
  {
    "line": <integer — exact line number in the new file>,
    "severity": "error" | "warning" | "suggestion",
    "category": "bug" | "security" | "performance" | "code-smell" | "best-practice",
    "comment": "<concise explanation of the problem and how to fix it>"
  }
]

If there are no issues, return exactly: []`,
		lang,
		filename,
		truncationNote,
		patch,
	)

	return prompt, truncated
}

// TruncatePatch shortens a patch to maxChars and returns the truncated patch
// and a flag. Used by the orchestrator before calling BuildPrompt.
func TruncatePatch(patch string, maxChars int) (string, bool) {
	if maxChars <= 0 || len(patch) <= maxChars {
		return patch, false
	}
	// Truncate at a newline boundary if possible to avoid cutting mid-line.
	cut := patch[:maxChars]
	if idx := strings.LastIndex(cut, "\n"); idx > maxChars/2 {
		cut = cut[:idx]
	}
	return cut, true
}

// languageHint maps a file extension to a natural language name that helps
// the model apply language-specific rules (e.g. Go error handling, SQL injection in JS).
func languageHint(ext string) string {
	hints := map[string]string{
		".go":    "Go",
		".js":    "JavaScript",
		".jsx":   "JavaScript (React)",
		".ts":    "TypeScript",
		".tsx":   "TypeScript (React)",
		".py":    "Python",
		".java":  "Java",
		".kt":    "Kotlin",
		".scala": "Scala",
		".rb":    "Ruby",
		".rs":    "Rust",
		".cpp":   "C++",
		".cc":    "C++",
		".cxx":   "C++",
		".c":     "C",
		".h":     "C/C++ Header",
		".hpp":   "C++ Header",
		".cs":    "C#",
		".swift": "Swift",
		".php":   "PHP",
		".sql":   "SQL",
		".sh":    "Shell",
		".bash":  "Bash",
		".zsh":   "Zsh",
		".yaml":  "YAML",
		".yml":   "YAML",
		".json":  "JSON",
		".toml":  "TOML",
		".tf":    "Terraform (HCL)",
		".hcl":   "HCL",
		".proto": "Protocol Buffers",
		".graphql": "GraphQL",
		".gql":   "GraphQL",
		".html":  "HTML",
		".css":   "CSS",
		".scss":  "SCSS",
		".lua":   "Lua",
		".ex":    "Elixir",
		".exs":   "Elixir",
		".erl":   "Erlang",
		".clj":   "Clojure",
		".hs":    "Haskell",
		".r":     "R",
		".dart":  "Dart",
		".vue":   "Vue",
		".svelte": "Svelte",
		".md":    "Markdown",
		".dockerfile": "Dockerfile",
	}

	lower := strings.ToLower(ext)
	if lang, ok := hints[lower]; ok {
		return lang
	}

	// Dockerfile has no extension — check the base name separately.
	return "Unknown"
}

// LanguageHintForFile returns the language hint for a full filename.
// Handles special cases like "Dockerfile" which has no extension.
func LanguageHintForFile(filename string) string {
	base := strings.ToLower(filepath.Base(filename))

	// Handle extension-less special files.
	switch base {
	case "dockerfile":
		return "Dockerfile"
	case "makefile", "gnumakefile":
		return "Makefile"
	case "jenkinsfile":
		return "Groovy (Jenkinsfile)"
	}

	ext := filepath.Ext(filename)
	return languageHint(ext)
}
