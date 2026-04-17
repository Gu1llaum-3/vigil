package agent

import (
	"sync"
	"time"
)

// dataCache is a simple TTL cache keyed by a uint16 interval in milliseconds.
type dataCache struct {
	sync.RWMutex
	cache map[uint16]*cacheNode
}

type cacheNode struct {
	data       any
	lastUpdate time.Time
}

// newDataCache creates a cache keyed by the polling interval in milliseconds.
func newDataCache() *dataCache {
	return &dataCache{
		cache: make(map[uint16]*cacheNode),
	}
}

// Get returns cached data when the entry is still considered fresh.
func (c *dataCache) Get(cacheTimeMs uint16) (data any, isCached bool) {
	c.RLock()
	defer c.RUnlock()

	node, ok := c.cache[cacheTimeMs]
	if !ok {
		return nil, false
	}
	isFresh := time.Since(node.lastUpdate) < time.Duration(cacheTimeMs/2)*time.Millisecond
	return node.data, isFresh
}

// Set stores the latest data snapshot for the given interval.
func (c *dataCache) Set(data any, cacheTimeMs uint16) {
	c.Lock()
	defer c.Unlock()

	node, ok := c.cache[cacheTimeMs]
	if !ok {
		node = &cacheNode{}
		c.cache[cacheTimeMs] = node
	}
	node.data = data
	node.lastUpdate = time.Now()
}
