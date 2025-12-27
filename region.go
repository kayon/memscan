package memscan

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"memscan/utils"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	memPageSize    = 4096
	scanBufferSize = memPageSize << 4

	regionSmallSize = scanBufferSize
	regionLargeSize = memPageSize << 9

	defRegionsCaps = 1 << 11
)

var _ io.ReadSeekCloser = (*RegionReader)(nil)

type RegionType uint8

const (
	REGION_TYPE_MISC RegionType = iota
	REGION_TYPE_CODE
	REGION_TYPE_EXE
	REGION_TYPE_HEAP
	REGION_TYPE_STACK
)

func (r RegionType) String() string {
	switch r {
	case REGION_TYPE_MISC:
		return "MISC"
	case REGION_TYPE_CODE:
		return "CODE"
	case REGION_TYPE_EXE:
		return "EXE"
	case REGION_TYPE_HEAP:
		return "HEAP"
	case REGION_TYPE_STACK:
		return "STACK"
	}
	return "UNKNOWN"
}

type Region struct {
	Start    uint64
	End      uint64
	Size     uint64
	Type     RegionType
	Perm     Permissions
	Filename string
	BaseAddr uint64
}

func (region Region) String() string {
	return fmt.Sprintf("%08X-%08X %10d %s %-5s %08X %q", region.Start, region.End, region.Size, region.Perm, region.Type, region.BaseAddr, region.Filename)
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

func ParseRegion(line []byte) *Region {
	i := bytes.IndexByte(line, ' ')
	if i <= 0 || i+5 > len(line) {
		return nil
	}

	dashIdx := bytes.IndexByte(line, '-')
	if dashIdx <= 0 {
		return nil
	}

	region := &Region{
		Perm: ParsePermissions(line[i+1 : i+5]),
	}
	region.Start, _ = strconv.ParseUint(utils.BytesToString(line[:dashIdx]), 16, 64)
	region.End, _ = strconv.ParseUint(utils.BytesToString(line[dashIdx+1:i]), 16, 64)
	region.Size = region.End - region.Start

	line = line[i+5:]

	for i = 0; i < 3; i++ {
		line = bytes.TrimLeft(line, " \t")
		nextSpace := bytes.IndexByte(line, ' ')
		if nextSpace == -1 {
			return region
		}
		line = line[nextSpace:]
	}

	region.Filename = string(bytes.TrimLeft(line, " \t"))

	return region
}
