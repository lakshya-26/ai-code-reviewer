package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	githubmodels "github.com/ai-code-reviewer/ai-code-reviewer/internal/github"
)

// reviewTimeout is the maximum time allowed to fully process one PR.
// A review involves fetching files, calling the AI per file, and posting results.
const reviewTimeout = 5 * time.Minute

// jobQueueBuffer is how many PR events can be queued before Submit starts
// dropping. This absorbs bursts without blocking the webhook HTTP handler.
const jobQueueBuffer = 100

// Reviewer is the interface the Pool calls for each PR event.
// reviewer.Reviewer implements this — the interface lives here to avoid an
// import cycle between worker and reviewer packages.
type Reviewer interface {
	Review(ctx context.Context, event githubmodels.PullRequestEvent) error
}

// Pool is a fixed-size goroutine pool that processes PR review jobs.
//
// Design choices:
//   - Fixed worker count — no goroutine per event, bounded concurrency
//   - Buffered channel — absorbs bursts, non-blocking Submit
//   - Drop on full queue — better to miss a review than to block the webhook
//   - 5-minute context per job — prevents a stuck AI call from leaking a goroutine
type Pool struct {
	jobs     chan githubmodels.PullRequestEvent
	workers  int
	reviewer Reviewer
	wg       sync.WaitGroup
}

// NewPool creates a worker pool. Call Start() to launch the goroutines.
func NewPool(workerCount int, reviewer Reviewer) *Pool {
	return &Pool{
		jobs:     make(chan githubmodels.PullRequestEvent, jobQueueBuffer),
		workers:  workerCount,
		reviewer: reviewer,
	}
}

// Start launches the worker goroutines. Should be called once at startup.
func (p *Pool) Start() {
	for i := range p.workers {
		p.wg.Add(1)
		go p.runWorker(i)
	}
	slog.Info("worker pool started", "workers", p.workers, "queue_buffer", jobQueueBuffer)
}

// Submit enqueues a PR event for processing. Non-blocking — drops the event
// and logs a warning if the queue is full.
func (p *Pool) Submit(event githubmodels.PullRequestEvent) {
	select {
	case p.jobs <- event:
	default:
		slog.Warn("worker queue full — PR event dropped",
			"repo", event.Repository.FullName,
			"pr", event.Number,
		)
	}
}

// Stop drains the queue and waits for all in-flight jobs to complete.
// Should be called on graceful shutdown.
func (p *Pool) Stop() {
	close(p.jobs)
	p.wg.Wait()
	slog.Info("worker pool stopped")
}

// runWorker is the body of each worker goroutine.
func (p *Pool) runWorker(id int) {
	defer p.wg.Done()
	slog.Debug("worker started", "id", id)

	for event := range p.jobs {
		p.processEvent(id, event)
	}

	slog.Debug("worker stopped", "id", id)
}

func (p *Pool) processEvent(workerID int, event githubmodels.PullRequestEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
	defer cancel()

	start := time.Now()
	log := slog.With(
		"worker", workerID,
		"repo", event.Repository.FullName,
		"pr", event.Number,
	)

	log.Info("review started")

	if err := p.reviewer.Review(ctx, event); err != nil {
		log.Error("review failed", "err", err, "elapsed", time.Since(start).Round(time.Millisecond))
		return
	}

	log.Info("review completed", "elapsed", time.Since(start).Round(time.Millisecond))
}
