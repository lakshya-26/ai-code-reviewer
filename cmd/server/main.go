package main

import (
	"log/slog"

	"github.com/ai-code-reviewer/ai-code-reviewer/internal/config"
)

func main() {
	cfg := config.Load()

	slog.Info("ai-code-reviewer starting",
		"port", cfg.Port,
		"env", cfg.Env,
		"ai_provider", cfg.AIProvider,
	)

	// Phases 2-9 will wire in the server, worker pool, and all components here.
}
