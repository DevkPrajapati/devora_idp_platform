package kubernetes

import (
	"context"
	"sync"
	"time"
)

// liveCacheTTL collapses the burst of identical list calls the dashboard
// fires on every page load (overview + pods + nodes + metrics all list the
// same objects within milliseconds) without serving stale inventory.
const liveCacheTTL = 3 * time.Second

type liveCache struct {
	mu    sync.Mutex
	items map[string]*liveCacheItem
}

type liveCacheItem struct {
	ready chan struct{}
	val   any
	err   error
	at    time.Time
}

func (c *liveCache) do(ctx context.Context, key string, fn func() (any, error)) (any, error) {
	if c == nil {
		return fn()
	}

	c.mu.Lock()
	if c.items == nil {
		c.items = make(map[string]*liveCacheItem)
	}
	item, ok := c.items[key]
	if ok && item.err == nil && item.val != nil && time.Since(item.at) < liveCacheTTL && item.ready == nil {
		val := item.val
		c.mu.Unlock()
		return val, nil
	}
	if ok && item.ready != nil {
		ready := item.ready
		c.mu.Unlock()
		return waitLiveCache(ctx, item, ready)
	}

	item = &liveCacheItem{ready: make(chan struct{})}
	c.items[key] = item
	c.mu.Unlock()

	val, err := fn()

	c.mu.Lock()
	item.val = val
	item.err = err
	if err == nil {
		item.at = time.Now()
	}
	ready := item.ready
	item.ready = nil
	if err != nil {
		delete(c.items, key)
	}
	c.mu.Unlock()
	close(ready)
	return val, err
}

func waitLiveCache(ctx context.Context, item *liveCacheItem, ready <-chan struct{}) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ready:
		return item.val, item.err
	}
}

func (c *liveCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = make(map[string]*liveCacheItem)
	c.mu.Unlock()
}

func cacheDo[T any](c *Client, ctx context.Context, key string, fn func() (T, error)) (T, error) {
	var zero T
	if c == nil {
		return fn()
	}
	v, err := c.cache.do(ctx, key, func() (any, error) {
		return fn()
	})
	if err != nil {
		return zero, err
	}
	out, ok := v.(T)
	if !ok {
		return zero, nil
	}
	return out, nil
}
