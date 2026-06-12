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
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/storage"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/web"
	"github.com/ai-code-reviewer/ai-code-reviewer/internal/worker"
)

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	cfg := config.Load()

	// ── AI provider (server default) ──────────────────────────────────────────
	provider, err := ai.NewProvider(cfg)
	if err != nil {
		slog.Error("failed to create AI provider", "err", err)
		os.Exit(1)
	}
	slog.Info("AI provider ready", "provider", provider.Name())

	// ── Per-installation store (optional) ─────────────────────────────────────
	// When DATABASE_URL is set, enables per-installation API keys and free-tier
	// usage tracking. When not set, all reviews use the server default provider.
	//
	// instStore is the concrete type (implements both reviewer.Store and web.Store).
	var instStore *storage.Store
	if cfg.DatabaseURL != "" {
		db, err := storage.Open(cfg.DatabaseURL)
		if err != nil {
			slog.Error("failed to connect to database — per-installation config disabled", "err", err)
		} else if cfg.EncryptionKey != "" {
			s, err := storage.NewStore(db, cfg.EncryptionKey)
			if err != nil {
				slog.Error("failed to initialise storage store — per-installation config disabled", "err", err)
			} else {
				instStore = s
				slog.Info("per-installation config enabled (PostgreSQL)")
			}
		} else {
			slog.Warn("ENCRYPTION_KEY not set — per-installation config disabled even though DATABASE_URL is set")
		}
	} else {
		slog.Info("DATABASE_URL not set — all reviews use server default provider")
	}

	// ── GitHub client cache ───────────────────────────────────────────────────
	clientCache := cache.NewClientCache(cfg.AppID, cfg.PrivateKeyPath)

	// ── Reviewer ──────────────────────────────────────────────────────────────
	// Only assign the interface when the concrete store is non-nil.
	// Passing a typed nil (*storage.Store) as reviewer.Store would produce a
	// non-nil interface value, causing a nil-pointer panic inside resolveProvider.
	var revStore reviewer.Store
	if instStore != nil {
		revStore = instStore
	}
	rev := reviewer.New(clientCache, provider, cfg, revStore)

	// ── Worker pool ───────────────────────────────────────────────────────────
	pool := worker.NewPool(cfg.WorkerCount, rev, cfg.ReviewTimeoutSeconds)
	pool.Start()

	// ── HTTP server ───────────────────────────────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", githubhandler.NewWebhookHandler(cfg.WebhookSecret, pool))
	mux.HandleFunc("/health", githubhandler.NewHealthHandler())

	// Setup page — users land here after installing the GitHub App.
	// Only available when the store is configured.
	if instStore != nil {
		mux.HandleFunc("/setup", web.SetupHandler(instStore))
		slog.Info("setup page available at /setup")
	}

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

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("HTTP server shutdown error", "err", err)
	}

	pool.Stop()

	slog.Info("shutdown complete",
		"cached_installations", clientCache.Size(),
	)
}
