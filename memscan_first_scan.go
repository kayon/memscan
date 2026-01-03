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

func (m *Memscan) FirstScan(value *scanner.Value, args ...bool) time.Duration {
	m.Reset()

	if m.results == nil {
		return 0
	}

	if m.proc == nil || !m.proc.Alive() {
		_ = m.Close()
		return 0
	}

	var processPaused bool
	if len(args) > 0 {
		processPaused = args[0]
	}

	if !processPaused {
		m.proc.Pause()
		defer m.proc.Resume()
	}

	st := time.Now()
	regions := m.maps.Parse(REGION_ALL_RW)
	regions = RegionsOptimize(regions)
	m.regionBuffers = make([]*MmapUint64, len(regions))

	var wg sync.WaitGroup
	scan := scanner.NewScanner(m.ctx, *value)

	for index, region := range regions {
		if err := m.sem.Acquire(m.ctx, 1); err != nil {
			break // Canceled
		}
		wg.Add(1)
		go m.taskFirstScan(scan, index, region, &wg)
	}

	wg.Wait()
	m.round += 1

	// 现在, 结果保存地址是有序的, 虽然增加了一点点内存
	// 但这很值得, 没有排序开销
	for _, buf := range m.regionBuffers {
		if buf == nil {
			continue
		}
		_ = m.results.Merge(buf)
		buf.Destroy()
	}
	m.regionBuffers = nil

	return time.Since(st)
}

func (m *Memscan) taskFirstScan(scan *scanner.Scanner, regionIndex int, region Region, wg *sync.WaitGroup) {
	defer m.sem.Release(1)
	defer wg.Done()

	buf, err := NewMmapUint64(regionResultsAllocCaps)
	if err != nil {
		return
	}

	var batch [collectBatchSize]uint64
	var count int

	collector := scanner.CollectorFunc(func(offset int) bool {
		batch[count] = uint64(offset) + region.Start
		count++
		if count == collectBatchSize {
			_ = buf.Put(batch[:]...)
			count = 0
		}
		return true
	})

	scan.ScanCollector(region.Pipe(m.proc.PID), collector, nil)

	if count > 0 {
		_ = buf.Put(batch[:count]...)
	}

	if buf.Len() > 0 {
		m.regionBuffers[regionIndex] = buf
	} else {
		buf.Destroy()
	}
}
