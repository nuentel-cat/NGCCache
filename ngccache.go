package ngccache

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/nuentel-cat/NGCCache/internal/offheap"
)

const (
	NumShards = 1024 // Power of two for bitwise AND
	ShardMask = NumShards - 1
	DefaultAvgKeyLength = 32 // Internal heuristic for index allocation
)

var (
	ErrOffHeapOutOfMemory = errors.New("off-heap memory exhausted")
	ErrDataTooLarge       = errors.New("data exceeds block size")
	ErrInvalidSession     = errors.New("invalid session")
	ErrCacheAlreadyExists = errors.New("cache entry already exists")
	ErrReadOnly           = errors.New("cache is in read-only mode")
)

type Config struct {
	LocalCacheMaxKeys  uint64
	SharedCacheMaxKeys uint64
	MaxValueSize       uint64
	Verbose            bool
}

type Cache struct {
	config     Config
	allocator  *offheap.Allocator
	isReadOnly bool 

	localDataBase    uint64
	localDataTotal   uint64
	localDataFreeHead uint64

	sharedDataBase    uint64
	sharedDataTotal   uint64
	sharedDataFreeHead uint64

	bucketBase    uint64
	bucketCount   uint64
	heapBase      uint64
	heapSize      uint64
	heapFreeHead  uint64

	sessions      sync.Map
	nextSessionID uint64
	shards        [NumShards]*shard
}

type shard struct { lock uint32 }
func (s *shard) Lock() { for !atomic.CompareAndSwapUint32(&s.lock, 0, 1) { runtime.Gosched() } }
func (s *shard) Unlock() { atomic.StoreUint32(&s.lock, 0) }

type session struct {
	id          uint64
	usedBlocks  []uint64
	usedEntries []uint64
	lock        uint32
}
func (s *session) Lock() { for !atomic.CompareAndSwapUint32(&s.lock, 0, 1) { runtime.Gosched() } }
func (s *session) Unlock() { atomic.StoreUint32(&s.lock, 0) }

func NewCache(config Config) (*Cache, error) {
	if config.LocalCacheMaxKeys == 0 && config.SharedCacheMaxKeys == 0 {
		config.SharedCacheMaxKeys = 32768 // Default to 32k shared keys
	}
	if config.MaxValueSize == 0 {
		config.MaxValueSize = 32 * 1024
	}

	totalMaxKeys := config.LocalCacheMaxKeys + config.SharedCacheMaxKeys
	sizeLocalData := config.LocalCacheMaxKeys * config.MaxValueSize
	sizeSharedData := config.SharedCacheMaxKeys * config.MaxValueSize
	
	bucketCount := uint64(float64(totalMaxKeys) * 1.5)
	sizeBuckets := bucketCount * 8
	sizeEntry := uint64(48)
	sizeHeap := totalMaxKeys * (sizeEntry + DefaultAvgKeyLength)
	totalMemory := sizeLocalData + sizeSharedData + sizeBuckets + sizeHeap + 1024

	alloc, err := offheap.NewAllocator(totalMemory)
	if err != nil { return nil, err }

	c := &Cache{ config: config, allocator: alloc }

	// Partition memory
	offset := uint64(1024)
	c.localDataBase = offset
	c.localDataTotal = config.LocalCacheMaxKeys
	offset += sizeLocalData
	c.sharedDataBase = offset
	c.sharedDataTotal = config.SharedCacheMaxKeys
	offset += sizeSharedData
	c.bucketBase = offset
	c.bucketCount = bucketCount
	offset += sizeBuckets
	c.heapBase = offset
	c.heapSize = sizeHeap

	// Init free lists
	c.initFreeList(&c.localDataFreeHead, c.localDataBase, c.localDataTotal, c.config.MaxValueSize, 0)
	c.initFreeList(&c.sharedDataFreeHead, c.sharedDataBase, c.sharedDataTotal, c.config.MaxValueSize, 0)
	c.initFreeList(&c.heapFreeHead, c.heapBase, totalMaxKeys, sizeEntry+DefaultAvgKeyLength, 40)

	for i := range c.shards {
		c.shards[i] = &shard{}
	}

	if config.Verbose {
		fmt.Println("--- NGCCache Initialization ---")
		fmt.Printf("  Local Data  : %d keys * %d bytes/key = %.2f MB\n", config.LocalCacheMaxKeys, config.MaxValueSize, float64(sizeLocalData)/(1024*1024))
		fmt.Printf("  Shared Data : %d keys * %d bytes/key = %.2f MB\n", config.SharedCacheMaxKeys, config.MaxValueSize, float64(sizeSharedData)/(1024*1024))
		fmt.Printf("  Index (Buckets) : %d buckets * 8 bytes/bucket = %.2f MB\n", bucketCount, float64(sizeBuckets)/(1024*1024))
		fmt.Printf("  Index (Heap)    : %d entries * (%d + %d) bytes/entry = %.2f MB\n", totalMaxKeys, sizeEntry, DefaultAvgKeyLength, float64(sizeHeap)/(1024*1024))
		fmt.Println("---------------------------------")
		fmt.Printf("  Total Off-Heap Memory Allocated: %.2f MB\n", float64(totalMemory)/(1024*1024))
		fmt.Printf("  Configuration: %d Shards, %d Hash Buckets\n", NumShards, bucketCount)
		fmt.Println("---------------------------------")
	}

	return c, nil
}

func (c *Cache) initFreeList(head *uint64, base, count, stride, nextOffset uint64) {
	if count == 0 {
		*head = ^uint64(0)
		return
	}
	*head = base
	for i := uint64(0); i < count-1; i++ {
		offset := base + i*stride
		next := offset + stride
		*(*uint64)(c.allocator.Pointer(offset + nextOffset)) = next
	}
	*(*uint64)(c.allocator.Pointer(base + (count-1)*stride + nextOffset)) = ^uint64(0)
}

func (c *Cache) popFreeBlock(head *uint64, nextFieldOffset uint64) (uint64, error) {
	for {
		oldHead := atomic.LoadUint64(head)
		if oldHead == ^uint64(0) || oldHead == 0 { return 0, ErrOffHeapOutOfMemory }
		next := *(*uint64)(c.allocator.Pointer(oldHead + nextFieldOffset))
		if atomic.CompareAndSwapUint64(head, oldHead, next) { return oldHead, nil }
		runtime.Gosched()
	}
}

func (c *Cache) pushFreeBlock(head *uint64, offset uint64, nextFieldOffset uint64) {
	for {
		oldHead := atomic.LoadUint64(head)
		*(*uint64)(c.allocator.Pointer(offset + nextFieldOffset)) = oldHead
		if atomic.CompareAndSwapUint64(head, oldHead, offset) { break }
		runtime.Gosched()
	}
}

func (c *Cache) setInternal(key string, data []byte, shared bool, sid uint64) error {
	if c.isReadOnly && shared { return ErrReadOnly }
	if uint64(len(data)) > c.config.MaxValueSize-8 { return ErrDataTooLarge }

	hash := hashKey(key)
	bucketIdx := hash % c.bucketCount
	var s *session
	
	if shared {
		sh := c.shards[hash&ShardMask]
		sh.Lock()
		defer sh.Unlock()
	} else {
		val, ok := c.sessions.Load(sid)
		if !ok { return ErrInvalidSession }
		s = val.(*session)
		s.Lock()
		defer s.Unlock()
	}

	entryOffset, _ := c.findEntry(bucketIdx, key, hash)
	if entryOffset != 0 {
		dataOffset := *(*uint64)(c.allocator.Pointer(entryOffset + 18))
		dest := c.allocator.Slice(dataOffset+8, uint32(len(data)))
		copy(dest, data)
		*(*uint32)(c.allocator.Pointer(entryOffset + 26)) = uint32(len(data))
		return nil
	}
	
	dataHead := &c.localDataFreeHead
	if shared { dataHead = &c.sharedDataFreeHead }
	dataOffset, err := c.popFreeBlock(dataHead, 0)
	if err != nil { return err }

	entryOffset, err = c.popFreeBlock(&c.heapFreeHead, 40)
	if err != nil {
		c.pushFreeBlock(dataHead, dataOffset, 0)
		return err
	}

	*(*uint64)(c.allocator.Pointer(entryOffset)) = hash
	*(*uint16)(c.allocator.Pointer(entryOffset + 8)) = uint16(len(key))
	keyOffsetInHeap := entryOffset + 48
	*(*uint64)(c.allocator.Pointer(entryOffset + 10)) = keyOffsetInHeap
	copy(c.allocator.Slice(keyOffsetInHeap, uint32(len(key))), []byte(key))
	*(*uint64)(c.allocator.Pointer(entryOffset + 18)) = dataOffset
	*(*uint32)(c.allocator.Pointer(entryOffset + 26)) = uint32(len(data))
	
	bucketPtr := (*uint64)(c.allocator.Pointer(c.bucketBase + bucketIdx*8))
	oldHead := atomic.LoadUint64(bucketPtr)
	*(*uint64)(c.allocator.Pointer(entryOffset + 32)) = oldHead
	atomic.StoreUint64(bucketPtr, entryOffset)

	if !shared {
		s.usedBlocks = append(s.usedBlocks, dataOffset)
		s.usedEntries = append(s.usedEntries, entryOffset)
	}
	
	dest := c.allocator.Slice(dataOffset+8, uint32(len(data)))
	copy(dest, data)
	return nil
}

func (c *Cache) getInternal(key string, shared bool, sid uint64) ([]byte, bool) {
	hash := hashKey(key)
	bucketIdx := hash % c.bucketCount
	var entryOffset uint64

	if shared {
		if c.isReadOnly {
			entryOffset, _ = c.findEntryNoBarrier(bucketIdx, key, hash)
		} else {
			sh := c.shards[hash&ShardMask]
			sh.Lock()
			entryOffset, _ = c.findEntry(bucketIdx, key, hash)
			sh.Unlock()
		}
	} else {
		val, ok := c.sessions.Load(sid)
		if !ok { return nil, false }
		s := val.(*session)
		s.Lock()
		entryOffset, _ = c.findEntry(bucketIdx, key, hash)
		s.Unlock()
	}

	if entryOffset == 0 { return nil, false }
	dataOffset := *(*uint64)(c.allocator.Pointer(entryOffset + 18))
	dataSize := *(*uint32)(c.allocator.Pointer(entryOffset + 26))
	src := c.allocator.Slice(dataOffset+8, dataSize)
	res := make([]byte, dataSize)
	copy(res, src)
	return res, true
}

func (c *Cache) Close() error { return c.allocator.Close() }
func (c *Cache) BeginSession() (uint64, func()) {
	sid := atomic.AddUint64(&c.nextSessionID, 1)
	s := &session{id: sid}; c.sessions.Store(sid, s)
	return sid, func() { c.endSession(sid) }
}
func (c *Cache) endSession(sid uint64) {
	val, ok := c.sessions.LoadAndDelete(sid)
	if !ok { return }
	s := val.(*session); s.Lock(); defer s.Unlock()
	for _, offset := range s.usedBlocks { c.pushFreeBlock(&c.localDataFreeHead, offset, 0) }
	for _, offset := range s.usedEntries { c.pushFreeBlock(&c.heapFreeHead, offset, 40) }
}
func (c *Cache) Set(sid uint64, key string, data []byte) error { return c.setInternal(key, data, false, sid) }
func (c *Cache) Get(sid uint64, key string) ([]byte, bool) { return c.getInternal(key, false, sid) }
func (c *Cache) SetShared(key string, data []byte) error { return c.setInternal(key, data, true, 0) }
func (c *Cache) GetShared(key string) ([]byte, bool) { return c.getInternal(key, true, 0) }
func (c *Cache) SetReadOnly() { c.isReadOnly = true }
