package ngccache

import (
	"strconv"
	"sync/atomic"
)

func (c *Cache) Exist(sid uint64, key string) bool {
	if _, ok := c.sessions.Load(sid); !ok {
		return false
	}
	hash := hashKey(key)
	bucketIdx := hash % c.bucketCount
	entryOffset, _ := c.findEntry(bucketIdx, key, hash)
	return entryOffset != 0
}

func (c *Cache) Delete(sid uint64, key string) bool {
	val, ok := c.sessions.Load(sid)
	if !ok { return false }
	s := val.(*session)
	hash := hashKey(key)
	bucketIdx := hash % c.bucketCount

	s.Lock()
	defer s.Unlock()

	entryOffset, prevOffset := c.findEntry(bucketIdx, key, hash)
	if entryOffset == 0 { return false }

	bucketPtr := (*uint64)(c.allocator.Pointer(c.bucketBase + bucketIdx*8))
	nextOffset := *(*uint64)(c.allocator.Pointer(entryOffset + 32))
	if prevOffset == 0 {
		atomic.StoreUint64(bucketPtr, nextOffset)
	} else {
		*(*uint64)(c.allocator.Pointer(prevOffset + 32)) = nextOffset
	}

	dataOffset := *(*uint64)(c.allocator.Pointer(entryOffset + 18))
	c.pushFreeBlock(&c.localDataFreeHead, dataOffset, 0)
	c.pushFreeBlock(&c.heapFreeHead, entryOffset, 40)
	
	// Remove from session tracked list
	// This is inefficient but simple for now.
	for i, off := range s.usedBlocks {
		if off == dataOffset {
			s.usedBlocks = append(s.usedBlocks[:i], s.usedBlocks[i+1:]...)
			break
		}
	}
	for i, off := range s.usedEntries {
		if off == entryOffset {
			s.usedEntries = append(s.usedEntries[:i], s.usedEntries[i+1:]...)
			break
		}
	}
	return true
}

func (c *Cache) Add(sid uint64, key string, data []byte) error {
	hash := hashKey(key)
	bucketIdx := hash % c.bucketCount
	val, ok := c.sessions.Load(sid)
	if !ok { return ErrInvalidSession }
	s := val.(*session)

	s.Lock()
	defer s.Unlock()

	entryOffset, _ := c.findEntry(bucketIdx, key, hash)
	if entryOffset != 0 {
		return ErrCacheAlreadyExists
	}
	
	// Since we hold the lock, we can call setInternal without it re-locking.
	// But setInternal has its own locking, so we will duplicate the "create" logic here.
	dataOffset, err := c.popFreeBlock(&c.localDataFreeHead, 0)
	if err != nil { return err }

	entryOffset, err = c.popFreeBlock(&c.heapFreeHead, 40)
	if err != nil {
		c.pushFreeBlock(&c.localDataFreeHead, dataOffset, 0)
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

	s.usedBlocks = append(s.usedBlocks, dataOffset)
	s.usedEntries = append(s.usedEntries, entryOffset)
	
	dest := c.allocator.Slice(dataOffset+8, uint32(len(data)))
	copy(dest, data)
	return nil
}

func (c *Cache) incrementInternal(key string, delta int64, shared bool, sid uint64) (int64, error) {
	hash := hashKey(key)
	bucketIdx := hash % c.bucketCount
	var s *session
	
	if shared {
		sh := c.shards[hash&ShardMask]
		sh.Lock()
		defer sh.Unlock()
	} else {
		val, ok := c.sessions.Load(sid)
		if !ok { return 0, ErrInvalidSession }
		s = val.(*session)
		s.Lock()
		defer s.Unlock()
	}

	entryOffset, _ := c.findEntry(bucketIdx, key, hash)
	var current int64 = 0
	if entryOffset != 0 {
		dataOffset := *(*uint64)(c.allocator.Pointer(entryOffset + 18))
		dataSize := *(*uint32)(c.allocator.Pointer(entryOffset + 26))
		val := c.allocator.Slice(dataOffset+8, dataSize)
		current, _ = strconv.ParseInt(string(val), 10, 64)
	}
	
	newVal := current + delta
	newData := []byte(strconv.FormatInt(newVal, 10))

	if uint64(len(newData)) > c.config.MaxValueSize-8 {
		return 0, ErrDataTooLarge
	}

	if entryOffset != 0 {
		dataOffset := *(*uint64)(c.allocator.Pointer(entryOffset + 18))
		dest := c.allocator.Slice(dataOffset+8, uint32(len(newData)))
		copy(dest, newData)
		*(*uint32)(c.allocator.Pointer(entryOffset + 26)) = uint32(len(newData))
		return newVal, nil
	}
	
	dataHead := &c.localDataFreeHead
	if shared { dataHead = &c.sharedDataFreeHead }
	dataOffset, err := c.popFreeBlock(dataHead, 0)
	if err != nil { return 0, err }

	newEntryOffset, err := c.popFreeBlock(&c.heapFreeHead, 40)
	if err != nil {
		c.pushFreeBlock(dataHead, dataOffset, 0)
		return 0, err
	}
	
	*(*uint64)(c.allocator.Pointer(newEntryOffset)) = hash
	*(*uint16)(c.allocator.Pointer(newEntryOffset + 8)) = uint16(len(key))
	keyOffsetInHeap := newEntryOffset + 48
	*(*uint64)(c.allocator.Pointer(newEntryOffset + 10)) = keyOffsetInHeap
	copy(c.allocator.Slice(keyOffsetInHeap, uint32(len(key))), []byte(key))
	*(*uint64)(c.allocator.Pointer(newEntryOffset + 18)) = dataOffset
	*(*uint32)(c.allocator.Pointer(newEntryOffset + 26)) = uint32(len(newData))
	
	bucketPtr := (*uint64)(c.allocator.Pointer(c.bucketBase + bucketIdx*8))
	oldHead := atomic.LoadUint64(bucketPtr)
	*(*uint64)(c.allocator.Pointer(newEntryOffset + 32)) = oldHead
	atomic.StoreUint64(bucketPtr, newEntryOffset)
	
	if !shared {
		s.usedBlocks = append(s.usedBlocks, dataOffset)
		s.usedEntries = append(s.usedEntries, newEntryOffset)
	}

	dest := c.allocator.Slice(dataOffset+8, uint32(len(newData)))
	copy(dest, newData)
	return newVal, nil
}


func (c *Cache) Increment(sid uint64, key string, delta int64) (int64, error) {
	return c.incrementInternal(key, delta, false, sid)
}

func (c *Cache) Decrement(sid uint64, key string, delta int64) (int64, error) {
	return c.incrementInternal(key, -delta, false, sid)
}

func (c *Cache) IncrementShared(key string, delta int64) (int64, error) {
	return c.incrementInternal(key, delta, true, 0)
}

func (c *Cache) DecrementShared(key string, delta int64) (int64, error) {
	return c.incrementInternal(key, -delta, true, 0)
}

func (c *Cache) GetBatch(sid uint64, keys []string) map[string][]byte {
	res := make(map[string][]byte)
	for _, key := range keys {
		if val, ok := c.Get(sid, key); ok {
			res[key] = val
		}
	}
	return res
}
