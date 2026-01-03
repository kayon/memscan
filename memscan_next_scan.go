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
	"sync"
	"time"

	"github.com/kayon/memscan/scanner"
)

const (
	nextScanTaskSize        = IOV_MAX * 32
	nextScanSparseThreshold = IOV_MAX * 32
)

func (m *Memscan) NextScanForceDense(value *scanner.Value) time.Duration {
	if m.results == nil || value == nil {
		return 0
	}
	count := m.results.Len()
	if count == 0 {
		return 0
	}
	return m.nextScanDense(value)
}

func (m *Memscan) NextScanForceSparse(value *scanner.Value) time.Duration {
	if m.results == nil || value == nil {
		return 0
	}
	count := m.results.Len()
	if count == 0 {
		return 0
	}
	return m.nextScanSparse(value)
}

func (m *Memscan) NextScan(value *scanner.Value) time.Duration {
	if m.results == nil || value == nil {
		return 0
	}
	count := m.results.Len()
	if count == 0 {
		return 0
	}

	if count > nextScanSparseThreshold {
		return m.nextScanDense(value)
	}
	return m.nextScanSparse(value)
}

// nextScanDense 使用了 VirtualRegion 避免了稀疏读取的性能开销
// 对于千万级结果 nextScanDense 耗时应该也是毫秒级的
func (m *Memscan) nextScanDense(value *scanner.Value) time.Duration {
	st := time.Now()
	regions := BuildVirtualRegions(m.results.Data(), uint64(value.Size()))
	m.regionBuffers = make([]*MmapUint64, len(regions))

	var wg sync.WaitGroup
	scan := scanner.NewScanner(m.ctx, *value)

	for index, region := range regions {
		if err := m.sem.Acquire(m.ctx, 1); err != nil {
			break // Canceled
		}
		wg.Add(1)
		go m.taskNextScanDense(scan, index, region, int(region.Size)/value.Size(), &wg)
	}

	wg.Wait()
	m.round += 1
	m.canUndo = true

	m.prevResults.Clear()
	for _, buf := range m.regionBuffers {
		if buf == nil {
			continue
		}
		_ = m.prevResults.Merge(buf)
		buf.Destroy()
	}
	m.regionBuffers = nil

	m.results, m.prevResults = m.prevResults, m.results

	return time.Since(st)
}

func (m *Memscan) taskNextScanDense(scan *scanner.Scanner, regionIndex int, region *VirtualRegion, bufSize int, wg *sync.WaitGroup) {
	defer m.sem.Release(1)
	defer wg.Done()

	buf, err := NewMmapUint64(bufSize)
	if err != nil {
		return
	}

	var batch [collectBatchSize]uint64
	var count int

	collector := func(offset int) bool {
		address := uint64(offset) + region.Start
		if region.Match(address) {
			batch[count] = address
			count++
			if count == collectBatchSize {
				_ = buf.Put(batch[:]...)
				count = 0
			}
			return !region.IsFinished()
		}
		return true
	}

	scan.ScanCollector(region.Pipe(m.proc.PID), collector, &scanner.Options{
		ExpectedSize: region.Size,
	})

	if count > 0 {
		_ = buf.Put(batch[:count]...)
	}

	if buf.Len() > 0 {
		m.regionBuffers[regionIndex] = buf
	} else {
		buf.Destroy()
	}
}

// 对于千万级结果, nextScanSparse 可能会耗时几秒, 是 FirstScan 全量扫描耗时的几倍多, 这太慢了
// 主要的性能开销在于非连续地址的稀疏读取, 和 IOV_MAX 以及 bad address 跳过
// 对于少量结果, 相比 nextScanDense 略微快几十毫秒
// 虽然微乎其微, 但我还是保留了这个方法
func (m *Memscan) nextScanSparse(value *scanner.Value) time.Duration {
	count := m.results.Len()
	st := time.Now()

	var wg sync.WaitGroup
	taskSize := nextScanTaskSize
	comp := value.Comparable()
	regionSize := count / taskSize
	if count%taskSize != 0 {
		regionSize += 1
	}
	m.regionBuffers = make([]*MmapUint64, regionSize)

	var index int
	for start := 0; start < count; start += taskSize {
		if err := m.sem.Acquire(m.ctx, 1); err != nil {
			break
		}
		batchCount := taskSize
		if start+batchCount > count {
			batchCount = count - start
		}
		addresses := m.results.GetN(start, batchCount)
		if len(addresses) == 0 {
			break
		}

		wg.Add(1)
		go m.taskNextScanSparse(index, addresses, comp, &wg)
		index++
	}

	wg.Wait()
	m.round += 1
	m.canUndo = true

	m.prevResults.Clear()
	for _, buf := range m.regionBuffers {
		if buf == nil {
			continue
		}
		_ = m.prevResults.Merge(buf)
		buf.Destroy()
	}
	m.regionBuffers = nil

	m.results, m.prevResults = m.prevResults, m.results

	return time.Since(st)
}

func (m *Memscan) taskNextScanSparse(index int, addresses []uint64, comp scanner.ValueComparable, wg *sync.WaitGroup) {
	defer m.sem.Release(1)
	defer wg.Done()

	results, err := NewMmapUint64(nextScanTaskSize)
	if err != nil {
		return
	}
	n := len(addresses)
	valueSize := comp.Size()
	readBuffer := getReadBuffer(IOV_MAX * valueSize)
	disturb := comp.DisturbingByte()

	for start := 0; start < n; start += IOV_MAX {
		end := start + IOV_MAX
		if end > n {
			end = n
		}
		m.filterResults(addresses[start:end], valueSize, comp, disturb, readBuffer, results)
	}

	freeReadBuffer(readBuffer)

	if results.Len() > 0 {
		m.regionBuffers[index] = results
	} else {
		results.Destroy()
	}
}
