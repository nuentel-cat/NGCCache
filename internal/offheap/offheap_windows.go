//go:build windows

package offheap

import (
	"reflect"
	"syscall"
	"unsafe"
)

const (
	MEM_COMMIT     = 0x00001000
	MEM_RESERVE    = 0x00002000
	PAGE_READWRITE = 0x04
	MEM_RELEASE    = 0x00008000
)

var (
	modkernel32       = syscall.NewLazyDLL("kernel32.dll")
	procVirtualAlloc  = modkernel32.NewProc("VirtualAlloc")
	procVirtualFree   = modkernel32.NewProc("VirtualFree")
)

// Allocator manages a large off-heap memory block.
type Allocator struct {
	base uintptr
	size uint64
}

// NewAllocator allocates a new off-heap memory region using VirtualAlloc.
func NewAllocator(size uint64) (*Allocator, error) {
	// VirtualAlloc aligns to page size automatically, but we keep it explicit
	res, _, err := procVirtualAlloc.Call(0, uintptr(size), MEM_COMMIT|MEM_RESERVE, PAGE_READWRITE)
	if res == 0 {
		return nil, err
	}

	return &Allocator{
		base: res,
		size: size,
	}, nil
}

// Close releases the off-heap memory.
func (a *Allocator) Close() error {
	if a.base == 0 {
		return nil
	}
	res, _, err := procVirtualFree.Call(a.base, 0, MEM_RELEASE)
	if res == 0 {
		return err
	}
	a.base = 0
	a.size = 0
	return nil
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
