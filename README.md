# NGCCache (Pure Go)

<div align="center">
  <p><strong>A blazing fast, zero-GC, off-heap arena cache for Go.</strong></p>

  [![Go Reference](https://pkg.go.dev/badge/github.com/nuentel-cat/NGCCache.svg)](https://pkg.go.dev/github.com/nuentel-cat/NGCCache)
  [![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
</div>

<br />

NGCCache is a high-performance, Pure Go off-heap memory cache library designed to completely bypass Go's Garbage Collection (GC) overhead. It uses `syscall.Mmap` (or `VirtualAlloc` on Windows) to manage memory outside of Go's runtime, making it ideal for storing massive numbers of entries without causing GC-related latency spikes.

## 🚀 Core Concepts

- **Zero-GC & Off-Heap**: All cache data and index structures are stored in memory allocated directly from the OS, making them invisible to Go's GC.
- **Goroutine-Scoped Arena**: Caches can be tied to a goroutine's lifecycle. When the goroutine ends, all memory used in that session is instantly recycled in O(1) time.
- **Deterministic Memory**: Configure the cache by the number of keys and a fixed value size, leading to predictable memory usage.
- **Read-Optimized for High Throughput**: Specialized features like `SetReadOnly` mode provide lock-free, ultra-fast reads for "write-once, read-many" workloads.

---

## 🔧 Configuration

The `Config` struct allows for clear, intention-driven setup.

```go
type Config struct {
    // --- Goroutine-Scoped (Local) Cache ---
    // The maximum number of keys that can be stored across ALL active sessions.
    LocalCacheMaxKeys uint64

    // --- Global Shared Cache ---
    // The maximum number of keys for the global, shared cache.
    SharedCacheMaxKeys uint64

    // --- Overall Cache Sizing ---
    // The default maximum size for a single value (in bytes).
    // This determines the slab size and affects memory usage.
    MaxValueSize uint64

    // Verbose logging on startup.
    Verbose bool
}
```

### Example

```go
cache, err := ngccache.NewCache(ngccache.Config{
    // A pool for 1 million session-scoped items.
    LocalCacheMaxKeys:  1000000,
    // A global cache for 50,000 master data items.
    SharedCacheMaxKeys: 50000,
    // Each item can be up to 4KB.
    MaxValueSize:       4 * 1024,
    // Print memory layout on startup.
    Verbose:      true,
})
```

---

## ⚡️ API and Usage

### Basic Usage (Session Cache)
```go
// Inside a goroutine for a request
sessionID, endSession := cache.BeginSession()
defer endSession()

cache.Set(sessionID, "request_specific_key", []byte("some data"))
val, found := cache.Get(sessionID, "request_specific_key")
```

### Shared Cache
```go
// At startup
cache.SetShared("master_data_1", []byte("..."))

// In any goroutine
val, found := cache.GetShared("master_data_1")
```

---

## 🚀 Advanced Performance Tuning

### `SetReadOnly()` Mode
For "write-once, read-many" scenarios, you can dramatically improve read performance by disabling writes.

```go
// 1. Populate the shared cache at startup
for _, item := range masterData {
    cache.SetShared(item.Key, item.Value)
}

// 2. Transition to read-only mode
cache.SetReadOnly()

// 3. All subsequent GetShared calls are now lock-free and extremely fast
val, found := cache.GetShared("some_master_key")
```

### `GetUnsafe()` - Zero-Copy Reads
For the absolute lowest latency, `GetUnsafe` returns a slice that points directly to the off-heap memory, avoiding a data copy.

**WARNING**: This is an advanced feature. The returned slice is only valid as long as the key is not deleted and the session has not ended. Use with extreme caution.

```go
// This operation has almost zero overhead
val, found := cache.GetSharedUnsafe("some_key")
if found {
    // Do NOT store `val` or pass it to other goroutines.
    // Use it immediately and discard.
    fmt.Printf("Read %d bytes without copying", len(val))
}
```

---

## 📊 Benchmarks (vs. freecache)
`NGCCache` excels in read-intensive workloads, especially when `SetReadOnly` can be used.

| Benchmark | **NGCCache (ReadOnly)** | **NGCCache (Unsafe)** | **freecache** |
|:---|:---:|:---:|:---:|
| Shared Read | **26 ns/op** | **15 ns/op** | 33 ns/op |

In scenarios with heavy concurrent writes (`Concurrent Mixed`), `freecache`'s architecture currently gives it an edge. `NGCCache` is specialized for read-heavy workloads where predictable low latency and zero GC impact are critical.
