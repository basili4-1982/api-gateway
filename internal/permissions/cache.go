package permissions

import (
	"sync"
	"time"
)

type Cache struct {
	mu    sync.RWMutex
	store map[int]*cacheEntry
	ttl   time.Duration
}

type cacheEntry struct {
	permissions *UserPermissions
	expiresAt   time.Time
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		store: make(map[int]*cacheEntry),
		ttl:   ttl,
	}
}

func (c *Cache) Get(userID int) (*UserPermissions, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.store[userID]
	if !ok {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return entry.permissions, true
}

func (c *Cache) Set(userID int, permissions *UserPermissions) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.store[userID] = &cacheEntry{
		permissions: permissions,
		expiresAt:   time.Now().Add(c.ttl),
	}
}

func (c *Cache) Invalidate(userID int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.store, userID)
}

func (c *Cache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.store = make(map[int]*cacheEntry)
}
