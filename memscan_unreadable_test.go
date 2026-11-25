package memscan

import (
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/kayon/memscan/scanner"
)

// A game frees or protects memory between two scans: the addresses found by
// the first scan can become unreadable. Reading them must skip them, whatever
// the size of the value. A failed process_vm_readv/writev returns -1, and with
// one-byte values (-1 / 1 = -1) the loops used to go back as far as they went
// forward and never ended: the scan of an Int8 spun forever.

const (
	testPage = 4096
	// the engine is expected to answer in milliseconds
	hangLimit = 5 * time.Second
)

// threePages maps three pages and makes the middle one unreadable.
func threePages(t *testing.T) (mem []byte, base uint64) {
	t.Helper()
	mem, err := unix.Mmap(-1, 0, 3*testPage, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Skipf("cannot map memory: %v", err)
	}
	t.Cleanup(func() { _ = unix.Munmap(mem) })
	base = uint64(uintptr(unsafe.Pointer(&mem[0])))
	if err := unix.Mprotect(mem[testPage:2*testPage], unix.PROT_NONE); err != nil {
		t.Skipf("cannot protect memory: %v", err)
	}
	return mem, base
}

func within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(hangLimit):
		t.Fatalf("%s does not end: it spins on the unreadable addresses", what)
	}
}

func selfMemscan(t *testing.T) *Memscan {
	t.Helper()
	m := newMemscanCaps(1 << 12)
	m.Reset()
	m.proc = selfProcess(t)
	t.Cleanup(m.release)
	return m
}

func TestReadValuesRawSkipsUnreadableAddresses(t *testing.T) {
	for _, size := range []int{1, 2, 4, 8} {
		mem, base := threePages(t)
		for i := 0; i < testPage; i++ {
			mem[i] = byte(i%250) + 1
			mem[2*testPage+i] = byte(i%250) + 1
		}

		// readable, unreadable (many), readable again
		var addrs []uint64
		for _, page := range []int{0, 1, 2} {
			for k := 0; k < 40; k++ {
				addrs = append(addrs, base+uint64(page*testPage+k*size*3))
			}
		}
		const disturb = 0xEE
		buf := make([]byte, len(addrs)*size)

		m := selfMemscan(t)
		within(t, "reading values", func() { m.readValuesRaw(addrs, size, disturb, buf) })

		for i, addr := range addrs {
			off := int(addr - base)
			got := buf[i*size : (i+1)*size]
			if off >= testPage && off < 2*testPage {
				if got[0] != disturb {
					t.Fatalf("size %d: unreadable address %#x was read as %#x", size, addr, got[0])
				}
				continue
			}
			for j := 0; j < size; j++ {
				if got[j] != mem[off+j] {
					t.Fatalf("size %d: address %#x holds %#x, read %#x", size, addr, mem[off+j], got[j])
				}
			}
		}
	}
}

func TestWriteValuesSkipsUnwritableAddresses(t *testing.T) {
	for _, size := range []int{1, 2, 4, 8} {
		mem, base := threePages(t)
		var addrs []uint64
		for _, page := range []int{0, 1, 2} {
			for k := 0; k < 40; k++ {
				addrs = append(addrs, base+uint64(page*testPage+k*size*3))
			}
		}
		var value *scanner.Value
		switch size {
		case 1:
			value = scanner.NewInt8(0x5A)
		case 2:
			value = scanner.NewInt16(0x5A5A)
		case 4:
			value = scanner.NewInt32(0x5A5A5A5A)
		default:
			value = scanner.NewInt64(0x5A5A5A5A5A5A5A5A)
		}

		m := selfMemscan(t)
		within(t, "writing values", func() { _, _ = m.writeValues(addrs, value) })

		for _, addr := range addrs {
			off := int(addr - base)
			if off >= testPage && off < 2*testPage {
				continue
			}
			if mem[off] != 0x5A {
				t.Fatalf("size %d: %#x was not written", size, addr)
			}
		}
	}
}

// What happened in a game: a few Int8 results, some of them became unreadable.
func TestNextScanWithResultsInUnreadablePages(t *testing.T) {
	mem, base := threePages(t)
	needle := byte(0xE7) // a variable: its int8 conversion wraps instead of overflowing
	var addrs, kept []uint64
	for _, page := range []int{0, 1, 2} {
		for k := 0; k < 100; k++ {
			addr := base + uint64(page*testPage+k*7)
			addrs = append(addrs, addr)
			if page != 1 {
				mem[addr-base] = needle
				kept = append(kept, addr)
			}
		}
	}

	m := selfMemscan(t)
	if err := m.results.AppendSlice(addrs); err != nil {
		t.Fatal(err)
	}
	m.round = 1

	within(t, "the next scan", func() { m.NextScan(scanner.NewInt8(int8(needle))) })

	got := m.Results()
	if len(got) != len(kept) {
		t.Fatalf("%d results kept, want the %d readable ones", len(got), len(kept))
	}
	for i := range kept {
		if got[i] != kept[i] {
			t.Fatalf("result %d is %#x, want %#x", i, got[i], kept[i])
		}
	}
}
