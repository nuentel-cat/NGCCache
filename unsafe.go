package ngccache

import (
	"reflect"
	"unsafe"
)

// GetUnsafe returns a byte slice that directly points to the off-heap memory.
// WARNING: The returned slice is only valid as long as the key is not updated/deleted
// and the session is not ended. Use with extreme caution for maximum performance.
func (c *Cache) GetUnsafe(sid uint64, key string) ([]byte, bool) {
	return c.getUnsafeInternal(key, false, sid)
}

// GetSharedUnsafe is the shared cache version of GetUnsafe.
func (c *Cache) GetSharedUnsafe(key string) ([]byte, bool) {
	return c.getUnsafeInternal(key, true, 0)
}

func (c *Cache) getUnsafeInternal(key string, shared bool, sid uint64) ([]byte, bool) {
	hash := hashKey(key)
	bucketIdx := hash % c.bucketCount

	var entryOffset uint64
	if shared {
		sh := c.shards[hash%256]
		sh.Lock()
		entryOffset, _ = c.findEntry(bucketIdx, key, hash)
		sh.Unlock()
	} else {
		val, ok := c.sessions.Load(sid)
		if !ok {
			return nil, false
		}
		s := val.(*session)
		s.Lock()
		entryOffset, _ = c.findEntry(bucketIdx, key, hash)
		s.Unlock()
	}

	if entryOffset == 0 {
		return nil, false
	}

	dataOffset := *(*uint64)(c.allocator.Pointer(entryOffset + 18))
	dataSize := *(*uint32)(c.allocator.Pointer(entryOffset + 26))
	
	// Create slice header pointing to off-heap memory
	var res []byte
	header := (*reflect.SliceHeader)(unsafe.Pointer(&res))
	header.Data = c.allocator.GetBase() + uintptr(dataOffset+8)
	header.Len = int(dataSize)
	header.Cap = int(dataSize)
	
	return res, true
}
