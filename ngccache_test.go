package ngccache

import (
	"sync"
	"testing"
)

func TestNewCache_Initialization(t *testing.T) {
	t.Run("Only Shared", func(t *testing.T) {
		cache, err := NewCache(Config{SharedCacheMaxKeys: 100, MaxValueSize: 1024})
		if err != nil {
			t.Fatalf("Failed to create shared-only cache: %v", err)
		}
		cache.Close()
	})

	t.Run("Only Local", func(t *testing.T) {
		cache, err := NewCache(Config{LocalCacheMaxKeys: 100, MaxValueSize: 1024})
		if err != nil {
			t.Fatalf("Failed to create local-only cache: %v", err)
		}
		cache.Close()
	})
	
	t.Run("Defaults", func(t *testing.T) {
		cache, err := NewCache(Config{})
		if err != nil {
			t.Fatalf("Failed to create cache with defaults: %v", err)
		}
		cache.Close()
	})
}

func TestSessionCache_ErrorCases(t *testing.T) {
	config := Config{LocalCacheMaxKeys: 2, MaxValueSize: 128}
	cache, _ := NewCache(config)
	defer cache.Close()

	sid, end := cache.BeginSession()
	
	// Data too large
	if err := cache.Set(sid, "key1", make([]byte, 256)); err != ErrDataTooLarge {
		t.Errorf("Expected ErrDataTooLarge, got %v", err)
	}

	// OOM
	cache.Set(sid, "key1", []byte("a"))
	cache.Set(sid, "key2", []byte("b"))
	if err := cache.Set(sid, "key3", []byte("c")); err != ErrOffHeapOutOfMemory {
		t.Errorf("Expected ErrOffHeapOutOfMemory, got %v", err)
	}
	
	end()

	// Invalid Session
	if err := cache.Set(sid, "key4", []byte("d")); err != ErrInvalidSession {
		t.Errorf("Expected ErrInvalidSession, got %v", err)
	}
}

func TestSession_Recycling(t *testing.T) {
	config := Config{LocalCacheMaxKeys: 1, MaxValueSize: 128}
	cache, _ := NewCache(config)
	defer cache.Close()

	// First session uses the only block
	sid1, end1 := cache.BeginSession()
	if err := cache.Set(sid1, "key1", []byte("a")); err != nil {
		t.Fatalf("Set in first session failed: %v", err)
	}
	// This should fail
	if err := cache.Set(sid1, "key2", []byte("b")); err == nil {
		t.Fatalf("Should have been OOM in first session")
	}
	end1()

	// After end, block should be free again for a new session
	sid2, end2 := cache.BeginSession()
	defer end2()
	if err := cache.Set(sid2, "key1", []byte("c")); err != nil {
		t.Fatalf("Set in second session failed, recycling failed: %v", err)
	}
}

func TestSharedCache_ErrorCases(t *testing.T) {
	config := Config{SharedCacheMaxKeys: 1, MaxValueSize: 128}
	cache, _ := NewCache(config)
	defer cache.Close()

	// OOM
	cache.SetShared("key1", []byte("a"))
	if err := cache.SetShared("key2", []byte("b")); err != ErrOffHeapOutOfMemory {
		t.Errorf("Expected ErrOffHeapOutOfMemory for shared cache, got %v", err)
	}

	// ReadOnly
	cache.SetReadOnly()
	if err := cache.SetShared("key1", []byte("c")); err != ErrReadOnly {
		t.Errorf("Expected ErrReadOnly, got %v", err)
	}
	// Get should still work
	if _, ok := cache.GetShared("key1"); !ok {
		t.Errorf("GetShared failed in ReadOnly mode")
	}
}

func TestIncrement_Atomicity(t *testing.T) {
	config := Config{LocalCacheMaxKeys: 1, MaxValueSize: 128}
	cache, _ := NewCache(config)
	defer cache.Close()

	sid, end := cache.BeginSession()
	defer end()

	key := "atomic_counter"
	const numGoroutines = 10
	const incrementsPerGoroutine = 1000
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				cache.Increment(sid, key, 1)
			}
		}()
	}
	wg.Wait()

	finalVal, ok := cache.Get(sid, key)
	if !ok {
		t.Fatalf("Counter key not found")
	}

	if string(finalVal) != "10000" {
		t.Errorf("Expected counter to be 10000, got %s", string(finalVal))
	}
}
