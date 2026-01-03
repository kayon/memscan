// Copyright (C) 2025 kayon <kayon.hu@gmail.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package memscan

import (
	"errors"
	"io"

	"golang.org/x/sys/unix"
)

var _ io.ReadSeekCloser = (*RegionReader)(nil)

type RegionReader struct {
	start, end uint64
	size       uint64
	off        uint64
	pid        int

	// pre-allocation
	lIov [1]unix.Iovec
	rIov [1]unix.RemoteIovec
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

	r.rIov[0].Base = uintptr(addr)
	r.rIov[0].Len = int(size)

	n, err = unix.ProcessVMReadv(r.pid, r.lIov[:], r.rIov[:], 0)
	if n > 0 {
		r.off += uint64(n)
	}
	return
}
