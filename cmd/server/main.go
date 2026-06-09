package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ai-code-reviewer/ai-code-reviewer/internal/cache"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/config"
	githubhandler "github.com/ai-code-reviewer/ai-code-reviewer/internal/github"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/worker"
)

func main() {
	cfg := config.Load()

	// Shared GitHub client cache — all workers share one instance.
	clientCache := cache.NewClientCache(cfg.AppID, cfg.PrivateKeyPath)

	// noopReviewer is a placeholder until Phase 9 wires in the real reviewer.
	// It satisfies worker.Reviewer so the server boots and handles webhooks today.
	rev := &noopReviewer{cache: clientCache}

	// Worker pool — processes PR events concurrently, one goroutine per worker.
	pool := worker.NewPool(cfg.WorkerCount, rev)
	pool.Start()

	// HTTP routes.
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

	// Start server in a goroutine so the main goroutine can block on signals.
	go func() {
		slog.Info("server listening",
			"port", cfg.Port,
			"env", cfg.Env,
			"ai_provider", cfg.AIProvider,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// Block until SIGINT or SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutdown signal received — draining workers")

	// Give in-flight HTTP requests 10 s to finish.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "err", err)
	}

	// Drain worker pool — wait for all in-flight reviews to complete.
	pool.Stop()

	slog.Info("shutdown complete", "cached_installations", clientCache.Size())
}

// noopReviewer satisfies worker.Reviewer and is replaced in Phase 9.
type noopReviewer struct {
	cache *cache.ClientCache
}

func (n *noopReviewer) Review(ctx context.Context, event githubhandler.PullRequestEvent) error {
	slog.Info("noop reviewer — real reviewer not yet wired",
		"repo", event.Repository.FullName,
		"pr", event.Number,
	)
	return nil
}
