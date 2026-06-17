package session

import (
	"fmt"
	"os"
	"sync"
)

type agentLogFingerprint string

type agentLogCacheEntry struct {
	cwd         string
	fingerprint agentLogFingerprint
}

type agentLogCacheStore struct {
	entries map[string]agentLogCacheEntry
	mu      sync.Mutex
}

var agentLogCache = agentLogCacheStore{
	entries: make(map[string]agentLogCacheEntry),
}

var statFileFn = os.Stat

func resetAgentLogCache() {
	agentLogCache.reset()
}

func cachedAgentLogCWD(
	cacheKey string,
	fingerprint agentLogFingerprint,
	load func() (string, error),
) (string, error) {
	entry, ok := agentLogCache.load(cacheKey)
	if ok && entry.fingerprint == fingerprint {
		return entry.cwd, nil
	}

	cwd, err := load()
	if err != nil {
		return "", err
	}

	agentLogCache.store(cacheKey, agentLogCacheEntry{
		cwd:         cwd,
		fingerprint: fingerprint,
	})

	return cwd, nil
}

func localFileFingerprint(path string) (agentLogFingerprint, error) {
	info, err := statFileFn(path)
	if err != nil {
		return "", err
	}
	return agentLogFingerprint(fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())), nil
}

func (c *agentLogCacheStore) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]agentLogCacheEntry)
}

func (c *agentLogCacheStore) load(cacheKey string) (agentLogCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[cacheKey]
	return entry, ok
}

func (c *agentLogCacheStore) store(cacheKey string, entry agentLogCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[cacheKey] = entry
}
