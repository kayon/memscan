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
	"runtime"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

type MmapUint64 struct {
	data   []uint64
	raw    []byte
	cursor uint64
}

func NewMmapUint64(caps int) (*MmapUint64, error) {
	size := caps * 8
	// 匿名私有
	b, err := unix.Mmap(-1, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		return nil, err
	}

	data := unsafe.Slice((*uint64)(unsafe.Pointer(&b[0])), caps)
	m := &MmapUint64{
		data: data,
		raw:  b,
	}

	runtime.SetFinalizer(m, func(obj *MmapUint64) {
		if len(obj.raw) > 0 {
			unix.Munmap(obj.raw)
		}
	})

	return m, nil
}

func (m *MmapUint64) Data() []uint64 {
	return m.data[:m.cursor]
}

func (m *MmapUint64) Put(data ...uint64) error {
	count := uint64(len(data))
	startIdx := atomic.AddUint64(&m.cursor, count) - count

	if startIdx+count > uint64(len(m.data)) {
		return errors.New("mmap capacity exceeded")
	}

	copy(m.data[startIdx:], data)
	return nil
}

func (m *MmapUint64) Index(offset int) (uint64, bool) {
	if offset < 0 {
		return 0, false
	}
	curr := int(atomic.LoadUint64(&m.cursor))
	if offset >= curr {
		return 0, false
	}
	return m.data[offset], true
}

func (m *MmapUint64) Get(offset, n int) []uint64 {
	if offset < 0 || n < 1 {
		return nil
	}
	curr := int(atomic.LoadUint64(&m.cursor))
	if offset >= curr {
		return nil
	}
	end := offset + n
	if end > curr {
		end = curr
	}
	return m.data[offset:end]
}

func (m *MmapUint64) Len() int {
	return int(atomic.LoadUint64(&m.cursor))
}

func (m *MmapUint64) Clear() {
	atomic.StoreUint64(&m.cursor, 0)
	if len(m.raw) > 0 {
		// 释放对应的物理页(RSS)，但保留虚拟地址空间(VIRT)
		unix.Madvise(m.raw, unix.MADV_DONTNEED)
	}
}
