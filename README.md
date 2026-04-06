# NGCCache (Pure Go Edition)

<div align="center">
  <p><strong>A blazing fast, Zero-GC, Off-Heap KVS for Go.</strong></p>

  [![Go Reference](https://pkg.go.dev/badge/github.com/nuentel-cat/NGCCache.svg)](https://pkg.go.dev/github.com/nuentel-cat/NGCCache)
  [![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
</div>

<br />

NGCCache is a high-performance, **Pure Go** off-heap memory cache library. By managing memory directly via OS system calls (`mmap` on Unix, `VirtualAlloc` on Windows), it completely bypasses Go's Garbage Collection (GC) overhead. 

It is specifically engineered for **"Write-Once, Read-Many"** workloads in massive-traffic environments such as Ad-Tech, Gaming backends, and High-Frequency Trading systems where even micro-second latency spikes from GC (Stop-The-World) are unacceptable.

---

## 🚀 Why NGCCache?

- **Zero GC Overhead**: Both data values and index structures (hash tables) are stored off-heap. GC scan time remains constant regardless of whether you have 100 or 100 million entries.
- **Pure Go Integration**: No Cgo required. Easy cross-compilation, faster builds, and better security.
- **Goroutine-Scoped Arena**: Bind caches to a request's lifecycle. When the goroutine ends, all its memory is recycled in O(1) time.
- **The "InnoDB" of In-Memory**: Optimized for read-intensive workloads with advanced tuning options like `SetReadOnly` and `GetUnsafe`.

---

## 🔧 Installation

```bash
go get github.com/nuentel-cat/NGCCache
```

---

## 🔧 Configuration

Configuring NGCCache is intuitive and intention-driven. You define the capacity by the number of keys.

```go
type Config struct {
    LocalCacheMaxKeys  uint64 // Max keys for request-scoped (local) sessions
    SharedCacheMaxKeys uint64 // Max keys for the global shared cache
    MaxValueSize       uint64 // Max data size per key (in bytes)
    Verbose            bool   // detailed memory breakdown on startup
}
```

### Initialization Example
```go
import "github.com/nuentel-cat/NGCCache"

cache, err := ngccache.NewCache(ngccache.Config{
    LocalCacheMaxKeys:  1000000,
    SharedCacheMaxKeys: 50000,
    MaxValueSize:       4096, // 4KB
    Verbose:            true,
})
defer cache.Close() // Essential for freeing OS memory
```

---

## ⚡️ API Reference

### Session Cache (Local)
Create a session to track and instantly recycle memory at the end of a request.
- `BeginSession() (sessionID uint64, endSession func())`
- `Set(sid uint64, key string, data []byte) error`
- `Get(sid uint64, key string) ([]byte, bool)`
- `Delete(sid uint64, key string) bool`
- `Exist(sid uint64, key string) bool`
- `Add(sid uint64, key string, data []byte) error` (Sets only if key is missing)
- `Increment(sid uint64, key string, delta int64) (int64, error)` (Atomic)
- `Decrement(sid uint64, key string, delta int64) (int64, error)` (Atomic)

### Shared Cache (Global)
Persistent cache accessible across all goroutines.
- `SetShared(key string, data []byte) error`
- `GetShared(key string) ([]byte, bool)`
- `DeleteShared(key string) bool`
- `ExistShared(key string) bool`
- `IncrementShared(key string, delta int64) (int64, error)`
- `DecrementShared(key string, delta int64) (int64, error)`

---

## 🚀 Extreme Performance Tuning

### 1. `SetReadOnly()` - Lock-Free Reads
Call `cache.SetReadOnly()` after pre-loading your master data. This makes `GetShared` operations **lock-free** and eliminates atomic memory barriers, reaching speeds of **~26 ns/op**.
*Note: Any subsequent writes will return `ErrReadOnly`.*

### 2. `GetUnsafe()` - Zero-Copy Reference
For ultimate speed, use `GetUnsafe(sid, key)` or `GetSharedUnsafe(key)`. This returns a `[]byte` pointing **directly to off-heap memory** (approx **15 ns/op**).
- **CRITICAL**: The slice is only valid until the key is modified/deleted or the session ends.
- **DO NOT** modify, append to, or store this slice. Use it for immediate read-only processing.

---

## ❌ Error Handling

NGCCache uses explicit errors for predictable behavior:
- `ErrOffHeapOutOfMemory`: The allocated pool for keys is exhausted.
- `ErrDataTooLarge`: Provided data exceeds the configured `MaxValueSize`.
- `ErrInvalidSession`: Operating on a session that has already ended.
- `ErrCacheAlreadyExists`: Returned by `Add` when the key exists.
- `ErrReadOnly`: Attempting to write to a cache in ReadOnly mode.

---

## 📊 Benchmarks (Shared Read)

NGCCache is optimized for extreme read-heavy scenarios, significantly outperforming other popular Go caching libraries.

Tested on Intel Core i9-14900HX:

| Engine | Strategy | Performance |
|:---|:---|:---:|
| **NGCCache** | **ReadOnly (Safe)** | **26 ns/op** |
| **NGCCache** | **Unsafe (Zero-Copy)** | **15 ns/op** |
| Other High-Perf Cache | Default | ~33 ns/op |

In scenarios with heavy concurrent writes, universal general-purpose caches might have an edge. NGCCache is specialized for read-heavy workloads where predictable low latency and zero GC impact are critical.

---

## 💻 Platform Support
- **Linux / macOS**: via `syscall.Mmap`
- **Windows**: via `syscall.VirtualAlloc`

---

## 📄 License
NGCCache is released under the **Apache License 2.0**. See [LICENSE](LICENSE) for details.
