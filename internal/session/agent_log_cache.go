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
	agentLogCache.mu.Lock()
	defer agentLogCache.mu.Unlock()

	agentLogCache.entries = make(map[string]agentLogCacheEntry)
}

func cachedAgentLogCWD(
	cacheKey string,
	fingerprint agentLogFingerprint,
	load func() (string, error),
) (string, error) {
	agentLogCache.mu.Lock()
	entry, ok := agentLogCache.entries[cacheKey]
	agentLogCache.mu.Unlock()

	if ok && entry.fingerprint == fingerprint {
		return entry.cwd, nil
	}

	cwd, err := load()
	if err != nil {
		return "", err
	}

	agentLogCache.mu.Lock()
	agentLogCache.entries[cacheKey] = agentLogCacheEntry{
		cwd:         cwd,
		fingerprint: fingerprint,
	}
	agentLogCache.mu.Unlock()

	return cwd, nil
}

func localFileFingerprint(path string) (agentLogFingerprint, error) {
	info, err := statFileFn(path)
	if err != nil {
		return "", err
	}
	return agentLogFingerprint(fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())), nil
}
