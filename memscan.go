package memscan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/sys/unix"

	"memscan/deck"
	"memscan/scanner"
)

const (
	IOV_MAX           = 1024
	scanMaxGoroutines = 2048
	resultsAllocCaps  = 256 * 1024 * 1024
)

func NewMemscan() *Memscan {
	results, err := NewMmapUint64(resultsAllocCaps)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	tempResults, err := NewMmapUint64(resultsAllocCaps)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	return &Memscan{
		results:     results,
		tempResults: tempResults,
		sem:         semaphore.NewWeighted(scanMaxGoroutines),
	}
}

type Memscan struct {
	proc        *deck.Process
	maps        *Maps
	results     *MmapUint64
	tempResults *MmapUint64
	round       uint
	sem         *semaphore.Weighted
	ctx         context.Context
	cancel      context.CancelFunc
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

// Cancel
// The scanning process is too fast.
// adding a cancel button to the UI would make it difficult for users to react in time, haha.
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
	if m.tempResults != nil {
		m.tempResults.Clear()
	}
	m.round = 0
}

func (m *Memscan) FirstScan(value *scanner.Value) time.Duration {
	m.Reset()

	if m.results == nil || m.tempResults == nil {
		return 0
	}

	if m.proc == nil || !m.proc.Alive() {
		_ = m.Close()
		return 0
	}

	m.proc.Pause()
	defer m.proc.Resume()

	st := time.Now()
	regions := m.maps.Parse(REGION_ALL_RW)
	regions = RegionsOptimize(regions)

	var wg sync.WaitGroup
	scan := scanner.NewScanner(m.ctx, *value)

	for _, region := range regions {
		// 使用 semaphore.Acquire 替代 select-case
		if err := m.sem.Acquire(m.ctx, 1); err != nil {
			break // Context 取消
		}

		wg.Add(1)
		go m.firstScan(scan, region, &wg)
	}

	wg.Wait()
	m.round += 1

	return time.Since(st)
}

func (m *Memscan) NextScan(value *scanner.Value) time.Duration {
	count := m.results.Len()
	if count == 0 || m.tempResults == nil {
		return 0
	}

	st := time.Now()
	m.tempResults.Clear()

	var wg sync.WaitGroup
	pageSize := IOV_MAX

	for start := 0; start < count; start += pageSize {
		if err := m.sem.Acquire(m.ctx, 1); err != nil {
			break
		}
		addresses := m.results.Get(start, pageSize)
		if len(addresses) == 0 {
			break
		}

		wg.Add(1)
		go m.filterResults(addresses, value.Comparable(), &wg)
	}

	wg.Wait()

	m.results, m.tempResults = m.tempResults, m.results
	m.tempResults.Clear()

	m.round += 1
	return time.Since(st)
}

func (m *Memscan) filterResults(addresses []uint64, value scanner.ValueComparable, wg *sync.WaitGroup) {
	defer m.sem.Release(1)
	defer wg.Done()

	buf := getReadBuffer(len(addresses) * value.Size())
	data, invalidIndex := m.readValuesRaw(addresses, value.Size(), buf)

	matched := make([]uint64, 0, len(addresses))
	for idx, item := range data {
		if slices.Contains(invalidIndex, idx) {
			continue
		}
		if value.EqualBytes(item) {
			matched = append(matched, addresses[idx])
		}
	}

	if len(matched) > 0 {
		m.tempResults.Put(matched...)
	}

	freeReadBuffer(buf)
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

	buf := getReadBuffer(count * valueType.Size())
	defer freeReadBuffer(buf)

	data, invalidIndex := m.readValuesRaw(m.results.Data(), valueType.Size(), buf)
	value := &scanner.Value{}
	value.SetType(valueType.Type())

	rows = make([][2]string, 0, count)
	for i := 0; i < count; i++ {
		if slices.Contains(invalidIndex, i) {
			continue
		}
		addr, ok := m.results.Index(i)
		if !ok {
			continue
		}
		value.SetBytes(data[i])
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

func (m *Memscan) firstScan(scan *scanner.Scanner, region Region, wg *sync.WaitGroup) {
	defer m.sem.Release(1)
	defer wg.Done()

	collector := scanner.CollectorFunc(func(offset int) {
		m.results.Put(uint64(offset) + region.Start)
	})

	scan.ScanCollector(region.Pipe(m.proc.PID), collector)
}

func (m *Memscan) readValuesRaw(addresses []uint64, size int, buf []byte) (data [][]byte, invalidIndex []int) {
	if size <= 0 {
		return
	}
	n := len(addresses)
	if n == 0 {
		return nil, nil
	}

	data = make([][]byte, n)
	invalidIndex = make([]int, 0, n>>2)
	localPtr := getLocalIovec()
	remotePtr := getRemoteIovec()

	local := *localPtr
	remote := *remotePtr

	currentPos := 0
	for currentPos < n {
		remaining := n - currentPos
		for i := 0; i < remaining; i++ {
			idx := currentPos + i
			start := idx * size
			end := start + size
			data[idx] = buf[start:end]

			local[i].Base = &data[idx][0]
			local[i].Len = uint64(size)

			remote[i].Base = uintptr(addresses[idx])
			remote[i].Len = size
		}

		nRead, err := unix.ProcessVMReadv(m.proc.PID, local[:remaining], remote[:remaining], 0)
		successCount := nRead / size
		currentPos += successCount

		if currentPos < n {
			// 清空后续, 防止脏读
			for i := range data[currentPos] {
				data[currentPos][i] = 0
			}
			// skip invalid address
			invalidIndex = append(invalidIndex, currentPos)
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
