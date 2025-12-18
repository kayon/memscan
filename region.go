package memscan

import (
	"errors"
	"fmt"
	"io"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	memPageSize    = 1 << 12
	scanBufferSize = memPageSize << 4

	regionLargeSize = memPageSize << 9
	regionSmallSize = scanBufferSize

	defRegionsCaps = 1 << 11
)

var _ io.ReadSeekCloser = (*RegionReader)(nil)

type Region struct {
	Start uint64
	End   uint64
	Size  uint64
}

func (region Region) split() (regions Regions) {
	n := region.Size / regionLargeSize
	if region.Size%regionLargeSize != 0 {
		n += 1
	}
	regions = make(Regions, n)
	start := region.Start
	var end uint64

	for i := range regions {
		end = start + regionLargeSize
		if end > region.End {
			end = region.End
		}
		regions[i].Start = start
		regions[i].End = end
		regions[i].Size = end - start
		start = end
	}

	return
}

func (region Region) String() string {
	return fmt.Sprintf("%08X-%08X %d", region.Start, region.End, region.Size)
}

func (region Region) Pipe(pid int) io.ReadSeekCloser {
	return getRegionReader(pid, region.Start, region.End)
}

type RegionReader struct {
	start, end uint64
	size       uint64
	off        uint64
	pid        int

	// pre-allocation
	lIov [1]unix.Iovec
	rIov [1]unix.Iovec
}

func (r *RegionReader) Close() error {
	freeRegionReader(r)
	return nil
}

func (r *RegionReader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = int64(r.off) + offset
	case io.SeekEnd:
		abs = int64(r.size) + offset
	default:
		return 0, errors.New("RegionReader.Seek: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("RegionReader.Seek: negative position")
	}
	r.off = uint64(abs)
	return abs, nil
}

func (r *RegionReader) Read(p []byte) (n int, err error) {
	// all data has been read
	if r.off >= r.size {
		return 0, io.EOF
	}

	if len(p) == 0 {
		return 0, nil
	}

	size := uint64(len(p))
	// limit the readable range to the Region
	if size > r.size-r.off {
		size = r.size - r.off
	}

	addr := r.start + r.off

	r.lIov[0].Base = &p[0]
	r.lIov[0].Len = size

	r.rIov[0].Base = (*byte)(unsafe.Pointer(uintptr(addr)))
	r.rIov[0].Len = size

	// err will never be an EOF, but may be a bad address
	n, err = processVmReadv(r.pid, r.lIov[:], r.rIov[:])
	if err != nil {
		err = fmt.Errorf("%w at %08X", err, addr)
		// when an error occurs, n may be -1
		return 0, err
	}

	r.off += uint64(n)
	return
}
