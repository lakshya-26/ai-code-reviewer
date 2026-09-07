package filter

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPathPromptFor_DoubleStarPrefix(t *testing.T) {
	cfg := &RepoConfig{
		PathPrompts: []PathPrompt{
			{Path: "internal/ai/**", Prompt: "Return JSON only"},
			{Path: "*.go", Prompt: "Go specific"},
		},
	}
	if got := PathPromptFor("internal/ai/prompt.go", cfg); got != "Return JSON only" {
		t.Fatalf("got %q", got)
	}
	if got := PathPromptFor("internal/filter/file.go", cfg); got != "Go specific" {
		t.Fatalf("basename *.go: got %q", got)
	}
	if got := PathPromptFor("README.md", cfg); got != "" {
		t.Fatalf("no match should be empty, got %q", got)
	}
}

func TestPathPromptFor_FirstMatchWins(t *testing.T) {
	cfg := &RepoConfig{
		PathPrompts: []PathPrompt{
			{Path: "internal/ai/**", Prompt: "first"},
			{Path: "internal/ai/**", Prompt: "second"},
		},
	}
	if got := PathPromptFor("internal/ai/x.go", cfg); got != "first" {
		t.Fatalf("got %q", got)
	}
}

func TestRepoConfig_PathPromptsYAML(t *testing.T) {
	raw := []byte(`
skip_drafts: true
path_prompts:
  - path: "internal/ai/**"
    prompt: "Return JSON only; do not flag style."
`)
	cfg := DefaultRepoConfig()
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.PathPrompts) != 1 {
		t.Fatalf("len=%d", len(cfg.PathPrompts))
	}
	if cfg.PathPrompts[0].Prompt == "" {
		t.Fatal("prompt empty")
	}
	if PathPromptFor("internal/ai/provider.go", cfg) == "" {
		t.Fatal("should match after unmarshal")
	}
}

func TestPathPromptFor_NilConfig(t *testing.T) {
	if PathPromptFor("x.go", nil) != "" {
		t.Fatal("nil config")
	}
}
