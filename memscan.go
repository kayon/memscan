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
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"golang.org/x/sync/semaphore"
	"golang.org/x/sys/unix"

	"github.com/kayon/memscan/deck"
	"github.com/kayon/memscan/scanner"
)

const (
	IOV_MAX           = 1024
	scanMaxGoroutines = 2048

	resultsAllocCaps = 64 * 1024 * 1024

	regionResultsAllocCaps = regionLargeSize / 8
	collectBatchSize       = 512
)

func NewMemscan() *Memscan {
	results, err := NewMmapUint64(resultsAllocCaps)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}

	lastResults, err := NewMmapUint64(resultsAllocCaps)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}

	return &Memscan{
		results:     results,
		prevResults: lastResults,
		sem:         semaphore.NewWeighted(scanMaxGoroutines),
	}
}

type Memscan struct {
	proc          *deck.Process
	maps          *Maps
	results       *MmapUint64
	prevResults   *MmapUint64
	regionBuffers []*MmapUint64

	round   uint
	canUndo bool
	sem     *semaphore.Weighted
	ctx     context.Context
	cancel  context.CancelFunc
}

func (m *Memscan) CanUndo() bool {
	return m.canUndo
}

func (m *Memscan) UndoScan() bool {
	if !m.canUndo {
		return false
	}
	m.canUndo = false
	m.results, m.prevResults = m.prevResults, m.results
	m.prevResults.Clear()
	m.round -= 1
	return true
}

func (m *Memscan) Open(proc *deck.Process) (err error) {
	_ = m.Close()

	m.proc = proc
	m.maps, err = OpenMaps(proc.PID)
	if err != nil {
		return
	}
	m.Reset()
	return nil
}

func (m *Memscan) Cancel() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Memscan) Close() (err error) {
	if m.maps != nil {
		err = m.maps.Close()
		m.maps = nil
	}
	return
}

func (m *Memscan) Results() []uint64 {
	if m.results == nil {
		return nil
	}
	return m.results.Data()
}

func (m *Memscan) Count() int {
	if m.results == nil {
		return 0
	}
	return m.results.Len()
}
func (m *Memscan) Rounds() uint {
	return m.round
}

func (m *Memscan) String() string {
	if m.maps == nil {
		return "The process does not exist"
	}
	return fmt.Sprintf("Scan #%d, Results %d", m.round, m.Count())
}

func (m *Memscan) Reset() {
	m.Cancel()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	if m.results != nil {
		m.results.Clear()
	}
	if m.prevResults != nil {
		m.prevResults.Clear()
	}
	m.round = 0
	m.canUndo = false
}

func (m *Memscan) SearchInResults(address uint64) int {
	if m.results == nil || m.results.Len() == 0 {
		return -1
	}
	i, found := slices.BinarySearch(m.results.Data(), address)
	if found {
		return i
	}
	return -1
}

func (m *Memscan) RenderResults(valueType *scanner.Value) (rows [][2]string) {
	if valueType == nil {
		return
	}
	count := m.Count()
	if count == 0 {
		return
	}
	// 这里不考虑地址数量 > IOV_MAX

	valueSize := valueType.Size()
	buf := getReadBuffer(count * valueSize)
	defer freeReadBuffer(buf)

	m.readValuesRaw(m.results.Data(), valueSize, valueType.DisturbingByte(), buf)
	value := &scanner.Value{}
	value.SetType(valueType.Type())

	rows = make([][2]string, 0, count)
	for i := 0; i < count; i++ {
		addr, ok := m.results.Index(i)
		if !ok {
			continue
		}
		offset := i * valueSize
		value.SetBytes(buf[offset : offset+valueSize])
		rows = append(rows, [2]string{
			fmt.Sprintf("%08X", addr),
			value.Format(value.Type()),
		})
	}
	return
}

func (m *Memscan) ChangeResultsValues(retIndexes []int, value *scanner.Value) {
	n := m.Count()
	if n == 0 {
		return
	}
	c := len(retIndexes)
	var address []uint64
	// change all values
	if c == 0 {
		address = m.results.Data()
	} else {
		address = make([]uint64, 0, c)
		for _, idx := range retIndexes {
			if idx > -1 && idx < n {
				addr, ok := m.results.Index(idx)
				if ok {
					address = append(address, addr)
				}
			}
		}
	}
	if len(address) > 0 {
		_, _ = m.writeValues(address, value)
	}
}

func (m *Memscan) ChangeValues(address []uint64, value *scanner.Value) {
	for start := 0; start < len(address); start += IOV_MAX {
		end := start + IOV_MAX
		if end > len(address) {
			end = len(address)
		}
		_, _ = m.writeValues(address[start:end], value)
	}
}

// 确保 len(addresses) <= IOV_MAX
func (m *Memscan) filterResults(addresses []uint64, valueSize int, comp scanner.ValueComparable, disturb byte, readBuffer []byte, results *MmapUint64) {
	m.readValuesRaw(addresses, valueSize, disturb, readBuffer)

	var batch [collectBatchSize]uint64
	var count int

	for i := range addresses {
		offset := i * valueSize
		if comp.EqualBytes(readBuffer[offset : offset+valueSize]) {
			batch[count] = addresses[i]
			count++
			if count == collectBatchSize {
				_ = results.Put(batch[:]...)
				count = 0
			}
		}
	}
	if count > 0 {
		_ = results.Put(batch[:count]...)
	}
}

func (m *Memscan) readValuesRaw(addresses []uint64, size int, disturb byte, buf []byte) {
	if size <= 0 {
		return
	}
	n := len(addresses)
	if n == 0 {
		return
	}

	localPtr := getLocalIovec()
	remotePtr := getRemoteIovec()

	local := *localPtr
	remote := *remotePtr

	currentPos := 0
	for currentPos < n {
		remaining := n - currentPos
		for i := 0; i < remaining; i++ {
			idx := currentPos + i

			local[i].Base = &buf[idx*size]
			local[i].Len = uint64(size)

			remote[i].Base = uintptr(addresses[idx])
			remote[i].Len = size
		}

		nRead, err := unix.ProcessVMReadv(m.proc.PID, local[:remaining], remote[:remaining], 0)
		successCount := nRead / size
		currentPos += successCount

		if currentPos < n {
			offset := currentPos * size
			buf[offset] = disturb
			currentPos++
		}

		if err != nil && !errors.Is(err, unix.EFAULT) && nRead == 0 {
			break
		}
	}

	freeLocalIovec(localPtr)
	freeRemoteIovec(remotePtr)

	return
}

func (m *Memscan) writeValues(addresses []uint64, value *scanner.Value) (int, error) {
	n := len(addresses)
	size := value.Size()
	if n == 0 || size == 0 {
		return 0, nil
	}

	rawData := value.Bytes()
	basePtr := &rawData[0]

	totalWritten := 0
	currentPos := 0
	var lastErr error

	localPtr := getLocalIovec()
	remotePtr := getRemoteIovec()
	defer freeLocalIovec(localPtr)
	defer freeRemoteIovec(remotePtr)

	local := *localPtr
	remote := *remotePtr

	for currentPos < n {
		remaining := n - currentPos

		for i := 0; i < remaining; i++ {
			local[i] = unix.Iovec{
				Base: basePtr,
				Len:  uint64(size),
			}
			remote[i] = unix.RemoteIovec{
				Base: uintptr(addresses[currentPos+i]),
				Len:  size,
			}
		}

		nWrite, err := unix.ProcessVMWritev(m.proc.PID, local[:remaining], remote[:remaining], 0)

		totalWritten += nWrite
		successCount := nWrite / size
		currentPos += successCount

		// 如果没写完，说明 currentPos 对应的地址无效
		if currentPos < n {
			lastErr = err // 通常是 EFAULT
			// 只有在遇到致命错误（如进程不存在）且一个都没写成功时才彻底退出
			if err != nil && !errors.Is(err, unix.EFAULT) && nWrite == 0 {
				return totalWritten, err
			}

			// skip invalid address
			currentPos++
		}
	}

	return totalWritten, lastErr
}
