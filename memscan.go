package memscan

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"memscan/deck"
	"memscan/scanner"
)

const (
	scanMaxGoroutines = 2048

	// The results will be displayed when the number of results is less than or equal to this value.
	scanDisplayResults = 10
)

type Memscan struct {
	proc    *deck.Process
	maps    *Maps
	results []uint64
	// Number of consecutive search rounds
	turns  uint
	ctx    context.Context
	cancel context.CancelFunc
}

func (m *Memscan) Open(proc *deck.Process) (err error) {
	m.proc = proc
	m.maps, err = openMaps(proc.PID)
	if err != nil {
		return
	}
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

func (m *Memscan) Count() int {
	return len(m.results)
}

func (m *Memscan) String() string {
	if m.maps == nil {
		return "The process does not exist"
	}
	return fmt.Sprintf("Scan #%d, Results %d", m.turns, m.Count())
}

func (m *Memscan) Reset() {
	m.Cancel()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.results = make([]uint64, 0, 16834)
	m.turns = 0
}

func (m *Memscan) FirstScan(value *scanner.Value) time.Duration {
	m.Reset()

	if m.proc == nil || !m.proc.Alive() {
		_ = m.Close()
		return 0
	}

	m.proc.Pause()
	defer m.proc.Resume()

	st := time.Now()
	regions := m.maps.Parse()

	regions.Optimize()
	wg := &sync.WaitGroup{}
	returns := make(chan uint64)
	scan := scanner.NewScanner(m.ctx, value)
	sem := make(chan struct{}, scanMaxGoroutines)
	quit := make(chan struct{})
	finish := make(chan struct{})

	go func() {
		for {
			select {
			case <-m.ctx.Done():
				return
			case ret := <-returns:
				m.results = append(m.results, ret)
			case <-quit:
				for len(returns) > 0 {
					m.results = append(m.results, <-returns)
				}
				slices.Sort(m.results)
				finish <- struct{}{}
				return
			}
		}
	}()

	for _, region := range regions {
		select {
		case <-m.ctx.Done():
			goto END
		case sem <- struct{}{}:
			wg.Add(1)
			go func(region Region) {
				m.firstScan(scan, region, returns)
				<-sem
				wg.Done()
			}(region)
		}
	}

END:
	wg.Wait()
	close(quit)
	<-finish
	m.turns += 1
	return time.Since(st)
}

func (m *Memscan) NextScan(value *scanner.Value) time.Duration {
	c := m.Count()
	if c == 0 {
		// no results
		return 0
	}

	st := time.Now()

	last := make([]uint64, c)
	copy(last, m.results)
	m.results = m.results[:0]
	wg := &sync.WaitGroup{}
	returns := make(chan []uint64)
	sem := make(chan struct{}, scanMaxGoroutines)
	collectDone := make(chan struct{})

	go func() {
		for rets := range returns {
			m.results = append(m.results, rets...)
		}
		close(collectDone)
	}()

	var size = value.Size()
	for start := 0; start < c; start += IOV_MAX {
		select {
		case <-m.ctx.Done():
			goto END
		case sem <- struct{}{}:
			wg.Add(1)

			end := start + IOV_MAX
			if end > c {
				end = c
			}

			go func(addr []uint64) {
				defer func() {
					<-sem
					wg.Done()
				}()

				data := m.readBytes(addr, size)
				rets := make([]uint64, 0, len(addr))
				for idx, item := range data {
					if value.EqualBytes(item) {
						rets = append(rets, addr[idx])
					}
				}
				returns <- rets
			}(last[start:end])
		}
	}

END:
	wg.Wait()
	close(returns)
	<-collectDone

	slices.Sort(m.results)
	m.turns += 1
	return time.Since(st)
}

func (m *Memscan) ChangeValues(address []uint64, value *scanner.Value) {
	for start := 0; start < len(address); start += IOV_MAX {
		end := start + IOV_MAX
		if end > len(address) {
			end = len(address)
		}
		_, _ = m.writeBytes(address[start:end], value)
	}
}

func (m *Memscan) firstScan(scan *scanner.Scanner, region Region, returns chan uint64) {
	indexes := scan.Scan(region.Pipe(m.proc.PID))
	for _, index := range indexes {
		returns <- uint64(index) + region.Start
	}
}

func (m *Memscan) readBytes(address []uint64, size int) (data [][]byte) {
	if size <= 0 {
		return
	}
	n := len(address)
	if n == 0 {
		return nil
	}

	buf := getReadBuffer(n * size)
	defer freeReadBuffer(buf)

	data = make([][]byte, n)

	var local = make([]unix.Iovec, 0, n)
	var remote = make([]unix.Iovec, 0, n)

	for i, addr := range address {
		start := i * size
		end := start + size

		data[i] = buf[start:end]

		local = append(local, unix.Iovec{
			Base: &data[i][0],
			Len:  uint64(size),
		})

		remote = append(remote, unix.Iovec{
			Base: (*byte)(unsafe.Pointer(uintptr(addr))),
			Len:  uint64(size),
		})
	}
	_, _ = processVmReadv(m.proc.PID, local, remote)
	return
}

func (m *Memscan) writeBytes(address []uint64, value *scanner.Value) (int, error) {
	n := len(address)
	size := value.Size()
	if n == 0 || size == 0 {
		return 0, nil
	}

	buf := make([]byte, n*size)
	rawData := value.Raw()

	var local = make([]unix.Iovec, 0, n)
	var remote = make([]unix.Iovec, 0, n)
	for i, addr := range address {
		start := i * size
		end := start + size
		copy(buf[start:end], rawData)

		local = append(local, unix.Iovec{
			Base: &buf[start],
			Len:  uint64(size),
		})

		remote = append(remote, unix.Iovec{
			Base: (*byte)(unsafe.Pointer(uintptr(addr))),
			Len:  uint64(size),
		})
	}
	return processVmWritev(m.proc.PID, local, remote)
}
