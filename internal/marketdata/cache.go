package marketdata

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// pulseCacheTTL：数据本身延迟 15min 级，60s 内重复请求无新信息。
const pulseCacheTTL = 60 * time.Second

type cachedSnap struct {
	snap   *PulseSnapshot
	expiry time.Time
}

type pulseCache struct {
	mu    sync.RWMutex
	byKey map[string]cachedSnap
	sf    singleflight.Group
}

func newPulseCache() *pulseCache {
	return &pulseCache{byKey: map[string]cachedSnap{}}
}

func (c *pulseCache) get(key string) *PulseSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.byKey[key]
	if !ok || time.Now().After(e.expiry) {
		return nil
	}
	return e.snap
}

func (c *pulseCache) set(key string, s *PulseSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byKey[key] = cachedSnap{snap: s, expiry: time.Now().Add(pulseCacheTTL)}
}
