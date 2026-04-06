package ngccache

import (
	"sync/atomic"
)

// hashKey calculates FNV-1a hash of a string.
func hashKey(key string) uint64 {
	var hash uint64 = 14695981039346656037
	for i := 0; i < len(key); i++ {
		hash ^= uint64(key[i])
		hash *= 1099511628211
	}
	return hash
}

func (c *Cache) findEntry(bucketIdx uint64, key string, hash uint64) (uint64, uint64) {
	bucketOffset := c.bucketBase + bucketIdx*8
	entryOffset := atomic.LoadUint64((*uint64)(c.allocator.Pointer(bucketOffset)))
	
	var prevOffset uint64 = 0
	for entryOffset != ^uint64(0) && entryOffset != 0 {
		ePtr := c.allocator.Pointer(entryOffset)
		
		// 1. Compare Hash
		eHash := *(*uint64)(ePtr)
		if eHash == hash {
			// 2. Compare Key
			eKeyLen := uint64(*(*uint16)(c.allocator.Pointer(entryOffset + 8)))
			if eKeyLen == uint64(len(key)) {
				eKeyOffset := *(*uint64)(c.allocator.Pointer(entryOffset + 10))
				
				// Byte comparison (avoiding string conversion overhead)
				eKey := c.allocator.Slice(eKeyOffset, uint32(eKeyLen))
				match := true
				for i := 0; i < len(key); i++ {
					if eKey[i] != key[i] {
						match = false
						break
					}
				}
				if match {
					return entryOffset, prevOffset
				}
			}
		}
		
		prevOffset = entryOffset
		entryOffset = *(*uint64)(c.allocator.Pointer(entryOffset + 32)) // NextOffset
	}
	
	return 0, prevOffset
}

func (c *Cache) findEntryNoBarrier(bucketIdx uint64, key string, hash uint64) (uint64, uint64) {
	bucketOffset := c.bucketBase + bucketIdx*8
	entryOffset := *(*uint64)(c.allocator.Pointer(bucketOffset))
	
	var prevOffset uint64 = 0
	for entryOffset != ^uint64(0) && entryOffset != 0 {
		ePtr := c.allocator.Pointer(entryOffset)
		eHash := *(*uint64)(ePtr)
		if eHash == hash {
			eKeyLen := uint64(*(*uint16)(c.allocator.Pointer(entryOffset + 8)))
			if eKeyLen == uint64(len(key)) {
				eKeyOffset := *(*uint64)(c.allocator.Pointer(entryOffset + 10))
				eKey := c.allocator.Slice(eKeyOffset, uint32(eKeyLen))
				match := true
				for i := 0; i < len(key); i++ {
					if eKey[i] != key[i] {
						match = false
						break
					}
				}
				if match {
					return entryOffset, prevOffset
				}
			}
		}
		prevOffset = entryOffset
		entryOffset = *(*uint64)(c.allocator.Pointer(entryOffset + 32))
	}
	return 0, prevOffset
}
