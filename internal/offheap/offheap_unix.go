//go:build !windows

package offheap

import (
	"reflect"
	"syscall"
	"unsafe"
)

// Allocator manages a large off-heap memory block.
type Allocator struct {
	data []byte
	base uintptr
	size uint64
}

// NewAllocator allocates a new off-heap memory region using mmap.
func NewAllocator(size uint64) (*Allocator, error) {
	// Ensure size is page-aligned
	pageSize := uint64(syscall.Getpagesize())
	alignedSize := (size + pageSize - 1) & ^(pageSize - 1)

	data, err := syscall.Mmap(-1, 0, int(alignedSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, err
	}

	return &Allocator{
		data: data,
		base: uintptr(unsafe.Pointer(&data[0])),
		size: alignedSize,
	}, nil
}

// Close releases the off-heap memory.
func (a *Allocator) Close() error {
	if a.data == nil {
		return nil
	}
	err := syscall.Munmap(a.data)
	a.data = nil
	a.base = 0
	a.size = 0
	return err
}

// GetBase returns the base address of the allocated region.
func (a *Allocator) GetBase() uintptr {
	return a.base
}

// GetSize returns the total size of the allocated region.
func (a *Allocator) GetSize() uint64 {
	return a.size
}

// Pointer returns a pointer at the given offset.
func (a *Allocator) Pointer(offset uint64) unsafe.Pointer {
	if offset >= a.size {
		return nil
	}
	return unsafe.Pointer(a.base + uintptr(offset))
}

// Slice returns a byte slice at the given offset and size.
func (a *Allocator) Slice(offset uint64, size uint32) []byte {
	if offset+uint64(size) > a.size {
		return nil
	}
	
	var res []byte
	header := (*reflect.SliceHeader)(unsafe.Pointer(&res))
	header.Data = a.base + uintptr(offset)
	header.Len = int(size)
	header.Cap = int(size)
	
	return res
}
