package memscan

import (
	"sync"

	"golang.org/x/sys/unix"
)

var readBufferPool = sync.Pool{
	New: func() any {
		// 满足scanner.Value除Bytes类型外一次最大读取
		return make([]byte, IOV_MAX*8)
	},
}

func getReadBuffer(needed int) []byte {
	buf := readBufferPool.Get().([]byte)
	if cap(buf) < needed {
		return make([]byte, needed)
	}
	buf = buf[:needed]
	clear(buf)
	return buf
}

func freeReadBuffer(buf []byte) {
	readBufferPool.Put(buf)
}

var regionReaderPool = sync.Pool{
	New: func() any {
		return &RegionReader{}
	},
}

func getRegionReader(pid int, start, end uint64) *RegionReader {
	r := regionReaderPool.Get().(*RegionReader)
	r.pid = pid
	r.start = start
	r.end = end
	r.size = end - start
	r.off = 0
	return r
}

func freeRegionReader(r *RegionReader) {
	r.pid = 0
	regionReaderPool.Put(r)
}

var (
	localIovecPool = sync.Pool{
		New: func() any {
			s := make([]unix.Iovec, IOV_MAX)
			return &s
		},
	}
	remoteIovecPool = sync.Pool{
		New: func() any {
			s := make([]unix.RemoteIovec, IOV_MAX)
			return &s
		},
	}
)

func getLocalIovec() *[]unix.Iovec {
	return localIovecPool.Get().(*[]unix.Iovec)
}

func getRemoteIovec() *[]unix.RemoteIovec {
	return remoteIovecPool.Get().(*[]unix.RemoteIovec)
}

func freeLocalIovec(localPtr *[]unix.Iovec) {
	localIovecPool.Put(localPtr)
}

func freeRemoteIovec(remotePtr *[]unix.RemoteIovec) {
	remoteIovecPool.Put(remotePtr)
}
