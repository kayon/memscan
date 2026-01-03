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
	"fmt"
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
	raw, err := unix.Mmap(-1, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		return nil, err
	}

	m := &MmapUint64{
		data: unsafe.Slice((*uint64)(unsafe.Pointer(&raw[0])), caps),
		raw:  raw,
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

func (m *MmapUint64) Merge(other *MmapUint64) error {
	if other == nil {
		return nil
	}

	n := uint64(other.Len())
	if n == 0 {
		return nil
	}

	newCount := m.cursor + n
	if newCount > uint64(len(m.data)) {
		newCap := len(m.data) * 2
		if uint64(newCap) < newCount {
			newCap = int(newCount)
		}
		if err := m.grow(newCap); err != nil {
			return fmt.Errorf("merge grow failed: %w", err)
		}
	}

	copy(m.data[m.cursor:], other.Data())

	m.cursor += n
	return nil
}

// Put
// 注意, 按需创建, 没有自动扩容
func (m *MmapUint64) Put(items ...uint64) error {
	n := uint64(len(items))
	if n == 0 {
		return nil
	}
	startIdx := atomic.AddUint64(&m.cursor, n) - n
	if startIdx+n > uint64(len(m.data)) {
		return errors.New("mmap capacity exceeded")
	}

	copy(m.data[startIdx:], items)
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

func (m *MmapUint64) GetN(offset, n int) []uint64 {
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

func (m *MmapUint64) Destroy() {
	if len(m.raw) > 0 {
		_ = unix.Munmap(m.raw)
		m.raw = nil
		m.data = nil
		atomic.StoreUint64(&m.cursor, 0)
		runtime.SetFinalizer(m, nil)
	}
}

func (m *MmapUint64) grow(caps int) error {
	if caps <= len(m.data) {
		return nil
	}

	size := caps * 8

	raw, err := unix.Mremap(m.raw, size, unix.MREMAP_MAYMOVE)
	if err != nil {
		return err
	}

	m.raw = raw
	m.data = unsafe.Slice((*uint64)(unsafe.Pointer(&m.raw[0])), caps)
	return nil
}
