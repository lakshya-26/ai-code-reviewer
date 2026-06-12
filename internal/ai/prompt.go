package ai

import (
	"fmt"
	"path/filepath"
	"strings"
)

const maxPatchCharsDefault = 10_000

// ─── Prompt entry point ───────────────────────────────────────────────────────

// BuiltPrompt holds the system and user messages separately so each provider
// can place them in the correct field of its API request format.
type BuiltPrompt struct {
	System    string
	User      string
	Truncated bool
}

// BuildPrompt constructs the full system + user prompt for one file review.
func BuildPrompt(filename, patch string, maxPatchChars int, prCtx PRContext) BuiltPrompt {
	if maxPatchChars <= 0 {
		maxPatchChars = maxPatchCharsDefault
	}

	truncated := false
	if len(patch) > maxPatchChars {
		patch, truncated = TruncatePatch(patch, maxPatchChars)
	}

	return BuiltPrompt{
		System:    buildSystemPrompt(),
		User:      buildUserPrompt(filename, patch, truncated, prCtx),
		Truncated: truncated,
	}
}

// ─── System prompt ────────────────────────────────────────────────────────────

func buildSystemPrompt() string {
	return `You are a senior software engineer with 10+ years of experience conducting thorough code reviews across multiple languages and systems. Your reviews are precise, actionable, and focused only on real problems — not style preferences.

CORE RULES:
- Review ONLY added lines (lines starting with + in the diff). Never comment on removed lines (-) or unchanged context lines.
- Only flag issues you are CONFIDENT are real bugs, vulnerabilities, or significant problems.
- Do NOT comment on: code style, formatting, naming conventions, indentation, missing comments, or documentation.
- Every comment must reference the exact line number of the added line where the issue occurs.
- Be concise and specific — explain the problem AND how to fix it in one or two sentences.
- If you find no real issues, return an empty array. Silence is better than noise.

WHAT TO CHECK:
Bug: Logic errors, off-by-one errors, nil/null dereference, incorrect conditionals, unhandled errors, wrong operator usage, unreachable code, incorrect loop bounds.
Security: SQL injection, XSS, hardcoded credentials/secrets, insecure random number generation, improper authentication checks, path traversal, command injection, insecure deserialization, SSRF, open redirects.
Performance: N+1 query patterns, unnecessary allocations inside loops, missing pagination on unbounded queries, blocking calls in hot paths, missing indexes hinted by query patterns, quadratic complexity where linear is possible.
Code-smell: Deeply nested conditionals (3+ levels), duplicated logic that should be extracted, dead code, misleading variable names that invert meaning, functions doing more than one thing.
Best-practice: Unchecked errors, missing input validation on user-supplied data, resource leaks (unclosed files, DB connections, goroutines, HTTP response bodies), missing context propagation, incorrect use of concurrency primitives.

LANGUAGE-SPECIFIC RULES (apply when relevant):
Go: Always check error return values. defer close() for resources. Use context.Context for cancellations. Avoid goroutine leaks — ensure goroutines have an exit condition. Never write to a nil map.
JavaScript/TypeScript: Avoid == (use ===). Check for prototype pollution. Async functions should have try/catch or .catch(). Never use eval() with user input.
Python: Use parameterized queries, never string formatting for SQL. Check for mutable default arguments. Handle exceptions specifically, not bare except.
SQL: Parameterized queries only — never string concatenation. Check for missing WHERE clause on UPDATE/DELETE. Check for missing LIMIT on SELECT.
Java/Kotlin: Always close streams in finally or use try-with-resources. Check for NullPointerException risks. Avoid catching Exception broadly.

FEW-SHOT EXAMPLES:

Example 1 — SQL injection (security, error):
Diff:
+    query := "SELECT * FROM users WHERE id = " + userID
+    rows, err := db.Query(query)
Output:
[{"line":1,"severity":"error","category":"security","comment":"SQL injection — userID is concatenated directly into the query. An attacker can manipulate the SQL statement. Use a parameterized query: db.Query(\"SELECT * FROM users WHERE id = $1\", userID)"}]

Example 2 — Resource leak (best-practice, warning):
Diff:
+    resp, err := http.Get(url)
+    if err != nil { return err }
+    body, _ := io.ReadAll(resp.Body)
Output:
[{"line":1,"severity":"warning","category":"best-practice","comment":"HTTP response body is never closed — this leaks the underlying TCP connection. Add: defer resp.Body.Close() immediately after the nil check on err."}]

Example 3 — No issues found:
Diff:
+    func Add(a, b int) int {
+        return a + b
+    }
Output:
[]

OUTPUT FORMAT:
Return a JSON array only. No explanation. No markdown. No code fences. Just raw JSON.
Schema: [{"line":<integer>,"severity":"error"|"warning"|"suggestion","category":"bug"|"security"|"performance"|"code-smell"|"best-practice","comment":"<explanation and fix>"}]`
}

// ─── User prompt ──────────────────────────────────────────────────────────────

func buildUserPrompt(filename, patch string, truncated bool, prCtx PRContext) string {
	var sb strings.Builder

	// PR context — helps the model understand the intent of the change.
	if prCtx.Title != "" || prCtx.RepoName != "" {
		sb.WriteString("PULL REQUEST CONTEXT:\n")
		if prCtx.RepoName != "" {
			fmt.Fprintf(&sb, "Repository: %s\n", prCtx.RepoName)
		}
		if prCtx.Title != "" {
			fmt.Fprintf(&sb, "PR Title: %s\n", prCtx.Title)
		}
		if prCtx.Description != "" {
			desc := prCtx.Description
			if len(desc) > 500 {
				desc = desc[:500] + "... (truncated)"
			}
			fmt.Fprintf(&sb, "PR Description: %s\n", desc)
		}
		sb.WriteString("\n")
	}

	// File context.
	lang := LanguageHintForFile(filename)
	fmt.Fprintf(&sb, "FILE: %s\nLANGUAGE: %s\n", filename, lang)

	if truncated {
		sb.WriteString("NOTE: This diff was truncated due to size. Review only what is shown.\n")
	}

	sb.WriteString("\nDIFF (lines starting with + are additions — review only these):\n")
	sb.WriteString(patch)

	sb.WriteString("\n\nReview the added lines above. Think briefly about each potential issue, then output the JSON array.")

	return sb.String()
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// TruncatePatch shortens a patch to maxChars, cutting at a newline boundary.
func TruncatePatch(patch string, maxChars int) (string, bool) {
	if maxChars <= 0 || len(patch) <= maxChars {
		return patch, false
	}
	cut := patch[:maxChars]
	if idx := strings.LastIndex(cut, "\n"); idx > maxChars/2 {
		cut = cut[:idx]
	}
	return cut, true
}

// languageHint maps a file extension to a natural language name.
func languageHint(ext string) string {
	hints := map[string]string{
		".go":      "Go",
		".js":      "JavaScript",
		".jsx":     "JavaScript (React)",
		".ts":      "TypeScript",
		".tsx":     "TypeScript (React)",
		".py":      "Python",
		".java":    "Java",
		".kt":      "Kotlin",
		".scala":   "Scala",
		".rb":      "Ruby",
		".rs":      "Rust",
		".cpp":     "C++",
		".cc":      "C++",
		".cxx":     "C++",
		".c":       "C",
		".h":       "C/C++ Header",
		".hpp":     "C++ Header",
		".cs":      "C#",
		".swift":   "Swift",
		".php":     "PHP",
		".sql":     "SQL",
		".sh":      "Shell",
		".bash":    "Bash",
		".zsh":     "Zsh",
		".yaml":    "YAML",
		".yml":     "YAML",
		".json":    "JSON",
		".toml":    "TOML",
		".tf":      "Terraform (HCL)",
		".hcl":     "HCL",
		".proto":   "Protocol Buffers",
		".graphql": "GraphQL",
		".gql":     "GraphQL",
		".html":    "HTML",
		".css":     "CSS",
		".scss":    "SCSS",
		".lua":     "Lua",
		".ex":      "Elixir",
		".exs":     "Elixir",
		".erl":     "Erlang",
		".clj":     "Clojure",
		".hs":      "Haskell",
		".r":       "R",
		".dart":    "Dart",
		".vue":     "Vue",
		".svelte":  "Svelte",
		".md":      "Markdown",
	}
	if lang, ok := hints[strings.ToLower(ext)]; ok {
		return lang
	}
	return "Unknown"
}

// LanguageHintForFile returns the language for a full filename,
// handling extension-less special files like Dockerfile and Makefile.
func LanguageHintForFile(filename string) string {
	base := strings.ToLower(filepath.Base(filename))
	switch base {
	case "dockerfile":
		return "Dockerfile"
	case "makefile", "gnumakefile":
		return "Makefile"
	case "jenkinsfile":
		return "Groovy (Jenkinsfile)"
	}
	return languageHint(filepath.Ext(filename))
}
