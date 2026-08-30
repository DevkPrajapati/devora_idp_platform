package kubernetes

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveCacheSharesInflight(t *testing.T) {
	var cache liveCache
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})

	fn := func() (any, error) {
		calls.Add(1)
		close(started)
		<-release
		return "ok", nil
	}

	ctx := context.Background()
	var first any
	var firstErr error
	go func() {
		first, firstErr = cache.do(ctx, "k", fn)
	}()
	<-started

	var second any
	var secondErr error
	joined := make(chan struct{})
	go func() {
		second, secondErr = cache.do(ctx, "k", func() (any, error) {
			t.Error("second caller must join the in-flight fetch")
			return nil, nil
		})
		close(joined)
	}()
	close(release)
	<-joined

	if firstErr != nil || secondErr != nil {
		t.Fatalf("errors: %v %v", firstErr, secondErr)
	}
	if first != "ok" || second != "ok" {
		t.Fatalf("values: %v %v", first, second)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestLiveCacheDoesNotStoreErrors(t *testing.T) {
	var cache liveCache
	ctx := context.Background()
	_, err := cache.do(ctx, "k", func() (any, error) {
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected error")
	}

	val, err := cache.do(ctx, "k", func() (any, error) {
		return "recovered", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if val != "recovered" {
		t.Fatalf("val = %v", val)
	}
}

func TestLiveCacheHitWithinTTL(t *testing.T) {
	var cache liveCache
	ctx := context.Background()
	var calls atomic.Int32
	fn := func() (any, error) {
		calls.Add(1)
		return 7, nil
	}
	if _, err := cache.do(ctx, "n", fn); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.do(ctx, "n", fn); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestLiveCacheWaitRespectsCancel(t *testing.T) {
	var cache liveCache
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_, _ = cache.do(context.Background(), "k", func() (any, error) {
			close(started)
			time.Sleep(time.Second)
			return "late", nil
		})
	}()
	<-started
	cancel()

	_, err := cache.do(ctx, "k", func() (any, error) {
		return "nope", nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
}

func TestLiveCacheClearDropsHits(t *testing.T) {
	var cache liveCache
	ctx := context.Background()
	var calls atomic.Int32
	fn := func() (any, error) {
		calls.Add(1)
		return "x", nil
	}
	if _, err := cache.do(ctx, "k", fn); err != nil {
		t.Fatal(err)
	}
	cache.clear()
	if _, err := cache.do(ctx, "k", fn); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestCacheDoConcurrent(t *testing.T) {
	c := &Client{}
	ctx := context.Background()
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cacheDo(c, ctx, "pods", func() ([]string, error) {
				calls.Add(1)
				time.Sleep(20 * time.Millisecond)
				return []string{"a"}, nil
			})
			if err != nil {
				t.Errorf("cacheDo: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}
