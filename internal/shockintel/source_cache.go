package shockintel

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/IS908/optix/pkg/model"
	"golang.org/x/sync/singleflight"
)

type ttlMarketCache struct {
	inner MarketSource
	ttl   time.Duration

	sf singleflight.Group
	mu sync.Mutex

	quotes       map[string]ShockQuote
	quoteFetched map[string]bool
	quoteErr     error
	quoteExpiry  time.Time

	depth        map[depthCacheKey]DepthSnapshot
	depthFetched map[depthCacheKey]bool
	depthErr     error
	depthExpiry  time.Time

	options       map[string]OptionStress
	optionFetched map[string]bool
	optionErr     error
	optionExpiry  time.Time
}

func newTTLMarketCache(inner MarketSource, ttl time.Duration) *ttlMarketCache {
	return &ttlMarketCache{inner: inner, ttl: ttl}
}

func (c *ttlMarketCache) Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error) {
	ids = uniqueStrings(ids)
	now := time.Now()
	missing := c.missingTTLQuoteIDs(ids, now)
	if len(missing) > 0 {
		v, err, _ := c.sf.Do("quotes:"+strings.Join(missing, ","), func() (any, error) {
			return c.inner.Quotes(ctx, missing)
		})
		var quotes map[string]ShockQuote
		if v != nil {
			quotes = v.(map[string]ShockQuote)
		}
		c.storeTTLQuotes(missing, quotes, err, now)
	}
	return c.cachedTTLQuotes(ids, now)
}

func (c *ttlMarketCache) Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error) {
	return c.inner.Bars(ctx, ids, interval, lookback)
}

func (c *ttlMarketCache) Depth(ctx context.Context, ids []string, levels int) (map[string]DepthSnapshot, error) {
	ids = uniqueStrings(ids)
	now := time.Now()
	missing := c.missingTTLDepthIDs(ids, levels, now)
	if len(missing) > 0 {
		v, err, _ := c.sf.Do(depthSFKey(missing, levels), func() (any, error) {
			return c.inner.Depth(ctx, missing, levels)
		})
		var depth map[string]DepthSnapshot
		if v != nil {
			depth = v.(map[string]DepthSnapshot)
		}
		c.storeTTLDepth(missing, levels, depth, err, now)
	}
	return c.cachedTTLDepth(ids, levels, now)
}

func (c *ttlMarketCache) OptionMetrics(ctx context.Context, underlyings []string) (map[string]OptionStress, error) {
	underlyings = uniqueStrings(underlyings)
	now := time.Now()
	missing := c.missingTTLOptions(underlyings, now)
	if len(missing) > 0 {
		v, err, _ := c.sf.Do("options:"+strings.Join(missing, ","), func() (any, error) {
			return c.inner.OptionMetrics(ctx, missing)
		})
		var options map[string]OptionStress
		if v != nil {
			options = v.(map[string]OptionStress)
		}
		c.storeTTLOptions(missing, options, err, now)
	}
	return c.cachedTTLOptions(underlyings, now)
}

func (c *ttlMarketCache) missingTTLQuoteIDs(ids []string, now time.Time) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.After(c.quoteExpiry) {
		c.quotes = map[string]ShockQuote{}
		c.quoteFetched = map[string]bool{}
		c.quoteErr = nil
	}
	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if !c.quoteFetched[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func (c *ttlMarketCache) storeTTLQuotes(ids []string, quotes map[string]ShockQuote, err error, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.quotes == nil {
		c.quotes = map[string]ShockQuote{}
	}
	if c.quoteFetched == nil {
		c.quoteFetched = map[string]bool{}
	}
	for _, id := range ids {
		c.quoteFetched[id] = true
	}
	for id, quote := range quotes {
		c.quotes[id] = quote
	}
	c.quoteErr = err
	c.quoteExpiry = now.Add(c.ttl)
}

func (c *ttlMarketCache) cachedTTLQuotes(ids []string, now time.Time) (map[string]ShockQuote, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.After(c.quoteExpiry) {
		return map[string]ShockQuote{}, nil
	}
	out := make(map[string]ShockQuote, len(ids))
	for _, id := range ids {
		if quote, ok := c.quotes[id]; ok {
			out[id] = quote
		}
	}
	return out, c.quoteErr
}

func (c *ttlMarketCache) missingTTLDepthIDs(ids []string, levels int, now time.Time) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.After(c.depthExpiry) {
		c.depth = map[depthCacheKey]DepthSnapshot{}
		c.depthFetched = map[depthCacheKey]bool{}
		c.depthErr = nil
	}
	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if !c.depthFetched[depthCacheKey{id: id, levels: levels}] {
			missing = append(missing, id)
		}
	}
	return missing
}

func (c *ttlMarketCache) storeTTLDepth(ids []string, levels int, depth map[string]DepthSnapshot, err error, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.depth == nil {
		c.depth = map[depthCacheKey]DepthSnapshot{}
	}
	if c.depthFetched == nil {
		c.depthFetched = map[depthCacheKey]bool{}
	}
	for _, id := range ids {
		c.depthFetched[depthCacheKey{id: id, levels: levels}] = true
	}
	for id, snapshot := range depth {
		c.depth[depthCacheKey{id: id, levels: levels}] = snapshot
	}
	c.depthErr = err
	c.depthExpiry = now.Add(c.ttl)
}

func (c *ttlMarketCache) cachedTTLDepth(ids []string, levels int, now time.Time) (map[string]DepthSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.After(c.depthExpiry) {
		return map[string]DepthSnapshot{}, nil
	}
	out := make(map[string]DepthSnapshot, len(ids))
	for _, id := range ids {
		if snapshot, ok := c.depth[depthCacheKey{id: id, levels: levels}]; ok {
			out[id] = snapshot
		}
	}
	return out, c.depthErr
}

func (c *ttlMarketCache) missingTTLOptions(underlyings []string, now time.Time) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.After(c.optionExpiry) {
		c.options = map[string]OptionStress{}
		c.optionFetched = map[string]bool{}
		c.optionErr = nil
	}
	missing := make([]string, 0, len(underlyings))
	for _, underlying := range underlyings {
		if !c.optionFetched[underlying] {
			missing = append(missing, underlying)
		}
	}
	return missing
}

func (c *ttlMarketCache) storeTTLOptions(underlyings []string, options map[string]OptionStress, err error, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.options == nil {
		c.options = map[string]OptionStress{}
	}
	if c.optionFetched == nil {
		c.optionFetched = map[string]bool{}
	}
	for _, underlying := range underlyings {
		c.optionFetched[underlying] = true
	}
	for underlying, stress := range options {
		c.options[underlying] = stress
	}
	c.optionErr = err
	c.optionExpiry = now.Add(c.ttl)
}

func (c *ttlMarketCache) cachedTTLOptions(underlyings []string, now time.Time) (map[string]OptionStress, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.After(c.optionExpiry) {
		return map[string]OptionStress{}, nil
	}
	out := make(map[string]OptionStress, len(underlyings))
	for _, underlying := range underlyings {
		if stress, ok := c.options[underlying]; ok {
			out[underlying] = stress
		}
	}
	return out, c.optionErr
}

func depthSFKey(ids []string, levels int) string {
	return "depth:" + strings.Join(ids, ",") + ":" + strconv.Itoa(levels)
}

type bundleMarketCache struct {
	inner MarketSource

	mu            sync.Mutex
	quotes        map[string]ShockQuote
	quoteFetched  map[string]bool
	quoteErr      error
	depth         map[depthCacheKey]DepthSnapshot
	depthFetched  map[depthCacheKey]bool
	depthErr      error
	options       map[string]OptionStress
	optionFetched map[string]bool
	optionErr     error
}

type depthCacheKey struct {
	id     string
	levels int
}

func newBundleMarketCache(inner MarketSource) *bundleMarketCache {
	return &bundleMarketCache{
		inner:         inner,
		quotes:        map[string]ShockQuote{},
		quoteFetched:  map[string]bool{},
		depth:         map[depthCacheKey]DepthSnapshot{},
		depthFetched:  map[depthCacheKey]bool{},
		options:       map[string]OptionStress{},
		optionFetched: map[string]bool{},
	}
}

func (c *bundleMarketCache) Quotes(ctx context.Context, ids []string) (map[string]ShockQuote, error) {
	ids = uniqueStrings(ids)
	missing := c.missingQuoteIDs(ids)
	if len(missing) > 0 {
		quotes, err := c.inner.Quotes(ctx, missing)
		c.mu.Lock()
		for _, id := range missing {
			c.quoteFetched[id] = true
		}
		for id, quote := range quotes {
			c.quotes[id] = quote
		}
		if err != nil {
			c.quoteErr = err
		}
		c.mu.Unlock()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]ShockQuote, len(ids))
	for _, id := range ids {
		if quote, ok := c.quotes[id]; ok {
			out[id] = quote
		}
	}
	return out, c.quoteErr
}

func (c *bundleMarketCache) missingQuoteIDs(ids []string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if !c.quoteFetched[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func (c *bundleMarketCache) Bars(ctx context.Context, ids []string, interval string, lookback time.Duration) (map[string][]model.OHLCV, error) {
	return c.inner.Bars(ctx, ids, interval, lookback)
}

func (c *bundleMarketCache) Depth(ctx context.Context, ids []string, levels int) (map[string]DepthSnapshot, error) {
	ids = uniqueStrings(ids)
	missing := c.missingDepthIDs(ids, levels)
	if len(missing) > 0 {
		depth, err := c.inner.Depth(ctx, missing, levels)
		c.mu.Lock()
		for _, id := range missing {
			c.depthFetched[depthCacheKey{id: id, levels: levels}] = true
		}
		for id, snapshot := range depth {
			c.depth[depthCacheKey{id: id, levels: levels}] = snapshot
		}
		if err != nil {
			c.depthErr = err
		}
		c.mu.Unlock()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]DepthSnapshot, len(ids))
	for _, id := range ids {
		if snapshot, ok := c.depth[depthCacheKey{id: id, levels: levels}]; ok {
			out[id] = snapshot
		}
	}
	return out, c.depthErr
}

func (c *bundleMarketCache) missingDepthIDs(ids []string, levels int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if !c.depthFetched[depthCacheKey{id: id, levels: levels}] {
			missing = append(missing, id)
		}
	}
	return missing
}

func (c *bundleMarketCache) OptionMetrics(ctx context.Context, underlyings []string) (map[string]OptionStress, error) {
	underlyings = uniqueStrings(underlyings)
	missing := c.missingOptionUnderlyings(underlyings)
	if len(missing) > 0 {
		options, err := c.inner.OptionMetrics(ctx, missing)
		c.mu.Lock()
		for _, underlying := range missing {
			c.optionFetched[underlying] = true
		}
		for underlying, stress := range options {
			c.options[underlying] = stress
		}
		if err != nil {
			c.optionErr = err
		}
		c.mu.Unlock()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]OptionStress, len(underlyings))
	for _, underlying := range underlyings {
		if stress, ok := c.options[underlying]; ok {
			out[underlying] = stress
		}
	}
	return out, c.optionErr
}

func (c *bundleMarketCache) missingOptionUnderlyings(underlyings []string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	missing := make([]string, 0, len(underlyings))
	for _, underlying := range underlyings {
		if !c.optionFetched[underlying] {
			missing = append(missing, underlying)
		}
	}
	return missing
}
