package cache

import (
	"container/list"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/go-github/v62/github"

	githubapp "github.com/ai-code-reviewer/ai-code-reviewer/internal/github"
)

const maxCachedClients = 1000

// entry holds a cached GitHub client alongside its LRU list element.
type entry struct {
	installationID int64
	client         *github.Client
	elem           *list.Element // pointer back into lruList
}

// ClientCache is a thread-safe LRU cache of GitHub installation clients.
//
// Creating a new installation transport is cheap but not free — it allocates a
// new HMAC signer and HTTP transport. More importantly, each transport manages
// its own token refresh goroutine internally, so we want to reuse transports
// across requests for the same installation.
//
// Cap: when more than maxCachedClients installations are active the least-recently-used
// entry is evicted to avoid unbounded memory growth.
type ClientCache struct {
	mu      sync.Mutex
	appID   int64
	keyPath string // file path OR raw PEM content

	clients map[int64]*entry
	lruList *list.List // front = most recently used
}

// NewClientCache creates an empty cache for the given GitHub App credentials.
func NewClientCache(appID int64, privateKeySource string) *ClientCache {
	return &ClientCache{
		appID:   appID,
		keyPath: privateKeySource,
		clients: make(map[int64]*entry, 64),
		lruList: list.New(),
	}
}

// Get returns a cached GitHub client for installationID, creating one if needed.
// Thread-safe.
func (c *ClientCache) Get(installationID int64) (*github.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.clients[installationID]; ok {
		// Move to front — mark as recently used.
		c.lruList.MoveToFront(e.elem)
		return e.client, nil
	}

	// Cache miss — build a new installation client.
	client, err := githubapp.NewInstallationClient(c.appID, installationID, c.keyPath)
	if err != nil {
		return nil, fmt.Errorf("building client for installation %d: %w", installationID, err)
	}

	// Evict LRU entry if at capacity.
	if c.lruList.Len() >= maxCachedClients {
		c.evictLRU()
	}

	e := &entry{
		installationID: installationID,
		client:         client,
	}
	e.elem = c.lruList.PushFront(e)
	c.clients[installationID] = e

	slog.Debug("github client cached", "installation_id", installationID, "cache_size", len(c.clients))
	return client, nil
}

// Invalidate removes a specific installation from the cache.
// Call this if an installation is uninstalled or its credentials rotate.
func (c *ClientCache) Invalidate(installationID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.clients[installationID]; ok {
		c.lruList.Remove(e.elem)
		delete(c.clients, installationID)
		slog.Debug("github client evicted", "installation_id", installationID)
	}
}

// Size returns the current number of cached clients.
func (c *ClientCache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.clients)
}

// evictLRU removes the least-recently-used entry. Must be called with c.mu held.
func (c *ClientCache) evictLRU() {
	back := c.lruList.Back()
	if back == nil {
		return
	}
	e := back.Value.(*entry)
	c.lruList.Remove(back)
	delete(c.clients, e.installationID)
	slog.Warn("github client cache full, evicted LRU entry",
		"evicted_installation_id", e.installationID,
		"cache_size", len(c.clients),
	)
}
