package proxyserver

import (
	"bytes"
	"sync"
)

// Buffer pool constants
const (
	SmallBufferSize  = 4 * 1024   // 4KB
	MediumBufferSize = 32 * 1024  // 32KB
	LargeBufferSize  = 256 * 1024 // 256KB
)

var smallBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, SmallBufferSize)
		return &buf
	},
}

var mediumBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, MediumBufferSize)
		return &buf
	},
}

var largeBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, LargeBufferSize)
		return &buf
	},
}

var bytesBufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// GetBytesBuffer gets a bytes.Buffer from pool.
func GetBytesBuffer() *bytes.Buffer {
	buf := bytesBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutBytesBuffer returns a bytes.Buffer to pool.
func PutBytesBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	if buf.Cap() <= 1024*1024 {
		buf.Reset()
		bytesBufferPool.Put(buf)
	}
}

// GetBufferForSize returns an appropriately sized buffer.
func GetBufferForSize(size int64) *[]byte {
	switch {
	case size <= SmallBufferSize:
		return smallBufferPool.Get().(*[]byte)
	case size <= MediumBufferSize:
		return mediumBufferPool.Get().(*[]byte)
	default:
		return largeBufferPool.Get().(*[]byte)
	}
}

// PutBufferBySize returns a buffer to the appropriate pool.
func PutBufferBySize(buf *[]byte) {
	if buf == nil {
		return
	}
	switch cap(*buf) {
	case SmallBufferSize:
		*buf = (*buf)[:0]
		smallBufferPool.Put(buf)
	case MediumBufferSize:
		*buf = (*buf)[:0]
		mediumBufferPool.Put(buf)
	case LargeBufferSize:
		*buf = (*buf)[:0]
		largeBufferPool.Put(buf)
	}
}
