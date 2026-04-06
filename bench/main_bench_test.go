package bench

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/nuentel-cat/NGCCache"
)

// ... (previous benchmarks)

func Benchmark_PureGo_HighThroughput(b *testing.B) {
	config := ngccache.Config{
		MaxKeys:      100000,
		BlockSize:    32 * 1024,
		AvgKeyLength: 16,
	}
	cache, err := ngccache.NewCache(config)
	if err != nil {
		b.Fatalf("Failed to initialize cache: %v", err)
	}
	defer cache.Close()

	const numOps = 100000
	keys := make([]string, numOps)
	data := make([][]byte, numOps)

	for i := 0; i < numOps; i++ {
		keys[i] = fmt.Sprintf("bench_key_%d", i)
		size := rand.Intn(15*1024) + 1024
		val := make([]byte, size)
		rand.Read(val)
		data[i] = val
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sessionID, endSession := cache.BeginSession()
		for j := 0; j < numOps; j++ {
			_ = cache.Set(sessionID, keys[j], data[j])
		}
		for j := 0; j < numOps; j++ {
			_, _ = cache.Get(sessionID, keys[j])
		}
		endSession()
	}
}

func Benchmark_PureGo_ConcurrentMixed(b *testing.B) {
	config := ngccache.Config{
		MaxKeys:      50000,
		BlockSize:    32 * 1024,
		AvgKeyLength: 16,
		SharedRatio:  20,
	}
	cache, _ := ngccache.NewCache(config)
	defer cache.Close()

	const numSharedKeys = 1000
	const numLocalKeys = 1000
	const numGoroutines = 20

	for i := 0; i < numSharedKeys; i++ {
		_ = cache.SetShared(fmt.Sprintf("shared_key_%d", i), make([]byte, 1024))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(numGoroutines)
		for g := 0; g < numGoroutines; g++ {
			go func() {
				defer wg.Done()
				sid, end := cache.BeginSession()
				defer end()
				for j := 0; j < numLocalKeys; j++ {
					_ = cache.Set(sid, fmt.Sprintf("local_key_%d", j), make([]byte, 1024))
				}
				for j := 0; j < numLocalKeys; j++ {
					_, _ = cache.Get(sid, fmt.Sprintf("local_key_%d", j))
				}
				for j := 0; j < numSharedKeys; j++ {
					_, _ = cache.GetShared(fmt.Sprintf("shared_key_%d", j))
				}
			}()
		}
		wg.Wait()
	}
}

func Benchmark_PureGo_SharedRead(b *testing.B) {
	const itemCount = 10000
	cache, _ := ngccache.NewCache(ngccache.Config{MaxKeys: itemCount, SharedRatio: 100})
	defer cache.Close()
	for i := 0; i < itemCount; i++ {
		_ = cache.SetShared(fmt.Sprintf("key_%d", i), make([]byte, 512))
	}

	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		for pb.Next() {
			_, _ = cache.GetShared(fmt.Sprintf("key_%d", r.Intn(itemCount)))
		}
	})
}

func Benchmark_PureGo_SharedReadReadOnly(b *testing.B) {
	const itemCount = 10000
	cache, _ := ngccache.NewCache(ngccache.Config{MaxKeys: itemCount, SharedRatio: 100})
	defer cache.Close()
	for i := 0; i < itemCount; i++ {
		_ = cache.SetShared(fmt.Sprintf("key_%d", i), make([]byte, 512))
	}
	cache.SetReadOnly()

	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		for pb.Next() {
			_, _ = cache.GetShared(fmt.Sprintf("key_%d", r.Intn(itemCount)))
		}
	})
}

func Benchmark_PureGo_SharedReadUnsafe(b *testing.B) {
	const itemCount = 10000
	cache, _ := ngccache.NewCache(ngccache.Config{MaxKeys: itemCount, SharedRatio: 100})
	defer cache.Close()
	for i := 0; i < itemCount; i++ {
		_ = cache.SetShared(fmt.Sprintf("key_%d", i), make([]byte, 512))
	}

	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		for pb.Next() {
			_, _ = cache.GetSharedUnsafe(fmt.Sprintf("key_%d", r.Intn(itemCount)))
		}
	})
}
