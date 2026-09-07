package github

import "testing"

func TestCheckConclusion_CleanReview(t *testing.T) {
	if got := CheckConclusion(nil, 0); got != "success" {
		t.Errorf("clean review: got %q, want success", got)
	}
}

func TestCheckConclusion_IssuesFound(t *testing.T) {
	if got := CheckConclusion(nil, 3); got != "neutral" {
		t.Errorf("issues found: got %q, want neutral", got)
	}
}

func TestCheckConclusion_PipelineError(t *testing.T) {
	if got := CheckConclusion(errSentinel{}, 0); got != "neutral" {
		t.Errorf("pipeline error: got %q, want neutral", got)
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "boom" }
