package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ai-code-reviewer/ai-code-reviewer/internal/ai"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/cache"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/config"
	githubhandler "github.com/ai-code-reviewer/ai-code-reviewer/internal/github"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/reviewer"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/worker"
)

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	cfg := config.Load()

	// ── AI provider ───────────────────────────────────────────────────────────
	// NewProvider reads cfg.AIProvider and returns the right implementation.
	// It fails fast if the required API key is missing.
	provider, err := ai.NewProvider(cfg)
	if err != nil {
		slog.Error("failed to create AI provider", "err", err)
		os.Exit(1)
	}
	slog.Info("AI provider ready", "provider", provider.Name())

	// ── GitHub client cache ───────────────────────────────────────────────────
	clientCache := cache.NewClientCache(cfg.AppID, cfg.PrivateKeyPath)

	// ── Reviewer ──────────────────────────────────────────────────────────────
	rev := reviewer.New(clientCache, provider, cfg)

	// ── Worker pool ───────────────────────────────────────────────────────────
	pool := worker.NewPool(cfg.WorkerCount, rev)
	pool.Start()

	// ── HTTP server ───────────────────────────────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", githubhandler.NewWebhookHandler(cfg.WebhookSecret, pool))
	mux.HandleFunc("/health", githubhandler.NewHealthHandler())

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("server listening",
			"port", cfg.Port,
			"env", cfg.Env,
			"ai_provider", cfg.AIProvider,
			"workers", cfg.WorkerCount,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutdown signal received — draining in-flight reviews")

	// Stop accepting new HTTP requests (10 s grace period).
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("HTTP server shutdown error", "err", err)
	}

	// Drain the worker pool — wait for all in-flight PR reviews to finish.
	pool.Stop()

	slog.Info("shutdown complete",
		"cached_installations", clientCache.Size(),
	)
}
